package media

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRetentionKeepsProtectedSegment(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "cam", "2026-01-01")
	if err := os.MkdirAll(dir, 0o750); err != nil { t.Fatal(err) }

	old := filepath.Join(dir, "old.h264")
	protected := filepath.Join(dir, "protected.h264")
	if err := os.WriteFile(old, make([]byte, 10), 0o640); err != nil { t.Fatal(err) }
	if err := os.WriteFile(protected, make([]byte, 10), 0o640); err != nil { t.Fatal(err) }
	if err := os.WriteFile(protected+".protected", []byte("yes"), 0o640); err != nil { t.Fatal(err) }
	oldTime := time.Now().Add(-72*time.Hour)
	_ = os.Chtimes(old, oldTime, oldTime)
	_ = os.Chtimes(protected, oldTime, oldTime)

	report, err := RunRetention(root, 1, 0)
	if err != nil { t.Fatal(err) }
	if report.DeletedFiles != 1 { t.Fatalf("deleted=%d", report.DeletedFiles) }
	if _, err := os.Stat(protected); err != nil { t.Fatal("protected segment was deleted") }
}
