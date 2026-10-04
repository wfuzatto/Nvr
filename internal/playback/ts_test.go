package playback

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

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

	framesPath:=videoPath+".frames.idx"
	ff,err:=os.Create(framesPath)
	if err!=nil { t.Fatal(err) }
	header:=make([]byte,media.FrameIndexHeaderSize)
	copy(header[:4],[]byte("NVFI"))
	header[4]=1
	header[5]=media.FrameIndexRecordSize
	if _,err:=ff.Write(header); err!=nil { t.Fatal(err) }
	writeRecord:=func(offset int64,length int,timestamp uint32,keyframe bool) {
		record:=make([]byte,media.FrameIndexRecordSize)
		binary.BigEndian.PutUint64(record[0:8],uint64(offset))
		binary.BigEndian.PutUint32(record[8:12],uint32(length))
		binary.BigEndian.PutUint32(record[12:16],timestamp)
		if keyframe { record[16]=1 }
		if _,err:=ff.Write(record); err!=nil { t.Fatal(err) }
	}
	writeRecord(int64(len(bootstrap)),len(frame1),1000,true)
	writeRecord(int64(len(bootstrap)+len(frame1)),len(frame2),4600,false)
	_ = ff.Close()

	segment:=media.Segment{CameraID:"cam-1",Codec:"H264",ClockRate:90000,Path:"cam-1/2026-10-04/test.h264",FramesPath:"cam-1/2026-10-04/test.h264.frames.idx"}
	var out bytes.Buffer
	if err:=MuxSegmentTS(root,segment,&out); err!=nil { t.Fatal(err) }
	if out.Len()==0 || out.Len()%188!=0 { t.Fatalf("invalid TS length %d",out.Len()) }
	data:=out.Bytes()
	for i:=0;i<len(data);i+=188 {
		if data[i]!=0x47 { t.Fatalf("missing sync byte at packet %d",i/188) }
	}
}
