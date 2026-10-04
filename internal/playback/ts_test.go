package playback

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wfuzatto/Nvr/internal/media"
)

func TestPATPMTCRC(t *testing.T) {
	pat:=buildPAT()
	if got:=mpegCRC32(pat); got!=0 { t.Fatalf("PAT crc remainder=%08x",got) }
	pmt:=buildPMT("H264")
	if got:=mpegCRC32(pmt); got!=0 { t.Fatalf("PMT crc remainder=%08x",got) }
}

func TestPTSMarkers(t *testing.T) {
	buf:=make([]byte,5)
	writePTS(buf,90000)
	if buf[0]&1==0 || buf[2]&1==0 || buf[4]&1==0 { t.Fatalf("missing marker bits: %x",buf) }
}

func TestMuxSegmentTS(t *testing.T) {
	root:=t.TempDir()
	dir:=filepath.Join(root,"cam-1","2026-10-04")
	if err:=os.MkdirAll(dir,0o750); err!=nil { t.Fatal(err) }

	videoPath:=filepath.Join(dir,"test.h264")
	bootstrap:=[]byte{0,0,0,1,0x67,1,2,0,0,0,1,0x68,3,4}
	frame1:=[]byte{0,0,0,1,0x65,5,6,7}
	frame2:=[]byte{0,0,0,1,0x41,8,9}
	all:=append(append(append([]byte{},bootstrap...),frame1...),frame2...)
	if err:=os.WriteFile(videoPath,all,0o640); err!=nil { t.Fatal(err) }

	framesPath:=videoPath+".frames.jsonl"
	ff,err:=os.Create(framesPath)
	if err!=nil { t.Fatal(err) }
	enc:=json.NewEncoder(ff)
	now:=time.Now().UTC()
	_ = enc.Encode(media.FrameIndexEntry{Offset:int64(len(bootstrap)),Length:len(frame1),Timestamp:1000,Keyframe:true,Received:now})
	_ = enc.Encode(media.FrameIndexEntry{Offset:int64(len(bootstrap)+len(frame1)),Length:len(frame2),Timestamp:4600,Received:now.Add(40*time.Millisecond)})
	_ = ff.Close()

	segment:=media.Segment{CameraID:"cam-1",Codec:"H264",ClockRate:90000,Path:"cam-1/2026-10-04/test.h264",FramesPath:"cam-1/2026-10-04/test.h264.frames.jsonl"}
	var out bytes.Buffer
	if err:=MuxSegmentTS(root,segment,&out); err!=nil { t.Fatal(err) }
	if out.Len()==0 || out.Len()%188!=0 { t.Fatalf("invalid TS length %d",out.Len()) }
	data:=out.Bytes()
	for i:=0;i<len(data);i+=188 {
		if data[i]!=0x47 { t.Fatalf("missing sync byte at packet %d",i/188) }
	}
}
