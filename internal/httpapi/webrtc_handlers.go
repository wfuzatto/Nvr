package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/wfuzatto/Nvr/internal/store"
	"github.com/wfuzatto/Nvr/internal/webrtclive"
)

type webRTCOfferInput struct {
	Type string `json:"type"`
	SDP  string `json:"sdp"`
}

func (s *Server) handleWebRTCSession(w http.ResponseWriter, r *http.Request) {
	if s.deps.WebRTC==nil {
		writeError(w,http.StatusServiceUnavailable,"WebRTC unavailable")
		return
	}
	camera,err:=s.deps.Cameras.Get(r.PathValue("id"))
	if errors.Is(err,store.ErrNotFound) {
		writeError(w,http.StatusNotFound,"camera not found")
		return
	}
	if err!=nil {
		writeError(w,http.StatusInternalServerError,err.Error())
		return
	}
	if !camera.Enabled {
		writeError(w,http.StatusConflict,"camera is disabled")
		return
	}
	if s.deps.Media==nil {
		writeError(w,http.StatusServiceUnavailable,"media engine unavailable")
		return
	}
	status,ok:=s.deps.Media.Status(camera.ID)
	if !ok || strings.ToLower(status.State)!="recording" {
		writeJSON(w,http.StatusConflict,map[string]any{
			"error":"camera is not currently recording",
			"fallback":"hls",
		})
		return
	}
	if strings.ToUpper(status.Codec)!="H264" {
		writeJSON(w,http.StatusConflict,map[string]any{
			"error":"WebRTC low-latency requires H264 without transcoding",
			"codec":status.Codec,
			"fallback":"hls",
		})
		return
	}

	var in webRTCOfferInput
	if err:=decodeJSON(r,&in); err!=nil {
		writeError(w,http.StatusBadRequest,err.Error())
		return
	}
	if strings.ToLower(strings.TrimSpace(in.Type))!="offer" || strings.TrimSpace(in.SDP)=="" {
		writeError(w,http.StatusBadRequest,"type=offer and sdp are required")
		return
	}

	ctx,cancel:=context.WithTimeout(r.Context(),12*time.Second)
	defer cancel()
	answer,err:=s.deps.WebRTC.StartSession(ctx,camera.ID,status.Codec,in.SDP)
	if err!=nil {
		switch {
		case errors.Is(err,webrtclive.ErrUnsupportedCodec):
			writeJSON(w,http.StatusConflict,map[string]any{
				"error":err.Error(),
				"fallback":"hls",
			})
		case errors.Is(err,webrtclive.ErrDisabled):
			writeJSON(w,http.StatusServiceUnavailable,map[string]any{
				"error":err.Error(),
				"fallback":"hls",
			})
		default:
			writeJSON(w,http.StatusBadGateway,map[string]any{
				"error":err.Error(),
				"fallback":"hls",
			})
		}
		return
	}
	writeJSON(w,http.StatusCreated,answer)
}

func (s *Server) handleWebRTCClose(w http.ResponseWriter, r *http.Request) {
	if s.deps.WebRTC==nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.deps.WebRTC.CloseSession(r.PathValue("session"))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleWebRTCStatus(w http.ResponseWriter, _ *http.Request) {
	if s.deps.WebRTC==nil {
		writeJSON(w,http.StatusOK,map[string]any{"enabled":false})
		return
	}
	writeJSON(w,http.StatusOK,s.deps.WebRTC.Stats())
}
