package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/wfuzatto/Nvr/internal/live"
	"github.com/wfuzatto/Nvr/internal/playback"
	"github.com/wfuzatto/Nvr/internal/store"
)

func (s *Server) handleLiveSession(w http.ResponseWriter, r *http.Request) {
	if s.deps.Live==nil { writeError(w,http.StatusServiceUnavailable,"live HLS unavailable"); return }
	camera,err:=s.deps.Cameras.Get(r.PathValue("id"))
	if err!=nil {
		if errors.Is(err,store.ErrNotFound) { writeError(w,http.StatusNotFound,"camera not found") } else { writeError(w,http.StatusInternalServerError,err.Error()) }
		return
	}
	if !camera.Enabled { writeError(w,http.StatusConflict,"camera is disabled"); return }

	var in struct {
		TTLSeconds int `json:"ttl_seconds"`
	}
	if err:=decodeJSON(r,&in); err!=nil { writeError(w,http.StatusBadRequest,err.Error()); return }
	ttl:=8*time.Hour
	if in.TTLSeconds!=0 {
		if in.TTLSeconds<30 || in.TTLSeconds>43200 { writeError(w,http.StatusBadRequest,"ttl_seconds must be between 30 and 43200"); return }
		ttl=time.Duration(in.TTLSeconds)*time.Second
	}
	token:=playback.SignToken(s.deps.AdminToken,camera.ID,ttl)
	playlist:="/api/v1/live/"+url.PathEscape(camera.ID)+"/index.m3u8?token="+url.QueryEscape(token)
	writeJSON(w,http.StatusOK,map[string]any{
		"playlist_url":playlist,
		"expires_at":time.Now().Add(ttl).UTC(),
		"token_ttl_seconds":int(ttl.Seconds()),
	})
}

func (s *Server) handleLivePlaylist(w http.ResponseWriter, r *http.Request) {
	if s.deps.Live==nil { http.Error(w,"live HLS unavailable",http.StatusServiceUnavailable); return }
	cameraID:=r.PathValue("id")
	token:=r.URL.Query().Get("token")
	if err:=playback.ValidateToken(s.deps.AdminToken,cameraID,token); err!=nil {
		http.Error(w,err.Error(),http.StatusUnauthorized); return
	}

	ctx,cancel:=context.WithTimeout(r.Context(),8*time.Second)
	defer cancel()
	segments,err:=s.deps.Live.Playlist(ctx,cameraID)
	if err!=nil {
		http.Error(w,err.Error(),http.StatusServiceUnavailable)
		return
	}
	if len(segments)==0 {
		http.Error(w,"live stream has no segments",http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type","application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control","no-store")
	fmt.Fprintln(w,"#EXTM3U")
	fmt.Fprintln(w,"#EXT-X-VERSION:3")
	fmt.Fprintf(w,"#EXT-X-TARGETDURATION:%d\n",live.TargetDuration(segments))
	fmt.Fprintf(w,"#EXT-X-MEDIA-SEQUENCE:%d\n",segments[0].Sequence)
	fmt.Fprintln(w,"#EXT-X-INDEPENDENT-SEGMENTS")
	for i,segment:=range segments {
		if i>0 { fmt.Fprintln(w,"#EXT-X-DISCONTINUITY") }
		fmt.Fprintf(w,"#EXT-X-PROGRAM-DATE-TIME:%s\n",segment.ProgramDateTime.UTC().Format(time.RFC3339Nano))
		fmt.Fprintf(w,"#EXTINF:%.3f,\n",segment.Duration)
		q:=url.Values{}
		q.Set("token",token)
		q.Set("seq",strconv.FormatUint(segment.Sequence,10))
		fmt.Fprintf(w,"/api/v1/live/%s/segment.ts?%s\n",url.PathEscape(cameraID),q.Encode())
	}
}

func (s *Server) handleLiveSegment(w http.ResponseWriter, r *http.Request) {
	if s.deps.Live==nil { http.Error(w,"live HLS unavailable",http.StatusServiceUnavailable); return }
	cameraID:=r.PathValue("id")
	if err:=playback.ValidateToken(s.deps.AdminToken,cameraID,r.URL.Query().Get("token")); err!=nil {
		http.Error(w,err.Error(),http.StatusUnauthorized); return
	}
	sequence,err:=strconv.ParseUint(r.URL.Query().Get("seq"),10,64)
	if err!=nil { http.Error(w,"invalid live sequence",http.StatusBadRequest); return }
	segment,ok:=s.deps.Live.Segment(cameraID,sequence)
	if !ok { http.Error(w,"live segment expired",http.StatusNotFound); return }
	w.Header().Set("Content-Type","video/mp2t")
	w.Header().Set("Cache-Control","private, max-age=5")
	w.Header().Set("Content-Length",strconv.Itoa(len(segment.Data)))
	_,_=w.Write(segment.Data)
}
