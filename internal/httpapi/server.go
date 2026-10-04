package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wfuzatto/Nvr/internal/audit"
	"github.com/wfuzatto/Nvr/internal/config"
	"github.com/wfuzatto/Nvr/internal/evidence"
	"github.com/wfuzatto/Nvr/internal/live"
	"github.com/wfuzatto/Nvr/internal/media"
	"github.com/wfuzatto/Nvr/internal/model"
	"github.com/wfuzatto/Nvr/internal/rtsp"
	"github.com/wfuzatto/Nvr/internal/security"
	"github.com/wfuzatto/Nvr/internal/store"
	"github.com/wfuzatto/Nvr/internal/webui"
	"github.com/wfuzatto/Nvr/internal/webrtclive"
)

type Dependencies struct {
	Config config.Config
	Version string
	AdminToken string
	SecretBox *security.SecretBox
	Auth *security.AuthManager
	Audit *audit.Log
	Evidence *evidence.Manager
	Cameras store.CameraStore
	Media *media.Manager
	Live *live.Manager
	WebRTC *webrtclive.Manager
}

type Server struct {
	deps Dependencies
	mux *http.ServeMux
	startedAt time.Time
}

type cameraInput struct {
	Name string `json:"name"`
	Description string `json:"description"`
	City string `json:"city"`
	Site string `json:"site"`
	Latitude *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	Enabled *bool `json:"enabled"`
	RTSPURL string `json:"rtsp_url"`
	SnapshotURL *string `json:"snapshot_url"`
}

func New(deps Dependencies) *Server {
	s := &Server{deps: deps, mux: http.NewServeMux(), startedAt:time.Now().UTC()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return securityHeaders(s.requestLog(s.mux))
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	s.mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	s.mux.Handle("POST /api/v1/auth/logout", s.auth(http.HandlerFunc(s.handleLogout)))
	s.mux.Handle("GET /api/v1/auth/me", s.auth(http.HandlerFunc(s.handleMe)))

	s.mux.Handle("GET /api/v1/users", s.require("admin", http.HandlerFunc(s.handleListUsers)))
	s.mux.Handle("POST /api/v1/users", s.require("admin", s.audited("user.create","user",http.HandlerFunc(s.handleCreateUser))))
	s.mux.Handle("PUT /api/v1/users/{id}", s.require("admin", s.audited("user.update","user",http.HandlerFunc(s.handleUpdateUser))))
	s.mux.Handle("PUT /api/v1/users/{id}/password", s.require("admin", s.audited("user.password","user",http.HandlerFunc(s.handleSetUserPassword))))
	s.mux.Handle("DELETE /api/v1/users/{id}", s.require("admin", s.audited("user.delete","user",http.HandlerFunc(s.handleDeleteUser))))

	s.mux.Handle("GET /api/v1/audit", s.require("evidence", http.HandlerFunc(s.handleAuditList)))
	s.mux.Handle("GET /api/v1/audit/verify", s.require("evidence", http.HandlerFunc(s.handleAuditVerify)))

	s.mux.Handle("POST /api/v1/cameras/{id}/exports", s.require("evidence", s.audited("evidence.export.create","camera",http.HandlerFunc(s.handleCreateExport))))
	s.mux.Handle("GET /api/v1/exports", s.require("evidence", http.HandlerFunc(s.handleListExports)))
	s.mux.Handle("GET /api/v1/exports/{export}", s.require("evidence", http.HandlerFunc(s.handleGetExport)))
	s.mux.Handle("GET /api/v1/exports/{export}/download", s.require("evidence", http.HandlerFunc(s.handleDownloadExport)))

	s.mux.Handle("GET /api/v1/system/readiness", s.auth(http.HandlerFunc(s.handleReadiness)))
	s.mux.Handle("GET /api/v1/system/status", s.auth(http.HandlerFunc(s.handleSystemStatus)))
	s.mux.Handle("GET /metrics", s.auth(http.HandlerFunc(s.handlePrometheus)))
	s.mux.Handle("GET /api/v1/cameras", s.auth(http.HandlerFunc(s.handleListCameras)))
	s.mux.Handle("POST /api/v1/cameras", s.require("admin", s.audited("camera.create","camera",http.HandlerFunc(s.handleCreateCamera))))
	s.mux.Handle("GET /api/v1/cameras/{id}", s.auth(http.HandlerFunc(s.handleGetCamera)))
	s.mux.Handle("PUT /api/v1/cameras/{id}", s.require("admin", s.audited("camera.update","camera",http.HandlerFunc(s.handleUpdateCamera))))
	s.mux.Handle("DELETE /api/v1/cameras/{id}", s.require("admin", s.audited("camera.delete","camera",http.HandlerFunc(s.handleDeleteCamera))))
	s.mux.Handle("POST /api/v1/cameras/{id}/test", s.require("operate", http.HandlerFunc(s.handleTestCamera)))
	s.mux.Handle("GET /api/v1/cameras/{id}/media/status", s.auth(http.HandlerFunc(s.handleCameraMediaStatus)))
	s.mux.Handle("GET /api/v1/cameras/{id}/timeline", s.auth(http.HandlerFunc(s.handleTimeline)))
	s.mux.Handle("GET /api/v1/cameras/{id}/pre-event/segments", s.auth(http.HandlerFunc(s.handlePreEventSegments)))
	s.mux.Handle("GET /api/v1/cameras/{id}/snapshot", s.auth(http.HandlerFunc(s.handleSnapshot)))
	s.mux.Handle("GET /api/v1/media/status", s.auth(http.HandlerFunc(s.handleMediaStatuses)))
	s.mux.Handle("GET /api/v1/media/broker", s.auth(http.HandlerFunc(s.handleBrokerStats)))
	s.mux.Handle("POST /api/v1/media/retention/run", s.require("admin", s.audited("retention.run","storage",http.HandlerFunc(s.handleRetentionRun))))
	s.mux.Handle("POST /api/v1/media/protect", s.require("evidence", s.audited("evidence.protect","segment",http.HandlerFunc(s.handleProtectSegment))))

	s.mux.Handle("GET /api/v1/onvif/discover", s.require("admin", http.HandlerFunc(s.handleONVIFDiscover)))
	s.mux.Handle("POST /api/v1/onvif/inspect", s.require("admin", http.HandlerFunc(s.handleONVIFInspect)))
	s.mux.Handle("POST /api/v1/cameras/from-onvif", s.require("admin", s.audited("camera.create.onvif","camera",http.HandlerFunc(s.handleCreateCameraFromONVIF))))
	s.mux.Handle("POST /api/v1/cameras/{id}/onvif/sync", s.require("admin", s.audited("camera.sync.onvif","camera",http.HandlerFunc(s.handleONVIFSync))))
	s.mux.Handle("GET /api/v1/cameras/{id}/ptz/status", s.require("operate", http.HandlerFunc(s.handlePTZStatus)))
	s.mux.Handle("POST /api/v1/cameras/{id}/ptz/move", s.require("operate", s.audited("ptz.move","camera",http.HandlerFunc(s.handlePTZMove))))
	s.mux.Handle("POST /api/v1/cameras/{id}/ptz/stop", s.require("operate", http.HandlerFunc(s.handlePTZStop)))

	s.mux.Handle("POST /api/v1/cameras/{id}/webrtc/session", s.auth(http.HandlerFunc(s.handleWebRTCSession)))
	s.mux.Handle("DELETE /api/v1/webrtc/sessions/{session}", s.auth(http.HandlerFunc(s.handleWebRTCClose)))
	s.mux.Handle("GET /api/v1/webrtc/status", s.auth(http.HandlerFunc(s.handleWebRTCStatus)))
	s.mux.Handle("POST /api/v1/cameras/{id}/live/session", s.auth(http.HandlerFunc(s.handleLiveSession)))
	s.mux.HandleFunc("GET /api/v1/live/{id}/index.m3u8", s.handleLivePlaylist)
	s.mux.HandleFunc("GET /api/v1/live/{id}/segment.ts", s.handleLiveSegment)
	s.mux.Handle("POST /api/v1/cameras/{id}/playback/session", s.auth(http.HandlerFunc(s.handlePlaybackSession)))
	s.mux.HandleFunc("GET /api/v1/playback/{id}/index.m3u8", s.handlePlaybackPlaylist)
	s.mux.HandleFunc("GET /api/v1/playback/{id}/segment.ts", s.handlePlaybackSegment)

	s.mux.Handle("/", webui.Handler())
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"version": s.deps.Version,
		"offline_first": true,
		"time": time.Now().UTC(),
	})
}

func (s *Server) handleReadiness(w http.ResponseWriter, _ *http.Request) {
	checks := map[string]any{}
	ok := true
	if err := writableDir(s.deps.Config.DataDir); err != nil {
		checks["data_dir"] = err.Error()
		ok = false
	} else { checks["data_dir"] = "ok" }
	if err := writableDir(s.deps.Config.StorageDir); err != nil {
		checks["storage_dir"] = err.Error()
		ok = false
	} else { checks["storage_dir"] = "ok" }
	checks["runtime_dir"] = s.deps.Config.RuntimeDir
	if s.deps.WebRTC != nil {
		checks["webrtc"] = s.deps.WebRTC.Stats()
	} else {
		checks["webrtc"] = "optional_unavailable"
	}
	checks["network_download_required"] = false
	status := http.StatusOK
	if !ok { status = http.StatusServiceUnavailable }
	writeJSON(w, status, map[string]any{"ready": ok, "checks": checks})
}

func (s *Server) handleListCameras(w http.ResponseWriter, _ *http.Request) {
	all := s.deps.Cameras.List()
	out := make([]model.CameraPublic, 0, len(all))
	for _, c := range all { out = append(out, c.Public()) }
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

func (s *Server) handleGetCamera(w http.ResponseWriter, r *http.Request) {
	c, err := s.deps.Cameras.Get(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) { writeError(w, http.StatusNotFound, "camera not found"); return }
	if err != nil { writeError(w, http.StatusInternalServerError, err.Error()); return }
	writeJSON(w, http.StatusOK, c.Public())
}

func (s *Server) handleCreateCamera(w http.ResponseWriter, r *http.Request) {
	var in cameraInput
	if err := decodeJSON(r, &in); err != nil { writeError(w, http.StatusBadRequest, err.Error()); return }
	if err := validateCameraInput(in, true); err != nil { writeError(w, http.StatusBadRequest, err.Error()); return }
	cipher, redacted, err := s.protectRTSP(in.RTSPURL)
	if err != nil { writeError(w, http.StatusBadRequest, err.Error()); return }
	snapshotCipher, snapshotRedacted := "", ""
	if in.SnapshotURL != nil && strings.TrimSpace(*in.SnapshotURL) != "" {
		snapshotCipher, snapshotRedacted, err = s.protectSnapshotURL(*in.SnapshotURL)
		if err != nil { writeError(w, http.StatusBadRequest, err.Error()); return }
	}
	enabled := true
	if in.Enabled != nil { enabled = *in.Enabled }
	now := time.Now().UTC()
	c := model.Camera{
		ID: model.NewID(), Name: strings.TrimSpace(in.Name), Description: strings.TrimSpace(in.Description),
		City: strings.TrimSpace(in.City), Site: strings.TrimSpace(in.Site),
		Latitude: in.Latitude, Longitude: in.Longitude, Enabled: enabled,
		RTSPURLCipher: cipher, RTSPURLRedacted: redacted,
		SnapshotURLCipher: snapshotCipher, SnapshotURLRedacted: snapshotRedacted,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.deps.Cameras.Put(c); err != nil { writeError(w, http.StatusInternalServerError, err.Error()); return }
	writeJSON(w, http.StatusCreated, c.Public())
}

func (s *Server) handleUpdateCamera(w http.ResponseWriter, r *http.Request) {
	current, err := s.deps.Cameras.Get(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) { writeError(w, http.StatusNotFound, "camera not found"); return }
	if err != nil { writeError(w, http.StatusInternalServerError, err.Error()); return }
	var in cameraInput
	if err := decodeJSON(r, &in); err != nil { writeError(w, http.StatusBadRequest, err.Error()); return }
	if strings.TrimSpace(in.Name) == "" { writeError(w, http.StatusBadRequest, "name is required"); return }
	current.Name = strings.TrimSpace(in.Name)
	current.Description = strings.TrimSpace(in.Description)
	current.City = strings.TrimSpace(in.City)
	current.Site = strings.TrimSpace(in.Site)
	current.Latitude = in.Latitude
	current.Longitude = in.Longitude
	if in.Enabled != nil { current.Enabled = *in.Enabled }
	if strings.TrimSpace(in.RTSPURL) != "" && in.RTSPURL != current.RTSPURLRedacted {
		cipher, redacted, err := s.protectRTSP(in.RTSPURL)
		if err != nil { writeError(w, http.StatusBadRequest, err.Error()); return }
		current.RTSPURLCipher = cipher
		current.RTSPURLRedacted = redacted
	}
	if in.SnapshotURL != nil {
		if strings.TrimSpace(*in.SnapshotURL) == "" {
			current.SnapshotURLCipher = ""
			current.SnapshotURLRedacted = ""
		} else if *in.SnapshotURL != current.SnapshotURLRedacted {
			cipher, redacted, err := s.protectSnapshotURL(*in.SnapshotURL)
			if err != nil { writeError(w, http.StatusBadRequest, err.Error()); return }
			current.SnapshotURLCipher = cipher
			current.SnapshotURLRedacted = redacted
		}
	}
	current.UpdatedAt = time.Now().UTC()
	if err := s.deps.Cameras.Put(current); err != nil { writeError(w, http.StatusInternalServerError, err.Error()); return }
	writeJSON(w, http.StatusOK, current.Public())
}

func (s *Server) handleDeleteCamera(w http.ResponseWriter, r *http.Request) {
	err := s.deps.Cameras.Delete(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) { writeError(w, http.StatusNotFound, "camera not found"); return }
	if err != nil { writeError(w, http.StatusInternalServerError, err.Error()); return }
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTestCamera(w http.ResponseWriter, r *http.Request) {
	camera, err := s.deps.Cameras.Get(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) { writeError(w, http.StatusNotFound, "camera not found"); return }
	if err != nil { writeError(w, http.StatusInternalServerError, err.Error()); return }

	raw, err := s.deps.SecretBox.Decrypt(camera.RTSPURLCipher)
	if err != nil { writeError(w, http.StatusInternalServerError, "unable to decrypt camera URL"); return }

	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()

	result, err := rtsp.Probe(ctx, raw)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"reachable": false,
			"target": camera.RTSPURLRedacted,
			"error": err.Error(),
			"rtsp": result,
			"latency_ms": result.LatencyMS,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"reachable": true,
		"target": camera.RTSPURLRedacted,
		"rtsp": result,
		"latency_ms": result.LatencyMS,
	})
}

func (s *Server) handleCameraMediaStatus(w http.ResponseWriter, r *http.Request) {
	if s.deps.Media == nil { writeError(w, http.StatusServiceUnavailable, "media engine unavailable"); return }
	if _, err := s.deps.Cameras.Get(r.PathValue("id")); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "camera not found")
		return
	}
	status, ok := s.deps.Media.Status(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"camera_id":r.PathValue("id"), "state":"stopped"})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleMediaStatuses(w http.ResponseWriter, _ *http.Request) {
	if s.deps.Media == nil { writeError(w, http.StatusServiceUnavailable, "media engine unavailable"); return }
	items := s.deps.Media.Statuses()
	writeJSON(w, http.StatusOK, map[string]any{"items":items, "count":len(items)})
}

func (s *Server) handleBrokerStats(w http.ResponseWriter, _ *http.Request) {
	if s.deps.Media == nil { writeError(w, http.StatusServiceUnavailable, "media engine unavailable"); return }
	writeJSON(w, http.StatusOK, s.deps.Media.BrokerStats())
}

func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	if s.deps.Media == nil { writeError(w, http.StatusServiceUnavailable, "media engine unavailable"); return }
	if _, err := s.deps.Cameras.Get(r.PathValue("id")); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "camera not found")
		return
	}
	from, err := parseTimeQuery(r, "from")
	if err != nil { writeError(w, http.StatusBadRequest, err.Error()); return }
	to, err := parseTimeQuery(r, "to")
	if err != nil { writeError(w, http.StatusBadRequest, err.Error()); return }
	limit := 500
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 5000 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 5000")
			return
		}
	}
	items, err := s.deps.Media.Timeline(r.PathValue("id"), from, to, limit)
	if err != nil { writeError(w, http.StatusInternalServerError, err.Error()); return }
	writeJSON(w, http.StatusOK, map[string]any{"items":items, "count":len(items)})
}

func (s *Server) handlePreEventSegments(w http.ResponseWriter, r *http.Request) {
	if s.deps.Media == nil { writeError(w, http.StatusServiceUnavailable, "media engine unavailable"); return }
	if _, err := s.deps.Cameras.Get(r.PathValue("id")); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "camera not found")
		return
	}
	items, err := s.deps.Media.RecentSegments(r.PathValue("id"))
	if err != nil { writeError(w, http.StatusInternalServerError, err.Error()); return }
	writeJSON(w, http.StatusOK, map[string]any{
		"items":items, "count":len(items),
		"window_seconds":int(s.deps.Config.PreEventWindow.Seconds()),
	})
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	camera, err := s.deps.Cameras.Get(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) { writeError(w, http.StatusNotFound, "camera not found"); return }
	if err != nil { writeError(w, http.StatusInternalServerError, err.Error()); return }
	if camera.SnapshotURLCipher == "" {
		writeError(w, http.StatusConflict, "camera has no snapshot_url configured")
		return
	}
	raw, err := s.deps.SecretBox.Decrypt(camera.SnapshotURLCipher)
	if err != nil { writeError(w, http.StatusInternalServerError, "unable to decrypt snapshot URL"); return }
	ctx, cancel := context.WithTimeout(r.Context(), s.deps.Config.SnapshotTimeout)
	defer cancel()
	body, contentType, err := media.FetchSnapshot(ctx, raw, s.deps.Config.SnapshotTimeout)
	if err != nil { writeError(w, http.StatusBadGateway, err.Error()); return }
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *Server) handleRetentionRun(w http.ResponseWriter, _ *http.Request) {
	if s.deps.Media == nil { writeError(w, http.StatusServiceUnavailable, "media engine unavailable"); return }
	report, err := s.deps.Media.RunRetentionNow()
	if err != nil { writeError(w, http.StatusInternalServerError, err.Error()); return }
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleProtectSegment(w http.ResponseWriter, r *http.Request) {
	if s.deps.Media == nil { writeError(w, http.StatusServiceUnavailable, "media engine unavailable"); return }
	var in struct {
		Path string `json:"path"`
		Protect bool `json:"protect"`
	}
	if err := decodeJSON(r, &in); err != nil { writeError(w, http.StatusBadRequest, err.Error()); return }
	if strings.TrimSpace(in.Path) == "" { writeError(w, http.StatusBadRequest, "path is required"); return }
	if err := s.deps.Media.Protect(in.Path, in.Protect); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path":in.Path, "protected":in.Protect})
}

func parseTimeQuery(r *http.Request, key string) (time.Time, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" { return time.Time{}, nil }
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil { return time.Time{}, fmt.Errorf("%s must use RFC3339", key) }
	return value.UTC(), nil
}

func (s *Server) protectRTSP(raw string) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" { return "", "", fmt.Errorf("invalid RTSP URL") }
	if u.Scheme != "rtsp" && u.Scheme != "rtsps" { return "", "", fmt.Errorf("URL scheme must be rtsp or rtsps") }
	cipher, err := s.deps.SecretBox.Encrypt(u.String())
	if err != nil { return "", "", err }
	u.User = nil
	return cipher, u.String(), nil
}

func (s *Server) protectSnapshotURL(raw string) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" { return "", "", fmt.Errorf("invalid snapshot URL") }
	if u.Scheme != "http" && u.Scheme != "https" { return "", "", fmt.Errorf("snapshot URL scheme must be http or https") }
	cipher, err := s.deps.SecretBox.Encrypt(u.String())
	if err != nil { return "", "", err }
	u.User = nil
	return cipher, u.String(), nil
}

func validateCameraInput(in cameraInput, requireRTSP bool) error {
	if strings.TrimSpace(in.Name) == "" { return errors.New("name is required") }
	if requireRTSP && strings.TrimSpace(in.RTSPURL) == "" { return errors.New("rtsp_url is required") }
	if in.Latitude != nil && (*in.Latitude < -90 || *in.Latitude > 90) { return errors.New("latitude must be between -90 and 90") }
	if in.Longitude != nil && (*in.Longitude < -180 || *in.Longitude > 180) { return errors.New("longitude must be between -180 and 180") }
	return nil
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil { return fmt.Errorf("invalid JSON: %w", err) }
	if err := dec.Decode(&struct{}{}); err != io.EOF { return errors.New("request must contain one JSON object") }
	return nil
}

func writableDir(dir string) error {
	probe := filepath.Join(dir, ".nvr-write-test")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil { return err }
	return os.Remove(probe)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data: blob:; media-src 'self' blob:; worker-src 'self' blob:; style-src 'self'; script-src 'self'")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { next.ServeHTTP(w, r) })
}
