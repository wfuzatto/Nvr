package config

import (
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	ListenAddress string
	DataDir string
	RuntimeDir string
	StorageDir string
	CameraDBFile string
	MasterKeyFile string
	AdminTokenFile string
}

func Load() (Config, error) {
	dataDir := env("NVR_DATA_DIR", "./data")
	runtimeDir := env("NVR_RUNTIME_DIR", "./runtime")
	storageDir := env("NVR_STORAGE_DIR", filepath.Join(dataDir, "recordings"))

	cfg := Config{
		ListenAddress: env("NVR_LISTEN", "0.0.0.0:8080"),
		DataDir: dataDir,
		RuntimeDir: runtimeDir,
		StorageDir: storageDir,
		CameraDBFile: filepath.Join(dataDir, "cameras.json"),
		MasterKeyFile: filepath.Join(dataDir, "master.key"),
		AdminTokenFile: filepath.Join(dataDir, "admin.token"),
	}
	for _, dir := range []string{cfg.DataDir, cfg.RuntimeDir, cfg.StorageDir} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return Config{}, fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return cfg, nil
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
