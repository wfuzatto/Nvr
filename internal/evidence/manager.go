package evidence

import (
	"archive/tar"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wfuzatto/Nvr/internal/media"
	"github.com/wfuzatto/Nvr/internal/model"
	"github.com/wfuzatto/Nvr/internal/playback"
	"github.com/wfuzatto/Nvr/internal/store"
)

type Status string

const (
	StatusQueued Status = "queued"
	StatusRunning Status = "running"
	StatusReady Status = "ready"
	StatusFailed Status = "failed"
)

type Job struct {
	ID string `json:"id"`
	CameraID string `json:"camera_id"`
	CameraName string `json:"camera_name"`
	RequestedFrom time.Time `json:"requested_from"`
	RequestedTo time.Time `json:"requested_to"`
	CreatedBy string `json:"created_by"`
	Status Status `json:"status"`
	Progress int `json:"progress"`
	Error string `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	StartedAt time.Time `json:"started_at,omitempty"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
	BundlePath string `json:"bundle_path,omitempty"`
	BundleBytes int64 `json:"bundle_bytes,omitempty"`
	BundleSHA256 string `json:"bundle_sha256,omitempty"`
	Clip playback.RangeMuxInfo `json:"clip,omitempty"`
}

type Manifest struct {
	SchemaVersion int `json:"schema_version"`
	ExportID string `json:"export_id"`
	GeneratedAt time.Time `json:"generated_at"`
	GeneratedBy string `json:"generated_by"`
	Camera model.CameraPublic `json:"camera"`
	RequestedFrom time.Time `json:"requested_from"`
	RequestedTo time.Time `json:"requested_to"`
	Clip playback.RangeMuxInfo `json:"clip"`
	Sources []Source `json:"sources"`
}

type Source struct {
	Path string `json:"path"`
	Start time.Time `json:"start"`
	End time.Time `json:"end"`
	SHA256 string `json:"sha256"`
	Bytes int64 `json:"bytes"`
	Codec string `json:"codec"`
}

type Manager struct {
	mu sync.RWMutex
	root string
	exportsDir string
	cameras store.CameraStore
	jobs map[string]Job
	queue chan string
}

func NewManager(root,exportsDir string,cameras store.CameraStore) (*Manager,error) {
	if err:=os.MkdirAll(exportsDir,0o750); err!=nil { return nil,err }
	m:=&Manager{root:root,exportsDir:exportsDir,cameras:cameras,jobs:make(map[string]Job),queue:make(chan string,32)}
	if err:=m.loadJobs(); err!=nil { return nil,err }
	go m.worker()
	return m,nil
}

func (m *Manager) Create(cameraID string,from,to time.Time,actor string) (Job,error) {
	if !to.After(from) { return Job{},errors.New("to must be after from") }
	if to.Sub(from)>4*time.Hour { return Job{},errors.New("export range cannot exceed 4 hours") }
	camera,err:=m.cameras.Get(cameraID)
	if err!=nil { return Job{},err }
	now:=time.Now().UTC()
	job:=Job{ID:randomID(),CameraID:cameraID,CameraName:camera.Name,RequestedFrom:from.UTC(),RequestedTo:to.UTC(),CreatedBy:actor,Status:StatusQueued,Progress:0,CreatedAt:now}
	m.mu.Lock(); m.jobs[job.ID]=job; err=m.persistLocked(); m.mu.Unlock()
	if err!=nil { return Job{},err }
	select { case m.queue<-job.ID: default: return Job{},errors.New("export queue is full") }
	return job,nil
}

func (m *Manager) Get(id string) (Job,error) {
	m.mu.RLock(); defer m.mu.RUnlock()
	job,ok:=m.jobs[id]
	if !ok { return Job{},store.ErrNotFound }
	return job,nil
}

func (m *Manager) List(limit int) []Job {
	if limit<=0 { limit=100 }
	m.mu.RLock(); defer m.mu.RUnlock()
	out:=make([]Job,0,len(m.jobs))
	for _,job:=range m.jobs { out=append(out,job) }
	sort.Slice(out,func(i,j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out)>limit { out=out[:limit] }
	return out
}

func (m *Manager) BundlePath(id string) (string,Job,error) {
	job,err:=m.Get(id)
	if err!=nil { return "",Job{},err }
	if job.Status!=StatusReady || job.BundlePath=="" { return "",job,errors.New("export is not ready") }
	clean:=filepath.Clean(job.BundlePath)
	if filepath.IsAbs(clean) || strings.HasPrefix(clean,"..") { return "",job,os.ErrPermission }
	return filepath.Join(m.exportsDir,clean),job,nil
}

func (m *Manager) worker() {
	for id:=range m.queue { m.run(id) }
}

func (m *Manager) run(id string) {
	m.update(id,func(j *Job){ j.Status=StatusRunning; j.StartedAt=time.Now().UTC(); j.Progress=10 })
	job,err:=m.Get(id)
	if err!=nil { return }
	camera,err:=m.cameras.Get(job.CameraID)
	if err!=nil { m.fail(id,err); return }
	segments,err:=media.ListSegments(m.root,job.CameraID,job.RequestedFrom,job.RequestedTo,5000)
	if err!=nil { m.fail(id,err); return }
	if len(segments)==0 { m.fail(id,errors.New("no recordings in requested range")); return }
	m.update(id,func(j *Job){ j.Progress=25 })

	clipTmp:=filepath.Join(m.exportsDir,id+".clip.ts.partial")
	clip,err:=os.OpenFile(clipTmp,os.O_CREATE|os.O_TRUNC|os.O_WRONLY,0o640)
	if err!=nil { m.fail(id,err); return }
	clipInfo,err:=playback.ExportRangeTS(m.root,segments,job.RequestedFrom,job.RequestedTo,clip)
	syncErr:=clip.Sync(); closeErr:=clip.Close()
	if err==nil { err=syncErr }
	if err==nil { err=closeErr }
	if err!=nil { _=os.Remove(clipTmp); m.fail(id,err); return }
	m.update(id,func(j *Job){ j.Progress=60 })

	sources:=make([]Source,0,len(segments))
	for _,s:=range segments {
		if s.End.Before(job.RequestedFrom) || s.Start.After(job.RequestedTo) { continue }
		sources=append(sources,Source{Path:s.Path,Start:s.Start,End:s.End,SHA256:s.SHA256,Bytes:s.Bytes,Codec:s.Codec})
	}
	manifest:=Manifest{
		SchemaVersion:1,ExportID:id,GeneratedAt:time.Now().UTC(),GeneratedBy:job.CreatedBy,
		Camera:camera.Public(),RequestedFrom:job.RequestedFrom,RequestedTo:job.RequestedTo,Clip:clipInfo,Sources:sources,
	}
	manifestBytes,err:=json.MarshalIndent(manifest,"","  ")
	if err!=nil { _=os.Remove(clipTmp); m.fail(id,err); return }

	bundleName:=id+".evidence.tar.gz"
	bundleTmp:=filepath.Join(m.exportsDir,bundleName+".partial")
	bundleFinal:=filepath.Join(m.exportsDir,bundleName)
	if err:=writeBundle(bundleTmp,clipTmp,manifestBytes); err!=nil { _=os.Remove(clipTmp); _=os.Remove(bundleTmp); m.fail(id,err); return }
	_ = os.Remove(clipTmp)
	if err:=os.Rename(bundleTmp,bundleFinal); err!=nil { m.fail(id,err); return }
	hash,size,err:=hashFile(bundleFinal)
	if err!=nil { m.fail(id,err); return }
	m.update(id,func(j *Job){
		j.Status=StatusReady;j.Progress=100;j.CompletedAt=time.Now().UTC();j.BundlePath=bundleName;j.BundleBytes=size;j.BundleSHA256=hash;j.Clip=clipInfo
	})
}

func writeBundle(path,clipPath string,manifest []byte) error {
	f,err:=os.OpenFile(path,os.O_CREATE|os.O_TRUNC|os.O_WRONLY,0o640)
	if err!=nil { return err }
	gz:=gzip.NewWriter(f)
	tw:=tar.NewWriter(gz)
	clipInfo,err:=os.Stat(clipPath)
	if err!=nil { _=tw.Close(); _=gz.Close(); _=f.Close(); return err }
	if err:=tw.WriteHeader(&tar.Header{Name:"clip.ts",Mode:0o640,Size:clipInfo.Size(),ModTime:time.Now()}); err!=nil { return err }
	clip,err:=os.Open(clipPath)
	if err!=nil { return err }
	_,copyErr:=io.Copy(tw,clip); _=clip.Close()
	if copyErr!=nil { return copyErr }
	if err:=tw.WriteHeader(&tar.Header{Name:"manifest.json",Mode:0o640,Size:int64(len(manifest)),ModTime:time.Now()}); err!=nil { return err }
	if _,err:=tw.Write(manifest); err!=nil { return err }
	if err:=tw.Close(); err!=nil { return err }
	if err:=gz.Close(); err!=nil { return err }
	if err:=f.Sync(); err!=nil { return err }
	return f.Close()
}

func hashFile(path string) (string,int64,error) {
	f,err:=os.Open(path); if err!=nil { return "",0,err }; defer f.Close()
	h:=sha256.New(); n,err:=io.Copy(h,f); if err!=nil { return "",0,err }
	return hex.EncodeToString(h.Sum(nil)),n,nil
}

func (m *Manager) update(id string,fn func(*Job)) {
	m.mu.Lock(); defer m.mu.Unlock()
	j,ok:=m.jobs[id]; if !ok { return }
	fn(&j); m.jobs[id]=j; _=m.persistLocked()
}
func (m *Manager) fail(id string,err error) {
	m.update(id,func(j *Job){ j.Status=StatusFailed;j.Error=err.Error();j.CompletedAt=time.Now().UTC() })
}

func (m *Manager) loadJobs() error {
	path:=filepath.Join(m.exportsDir,"jobs.json")
	data,err:=os.ReadFile(path)
	if errors.Is(err,os.ErrNotExist) { return nil }
	if err!=nil { return err }
	var jobs []Job
	if err:=json.Unmarshal(data,&jobs); err!=nil { return err }
	for _,j:=range jobs {
		if j.Status==StatusRunning || j.Status==StatusQueued { j.Status=StatusFailed;j.Error="server restarted before export completed";j.CompletedAt=time.Now().UTC() }
		m.jobs[j.ID]=j
	}
	return nil
}

func (m *Manager) persistLocked() error {
	jobs:=make([]Job,0,len(m.jobs))
	for _,j:=range m.jobs { jobs=append(jobs,j) }
	sort.Slice(jobs,func(i,j int) bool { return jobs[i].CreatedAt.Before(jobs[j].CreatedAt) })
	data,err:=json.MarshalIndent(jobs,"","  "); if err!=nil { return err }
	path:=filepath.Join(m.exportsDir,"jobs.json")
	tmp:=path+".tmp"
	if err:=os.WriteFile(tmp,append(data, 10),0o600); err!=nil { return err }
	if err:=os.Rename(tmp,path); err!=nil { return err }
	return os.Chmod(path,0o600)
}

func randomID() string {
	var b [16]byte
	if _,err:=rand.Read(b[:]); err!=nil { return fmt.Sprintf("%d",time.Now().UnixNano()) }
	return hex.EncodeToString(b[:])
}
