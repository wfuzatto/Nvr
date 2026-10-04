package audit

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Event struct {
	Sequence     uint64         `json:"sequence"`
	Time         time.Time      `json:"time"`
	ActorID      string         `json:"actor_id,omitempty"`
	Actor        string         `json:"actor,omitempty"`
	Role         string         `json:"role,omitempty"`
	Action       string         `json:"action"`
	Resource     string         `json:"resource,omitempty"`
	ResourceID   string         `json:"resource_id,omitempty"`
	RemoteAddr   string         `json:"remote_addr,omitempty"`
	Success      bool           `json:"success"`
	Details      map[string]any `json:"details,omitempty"`
	PreviousHash string         `json:"previous_hash,omitempty"`
	Hash         string         `json:"hash"`
}

type Log struct {
	mu sync.Mutex
	path string
	sequence uint64
	lastHash string
}

func Open(path string) (*Log,error) {
	l:=&Log{path:path}
	if err:=l.recoverTail(); err!=nil { return nil,err }
	return l,nil
}

func (l *Log) Append(event Event) (Event,error) {
	l.mu.Lock(); defer l.mu.Unlock()
	l.sequence++
	event.Sequence=l.sequence
	if event.Time.IsZero() { event.Time=time.Now().UTC() } else { event.Time=event.Time.UTC() }
	event.PreviousHash=l.lastHash
	event.Hash=""
	payload,err:=json.Marshal(event)
	if err!=nil { l.sequence--; return Event{},err }
	sum:=sha256.Sum256(payload)
	event.Hash=hex.EncodeToString(sum[:])
	final,err:=json.Marshal(event)
	if err!=nil { l.sequence--; return Event{},err }
	if err:=os.MkdirAll(filepath.Dir(l.path),0o750); err!=nil { l.sequence--; return Event{},err }
	f,err:=os.OpenFile(l.path,os.O_CREATE|os.O_APPEND|os.O_WRONLY,0o600)
	if err!=nil { l.sequence--; return Event{},err }
	if _,err=f.Write(append(final, 10)); err==nil { err=f.Sync() }
	closeErr:=f.Close()
	if err==nil { err=closeErr }
	if err!=nil { l.sequence--; return Event{},err }
	l.lastHash=event.Hash
	return event,nil
}

func (l *Log) List(limit int) ([]Event,error) {
	if limit<=0 { limit=200 }
	if limit>5000 { limit=5000 }
	f,err:=os.Open(l.path)
	if errors.Is(err,os.ErrNotExist) { return []Event{},nil }
	if err!=nil { return nil,err }
	defer f.Close()
	var items []Event
	scanner:=bufio.NewScanner(f)
	scanner.Buffer(make([]byte,4096),1024*1024)
	for scanner.Scan() {
		var e Event
		if json.Unmarshal(scanner.Bytes(),&e)==nil { items=append(items,e) }
	}
	if err:=scanner.Err(); err!=nil { return nil,err }
	if len(items)>limit { items=items[len(items)-limit:] }
	for i,j:=0,len(items)-1;i<j;i,j=i+1,j-1 { items[i],items[j]=items[j],items[i] }
	return items,nil
}

func (l *Log) Verify() error {
	f,err:=os.Open(l.path)
	if errors.Is(err,os.ErrNotExist) { return nil }
	if err!=nil { return err }
	defer f.Close()
	var previous string
	var sequence uint64
	scanner:=bufio.NewScanner(f)
	scanner.Buffer(make([]byte,4096),1024*1024)
	for scanner.Scan() {
		var e Event
		if err:=json.Unmarshal(scanner.Bytes(),&e); err!=nil { return err }
		if e.Sequence!=sequence+1 { return fmt.Errorf("audit sequence gap at %d",e.Sequence) }
		if e.PreviousHash!=previous { return fmt.Errorf("audit hash chain mismatch at %d",e.Sequence) }
		stored:=e.Hash
		e.Hash=""
		payload,_:=json.Marshal(e)
		sum:=sha256.Sum256(payload)
		if stored!=hex.EncodeToString(sum[:]) { return fmt.Errorf("audit hash mismatch at %d",e.Sequence) }
		previous=stored
		sequence=e.Sequence
	}
	return scanner.Err()
}

func (l *Log) recoverTail() error {
	if err:=l.Verify(); err!=nil { return err }
	f,err:=os.Open(l.path)
	if errors.Is(err,os.ErrNotExist) { return nil }
	if err!=nil { return err }
	defer f.Close()
	scanner:=bufio.NewScanner(f)
	scanner.Buffer(make([]byte,4096),1024*1024)
	for scanner.Scan() {
		var e Event
		if json.Unmarshal(scanner.Bytes(),&e)==nil {
			l.sequence=e.Sequence
			l.lastHash=strings.TrimSpace(e.Hash)
		}
	}
	return scanner.Err()
}
