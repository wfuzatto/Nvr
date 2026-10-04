package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wfuzatto/Nvr/internal/config"
	"github.com/wfuzatto/Nvr/internal/model"
	"github.com/wfuzatto/Nvr/internal/security"
	"github.com/wfuzatto/Nvr/internal/store"
	"github.com/wfuzatto/Nvr/internal/webui"
)

type Dependencies struct {
	Config config.Config
	Version string
	AdminToken string
	SecretBox *security.SecretBox
	Cameras store.CameraStore
}

type Server struct {
	deps Dependencies
	mux *http.ServeMux
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
}

func New(deps Dependencies) *Server {
	s := &Server{deps: deps, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return securityHeaders(s.requestLog(s.mux))
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	s.mux.Handle("GET /api/v1/system/readiness", s.auth(http.HandlerFunc(s.handleReadiness)))
	s.mux.Handle("GET /api/v1/cameras", s.auth(http.HandlerFunc(s.handleListCameras)))
	s.mux.Handle("POST /api/v1/cameras", s.auth(http.HandlerFunc(s.handleCreateCamera)))
	s.mux.Handle("GET /api/v1/cameras/{id}", s.auth(http.HandlerFunc(s.handleGetCamera)))
	s.mux.Handle("PUT /api/v1/cameras/{id}", s.auth(http.HandlerFunc(s.handleUpdateCamera)))
	s.mux.Handle("DELETE /api/v1/cameras/{id}", s.auth(http.HandlerFunc(s.handleDeleteCamera)))
	s.mux.Handle("POST /api/v1/cameras/{id}/test", s.auth(http.HandlerFunc(s.handleTestCamera)))
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
	enabled := true
	if in.Enabled != nil { enabled = *in.Enabled }
	now := time.Now().UTC()
	c := model.Camera{
		ID: model.NewID(), Name: strings.TrimSpace(in.Name), Description: strings.TrimSpace(in.Description),
		City: strings.TrimSpace(in.City), Site: strings.TrimSpace(in.Site),
		Latitude: in.Latitude, Longitude: in.Longitude, Enabled: enabled,
		RTSPURLCipher: cipher, RTSPURLRedacted: redacted, CreatedAt: now, UpdatedAt: now,
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
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" { writeError(w, http.StatusBadRequest, "invalid stored RTSP URL"); return }
	port := u.Port()
	if port == "" { port = "554" }
	host := net.JoinHostPort(u.Hostname(), port)
	started := time.Now()
	conn, err := net.DialTimeout("tcp", host, 3*time.Second)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"reachable": false, "target": camera.RTSPURLRedacted,
			"error": err.Error(), "latency_ms": time.Since(started).Milliseconds(),
		})
		return
	}
	_ = conn.Close()
	writeJSON(w, http.StatusOK, map[string]any{
		"reachable": true, "target": camera.RTSPURLRedacted,
		"latency_ms": time.Since(started).Milliseconds(),
		"note": "TCP connectivity only; RTSP stream validation belongs to the media engine.",
	})
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

func validateCameraInput(in cameraInput, requireRTSP bool) error {
	if strings.TrimSpace(in.Name) == "" { return errors.New("name is required") }
	if requireRTSP && strings.TrimSpace(in.RTSPURL) == "" { return errors.New("rtsp_url is required") }
	if in.Latitude != nil && (*in.Latitude < -90 || *in.Latitude > 90) { return errors.New("latitude must be between -90 and 90") }
	if in.Longitude != nil && (*in.Longitude < -180 || *in.Longitude > 180) { return errors.New("longitude must be between -180 and 180") }
	return nil
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "invalid or missing administrator token")
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.deps.AdminToken)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "invalid or missing administrator token")
			return
		}
		next.ServeHTTP(w, r)
	})
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
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { next.ServeHTTP(w, r) })
}
