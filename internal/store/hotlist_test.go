package store

import (
	"testing"

	"github.com/wfuzatto/Nvr/internal/model"
)

func TestHotlistMatchAndPersist(t *testing.T){
	path:=t.TempDir()+"/hotlist.json"
	s,err:=OpenFileHotlistStore(path);if err!=nil{t.Fatal(err)}
	e:=model.HotlistEntry{Plate:"abc-1d23",Label:"procurado",Enabled:true}
	if err:=s.Put(e);err!=nil{t.Fatal(err)}
	got:=s.List();if len(got)!=1||got[0].Plate!="ABC1D23"{t.Fatalf("got=%+v",got)}
	if match,ok:=s.Match("ABC1D23");!ok||match.Label!="procurado"{t.Fatalf("match=%+v ok=%v",match,ok)}
	s2,err:=OpenFileHotlistStore(path);if err!=nil{t.Fatal(err)}
	if _,ok:=s2.Match("ABC1D23");!ok{t.Fatal("persisted match missing")}
}
