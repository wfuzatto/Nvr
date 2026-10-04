package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wfuzatto/Nvr/internal/audit"
	"github.com/wfuzatto/Nvr/internal/config"
	"github.com/wfuzatto/Nvr/internal/evidence"
	"github.com/wfuzatto/Nvr/internal/framebroker"
	"github.com/wfuzatto/Nvr/internal/httpapi"
	"github.com/wfuzatto/Nvr/internal/live"
	"github.com/wfuzatto/Nvr/internal/media"
	"github.com/wfuzatto/Nvr/internal/security"
	"github.com/wfuzatto/Nvr/internal/store"
	"github.com/wfuzatto/Nvr/internal/webrtclive"
)

const version = "0.4.0-dev"

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	box, err := security.LoadOrCreateSecretBox(cfg.MasterKeyFile)
	if err != nil {
		log.Fatalf("master key: %v", err)
	}

	adminToken, created, err := security.LoadOrCreateAdminToken(cfg.AdminTokenFile)
	if err != nil {
		log.Fatalf("admin token: %v", err)
	}
	if created {
		log.Printf("IMPORTANT: first-run administrator token: %s", adminToken)
		log.Printf("The token is also stored at %s with restrictive permissions.", cfg.AdminTokenFile)
	}

	cameraStore, err := store.OpenFileCameraStore(cfg.CameraDBFile)
	if err != nil {
		log.Fatalf("camera store: %v", err)
	}
	authManager, err := security.OpenAuthManager(cfg.UsersFile, adminToken)
	if err != nil {
		log.Fatalf("auth manager: %v", err)
	}
	auditLog, err := audit.Open(cfg.AuditFile)
	if err != nil {
		log.Fatalf("audit log: %v", err)
	}
	evidenceManager, err := evidence.NewManager(cfg.StorageDir, cfg.ExportsDir, cameraStore)
	if err != nil {
		log.Fatalf("evidence manager: %v", err)
	}

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	broker := framebroker.New()
	mediaManager := media.NewManager(cfg, cameraStore, box, broker)
	mediaManager.Start(appCtx)
	liveManager := live.NewManager(appCtx, broker)
	var webRTCManager *webrtclive.Manager
	if cfg.WebRTCEnabled {
		webRTCManager, err = webrtclive.New(appCtx, broker, webrtclive.Config{
			Enabled: true,
			UDPPort: uint16(cfg.WebRTCUDPPort),
			PublicIP: cfg.WebRTCPublicIP,
		})
		if err != nil {
			log.Printf("WebRTC unavailable: %v; live HLS fallback remains enabled", err)
			webRTCManager = nil
		}
	}

	api := httpapi.New(httpapi.Dependencies{
		Config: cfg, Version: version, AdminToken: adminToken,
		SecretBox: box, Auth: authManager, Audit: auditLog, Evidence: evidenceManager, Cameras: cameraStore, Media: mediaManager, Live: liveManager, WebRTC: webRTCManager,
	})

	server := &http.Server{
		Addr: cfg.ListenAddress, Handler: api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout: 90 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("NVR %s listening on http://%s", version, cfg.ListenAddress)
		log.Printf("media engine enabled: segment=%s retention=%dd max_bytes=%d", cfg.SegmentDuration, cfg.RetentionDays, cfg.StorageMaxBytes)
		if webRTCManager != nil {
			log.Printf("WebRTC enabled: UDP %d (ICE mux) public_ip_configured=%t", cfg.WebRTCUDPPort, cfg.WebRTCPublicIP != "")
		} else {
			log.Printf("WebRTC disabled/unavailable; live HLS remains available")
		}
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		log.Printf("received signal %s; shutting down", sig)
	case err := <-errCh:
		log.Printf("server error: %v", err)
	}

	appCancel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
