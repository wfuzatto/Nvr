package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wfuzatto/Nvr/internal/model"
)

type EventQuery struct {
	Plate string
	CameraID string
	From time.Time
	To time.Time
	Limit int
	AlertOnly bool
}

type EventStore interface {
	Put(model.EventEnvelope) (bool,error)
	Search(EventQuery) ([]model.EventEnvelope,error)
	Get(string) (model.EventEnvelope,error)
	Count() int
}

type FileEventStore struct {
	mu sync.RWMutex
	path string
	byID map[string]model.EventEnvelope
	ordered []model.EventEnvelope
}

func OpenFileEventStore(path string)(*FileEventStore,error){
	s:=&FileEventStore{path:path,byID:make(map[string]model.EventEnvelope)}
	if err:=s.load();err!=nil{return nil,err}
	return s,nil
}

func (s *FileEventStore) Put(ev model.EventEnvelope)(bool,error){
	if strings.TrimSpace(ev.EventID)==""{return false,errors.New("event_id is required")}
	s.mu.Lock();defer s.mu.Unlock()
	if _,ok:=s.byID[ev.EventID];ok{return false,nil}
	if err:=os.MkdirAll(filepath.Dir(s.path),0o750);err!=nil{return false,err}
	f,err:=os.OpenFile(s.path,os.O_CREATE|os.O_APPEND|os.O_WRONLY,0o600)
	if err!=nil{return false,err}
	enc:=json.NewEncoder(f)
	if err=enc.Encode(ev);err==nil{err=f.Sync()}
	closeErr:=f.Close()
	if err!=nil{return false,err};if closeErr!=nil{return false,closeErr}
	s.byID[ev.EventID]=ev
	s.ordered=append(s.ordered,ev)
	return true,nil
}

func (s *FileEventStore) Get(id string)(model.EventEnvelope,error){
	s.mu.RLock();defer s.mu.RUnlock()
	ev,ok:=s.byID[id];if !ok{return model.EventEnvelope{},ErrNotFound}
	return ev,nil
}

func (s *FileEventStore) Search(q EventQuery)([]model.EventEnvelope,error){
	s.mu.RLock();defer s.mu.RUnlock()
	limit:=q.Limit;if limit<=0{limit=100};if limit>5000{limit=5000}
	needle:=strings.ToUpper(strings.TrimSpace(q.Plate))
	out:=make([]model.EventEnvelope,0,min(limit,len(s.ordered)))
	for i:=len(s.ordered)-1;i>=0&&len(out)<limit;i--{
		ev:=s.ordered[i]
		if q.CameraID!=""&&ev.CameraID!=q.CameraID{continue}
		if q.AlertOnly&&!ev.Alert{continue}
		if !q.From.IsZero()&&ev.ObservedAt.Before(q.From){continue}
		if !q.To.IsZero()&&ev.ObservedAt.After(q.To){continue}
		if needle!=""{
			p,err:=ev.Plate();if err!=nil{continue}
			if !strings.Contains(strings.ToUpper(p.NormalizedText),needle)&&!strings.Contains(strings.ToUpper(p.RawText),needle){continue}
		}
		out=append(out,ev)
	}
	return out,nil
}

func (s *FileEventStore) Count()int{s.mu.RLock();defer s.mu.RUnlock();return len(s.ordered)}

func (s *FileEventStore) load()error{
	s.mu.Lock();defer s.mu.Unlock()
	f,err:=os.Open(s.path)
	if errors.Is(err,os.ErrNotExist){return nil}
	if err!=nil{return err}
	defer f.Close()
	sc:=bufio.NewScanner(f);sc.Buffer(make([]byte,64*1024),2*1024*1024)
	line:=0
	for sc.Scan(){
		line++
		var ev model.EventEnvelope
		if err:=json.Unmarshal(sc.Bytes(),&ev);err!=nil{return fmt.Errorf("decode event store line %d: %w",line,err)}
		if ev.EventID==""{continue}
		if _,exists:=s.byID[ev.EventID];exists{continue}
		s.byID[ev.EventID]=ev;s.ordered=append(s.ordered,ev)
	}
	if err:=sc.Err();err!=nil{return err}
	sort.SliceStable(s.ordered,func(i,j int)bool{return s.ordered[i].ObservedAt.Before(s.ordered[j].ObservedAt)})
	return nil
}
func min(a,b int)int{if a<b{return a};return b}
