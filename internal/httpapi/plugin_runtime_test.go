package httpapi

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wfuzatto/Nvr/internal/config"
	"github.com/wfuzatto/Nvr/internal/model"
	"github.com/wfuzatto/Nvr/internal/security"
	"github.com/wfuzatto/Nvr/internal/store"
)

func TestPluginRuntimeEndToEnd(t *testing.T){
	dir:=t.TempDir()

	snapshotSrv:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		img:=image.NewRGBA(image.Rect(0,0,16,8))
		for y:=0;y<8;y++{for x:=0;x<16;x++{img.Set(x,y,color.RGBA{R:220,G:220,B:220,A:255})}}
		w.Header().Set("Content-Type","image/png")
		if err:=png.Encode(w,img);err!=nil{t.Fatal(err)}
	}))
	defer snapshotSrv.Close()

	box,err:=security.LoadOrCreateSecretBox(dir+"/master.key");if err!=nil{t.Fatal(err)}
	snapCipher,err:=box.Encrypt(snapshotSrv.URL);if err!=nil{t.Fatal(err)}
	cams,err:=store.OpenFileCameraStore(dir+"/cameras.json");if err!=nil{t.Fatal(err)}
	if err:=cams.Put(model.Camera{ID:"cam1",Name:"Camera 1",City:"Cidade",Site:"Centro",Enabled:true,SnapshotURLCipher:snapCipher});err!=nil{t.Fatal(err)}
	events,err:=store.OpenFileEventStore(dir+"/events.jsonl");if err!=nil{t.Fatal(err)}
	hotlist,err:=store.OpenFileHotlistStore(dir+"/hotlist.json");if err!=nil{t.Fatal(err)}
	if _,err:=hotlist.Put(model.HotlistEntry{Plate:"ABC1D23",Label:"teste",Enabled:true});err!=nil{t.Fatal(err)}
	auth,err:=security.OpenAuthManager(dir+"/users.json","admin");if err!=nil{t.Fatal(err)}

	api:=New(Dependencies{Config:config.Config{SnapshotTimeout:time.Second},AdminToken:"admin",SecretBox:box,Auth:auth,Cameras:cams})
	AttachPluginRoutes(api,PluginDependencies{Token:"plugin",Events:events,EvidenceDir:dir+"/evidence",Hotlist:hotlist})
	srv:=httptest.NewServer(api.Handler());defer srv.Close()

	req,_:=http.NewRequest(http.MethodGet,srv.URL+"/api/v1/plugin/v1/cameras",nil)
	req.Header.Set("Authorization","Bearer plugin")
	resp,err:=http.DefaultClient.Do(req);if err!=nil{t.Fatal(err)}
	if resp.StatusCode!=http.StatusOK{t.Fatalf("cameras status=%d",resp.StatusCode)}
	var cameraList struct{Count int `json:"count"`}
	if err:=json.NewDecoder(resp.Body).Decode(&cameraList);err!=nil{t.Fatal(err)};_ = resp.Body.Close()
	if cameraList.Count!=1{t.Fatalf("camera count=%d",cameraList.Count)}

	req,_=http.NewRequest(http.MethodGet,srv.URL+"/api/v1/plugin/v1/cameras/cam1/frame",nil)
	req.Header.Set("Authorization","Bearer plugin")
	resp,err=http.DefaultClient.Do(req);if err!=nil{t.Fatal(err)}
	if resp.StatusCode!=http.StatusOK{t.Fatalf("frame status=%d",resp.StatusCode)}
	if resp.Header.Get("Content-Type")!="image/jpeg"{t.Fatalf("frame content-type=%q",resp.Header.Get("Content-Type"))}
	frame:=new(bytes.Buffer);_,_=frame.ReadFrom(resp.Body);_ = resp.Body.Close()
	if frame.Len()==0{t.Fatal("empty frame")}

	req,_=http.NewRequest(http.MethodPost,srv.URL+"/api/v1/plugin/v1/evidence",bytes.NewReader(frame.Bytes()))
	req.Header.Set("Authorization","Bearer plugin");req.Header.Set("X-Event-ID","evt1");req.Header.Set("Content-Type","image/jpeg")
	resp,err=http.DefaultClient.Do(req);if err!=nil{t.Fatal(err)}
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
	var result struct{
		Count int `json:"count"`
		Items []model.EventEnvelope `json:"items"`
	}
	if err:=json.NewDecoder(resp.Body).Decode(&result);err!=nil{t.Fatal(err)}
	_ = resp.Body.Close()
	if result.Count!=1{t.Fatalf("count=%d",result.Count)}
	if len(result.Items)!=1||!result.Items[0].Alert||result.Items[0].AlertLabel!="teste"{t.Fatalf("alert=%+v",result.Items)}

	req,_=http.NewRequest(http.MethodGet,srv.URL+"/api/v1/events/evt1/evidence",nil)
	req.Header.Set("Authorization","Bearer admin")
	resp,err=http.DefaultClient.Do(req);if err!=nil{t.Fatal(err)}
	if resp.StatusCode!=http.StatusOK{t.Fatalf("evidence get status=%d",resp.StatusCode)}
	if resp.Header.Get("Content-Type")!="image/jpeg"{t.Fatalf("evidence type=%q",resp.Header.Get("Content-Type"))}
	_ = resp.Body.Close()
}
