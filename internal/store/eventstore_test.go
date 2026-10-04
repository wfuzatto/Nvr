package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/wfuzatto/Nvr/internal/model"
)

func TestEventStoreIdempotentAndSearchable(t *testing.T){
	s,err:=OpenFileEventStore(t.TempDir()+"/events.jsonl");if err!=nil{t.Fatal(err)}
	attrs,_:=json.Marshal(map[string]any{"raw_text":"ABC1D23","normalized_text":"ABC1D23","format":"BR_MERCOSUL"})
	ev:=model.EventEnvelope{EventID:"evt1",EventType:"nvr.event.plate.detected.v1",CameraID:"cam1",ObservedAt:time.Now().UTC(),Attributes:attrs}
	created,err:=s.Put(ev);if err!=nil||!created{t.Fatalf("put created=%v err=%v",created,err)}
	created,err=s.Put(ev);if err!=nil||created{t.Fatalf("duplicate created=%v err=%v",created,err)}
	items,err:=s.Search(EventQuery{Plate:"1D2",Limit:10});if err!=nil||len(items)!=1{t.Fatalf("search=%v err=%v",items,err)}
	if s.Count()!=1{t.Fatalf("count=%d",s.Count())}
}
