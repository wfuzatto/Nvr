package playback

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wfuzatto/Nvr/internal/framebroker"
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

func TestMuxEncodedFramesTS(t *testing.T) {
	frames:=[]framebroker.EncodedFrame{
		{CameraID:"cam",Codec:"H264",ClockRate:90000,Timestamp:1000,Keyframe:true,Bootstrap:[][]byte{{0x67,1},{0x68,2}},Data:[]byte{0,0,0,1,0x65,3}},
		{CameraID:"cam",Codec:"H264",ClockRate:90000,Timestamp:4600,Data:[]byte{0,0,0,1,0x41,4}},
	}
	var out bytes.Buffer
	if err:=MuxEncodedFramesTS(frames,&out); err!=nil { t.Fatal(err) }
	if out.Len()==0 || out.Len()%188!=0 { t.Fatalf("invalid TS length %d",out.Len()) }
	for i:=0;i<out.Len();i+=188 {
		if out.Bytes()[i]!=0x47 { t.Fatalf("missing sync at packet %d",i/188) }
	}
}

func TestExportRangeTS(t *testing.T) {
	root:=t.TempDir()
	dir:=filepath.Join(root,"cam-1","2026-10-04")
	if err:=os.MkdirAll(dir,0o750); err!=nil { t.Fatal(err) }
	videoPath:=filepath.Join(dir,"range.h264")
	bootstrap:=[]byte{0,0,0,1,0x67,1,2,0,0,0,1,0x68,3,4}
	frames:=[][]byte{
		{0,0,0,1,0x65,1},
		{0,0,0,1,0x41,2},
		{0,0,0,1,0x65,3},
		{0,0,0,1,0x41,4},
	}
	all:=append([]byte{},bootstrap...)
	offsets:=make([]int64,0,len(frames))
	for _,frame:=range frames { offsets=append(offsets,int64(len(all))); all=append(all,frame...) }
	if err:=os.WriteFile(videoPath,all,0o640); err!=nil { t.Fatal(err) }
	idx:=videoPath+".frames.idx"
	ff,err:=os.Create(idx); if err!=nil { t.Fatal(err) }
	header:=make([]byte,media.FrameIndexHeaderSize); copy(header[:4],[]byte("NVFI")); header[4]=1; header[5]=media.FrameIndexRecordSize
	if _,err:=ff.Write(header); err!=nil { t.Fatal(err) }
	for i,frame:=range frames {
		rec:=make([]byte,media.FrameIndexRecordSize)
		binary.BigEndian.PutUint64(rec[0:8],uint64(offsets[i]))
		binary.BigEndian.PutUint32(rec[8:12],uint32(len(frame)))
		binary.BigEndian.PutUint32(rec[12:16],uint32(1000+i*9000))
		if i==0 || i==2 { rec[16]=1 }
		if _,err:=ff.Write(rec); err!=nil { t.Fatal(err) }
	}
	_ = ff.Close()
	start:=time.Date(2026,10,4,12,0,0,0,time.UTC)
	segment:=media.Segment{CameraID:"cam-1",Codec:"H264",ClockRate:90000,Path:"cam-1/2026-10-04/range.h264",FramesPath:"cam-1/2026-10-04/range.h264.frames.idx",Start:start,End:start.Add(400*time.Millisecond)}
	var out bytes.Buffer
	info,err:=ExportRangeTS(root,[]media.Segment{segment},start.Add(50*time.Millisecond),start.Add(350*time.Millisecond),&out)
	if err!=nil { t.Fatal(err) }
	if !info.ActualFrom.Equal(start.Add(200*time.Millisecond)) { t.Fatalf("actual from=%s",info.ActualFrom) }
	if info.Frames!=2 || info.Keyframes!=1 { t.Fatalf("info=%+v",info) }
	if out.Len()==0 || out.Len()%188!=0 { t.Fatalf("invalid TS length=%d",out.Len()) }
}
