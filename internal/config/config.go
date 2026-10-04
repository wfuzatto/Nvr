package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type Config struct {
	ListenAddress      string
	DataDir            string
	RuntimeDir         string
	StorageDir         string
	CameraDBFile       string
	EventDBFile        string
	HotlistFile        string
	EvidenceDir        string
	MasterKeyFile      string
	AdminTokenFile     string
	PluginTokenFile    string
	UsersFile          string
	AuditFile          string
	ExportsDir         string
	SegmentDuration    time.Duration
	RetentionDays      int
	StorageMaxBytes    int64
	SupervisorInterval time.Duration
	RetentionInterval  time.Duration
	RTSPReadTimeout    time.Duration
	SnapshotTimeout    time.Duration
	PreEventWindow     time.Duration
	WebRTCEnabled      bool
	WebRTCUDPPort      int
	WebRTCPublicIP     string
}

func Load() (Config, error) {
	dataDir := env("NVR_DATA_DIR", "./data")
	runtimeDir := env("NVR_RUNTIME_DIR", "./runtime")
	storageDir := env("NVR_STORAGE_DIR", filepath.Join(dataDir, "recordings"))

	segmentDuration, err := envDurationSeconds("NVR_SEGMENT_SECONDS", 60)
	if err != nil { return Config{}, err }
	supervisorInterval, err := envDurationSeconds("NVR_SUPERVISOR_SECONDS", 5)
	if err != nil { return Config{}, err }
	retentionInterval, err := envDurationSeconds("NVR_RETENTION_INTERVAL_SECONDS", 300)
	if err != nil { return Config{}, err }
	rtspReadTimeout, err := envDurationSeconds("NVR_RTSP_READ_TIMEOUT_SECONDS", 15)
	if err != nil { return Config{}, err }
	snapshotTimeout, err := envDurationSeconds("NVR_SNAPSHOT_TIMEOUT_SECONDS", 5)
	if err != nil { return Config{}, err }
	preEventWindow, err := envDurationSeconds("NVR_PRE_EVENT_SECONDS", 30)
	if err != nil { return Config{}, err }
	retentionDays, err := envInt("NVR_RETENTION_DAYS", 7)
	if err != nil { return Config{}, err }
	storageMaxBytes, err := envInt64("NVR_STORAGE_MAX_BYTES", 0)
	if err != nil { return Config{}, err }
	webRTCEnabled, err := envBool("NVR_WEBRTC_ENABLED", true)
	if err != nil { return Config{}, err }
	webRTCUDPPort, err := envInt("NVR_WEBRTC_UDP_PORT", 50000)
	if err != nil { return Config{}, err }

	if segmentDuration < 5*time.Second { return Config{}, fmt.Errorf("NVR_SEGMENT_SECONDS must be at least 5") }
	if supervisorInterval < time.Second { return Config{}, fmt.Errorf("NVR_SUPERVISOR_SECONDS must be at least 1") }
	if retentionDays < 0 { return Config{}, fmt.Errorf("NVR_RETENTION_DAYS cannot be negative") }
	if storageMaxBytes < 0 { return Config{}, fmt.Errorf("NVR_STORAGE_MAX_BYTES cannot be negative") }
	if webRTCUDPPort < 1024 || webRTCUDPPort > 65535 { return Config{}, fmt.Errorf("NVR_WEBRTC_UDP_PORT must be between 1024 and 65535") }

	cfg := Config{
		ListenAddress:      env("NVR_LISTEN", "0.0.0.0:8080"),
		DataDir:            dataDir,
		RuntimeDir:         runtimeDir,
		StorageDir:         storageDir,
		CameraDBFile:       filepath.Join(dataDir, "cameras.json"),
		EventDBFile:        filepath.Join(dataDir, "events", "events.jsonl"),
		HotlistFile:        filepath.Join(dataDir, "events", "hotlist.json"),
		EvidenceDir:        filepath.Join(storageDir, "analytics", "plate"),
		MasterKeyFile:      filepath.Join(dataDir, "master.key"),
		AdminTokenFile:     filepath.Join(dataDir, "admin.token"),
		PluginTokenFile:    filepath.Join(dataDir, "plugin.token"),
		UsersFile:          filepath.Join(dataDir, "users.json"),
		AuditFile:          filepath.Join(dataDir, "audit.jsonl"),
		ExportsDir:         filepath.Join(dataDir, "exports"),
		SegmentDuration:    segmentDuration,
		RetentionDays:      retentionDays,
		StorageMaxBytes:    storageMaxBytes,
		SupervisorInterval: supervisorInterval,
		RetentionInterval:  retentionInterval,
		RTSPReadTimeout:    rtspReadTimeout,
		SnapshotTimeout:    snapshotTimeout,
		PreEventWindow:     preEventWindow,
		WebRTCEnabled:      webRTCEnabled,
		WebRTCUDPPort:      webRTCUDPPort,
		WebRTCPublicIP:     env("NVR_WEBRTC_PUBLIC_IP", ""),
	}
	for _, dir := range []string{cfg.DataDir, cfg.RuntimeDir, cfg.StorageDir, cfg.ExportsDir, filepath.Dir(cfg.EventDBFile), cfg.EvidenceDir} {
		if err := os.MkdirAll(dir, 0o750); err != nil { return Config{}, fmt.Errorf("create %s: %w", dir, err) }
	}
	return cfg, nil
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" { return value }
	return fallback
}

func envInt(name string, fallback int) (int, error) {
	raw := os.Getenv(name)
	if raw == "" { return fallback, nil }
	value, err := strconv.Atoi(raw)
	if err != nil { return 0, fmt.Errorf("%s must be an integer: %w", name, err) }
	return value, nil
}

func envInt64(name string, fallback int64) (int64, error) {
	raw := os.Getenv(name)
	if raw == "" { return fallback, nil }
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil { return 0, fmt.Errorf("%s must be an integer: %w", name, err) }
	return value, nil
}

func envDurationSeconds(name string, fallback int) (time.Duration, error) {
	value, err := envInt(name, fallback)
	if err != nil { return 0, err }
	if value <= 0 { return 0, fmt.Errorf("%s must be greater than zero", name) }
	return time.Duration(value) * time.Second, nil
}

func envBool(name string, fallback bool) (bool, error) {
	raw := os.Getenv(name)
	if raw == "" { return fallback, nil }
	value, err := strconv.ParseBool(raw)
	if err != nil { return false, fmt.Errorf("%s must be a boolean: %w", name, err) }
	return value, nil
}
