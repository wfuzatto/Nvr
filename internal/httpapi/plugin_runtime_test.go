package httpapi

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wfuzatto/Nvr/internal/config"
	"github.com/wfuzatto/Nvr/internal/model"
	"github.com/wfuzatto/Nvr/internal/store"
)

func TestPluginEventEvidenceAndSearch(t *testing.T){
	dir:=t.TempDir()
	cams,err:=store.OpenFileCameraStore(dir+"/cameras.json");if err!=nil{t.Fatal(err)}
	if err:=cams.Put(model.Camera{ID:"cam1",Name:"Camera 1",City:"Cidade",Site:"Centro",Enabled:true});err!=nil{t.Fatal(err)}
	events,err:=store.OpenFileEventStore(dir+"/events.jsonl");if err!=nil{t.Fatal(err)}
	api:=New(Dependencies{Config:config.Config{SnapshotTimeout:time.Second},AdminToken:"admin",Cameras:cams})
	AttachPluginRoutes(api,PluginDependencies{Token:"plugin",Events:events,EvidenceDir:dir+"/evidence"})
	srv:=httptest.NewServer(api.Handler());defer srv.Close()

	var img bytes.Buffer
	pic:=image.NewRGBA(image.Rect(0,0,8,8))
	for y:=0;y<8;y++{for x:=0;x<8;x++{pic.Set(x,y,color.RGBA{R:200,G:200,B:200,A:255})}}
	if err:=jpeg.Encode(&img,pic,nil);err!=nil{t.Fatal(err)}

	req, _:=http.NewRequest(http.MethodPost,srv.URL+"/api/v1/plugin/v1/evidence",bytes.NewReader(img.Bytes()))
	req.Header.Set("Authorization","Bearer plugin");req.Header.Set("X-Event-ID","evt1");req.Header.Set("Content-Type","image/jpeg")
	resp,err:=http.DefaultClient.Do(req);if err!=nil{t.Fatal(err)}
	if resp.StatusCode!=http.StatusCreated{t.Fatalf("evidence status=%d",resp.StatusCode)}
	_ = resp.Body.Close()

	event:=map[string]any{
		"event_id":"evt1","event_type":"nvr.event.plate.detected.v1","schema_version":"1",
		"camera_id":"cam1","observed_at":time.Now().UTC(),"plugin_id":"plate-ocr","plugin_version":"1.0.0",
		"confidence":0.9,"snapshot_ref":"evidence/evt1.jpg","dedupe_key":"cam1:ABC1D23",
		"attributes":map[string]any{"track_id":"t1","raw_text":"ABC1D23","normalized_text":"ABC1D23","format":"BR_MERCOSUL","detector_confidence":0.92},
	}
	payload,_:=json.Marshal(event)
	req,_=http.NewRequest(http.MethodPost,srv.URL+"/api/v1/plugin/v1/events",bytes.NewReader(payload))
	req.Header.Set("Authorization","Bearer plugin");req.Header.Set("Content-Type","application/json")
	resp,err=http.DefaultClient.Do(req);if err!=nil{t.Fatal(err)}
	if resp.StatusCode!=http.StatusCreated{t.Fatalf("event status=%d",resp.StatusCode)}
	_ = resp.Body.Close()

	req,_=http.NewRequest(http.MethodPost,srv.URL+"/api/v1/plugin/v1/events",bytes.NewReader(payload))
	req.Header.Set("Authorization","Bearer plugin");req.Header.Set("Content-Type","application/json")
	resp,err=http.DefaultClient.Do(req);if err!=nil{t.Fatal(err)}
	if resp.StatusCode!=http.StatusOK{t.Fatalf("duplicate status=%d",resp.StatusCode)}
	_ = resp.Body.Close()

	req,_=http.NewRequest(http.MethodGet,srv.URL+"/api/v1/events/plates?plate=1D2",nil)
	req.Header.Set("Authorization","Bearer admin")
	resp,err=http.DefaultClient.Do(req);if err!=nil{t.Fatal(err)}
	if resp.StatusCode!=http.StatusOK{t.Fatalf("search status=%d",resp.StatusCode)}
	var result struct{Count int `json:"count"`}
	if err:=json.NewDecoder(resp.Body).Decode(&result);err!=nil{t.Fatal(err)}
	_ = resp.Body.Close()
	if result.Count!=1{t.Fatalf("count=%d",result.Count)}

	req,_=http.NewRequest(http.MethodGet,srv.URL+"/api/v1/events/evt1/evidence",nil)
	req.Header.Set("Authorization","Bearer admin")
	resp,err=http.DefaultClient.Do(req);if err!=nil{t.Fatal(err)}
	if resp.StatusCode!=http.StatusOK{t.Fatalf("evidence get status=%d",resp.StatusCode)}
	_ = resp.Body.Close()
}
