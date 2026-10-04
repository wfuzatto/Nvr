package media

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wfuzatto/Nvr/internal/rtsp"
)

func TestRecorderRotatesOnKeyframe(t *testing.T) {
	root := t.TempDir()
	r, err := NewRecorder(root, "cam-1", "H264", 90000, 5*time.Second, [][]byte{{0x67,1},{0x68,2}})
	if err != nil { t.Fatal(err) }

	start := time.Now().UTC()
	_, err = r.Write(rtsp.AccessUnit{Codec:"H264", Keyframe:true, ReceivedAt:start, Data:[]byte{0,0,0,1,0x65,1}})
	if err != nil { t.Fatal(err) }
	_, err = r.Write(rtsp.AccessUnit{Codec:"H264", ReceivedAt:start.Add(2*time.Second), Data:[]byte{0,0,0,1,0x41,2}})
	if err != nil { t.Fatal(err) }
	completed, err := r.Write(rtsp.AccessUnit{Codec:"H264", Keyframe:true, ReceivedAt:start.Add(6*time.Second), Data:[]byte{0,0,0,1,0x65,3}})
	if err != nil { t.Fatal(err) }
	if completed == nil { t.Fatal("expected completed segment") }
	if completed.Bytes == 0 || completed.SHA256 == "" || completed.FramesPath == "" { t.Fatalf("invalid segment: %+v", completed) }
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(completed.Path))); err != nil { t.Fatal(err) }
	frameInfo, err := os.Stat(filepath.Join(root, filepath.FromSlash(completed.FramesPath)))
	if err != nil { t.Fatal(err) }
	wantIndexBytes := int64(FrameIndexHeaderSize + 2*FrameIndexRecordSize)
	if frameInfo.Size() != wantIndexBytes {
		t.Fatalf("frame index size=%d want=%d", frameInfo.Size(), wantIndexBytes)
	}

	last, err := r.Close()
	if err != nil { t.Fatal(err) }
	if last == nil { t.Fatal("expected final segment") }

	items, err := ListSegments(root, "cam-1", time.Time{}, time.Time{}, 10)
	if err != nil { t.Fatal(err) }
	if len(items) != 2 { t.Fatalf("segments=%d", len(items)) }
}
