package media

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func ListSegments(root, cameraID string, from, to time.Time, limit int) ([]Segment, error) {
	if limit <= 0 || limit > 5000 { limit = 500 }
	if cameraID == "" || strings.ContainsAny(cameraID, "/\\") { return nil, os.ErrPermission }

	cameraRoot := filepath.Join(root, cameraID)
	entries, err := os.ReadDir(cameraRoot)
	if os.IsNotExist(err) { return []Segment{}, nil }
	if err != nil { return nil, err }

	minDate, maxDate := "", ""
	if !from.IsZero() { minDate = from.UTC().Format("2006-01-02") }
	if !to.IsZero() { maxDate = to.UTC().Format("2006-01-02") }

	var days []string
	for _, entry := range entries {
		if !entry.IsDir() { continue }
		name := entry.Name()
		if _, err := time.Parse("2006-01-02", name); err != nil { continue }
		if minDate != "" && name < minDate { continue }
		if maxDate != "" && name > maxDate { continue }
		days = append(days, name)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(days)))

	var segments []Segment
	for _, day := range days {
		items, err := readDailyIndex(root, cameraID, day, from, to)
		if err != nil { return nil, err }
		segments = append(segments, items...)
		if len(segments) >= limit { break }
	}

	sort.Slice(segments, func(i, j int) bool { return segments[i].Start.Before(segments[j].Start) })
	if len(segments) > limit { segments = segments[len(segments)-limit:] }
	return segments, nil
}

func readDailyIndex(root, cameraID, day string, from, to time.Time) ([]Segment, error) {
	indexPath := filepath.Join(root, cameraID, day, "index.jsonl")
	f, err := os.Open(indexPath)
	if os.IsNotExist(err) { return nil, nil }
	if err != nil { return nil, err }
	defer f.Close()

	var segments []Segment
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 256*1024)
	for scanner.Scan() {
		var segment Segment
		if json.Unmarshal(scanner.Bytes(), &segment) != nil { continue }
		if segment.CameraID != cameraID { continue }
		if !from.IsZero() && segment.End.Before(from) { continue }
		if !to.IsZero() && segment.Start.After(to) { continue }

		absolute := filepath.Join(root, filepath.FromSlash(segment.Path))
		if _, err := os.Stat(absolute); err != nil { continue }
		if _, err := os.Stat(absolute + ".protected"); err == nil { segment.Protected = true }
		segments = append(segments, segment)
	}
	return segments, scanner.Err()
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
