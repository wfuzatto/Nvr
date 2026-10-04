package media

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func ListSegments(root, cameraID string, from, to time.Time, limit int) ([]Segment, error) {
	if limit <= 0 || limit > 5000 { limit = 500 }
	cameraRoot := filepath.Join(root, cameraID)
	var segments []Segment

	err := filepath.WalkDir(cameraRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) { return nil }
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "index.jsonl" { return nil }

		f, err := os.Open(path)
		if err != nil { return err }
		defer f.Close()

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 4096), 256*1024)
		for scanner.Scan() {
			var segment Segment
			if json.Unmarshal(scanner.Bytes(), &segment) != nil { continue }
			if !from.IsZero() && segment.End.Before(from) { continue }
			if !to.IsZero() && segment.Start.After(to) { continue }
			absolute := filepath.Join(root, filepath.FromSlash(segment.Path))
			if _, err := os.Stat(absolute); err != nil { continue }
			if _, err := os.Stat(absolute + ".protected"); err == nil { segment.Protected = true }
			segments = append(segments, segment)
		}
		return scanner.Err()
	})
	if err != nil && !os.IsNotExist(err) { return nil, err }

	sort.Slice(segments, func(i, j int) bool { return segments[i].Start.Before(segments[j].Start) })
	if len(segments) > limit { segments = segments[len(segments)-limit:] }
	return segments, nil
}

func ProtectSegment(root, relativePath string, protect bool) error {
	clean := filepath.Clean(filepath.FromSlash(relativePath))
	if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return os.ErrPermission
	}
	ext := strings.ToLower(filepath.Ext(clean))
	if ext != ".h264" && ext != ".h265" {
		return os.ErrPermission
	}
	absolute := filepath.Join(root, clean)
	if _, err := os.Stat(absolute); err != nil { return err }
	marker := absolute + ".protected"
	if protect {
		return os.WriteFile(marker, []byte(time.Now().UTC().Format(time.RFC3339Nano)+"\n"), 0o640)
	}
	err := os.Remove(marker)
	if os.IsNotExist(err) { return nil }
	return err
}
