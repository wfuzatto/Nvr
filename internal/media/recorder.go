package media

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wfuzatto/Nvr/internal/rtsp"
)

type Segment struct {
	ID         string    `json:"id"`
	CameraID   string    `json:"camera_id"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	Codec      string    `json:"codec"`
	ClockRate  int       `json:"clock_rate,omitempty"`
	Path       string    `json:"path"`
	FramesPath string    `json:"frames_path,omitempty"`
	Bytes      int64     `json:"bytes"`
	Keyframes  int       `json:"keyframes"`
	SHA256     string    `json:"sha256"`
	Protected  bool      `json:"protected,omitempty"`
	Partial    bool      `json:"partial,omitempty"`
}

const (
	FrameIndexHeaderSize = 8
	FrameIndexRecordSize = 20
)

var frameIndexMagic = [4]byte{'N','V','F','I'}

type FrameIndexEntry struct {
	Offset    int64
	Length    int
	Timestamp uint32
	Keyframe  bool
}

type Recorder struct {
	root            string
	cameraID        string
	codec           string
	clockRate       int
	segmentDuration time.Duration
	bootstrap       [][]byte

	file            *os.File
	frameFile       *os.File
	hasher          hash.Hash
	tmpPath         string
	finalPath       string
	frameTmpPath    string
	frameFinalPath  string
	current         Segment
}

func NewRecorder(root, cameraID, codec string, clockRate int, segmentDuration time.Duration, bootstrap [][]byte) (*Recorder, error) {
	if cameraID == "" { return nil, fmt.Errorf("camera ID is required") }
	if strings.ContainsAny(cameraID, "/\\") { return nil, fmt.Errorf("invalid camera ID") }
	codec = strings.ToUpper(codec)
	if codec == "HEVC" { codec = "H265" }
	if codec != "H264" && codec != "H265" { return nil, fmt.Errorf("unsupported codec %s", codec) }
	if clockRate <= 0 { clockRate = 90000 }
	if segmentDuration <= 0 { return nil, fmt.Errorf("segment duration must be positive") }

	copied := make([][]byte, 0, len(bootstrap))
	for _, nal := range bootstrap { copied = append(copied, append([]byte(nil), nal...)) }
	return &Recorder{
		root: root, cameraID: cameraID, codec: codec, clockRate: clockRate,
		segmentDuration: segmentDuration, bootstrap: copied,
	}, nil
}

func (r *Recorder) Write(au rtsp.AccessUnit) (*Segment, error) {
	when := au.ReceivedAt
	if when.IsZero() { when = time.Now().UTC() }

	var completed *Segment
	if r.file != nil && when.Sub(r.current.Start) >= r.segmentDuration && au.Keyframe {
		var err error
		completed, err = r.closeCurrent(when)
		if err != nil { return nil, err }
	}

	if r.file == nil {
		if !au.Keyframe { return completed, nil }
		if err := r.openSegment(when); err != nil { return nil, err }
	}

	offset := r.current.Bytes
	n, err := r.writeBytes(au.Data)
	if err != nil { _ = r.abortCurrent(); return completed, err }
	if n != len(au.Data) { _ = r.abortCurrent(); return completed, fmt.Errorf("short video write: %d/%d", n, len(au.Data)) }

	entry := FrameIndexEntry{
		Offset:offset, Length:n, Timestamp:au.Timestamp,
		Keyframe:au.Keyframe,
	}
	if err := r.writeFrameIndex(entry); err != nil { _ = r.abortCurrent(); return completed, err }

	r.current.Bytes += int64(n)
	if au.Keyframe { r.current.Keyframes++ }
	r.current.End = when.UTC()
	return completed, nil
}

func (r *Recorder) Current() *Segment {
	if r.file == nil { return nil }
	current := r.current
	if relative, err := filepath.Rel(r.root, r.tmpPath); err == nil { current.Path = filepath.ToSlash(relative) }
	if relative, err := filepath.Rel(r.root, r.frameTmpPath); err == nil { current.FramesPath = filepath.ToSlash(relative) }
	current.Partial = true
	current.SHA256 = ""
	return &current
}

func (r *Recorder) Close() (*Segment, error) {
	if r.file == nil { return nil, nil }
	end := r.current.End
	if end.IsZero() { end = time.Now().UTC() }
	return r.closeCurrent(end)
}

func (r *Recorder) openSegment(start time.Time) error {
	dateDir := start.UTC().Format("2006-01-02")
	dir := filepath.Join(r.root, r.cameraID, dateDir)
	if err := os.MkdirAll(dir, 0o750); err != nil { return err }

	ext := ".h264"
	if r.codec == "H265" { ext = ".h265" }
	name := fmt.Sprintf("%s_%d%s", start.UTC().Format("15-04-05.000"), start.UnixNano(), ext)
	finalPath := filepath.Join(dir, name)
	tmpPath := finalPath + ".partial"
	frameFinalPath := finalPath + ".frames.idx"
	frameTmpPath := frameFinalPath + ".partial"

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil { return err }
	ff, err := os.OpenFile(frameTmpPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil { _ = f.Close(); _ = os.Remove(tmpPath); return err }

	r.file = f
	r.frameFile = ff
	r.hasher = sha256.New()
	r.tmpPath = tmpPath
	r.finalPath = finalPath
	r.frameTmpPath = frameTmpPath
	r.frameFinalPath = frameFinalPath

	if err := writeFrameIndexHeader(ff); err != nil {
		_ = r.abortCurrent()
		return err
	}

	relative, _ := filepath.Rel(r.root, finalPath)
	frameRelative, _ := filepath.Rel(r.root, frameFinalPath)
	r.current = Segment{
		ID: fmt.Sprintf("%s-%d", r.cameraID, start.UnixNano()),
		CameraID:r.cameraID, Start:start.UTC(), End:start.UTC(),
		Codec:r.codec, ClockRate:r.clockRate,
		Path:filepath.ToSlash(relative), FramesPath:filepath.ToSlash(frameRelative),
	}

	for _, nal := range r.bootstrap {
		if len(nal)==0 { continue }
		n,err:=r.writeBytes([]byte{0,0,0,1})
		if err!=nil { _=r.abortCurrent(); return err }
		r.current.Bytes+=int64(n)
		n,err=r.writeBytes(nal)
		if err!=nil { _=r.abortCurrent(); return err }
		r.current.Bytes+=int64(n)
	}
	return nil
}

func (r *Recorder) closeCurrent(end time.Time) (*Segment, error) {
	if r.file == nil { return nil, nil }
	r.current.End=end.UTC()

	if err:=r.file.Sync(); err!=nil { _=r.abortCurrent(); return nil,err }
	if err:=r.frameFile.Sync(); err!=nil { _=r.abortCurrent(); return nil,err }
	if err:=r.file.Close(); err!=nil { r.file=nil; _=r.abortCurrent(); return nil,err }
	r.file=nil
	if err:=r.frameFile.Close(); err!=nil { r.frameFile=nil; _=r.abortCurrent(); return nil,err }
	r.frameFile=nil

	r.current.SHA256=hex.EncodeToString(r.hasher.Sum(nil))
	if err:=os.Rename(r.tmpPath,r.finalPath); err!=nil { return nil,err }
	if err:=os.Rename(r.frameTmpPath,r.frameFinalPath); err!=nil {
		_ = os.Remove(r.finalPath)
		return nil,err
	}
	if err:=appendIndex(r.root,r.current); err!=nil { return nil,err }

	completed:=r.current
	r.reset()
	return &completed,nil
}

func (r *Recorder) writeBytes(payload []byte) (int,error) {
	if len(payload)==0 { return 0,nil }
	n,err:=r.file.Write(payload)
	if n>0 { _,_=r.hasher.Write(payload[:n]) }
	return n,err
}

func writeFrameIndexHeader(w io.Writer) error {
	header:=make([]byte,FrameIndexHeaderSize)
	copy(header[:4],frameIndexMagic[:])
	header[4]=1
	header[5]=FrameIndexRecordSize
	_,err:=w.Write(header)
	return err
}

func (r *Recorder) writeFrameIndex(entry FrameIndexEntry) error {
	if entry.Offset<0 || entry.Length<=0 { return fmt.Errorf("invalid frame index entry") }
	record:=make([]byte,FrameIndexRecordSize)
	binary.BigEndian.PutUint64(record[0:8],uint64(entry.Offset))
	binary.BigEndian.PutUint32(record[8:12],uint32(entry.Length))
	binary.BigEndian.PutUint32(record[12:16],entry.Timestamp)
	if entry.Keyframe { record[16]=1 }
	_,err:=r.frameFile.Write(record)
	return err
}

func (r *Recorder) abortCurrent() error {
	var first error
	if r.file!=nil {
		if err:=r.file.Close(); err!=nil { first=err }
	}
	if r.frameFile!=nil {
		if err:=r.frameFile.Close(); err!=nil && first==nil { first=err }
	}
	for _,path:=range []string{r.tmpPath,r.frameTmpPath} {
		if path=="" { continue }
		if err:=os.Remove(path); err!=nil && !os.IsNotExist(err) && first==nil { first=err }
	}
	r.reset()
	return first
}

func (r *Recorder) reset() {
	r.file=nil
	r.frameFile=nil
	r.current=Segment{}
	r.tmpPath,r.finalPath="",""
	r.frameTmpPath,r.frameFinalPath="",""
	r.hasher=nil
}

func appendIndex(root string, segment Segment) error {
	absolute:=filepath.Join(root,filepath.FromSlash(segment.Path))
	dir:=filepath.Dir(absolute)
	indexPath:=filepath.Join(dir,"index.jsonl")
	f,err:=os.OpenFile(indexPath,os.O_CREATE|os.O_APPEND|os.O_WRONLY,0o640)
	if err!=nil { return err }
	defer f.Close()
	payload,err:=json.Marshal(segment)
	if err!=nil { return err }
	if _,err:=f.Write(append(payload,'\n')); err!=nil { return err }
	return f.Sync()
}
