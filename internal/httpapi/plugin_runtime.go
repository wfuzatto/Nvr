package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wfuzatto/Nvr/internal/media"
	"github.com/wfuzatto/Nvr/internal/model"
	"github.com/wfuzatto/Nvr/internal/store"
)

type PluginDependencies struct {
	Token       string
	Events      store.EventStore
	EvidenceDir string
}

func AttachPluginRoutes(s *Server, deps PluginDependencies) {
	s.mux.Handle("GET /api/v1/plugin/v1/cameras", pluginAuth(deps.Token, http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){ handlePluginCameras(s,w,r) })))
	s.mux.Handle("GET /api/v1/plugin/v1/cameras/{id}/frame", pluginAuth(deps.Token, http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){ handlePluginFrame(s,w,r) })))
	s.mux.Handle("POST /api/v1/plugin/v1/evidence", pluginAuth(deps.Token, http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){ handlePluginEvidence(deps,w,r) })))
	s.mux.Handle("POST /api/v1/plugin/v1/events", pluginAuth(deps.Token, http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){ handlePluginEvent(s,deps,w,r) })))
	s.mux.Handle("GET /api/v1/events/plates", s.require("evidence", http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){ handlePlateSearch(deps,w,r) })))
	s.mux.Handle("GET /api/v1/events/{id}/evidence", s.require("evidence", http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){ handleEvidenceGet(deps,w,r) })))
}

func pluginAuth(token string,next http.Handler)http.Handler{
	return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		auth:=r.Header.Get("Authorization")
		if !strings.HasPrefix(auth,"Bearer "){w.Header().Set("WWW-Authenticate","Bearer");writeError(w,http.StatusUnauthorized,"invalid or missing plugin token");return}
		got:=strings.TrimSpace(strings.TrimPrefix(auth,"Bearer "))
		if got==""||token==""||subtle.ConstantTimeCompare([]byte(got),[]byte(token))!=1{
			w.Header().Set("WWW-Authenticate","Bearer");writeError(w,http.StatusUnauthorized,"invalid or missing plugin token");return
		}
		next.ServeHTTP(w,r)
	})
}

func handlePluginCameras(s *Server,w http.ResponseWriter,_ *http.Request){
	all:=s.deps.Cameras.List()
	items:=make([]map[string]any,0,len(all))
	for _,c:=range all{
		items=append(items,map[string]any{
			"id":c.ID,"name":c.Name,"city":c.City,"site":c.Site,"enabled":c.Enabled,
			"snapshot_available":c.SnapshotURLCipher!="","latitude":c.Latitude,"longitude":c.Longitude,
		})
	}
	writeJSON(w,http.StatusOK,map[string]any{"items":items,"count":len(items)})
}

func handlePluginFrame(s *Server,w http.ResponseWriter,r *http.Request){
	camera,err:=s.deps.Cameras.Get(r.PathValue("id"))
	if errors.Is(err,store.ErrNotFound){writeError(w,http.StatusNotFound,"camera not found");return}
	if err!=nil{writeError(w,http.StatusInternalServerError,err.Error());return}
	if !camera.Enabled{writeError(w,http.StatusConflict,"camera is disabled");return}
	if camera.SnapshotURLCipher==""{writeError(w,http.StatusConflict,"camera has no snapshot_url configured");return}
	raw,err:=s.deps.SecretBox.Decrypt(camera.SnapshotURLCipher)
	if err!=nil{writeError(w,http.StatusInternalServerError,"unable to decrypt snapshot URL");return}
	ctx,cancel:=context.WithTimeout(r.Context(),s.deps.Config.SnapshotTimeout);defer cancel()
	body,contentType,err:=media.FetchSnapshot(ctx,raw,s.deps.Config.SnapshotTimeout)
	if err!=nil{writeError(w,http.StatusBadGateway,err.Error());return}
	jpegBody,err:=toJPEG(body,contentType)
	if err!=nil{writeError(w,http.StatusUnsupportedMediaType,err.Error());return}
	w.Header().Set("Content-Type","image/jpeg")
	w.Header().Set("Cache-Control","no-store")
	w.Header().Set("X-NVR-Observed-At",time.Now().UTC().Format(time.RFC3339Nano))
	w.Header().Set("Content-Length",strconv.Itoa(len(jpegBody)))
	w.WriteHeader(http.StatusOK);_,_=w.Write(jpegBody)
}

func toJPEG(body []byte,contentType string)([]byte,error){
	if strings.EqualFold(strings.TrimSpace(contentType),"image/jpeg") || http.DetectContentType(body)=="image/jpeg" { return body,nil }
	img,_,err:=image.Decode(bytes.NewReader(body))
	if err!=nil{return nil,errors.New("snapshot image format cannot be decoded")}
	var out bytes.Buffer
	if err:=jpeg.Encode(&out,img,&jpeg.Options{Quality:90});err!=nil{return nil,err}
	return out.Bytes(),nil
}

func handlePluginEvidence(deps PluginDependencies,w http.ResponseWriter,r *http.Request){
	eventID:=strings.TrimSpace(r.Header.Get("X-Event-ID"))
	if !validID(eventID){writeError(w,http.StatusBadRequest,"invalid X-Event-ID");return}
	const maxEvidence=int64(12<<20)
	body,err:=io.ReadAll(io.LimitReader(r.Body,maxEvidence+1))
	if err!=nil{writeError(w,http.StatusBadRequest,err.Error());return}
	if int64(len(body))>maxEvidence{writeError(w,http.StatusRequestEntityTooLarge,"evidence exceeds 12 MiB");return}
	if len(body)<4||http.DetectContentType(body)!="image/jpeg"{writeError(w,http.StatusUnsupportedMediaType,"evidence must be JPEG");return}
	if err:=os.MkdirAll(deps.EvidenceDir,0o750);err!=nil{writeError(w,http.StatusInternalServerError,err.Error());return}
	name:=eventID+".jpg";path:=filepath.Join(deps.EvidenceDir,name)
	sum:=sha256.Sum256(body)
	if existing,readErr:=os.ReadFile(path);readErr==nil{
		old:=sha256.Sum256(existing)
		if subtle.ConstantTimeCompare(old[:],sum[:])!=1{writeError(w,http.StatusConflict,"evidence already exists with different content");return}
		writeJSON(w,http.StatusCreated,map[string]any{"snapshot_ref":"evidence/"+name,"sha256":hex.EncodeToString(sum[:]),"bytes":len(body),"created":false})
		return
	}else if !errors.Is(readErr,os.ErrNotExist){writeError(w,http.StatusInternalServerError,readErr.Error());return}
	if err:=atomicEvidenceWrite(path,body);err!=nil{writeError(w,http.StatusInternalServerError,err.Error());return}
	writeJSON(w,http.StatusCreated,map[string]any{"snapshot_ref":"evidence/"+name,"sha256":hex.EncodeToString(sum[:]),"bytes":len(body),"created":true})
}

type pluginEventInput struct{
	EventID string `json:"event_id"`
	EventType string `json:"event_type"`
	SchemaVersion string `json:"schema_version"`
	CameraID string `json:"camera_id"`
	ObservedAt time.Time `json:"observed_at"`
	PluginID string `json:"plugin_id"`
	PluginVersion string `json:"plugin_version"`
	Confidence float64 `json:"confidence"`
	SnapshotRef string `json:"snapshot_ref,omitempty"`
	ClipRef string `json:"clip_ref,omitempty"`
	DedupeKey string `json:"dedupe_key,omitempty"`
	Attributes json.RawMessage `json:"attributes"`
}

func handlePluginEvent(s *Server,deps PluginDependencies,w http.ResponseWriter,r *http.Request){
	if deps.Events==nil{writeError(w,http.StatusServiceUnavailable,"event store unavailable");return}
	var in pluginEventInput
	if err:=decodeJSON(r,&in);err!=nil{writeError(w,http.StatusBadRequest,err.Error());return}
	if !validID(in.EventID){writeError(w,http.StatusBadRequest,"invalid event_id");return}
	if in.EventType!="nvr.event.plate.detected.v1"{writeError(w,http.StatusBadRequest,"unsupported event_type");return}
	if in.PluginID!="plate-ocr"{writeError(w,http.StatusBadRequest,"unsupported plugin_id");return}
	if strings.TrimSpace(in.SchemaVersion)==""||strings.TrimSpace(in.PluginVersion)==""{writeError(w,http.StatusBadRequest,"schema and plugin version are required");return}
	if in.Confidence<0||in.Confidence>1{writeError(w,http.StatusBadRequest,"confidence must be between 0 and 1");return}
	if in.ObservedAt.IsZero(){writeError(w,http.StatusBadRequest,"observed_at is required");return}
	camera,err:=s.deps.Cameras.Get(in.CameraID)
	if errors.Is(err,store.ErrNotFound){writeError(w,http.StatusBadRequest,"camera not found");return}
	if err!=nil{writeError(w,http.StatusInternalServerError,err.Error());return}
	var plate model.PlateAttributes
	if err:=json.Unmarshal(in.Attributes,&plate);err!=nil{writeError(w,http.StatusBadRequest,"invalid plate attributes");return}
	plate.NormalizedText=strings.ToUpper(strings.TrimSpace(plate.NormalizedText))
	plate.RawText=strings.ToUpper(strings.TrimSpace(plate.RawText))
	if !validPlateText(plate.NormalizedText){writeError(w,http.StatusBadRequest,"normalized_text must contain 7 A-Z/0-9 characters");return}
	cleanAttrs,err:=json.Marshal(plate);if err!=nil{writeError(w,http.StatusBadRequest,err.Error());return}
	if in.SnapshotRef!=""{
		expected:="evidence/"+in.EventID+".jpg"
		if in.SnapshotRef!=expected{writeError(w,http.StatusBadRequest,"snapshot_ref does not match event_id");return}
		if _,err:=os.Stat(filepath.Join(deps.EvidenceDir,in.EventID+".jpg"));err!=nil{writeError(w,http.StatusBadRequest,"referenced evidence does not exist");return}
	}
	ev:=model.EventEnvelope{
		EventID:in.EventID,EventType:in.EventType,SchemaVersion:in.SchemaVersion,
		CityID:camera.City,SiteID:camera.Site,CameraID:camera.ID,
		ObservedAt:in.ObservedAt.UTC(),ReceivedAt:time.Now().UTC(),
		PluginID:in.PluginID,PluginVersion:in.PluginVersion,Confidence:in.Confidence,
		SnapshotRef:in.SnapshotRef,ClipRef:in.ClipRef,Attributes:cleanAttrs,DedupeKey:in.DedupeKey,
	}
	created,err:=deps.Events.Put(ev)
	if err!=nil{writeError(w,http.StatusInternalServerError,err.Error());return}
	status:=http.StatusCreated;if !created{status=http.StatusOK}
	writeJSON(w,status,map[string]any{"accepted":true,"created":created,"event_id":ev.EventID})
}

func handlePlateSearch(deps PluginDependencies,w http.ResponseWriter,r *http.Request){
	if deps.Events==nil{writeError(w,http.StatusServiceUnavailable,"event store unavailable");return}
	from,err:=parseTimeQuery(r,"from");if err!=nil{writeError(w,http.StatusBadRequest,err.Error());return}
	to,err:=parseTimeQuery(r,"to");if err!=nil{writeError(w,http.StatusBadRequest,err.Error());return}
	limit:=100
	if raw:=r.URL.Query().Get("limit");raw!=""{
		limit,err=strconv.Atoi(raw);if err!=nil||limit<1||limit>5000{writeError(w,http.StatusBadRequest,"limit must be between 1 and 5000");return}
	}
	items,err:=deps.Events.Search(store.EventQuery{Plate:r.URL.Query().Get("plate"),CameraID:r.URL.Query().Get("camera_id"),From:from,To:to,Limit:limit})
	if err!=nil{writeError(w,http.StatusInternalServerError,err.Error());return}
	writeJSON(w,http.StatusOK,map[string]any{"items":items,"count":len(items),"total_events":deps.Events.Count()})
}

func handleEvidenceGet(deps PluginDependencies,w http.ResponseWriter,r *http.Request){
	if deps.Events==nil{writeError(w,http.StatusServiceUnavailable,"event store unavailable");return}
	ev,err:=deps.Events.Get(r.PathValue("id"))
	if errors.Is(err,store.ErrNotFound){writeError(w,http.StatusNotFound,"event not found");return}
	if err!=nil{writeError(w,http.StatusInternalServerError,err.Error());return}
	if ev.SnapshotRef==""{writeError(w,http.StatusNotFound,"event has no evidence");return}
	expected:="evidence/"+ev.EventID+".jpg"
	if ev.SnapshotRef!=expected{writeError(w,http.StatusConflict,"invalid stored evidence reference");return}
	path:=filepath.Join(deps.EvidenceDir,ev.EventID+".jpg")
	body,err:=os.ReadFile(path)
	if errors.Is(err,os.ErrNotExist){writeError(w,http.StatusNotFound,"evidence file not found");return}
	if err!=nil{writeError(w,http.StatusInternalServerError,err.Error());return}
	w.Header().Set("Content-Type","image/jpeg");w.Header().Set("Cache-Control","private, no-store")
	w.Header().Set("Content-Length",strconv.Itoa(len(body)));w.WriteHeader(http.StatusOK);_,_=w.Write(body)
}

func validID(v string)bool{
	if len(v)<1||len(v)>128{return false}
	for _,r:=range v{if !((r>='a'&&r<='z')||(r>='A'&&r<='Z')||(r>='0'&&r<='9')||r=='-'||r=='_'){return false}}
	return true
}
func validPlateText(v string)bool{
	if len(v)!=7{return false}
	for _,r:=range v{if !((r>='A'&&r<='Z')||(r>='0'&&r<='9')){return false}}
	return true
}
func atomicEvidenceWrite(path string,b []byte)error{
	tmp:=path+".tmp"
	f,err:=os.OpenFile(tmp,os.O_CREATE|os.O_TRUNC|os.O_WRONLY,0o640);if err!=nil{return err}
	if _,err=f.Write(b);err!=nil{_ = f.Close();return err}
	if err=f.Sync();err!=nil{_ = f.Close();return err}
	if err=f.Close();err!=nil{return err}
	if err=os.Rename(tmp,path);err!=nil{return err}
	return os.Chmod(path,0o640)
}
