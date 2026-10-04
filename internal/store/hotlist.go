package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wfuzatto/Nvr/internal/model"
)

type HotlistStore interface {
	List() []model.HotlistEntry
	Put(model.HotlistEntry) (model.HotlistEntry,error)
	Delete(string) error
	Match(string) (model.HotlistEntry,bool)
}

type FileHotlistStore struct {
	mu sync.RWMutex
	path string
	items map[string]model.HotlistEntry
}

func OpenFileHotlistStore(path string)(*FileHotlistStore,error){
	s:=&FileHotlistStore{path:path,items:make(map[string]model.HotlistEntry)}
	b,err:=os.ReadFile(path)
	if errors.Is(err,os.ErrNotExist){return s,nil}
	if err!=nil{return nil,err}
	var items []model.HotlistEntry
	if err:=json.Unmarshal(b,&items);err!=nil{return nil,err}
	for _,e:=range items{
		if e.ID!=""&&normalizePlate(e.Plate)!=""{e.Plate=normalizePlate(e.Plate);s.items[e.ID]=e}
	}
	return s,nil
}

func (s *FileHotlistStore) List()[]model.HotlistEntry{
	s.mu.RLock();defer s.mu.RUnlock()
	out:=make([]model.HotlistEntry,0,len(s.items))
	for _,e:=range s.items{out=append(out,e)}
	sort.Slice(out,func(i,j int)bool{return out[i].Plate<out[j].Plate})
	return out
}

func (s *FileHotlistStore) Put(e model.HotlistEntry)(model.HotlistEntry,error){
	e.Plate=normalizePlate(e.Plate)
	if len(e.Plate)!=7{return model.HotlistEntry{},errors.New("hotlist plate must contain exactly 7 alphanumeric characters")}
	e.Label=strings.TrimSpace(e.Label)
	now:=time.Now().UTC()
	s.mu.Lock();defer s.mu.Unlock()
	if e.ID==""{e.ID=randomHotlistID();e.CreatedAt=now}
	if old,ok:=s.items[e.ID];ok&&e.CreatedAt.IsZero(){e.CreatedAt=old.CreatedAt}
	if e.CreatedAt.IsZero(){e.CreatedAt=now}
	e.UpdatedAt=now
	s.items[e.ID]=e
	if err:=s.persistLocked();err!=nil{return model.HotlistEntry{},err}
	return e,nil
}

func (s *FileHotlistStore) Delete(id string)error{
	s.mu.Lock();defer s.mu.Unlock()
	if _,ok:=s.items[id];!ok{return ErrNotFound}
	delete(s.items,id)
	return s.persistLocked()
}

func (s *FileHotlistStore) Match(plate string)(model.HotlistEntry,bool){
	plate=normalizePlate(plate)
	s.mu.RLock();defer s.mu.RUnlock()
	for _,e:=range s.items{
		if e.Enabled&&e.Plate==plate{return e,true}
	}
	return model.HotlistEntry{},false
}

func (s *FileHotlistStore) persistLocked()error{
	if err:=os.MkdirAll(filepath.Dir(s.path),0o750);err!=nil{return err}
	items:=make([]model.HotlistEntry,0,len(s.items))
	for _,e:=range s.items{items=append(items,e)}
	sort.Slice(items,func(i,j int)bool{return items[i].ID<items[j].ID})
	b,err:=json.MarshalIndent(items,"","  ");if err!=nil{return err}
	tmp:=s.path+".tmp"
	if err:=os.WriteFile(tmp,append(b,10),0o600);err!=nil{return err}
	if err:=os.Rename(tmp,s.path);err!=nil{return err}
	return os.Chmod(s.path,0o600)
}

func normalizePlate(v string)string{
	var b strings.Builder
	for _,r:=range strings.ToUpper(v){
		if (r>='A'&&r<='Z')||(r>='0'&&r<='9'){b.WriteRune(r)}
	}
	return b.String()
}
func randomHotlistID()string{
	var b [12]byte
	if _,err:=rand.Read(b[:]);err!=nil{return hex.EncodeToString([]byte(time.Now().String()))}
	return hex.EncodeToString(b[:])
}
