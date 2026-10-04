package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/wfuzatto/Nvr/internal/model"
)

var ErrNotFound = errors.New("not found")

type CameraStore interface {
	List() []model.Camera
	Get(id string) (model.Camera, error)
	Put(camera model.Camera) error
	Delete(id string) error
}

type FileCameraStore struct {
	mu sync.RWMutex
	path string
	data map[string]model.Camera
}

type diskCameraStore struct {
	Version int `json:"version"`
	Cameras []model.Camera `json:"cameras"`
}

func OpenFileCameraStore(path string) (*FileCameraStore, error) {
	s := &FileCameraStore{path: path, data: make(map[string]model.Camera)}
	if err := s.load(); err != nil { return nil, err }
	return s, nil
}

func (s *FileCameraStore) List() []model.Camera {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Camera, 0, len(s.data))
	for _, c := range s.data { out = append(out, c) }
	sort.Slice(out, func(i, j int) bool {
		if out[i].City != out[j].City { return out[i].City < out[j].City }
		return out[i].Name < out[j].Name
	})
	return out
}

func (s *FileCameraStore) Get(id string) (model.Camera, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.data[id]
	if !ok { return model.Camera{}, ErrNotFound }
	return c, nil
}

func (s *FileCameraStore) Put(camera model.Camera) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[camera.ID] = camera
	return s.persistLocked()
}

func (s *FileCameraStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[id]; !ok { return ErrNotFound }
	delete(s.data, id)
	return s.persistLocked()
}

func (s *FileCameraStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	content, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) { return nil }
	if err != nil { return err }
	if len(content) == 0 { return nil }
	var disk diskCameraStore
	if err := json.Unmarshal(content, &disk); err != nil {
		return fmt.Errorf("decode camera store: %w", err)
	}
	for _, c := range disk.Cameras { s.data[c.ID] = c }
	return nil
}

func (s *FileCameraStore) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil { return err }
	disk := diskCameraStore{Version: 1, Cameras: make([]model.Camera, 0, len(s.data))}
	for _, c := range s.data { disk.Cameras = append(disk.Cameras, c) }
	sort.Slice(disk.Cameras, func(i, j int) bool { return disk.Cameras[i].ID < disk.Cameras[j].ID })
	payload, err := json.MarshalIndent(disk, "", "  ")
	if err != nil { return err }
	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil { return err }
	if _, err = f.Write(append(payload, '\n')); err != nil { _ = f.Close(); return err }
	if err = f.Sync(); err != nil { _ = f.Close(); return err }
	if err = f.Close(); err != nil { return err }
	if err = os.Rename(tmp, s.path); err != nil { return err }
	return os.Chmod(s.path, 0o600)
}
