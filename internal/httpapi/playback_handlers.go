package httpapi

import (
	"fmt"
	"math"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wfuzatto/Nvr/internal/media"
	"github.com/wfuzatto/Nvr/internal/playback"
	"github.com/wfuzatto/Nvr/internal/store"
)

func (s *Server) handlePlaybackSession(w http.ResponseWriter, r *http.Request) {
	if s.deps.Media == nil { writeError(w,http.StatusServiceUnavailable,"media engine unavailable"); return }
	if _,err:=s.deps.Cameras.Get(r.PathValue("id")); err!=nil {
		if err==store.ErrNotFound { writeError(w,http.StatusNotFound,"camera not found") } else { writeError(w,http.StatusInternalServerError,err.Error()) }
		return
	}
	var in struct {
		From string `json:"from"`
		To string `json:"to"`
		Limit int `json:"limit"`
		TTLSeconds int `json:"ttl_seconds"`
	}
	if err:=decodeJSON(r,&in); err!=nil { writeError(w,http.StatusBadRequest,err.Error()); return }
	limit:=in.Limit
	if limit==0 { limit=120 }
	if limit<1 || limit>500 { writeError(w,http.StatusBadRequest,"limit must be between 1 and 500"); return }
	ttl:=10*time.Minute
	if in.TTLSeconds!=0 {
		if in.TTLSeconds<30 || in.TTLSeconds>3600 { writeError(w,http.StatusBadRequest,"ttl_seconds must be between 30 and 3600"); return }
		ttl=time.Duration(in.TTLSeconds)*time.Second
	}
	if in.From!="" {
		if _,err:=time.Parse(time.RFC3339,in.From); err!=nil { writeError(w,http.StatusBadRequest,"from must use RFC3339"); return }
	}
	if in.To!="" {
		if _,err:=time.Parse(time.RFC3339,in.To); err!=nil { writeError(w,http.StatusBadRequest,"to must use RFC3339"); return }
	}
	cameraID:=r.PathValue("id")
	token:=playback.SignToken(s.deps.AdminToken,cameraID,ttl)
	q:=url.Values{}
	q.Set("token",token)
	q.Set("limit",strconv.Itoa(limit))
	if in.From!="" { q.Set("from",in.From) }
	if in.To!="" { q.Set("to",in.To) }
	playlist:="/api/v1/playback/"+url.PathEscape(cameraID)+"/index.m3u8?"+q.Encode()
	writeJSON(w,http.StatusOK,map[string]any{
		"playlist_url":playlist,
		"expires_at":time.Now().Add(ttl).UTC(),
		"token_ttl_seconds":int(ttl.Seconds()),
	})
}

func (s *Server) handlePlaybackPlaylist(w http.ResponseWriter, r *http.Request) {
	if s.deps.Media == nil { http.Error(w,"media engine unavailable",http.StatusServiceUnavailable); return }
	cameraID:=r.PathValue("id")
	if err:=playback.ValidateToken(s.deps.AdminToken,cameraID,r.URL.Query().Get("token")); err!=nil {
		http.Error(w,err.Error(),http.StatusUnauthorized); return
	}
	from,err:=parsePlaybackTime(r.URL.Query().Get("from"))
	if err!=nil { http.Error(w,"invalid from",http.StatusBadRequest); return }
	to,err:=parsePlaybackTime(r.URL.Query().Get("to"))
	if err!=nil { http.Error(w,"invalid to",http.StatusBadRequest); return }
	limit:=120
	if raw:=r.URL.Query().Get("limit"); raw!="" {
		limit,err=strconv.Atoi(raw)
		if err!=nil || limit<1 || limit>500 { http.Error(w,"invalid limit",http.StatusBadRequest); return }
	}
	items,err:=s.deps.Media.Timeline(cameraID,from,to,limit)
	if err!=nil { http.Error(w,err.Error(),http.StatusInternalServerError); return }

	playable:=make([]media.Segment,0,len(items))
	maxDuration:=1.0
	for _,segment:=range items {
		if segment.Partial || segment.Path=="" { continue }
		if segment.FramesPath=="" { segment.FramesPath=segment.Path+".frames.idx" }
		if !playback.CanMux(s.deps.Config.StorageDir,segment) { continue }
		duration:=segment.End.Sub(segment.Start).Seconds()
		if duration<=0 { continue }
		if duration>maxDuration { maxDuration=duration }
		playable=append(playable,segment)
	}

	w.Header().Set("Content-Type","application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control","no-store")
	fmt.Fprintln(w,"#EXTM3U")
	fmt.Fprintln(w,"#EXT-X-VERSION:3")
	fmt.Fprintf(w,"#EXT-X-TARGETDURATION:%d\n",int(math.Ceil(maxDuration)))
	fmt.Fprintln(w,"#EXT-X-MEDIA-SEQUENCE:0")
	fmt.Fprintln(w,"#EXT-X-INDEPENDENT-SEGMENTS")
	for i,segment:=range playable {
		if i>0 { fmt.Fprintln(w,"#EXT-X-DISCONTINUITY") }
		fmt.Fprintf(w,"#EXT-X-PROGRAM-DATE-TIME:%s\n",segment.Start.UTC().Format(time.RFC3339Nano))
		fmt.Fprintf(w,"#EXTINF:%.3f,\n",segment.End.Sub(segment.Start).Seconds())
		q:=url.Values{}
		q.Set("token",r.URL.Query().Get("token"))
		q.Set("path",segment.Path)
		clock:=segment.ClockRate
		if clock<=0 { clock=90000 }
		q.Set("clock",strconv.Itoa(clock))
		fmt.Fprintf(w,"/api/v1/playback/%s/segment.ts?%s\n",url.PathEscape(cameraID),q.Encode())
	}
	fmt.Fprintln(w,"#EXT-X-ENDLIST")
}

func (s *Server) handlePlaybackSegment(w http.ResponseWriter, r *http.Request) {
	cameraID:=r.PathValue("id")
	if err:=playback.ValidateToken(s.deps.AdminToken,cameraID,r.URL.Query().Get("token")); err!=nil {
		http.Error(w,err.Error(),http.StatusUnauthorized); return
	}
	relative:=filepath.ToSlash(filepath.Clean(filepath.FromSlash(r.URL.Query().Get("path"))))
	if relative=="." || strings.HasPrefix(relative,"../") || !strings.HasPrefix(relative,cameraID+"/") {
		http.Error(w,"invalid segment path",http.StatusBadRequest); return
	}
	ext:=strings.ToLower(filepath.Ext(relative))
	codec:=""
	switch ext {
	case ".h264": codec="H264"
	case ".h265": codec="H265"
	default: http.Error(w,"unsupported segment",http.StatusBadRequest); return
	}
	clock:=90000
	if rawClock:=r.URL.Query().Get("clock"); rawClock!="" {
		parsed,err:=strconv.Atoi(rawClock)
		if err!=nil || parsed<1000 || parsed>1000000 {
			http.Error(w,"invalid RTP clock",http.StatusBadRequest)
			return
		}
		clock=parsed
	}
	segment:=media.Segment{
		CameraID:cameraID, Path:relative, FramesPath:relative+".frames.idx",
		Codec:codec, ClockRate:clock,
	}
	if !playback.CanMux(s.deps.Config.StorageDir,segment) {
		http.Error(w,"segment is not playable",http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type","video/mp2t")
	w.Header().Set("Cache-Control","private, max-age=60")
	w.Header().Set("X-Content-Type-Options","nosniff")
	if err:=playback.MuxSegmentTS(s.deps.Config.StorageDir,segment,w); err!=nil {
		// When streaming has not started, clients receive an explicit status.
		// If bytes were already written the connection will simply be truncated.
		return
	}
}

func parsePlaybackTime(raw string) (time.Time,error) {
	raw=strings.TrimSpace(raw)
	if raw=="" { return time.Time{},nil }
	value,err:=time.Parse(time.RFC3339,raw)
	if err!=nil { return time.Time{},err }
	return value.UTC(),nil
}
