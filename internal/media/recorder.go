package media

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wfuzatto/Nvr/internal/rtsp"
)

type Segment struct {
	ID         string    `json:"id"`
	CameraID   string    `json:"camera_id"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	Codec      string    `json:"codec"`
	Path       string    `json:"path"`
	Bytes      int64     `json:"bytes"`
	Keyframes  int       `json:"keyframes"`
	SHA256     string    `json:"sha256"`
	Protected  bool      `json:"protected,omitempty"`
}

type Recorder struct {
	root            string
	cameraID        string
	codec           string
	segmentDuration time.Duration
	bootstrap       [][]byte

	file       *os.File
	hasher     hash.Hash
	tmpPath    string
	finalPath  string
	current    Segment
}

func NewRecorder(root, cameraID, codec string, segmentDuration time.Duration, bootstrap [][]byte) (*Recorder, error) {
	if cameraID == "" { return nil, fmt.Errorf("camera ID is required") }
	if strings.ContainsAny(cameraID, "/\\") { return nil, fmt.Errorf("invalid camera ID") }
	codec = strings.ToUpper(codec)
	if codec == "HEVC" { codec = "H265" }
	if codec != "H264" && codec != "H265" { return nil, fmt.Errorf("unsupported codec %s", codec) }
	if segmentDuration <= 0 { return nil, fmt.Errorf("segment duration must be positive") }

	copied := make([][]byte, 0, len(bootstrap))
	for _, nal := range bootstrap { copied = append(copied, append([]byte(nil), nal...)) }
	return &Recorder{
		root: root, cameraID: cameraID, codec: codec,
		segmentDuration: segmentDuration, bootstrap: copied,
	}, nil
}

func (r *Recorder) Write(au rtsp.AccessUnit) (*Segment, error) {
	when := au.ReceivedAt
	if when.IsZero() { when = time.Now().UTC() }

	var completed *Segment
	if r.file != nil && when.Sub(r.current.Start) >= r.segmentDuration && au.Keyframe {
		var err error
		completed, err = r.closeCurrent(when)
		if err != nil { return nil, err }
	}

	if r.file == nil {
		if !au.Keyframe { return completed, nil }
		if err := r.openSegment(when); err != nil { return nil, err }
	}

	if au.Keyframe { r.current.Keyframes++ }
	n, err := r.writeBytes(au.Data)
	if err != nil { return completed, err }
	r.current.Bytes += int64(n)
	r.current.End = when
	return completed, nil
}

func (r *Recorder) Close() (*Segment, error) {
	if r.file == nil { return nil, nil }
	end := r.current.End
	if end.IsZero() { end = time.Now().UTC() }
	return r.closeCurrent(end)
}

func (r *Recorder) openSegment(start time.Time) error {
	dateDir := start.UTC().Format("2006-01-02")
	dir := filepath.Join(r.root, r.cameraID, dateDir)
	if err := os.MkdirAll(dir, 0o750); err != nil { return err }

	ext := ".h264"
	if r.codec == "H265" { ext = ".h265" }
	name := fmt.Sprintf("%s_%d%s", start.UTC().Format("15-04-05.000"), start.UnixNano(), ext)
	finalPath := filepath.Join(dir, name)
	tmpPath := finalPath + ".partial"

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil { return err }
	r.file = f
	r.hasher = sha256.New()
	r.tmpPath = tmpPath
	r.finalPath = finalPath
	relative, _ := filepath.Rel(r.root, finalPath)
	r.current = Segment{
		ID: fmt.Sprintf("%s-%d", r.cameraID, start.UnixNano()),
		CameraID: r.cameraID, Start: start.UTC(), End: start.UTC(),
		Codec: r.codec, Path: filepath.ToSlash(relative),
	}

	for _, nal := range r.bootstrap {
		if len(nal) == 0 { continue }
		if _, err := r.writeBytes([]byte{0,0,0,1}); err != nil { _ = r.abortCurrent(); return err }
		r.current.Bytes += 4
		n, err := r.writeBytes(nal)
		if err != nil { _ = r.abortCurrent(); return err }
		r.current.Bytes += int64(n)
	}
	return nil
}

func (r *Recorder) closeCurrent(end time.Time) (*Segment, error) {
	if r.file == nil { return nil, nil }
	r.current.End = end.UTC()
	if err := r.file.Sync(); err != nil { _ = r.abortCurrent(); return nil, err }
	if err := r.file.Close(); err != nil {
		r.file = nil
		return nil, err
	}
	r.file = nil
	r.current.SHA256 = hex.EncodeToString(r.hasher.Sum(nil))
	if err := os.Rename(r.tmpPath, r.finalPath); err != nil { return nil, err }
	if err := appendIndex(r.root, r.current); err != nil { return nil, err }

	completed := r.current
	r.current = Segment{}
	r.tmpPath, r.finalPath = "", ""
	r.hasher = nil
	return &completed, nil
}

func (r *Recorder) writeBytes(payload []byte) (int, error) {
	if len(payload) == 0 { return 0, nil }
	n, err := r.file.Write(payload)
	if n > 0 { _, _ = r.hasher.Write(payload[:n]) }
	return n, err
}

func (r *Recorder) abortCurrent() error {
	var first error
	if r.file != nil {
		if err := r.file.Close(); err != nil { first = err }
	}
	r.file = nil
	if r.tmpPath != "" {
		if err := os.Remove(r.tmpPath); err != nil && !os.IsNotExist(err) && first == nil { first = err }
	}
	r.current = Segment{}
	r.tmpPath, r.finalPath = "", ""
	r.hasher = nil
	return first
}

func appendIndex(root string, segment Segment) error {
	absolute := filepath.Join(root, filepath.FromSlash(segment.Path))
	dir := filepath.Dir(absolute)
	indexPath := filepath.Join(dir, "index.jsonl")
	f, err := os.OpenFile(indexPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil { return err }
	defer f.Close()

	payload, err := json.Marshal(segment)
	if err != nil { return err }
	if _, err := f.Write(append(payload, '\n')); err != nil { return err }
	return f.Sync()
}
