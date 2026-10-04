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

	"github.com/wfuzatto/Nvr/internal/config"
	"github.com/wfuzatto/Nvr/internal/framebroker"
	"github.com/wfuzatto/Nvr/internal/httpapi"
	"github.com/wfuzatto/Nvr/internal/live"
	"github.com/wfuzatto/Nvr/internal/media"
	"github.com/wfuzatto/Nvr/internal/security"
	"github.com/wfuzatto/Nvr/internal/store"
	"github.com/wfuzatto/Nvr/internal/webrtclive"
)

const version = "0.5.0-dev"

func main() {
	cfg, err := config.Load()
	if err != nil { log.Fatalf("config: %v", err) }

	box, err := security.LoadOrCreateSecretBox(cfg.MasterKeyFile)
	if err != nil { log.Fatalf("master key: %v", err) }

	adminToken, created, err := security.LoadOrCreateAdminToken(cfg.AdminTokenFile)
	if err != nil { log.Fatalf("admin token: %v", err) }
	if created {
		log.Printf("IMPORTANT: first-run administrator token: %s", adminToken)
		log.Printf("The token is also stored at %s with restrictive permissions.", cfg.AdminTokenFile)
	}

	pluginToken, pluginCreated, err := security.LoadOrCreateToken(cfg.PluginTokenFile)
	if err != nil { log.Fatalf("plugin token: %v", err) }
	if pluginCreated { log.Printf("plugin runtime token created at %s", cfg.PluginTokenFile) }

	cameraStore, err := store.OpenFileCameraStore(cfg.CameraDBFile)
	if err != nil { log.Fatalf("camera store: %v", err) }
	eventStore, err := store.OpenFileEventStore(cfg.EventDBFile)
	if err != nil { log.Fatalf("event store: %v", err) }

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	broker := framebroker.New()
	mediaManager := media.NewManager(cfg, cameraStore, box, broker)
	mediaManager.Start(appCtx)
	liveManager := live.NewManager(appCtx, broker)
	var webRTCManager *webrtclive.Manager
	if cfg.WebRTCEnabled {
		webRTCManager, err = webrtclive.New(appCtx, broker, webrtclive.Config{
			Enabled: true, UDPPort: uint16(cfg.WebRTCUDPPort), PublicIP: cfg.WebRTCPublicIP,
		})
		if err != nil {
			log.Printf("WebRTC unavailable: %v; live HLS fallback remains enabled", err)
			webRTCManager = nil
		}
	}

	api := httpapi.New(httpapi.Dependencies{
		Config: cfg, Version: version, AdminToken: adminToken,
		SecretBox: box, Cameras: cameraStore, Media: mediaManager, Live: liveManager, WebRTC: webRTCManager,
	})
	httpapi.AttachPluginRoutes(api,httpapi.PluginDependencies{
		Token:pluginToken,Events:eventStore,EvidenceDir:cfg.EvidenceDir,
	})

	server := &http.Server{
		Addr: cfg.ListenAddress, Handler: api.Handler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 90 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("NVR %s listening on http://%s", version, cfg.ListenAddress)
		log.Printf("media engine enabled: segment=%s retention=%dd max_bytes=%d", cfg.SegmentDuration, cfg.RetentionDays, cfg.StorageMaxBytes)
		log.Printf("plugin runtime enabled: event_store=%s evidence_dir=%s",cfg.EventDBFile,cfg.EvidenceDir)
		if webRTCManager != nil {
			log.Printf("WebRTC enabled: UDP %d (ICE mux) public_ip_configured=%t", cfg.WebRTCUDPPort, cfg.WebRTCPublicIP != "")
		} else { log.Printf("WebRTC disabled/unavailable; live HLS remains available") }
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) { errCh <- err }
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	select {
	case sig := <-sigCh: log.Printf("received signal %s; shutting down", sig)
	case err := <-errCh: log.Printf("server error: %v", err)
	}

	appCancel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil { log.Printf("shutdown: %v", err) }
}
