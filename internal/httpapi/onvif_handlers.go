package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/wfuzatto/Nvr/internal/model"
	"github.com/wfuzatto/Nvr/internal/onvif"
	"github.com/wfuzatto/Nvr/internal/store"
)

type onvifInspectInput struct {
	Endpoint string `json:"endpoint"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type onvifCreateInput struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	City         string   `json:"city"`
	Site         string   `json:"site"`
	Latitude     *float64 `json:"latitude"`
	Longitude    *float64 `json:"longitude"`
	Enabled      *bool    `json:"enabled"`
	Endpoint     string   `json:"endpoint"`
	Username     string   `json:"username"`
	Password     string   `json:"password"`
	ProfileToken string   `json:"profile_token"`
	MediaVersion int      `json:"media_version"`
}

func (s *Server) handleONVIFDiscover(w http.ResponseWriter, r *http.Request) {
	timeout := 3 * time.Second
	if raw := strings.TrimSpace(r.URL.Query().Get("timeout_ms")); raw != "" {
		ms, err := strconv.Atoi(raw)
		if err != nil || ms < 500 || ms > 10000 {
			writeError(w, http.StatusBadRequest, "timeout_ms must be between 500 and 10000")
			return
		}
		timeout = time.Duration(ms) * time.Millisecond
	}
	items, err := onvif.Discover(r.Context(), timeout)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items":items,"count":len(items)})
}

func (s *Server) handleONVIFInspect(w http.ResponseWriter, r *http.Request) {
	var in onvifInspectInput
	if err := decodeJSON(r,&in); err != nil { writeError(w,http.StatusBadRequest,err.Error()); return }
	client, err := onvif.NewClient(in.Endpoint,in.Username,in.Password,8*time.Second)
	if err != nil { writeError(w,http.StatusBadRequest,err.Error()); return }

	ctx, cancel := context.WithTimeout(r.Context(),15*time.Second)
	defer cancel()
	info, infoErr := client.DeviceInformation(ctx)
	services, servicesErr := client.Services(ctx)
	profiles, profilesErr := client.Profiles(ctx)
	if infoErr != nil && servicesErr != nil && profilesErr != nil {
		writeError(w,http.StatusBadGateway,fmt.Sprintf("ONVIF unavailable: %v; %v; %v",infoErr,servicesErr,profilesErr))
		return
	}
	response := map[string]any{
		"device":info,
		"services":services,
		"profiles":profiles,
	}
	if infoErr != nil { response["device_error"]=infoErr.Error() }
	if servicesErr != nil { response["services_error"]=servicesErr.Error() }
	if profilesErr != nil { response["profiles_error"]=profilesErr.Error() }
	writeJSON(w,http.StatusOK,response)
}

func (s *Server) handleCreateCameraFromONVIF(w http.ResponseWriter, r *http.Request) {
	var in onvifCreateInput
	if err := decodeJSON(r,&in); err != nil { writeError(w,http.StatusBadRequest,err.Error()); return }
	if strings.TrimSpace(in.Name)=="" { writeError(w,http.StatusBadRequest,"name is required"); return }
	if strings.TrimSpace(in.Endpoint)=="" { writeError(w,http.StatusBadRequest,"endpoint is required"); return }
	if strings.TrimSpace(in.ProfileToken)=="" { writeError(w,http.StatusBadRequest,"profile_token is required"); return }
	if in.Latitude != nil && (*in.Latitude < -90 || *in.Latitude > 90) { writeError(w,http.StatusBadRequest,"latitude must be between -90 and 90"); return }
	if in.Longitude != nil && (*in.Longitude < -180 || *in.Longitude > 180) { writeError(w,http.StatusBadRequest,"longitude must be between -180 and 180"); return }

	client, err := onvif.NewClient(in.Endpoint,in.Username,in.Password,8*time.Second)
	if err != nil { writeError(w,http.StatusBadRequest,err.Error()); return }
	ctx, cancel := context.WithTimeout(r.Context(),20*time.Second)
	defer cancel()
	profiles, err := client.Profiles(ctx)
	if err != nil { writeError(w,http.StatusBadGateway,err.Error()); return }
	profile, ok := selectONVIFProfile(profiles,in.ProfileToken,in.MediaVersion)
	if !ok { writeError(w,http.StatusBadRequest,"selected ONVIF profile not found"); return }

	streamURI, err := client.StreamURI(ctx,profile.Token,profile.MediaVersion)
	if err != nil { writeError(w,http.StatusBadGateway,"GetStreamUri: "+err.Error()); return }
	streamURI, err = addURLCredentials(streamURI,in.Username,in.Password)
	if err != nil { writeError(w,http.StatusBadGateway,"invalid ONVIF stream URI: "+err.Error()); return }

	snapshotURI := ""
	if uri, snapErr := client.SnapshotURI(ctx,profile.Token,profile.MediaVersion); snapErr == nil {
		snapshotURI, _ = addURLCredentials(uri,in.Username,in.Password)
	}

	rtspCipher, rtspRedacted, err := s.protectRTSP(streamURI)
	if err != nil { writeError(w,http.StatusBadGateway,err.Error()); return }
	snapshotCipher, snapshotRedacted := "",""
	if snapshotURI != "" {
		snapshotCipher, snapshotRedacted, err = s.protectSnapshotURL(snapshotURI)
		if err != nil { writeError(w,http.StatusBadGateway,err.Error()); return }
	}
	onvifCipher, onvifRedacted, err := s.protectONVIFURL(in.Endpoint,in.Username,in.Password)
	if err != nil { writeError(w,http.StatusBadRequest,err.Error()); return }

	enabled := true
	if in.Enabled != nil { enabled=*in.Enabled }
	now:=time.Now().UTC()
	camera:=model.Camera{
		ID:model.NewID(), Name:strings.TrimSpace(in.Name), Description:strings.TrimSpace(in.Description),
		City:strings.TrimSpace(in.City), Site:strings.TrimSpace(in.Site), Latitude:in.Latitude, Longitude:in.Longitude,
		Enabled:enabled, RTSPURLCipher:rtspCipher, RTSPURLRedacted:rtspRedacted,
		SnapshotURLCipher:snapshotCipher, SnapshotURLRedacted:snapshotRedacted,
		ONVIFURLCipher:onvifCipher, ONVIFURLRedacted:onvifRedacted,
		ONVIFProfileToken:profile.Token, ONVIFMediaVersion:profile.MediaVersion, ONVIFPTZ:profile.PTZ,
		CreatedAt:now, UpdatedAt:now,
	}
	if err:=s.deps.Cameras.Put(camera); err!=nil { writeError(w,http.StatusInternalServerError,err.Error()); return }
	writeJSON(w,http.StatusCreated,map[string]any{"camera":camera.Public(),"profile":profile})
}

func (s *Server) handleONVIFSync(w http.ResponseWriter, r *http.Request) {
	camera, err:=s.deps.Cameras.Get(r.PathValue("id"))
	if errors.Is(err,store.ErrNotFound) { writeError(w,http.StatusNotFound,"camera not found"); return }
	if err!=nil { writeError(w,http.StatusInternalServerError,err.Error()); return }
	client, username, password, err:=s.onvifClientForCamera(camera)
	if err!=nil { writeError(w,http.StatusConflict,err.Error()); return }

	ctx,cancel:=context.WithTimeout(r.Context(),20*time.Second)
	defer cancel()
	profiles,err:=client.Profiles(ctx)
	if err!=nil { writeError(w,http.StatusBadGateway,err.Error()); return }
	profile,ok:=selectONVIFProfile(profiles,camera.ONVIFProfileToken,camera.ONVIFMediaVersion)
	if !ok { writeError(w,http.StatusConflict,"stored ONVIF profile no longer exists"); return }

	streamURI,err:=client.StreamURI(ctx,profile.Token,profile.MediaVersion)
	if err!=nil { writeError(w,http.StatusBadGateway,err.Error()); return }
	streamURI,_=addURLCredentials(streamURI,username,password)
	rtspCipher,rtspRedacted,err:=s.protectRTSP(streamURI)
	if err!=nil { writeError(w,http.StatusBadGateway,err.Error()); return }

	camera.RTSPURLCipher=rtspCipher
	camera.RTSPURLRedacted=rtspRedacted
	camera.ONVIFMediaVersion=profile.MediaVersion
	camera.ONVIFPTZ=profile.PTZ
	if uri,snapErr:=client.SnapshotURI(ctx,profile.Token,profile.MediaVersion); snapErr==nil {
		if uri,credErr:=addURLCredentials(uri,username,password); credErr==nil {
			if cipher,redacted,protectErr:=s.protectSnapshotURL(uri); protectErr==nil {
				camera.SnapshotURLCipher=cipher
				camera.SnapshotURLRedacted=redacted
			}
		}
	}
	camera.UpdatedAt=time.Now().UTC()
	if err:=s.deps.Cameras.Put(camera); err!=nil { writeError(w,http.StatusInternalServerError,err.Error()); return }
	writeJSON(w,http.StatusOK,map[string]any{"camera":camera.Public(),"profile":profile})
}

func (s *Server) handlePTZStatus(w http.ResponseWriter, r *http.Request) {
	camera,client,_,err:=s.cameraAndONVIFClient(r.PathValue("id"))
	if err!=nil { writeError(w,statusForCameraError(err),err.Error()); return }
	if !camera.ONVIFPTZ { writeError(w,http.StatusConflict,"selected ONVIF profile has no PTZ configuration"); return }
	ctx,cancel:=context.WithTimeout(r.Context(),8*time.Second); defer cancel()
	status,err:=client.PTZStatus(ctx,camera.ONVIFProfileToken)
	if err!=nil { writeError(w,http.StatusBadGateway,err.Error()); return }
	writeJSON(w,http.StatusOK,status)
}

func (s *Server) handlePTZMove(w http.ResponseWriter, r *http.Request) {
	camera,client,_,err:=s.cameraAndONVIFClient(r.PathValue("id"))
	if err!=nil { writeError(w,statusForCameraError(err),err.Error()); return }
	if !camera.ONVIFPTZ { writeError(w,http.StatusConflict,"selected ONVIF profile has no PTZ configuration"); return }
	var in struct {
		Pan float64 `json:"pan"`
		Tilt float64 `json:"tilt"`
		Zoom float64 `json:"zoom"`
		TimeoutMS int `json:"timeout_ms"`
	}
	if err:=decodeJSON(r,&in); err!=nil { writeError(w,http.StatusBadRequest,err.Error()); return }
	if in.Pan < -1 || in.Pan > 1 || in.Tilt < -1 || in.Tilt > 1 || in.Zoom < -1 || in.Zoom > 1 {
		writeError(w,http.StatusBadRequest,"pan, tilt and zoom must be between -1 and 1"); return
	}
	timeout:=time.Duration(0)
	if in.TimeoutMS!=0 {
		if in.TimeoutMS<100 || in.TimeoutMS>10000 { writeError(w,http.StatusBadRequest,"timeout_ms must be between 100 and 10000"); return }
		timeout=time.Duration(in.TimeoutMS)*time.Millisecond
	}
	ctx,cancel:=context.WithTimeout(r.Context(),8*time.Second); defer cancel()
	if err:=client.PTZContinuousMove(ctx,camera.ONVIFProfileToken,in.Pan,in.Tilt,in.Zoom,timeout); err!=nil {
		writeError(w,http.StatusBadGateway,err.Error()); return
	}
	writeJSON(w,http.StatusOK,map[string]any{"moving":true})
}

func (s *Server) handlePTZStop(w http.ResponseWriter, r *http.Request) {
	camera,client,_,err:=s.cameraAndONVIFClient(r.PathValue("id"))
	if err!=nil { writeError(w,statusForCameraError(err),err.Error()); return }
	ctx,cancel:=context.WithTimeout(r.Context(),8*time.Second); defer cancel()
	if err:=client.PTZStop(ctx,camera.ONVIFProfileToken,true,true); err!=nil {
		writeError(w,http.StatusBadGateway,err.Error()); return
	}
	writeJSON(w,http.StatusOK,map[string]any{"moving":false})
}

func (s *Server) protectONVIFURL(raw, username, password string) (string,string,error) {
	u,err:=url.Parse(strings.TrimSpace(raw))
	if err!=nil || u.Hostname()=="" { return "","",errors.New("invalid ONVIF endpoint") }
	if u.Scheme!="http" && u.Scheme!="https" { return "","",errors.New("ONVIF endpoint must use http or https") }
	if username!="" { u.User=url.UserPassword(username,password) }
	cipher,err:=s.deps.SecretBox.Encrypt(u.String())
	if err!=nil { return "","",err }
	u.User=nil
	return cipher,u.String(),nil
}

func (s *Server) onvifClientForCamera(camera model.Camera) (*onvif.Client,string,string,error) {
	if camera.ONVIFURLCipher=="" || camera.ONVIFProfileToken=="" { return nil,"","",errors.New("camera is not configured through ONVIF") }
	raw,err:=s.deps.SecretBox.Decrypt(camera.ONVIFURLCipher)
	if err!=nil { return nil,"","",errors.New("unable to decrypt ONVIF endpoint") }
	u,err:=url.Parse(raw)
	if err!=nil { return nil,"","",errors.New("invalid stored ONVIF endpoint") }
	username,password:="",""
	if u.User!=nil { username=u.User.Username(); password,_=u.User.Password() }
	client,err:=onvif.NewClient(raw,"","",8*time.Second)
	return client,username,password,err
}

func (s *Server) cameraAndONVIFClient(id string) (model.Camera,*onvif.Client,string,error) {
	camera,err:=s.deps.Cameras.Get(id)
	if errors.Is(err,store.ErrNotFound) { return model.Camera{},nil,"",store.ErrNotFound }
	if err!=nil { return model.Camera{},nil,"",err }
	client,user,_,err:=s.onvifClientForCamera(camera)
	return camera,client,user,err
}

func statusForCameraError(err error) int {
	if errors.Is(err,store.ErrNotFound) { return http.StatusNotFound }
	return http.StatusConflict
}

func selectONVIFProfile(profiles []onvif.Profile, token string, mediaVersion int) (onvif.Profile,bool) {
	for _,profile:=range profiles {
		if profile.Token!=token { continue }
		if mediaVersion==0 || profile.MediaVersion==mediaVersion { return profile,true }
	}
	return onvif.Profile{},false
}

func addURLCredentials(raw, username, password string) (string,error) {
	u,err:=url.Parse(strings.TrimSpace(raw))
	if err!=nil || u.Hostname()=="" { return "",errors.New("invalid URI") }
	if username!="" && u.User==nil { u.User=url.UserPassword(username,password) }
	return u.String(),nil
}
