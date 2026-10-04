package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/wfuzatto/Nvr/internal/model"
)

func TestFileCameraStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cameras.json")
	s, err := OpenFileCameraStore(path)
	if err != nil { t.Fatal(err) }

	c := model.Camera{ID: "cam-1", Name: "Teste", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.Put(c); err != nil { t.Fatal(err) }

	reopened, err := OpenFileCameraStore(path)
	if err != nil { t.Fatal(err) }

	got, err := reopened.Get("cam-1")
	if err != nil { t.Fatal(err) }
	if got.Name != c.Name { t.Fatalf("got %q want %q", got.Name, c.Name) }

	if err := reopened.Delete("cam-1"); err != nil { t.Fatal(err) }
	if _, err := reopened.Get("cam-1"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
