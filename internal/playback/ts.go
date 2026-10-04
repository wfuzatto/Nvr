package playback

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/wfuzatto/Nvr/internal/framebroker"
	"github.com/wfuzatto/Nvr/internal/media"
)

const (
	pidPAT   uint16 = 0x0000
	pidVideo uint16 = 0x0100
	pidPMT   uint16 = 0x1000
)

type tsMuxer struct {
	w io.Writer
	cc map[uint16]byte
	codec string
}

func CanMux(root string, segment media.Segment) bool {
	if segment.Path=="" { return false }
	if segment.FramesPath=="" { segment.FramesPath=segment.Path+".frames.idx" }
	videoPath,err:=safeStoragePath(root,segment.Path)
	if err!=nil { return false }
	framePath,err:=safeStoragePath(root,segment.FramesPath)
	if err!=nil { return false }
	videoInfo,err:=os.Stat(videoPath)
	if err!=nil || !videoInfo.Mode().IsRegular() || videoInfo.Size()==0 { return false }
	frameInfo,err:=os.Stat(framePath)
	return err==nil && frameInfo.Mode().IsRegular() && frameInfo.Size()>0
}

func MuxSegmentTS(root string, segment media.Segment, w io.Writer) error {
	if segment.Partial { return errors.New("cannot mux active partial segment") }
	if segment.Path=="" { return errors.New("segment path is required") }
	if segment.FramesPath=="" { segment.FramesPath=segment.Path+".frames.idx" }
	if segment.ClockRate<=0 { segment.ClockRate=90000 }

	videoPath,err:=safeStoragePath(root,segment.Path)
	if err!=nil { return err }
	framePath,err:=safeStoragePath(root,segment.FramesPath)
	if err!=nil { return err }

	frames,err:=readFrameIndex(framePath)
	if err!=nil { return err }
	if len(frames)==0 { return errors.New("segment has no frame index") }

	video,err:=os.Open(videoPath)
	if err!=nil { return err }
	defer video.Close()

	bootstrapLen:=frames[0].Offset
	if bootstrapLen<0 || bootstrapLen>16<<20 { return errors.New("invalid bootstrap offset") }
	bootstrap:=make([]byte,bootstrapLen)
	if bootstrapLen>0 {
		if _,err:=io.ReadFull(video,bootstrap); err!=nil { return err }
	}

	mux:=&tsMuxer{w:w,cc:make(map[uint16]byte),codec:strings.ToUpper(segment.Codec)}
	if mux.codec!="H264" && mux.codec!="H265" { return fmt.Errorf("unsupported playback codec %q",segment.Codec) }
	if err:=mux.writeTables(); err!=nil { return err }

	firstTS:=frames[0].Timestamp
	for i,frame:=range frames {
		if frame.Length<=0 || frame.Length>32<<20 { return errors.New("invalid frame length") }
		payload:=make([]byte,frame.Length)
		if _,err:=video.ReadAt(payload,frame.Offset); err!=nil { return err }

		if frame.Keyframe {
			if i>0 {
				if err:=mux.writeTables(); err!=nil { return err }
			}
			if len(bootstrap)>0 {
				combined:=make([]byte,0,len(bootstrap)+len(payload))
				combined=append(combined,bootstrap...)
				combined=append(combined,payload...)
				payload=combined
			}
		}
		delta:=uint32(frame.Timestamp-firstTS)
		pts:=uint64(delta)*90000/uint64(segment.ClockRate)
		if err:=mux.writePES(payload,pts); err!=nil { return err }
	}
	return nil
}

func MuxEncodedFramesTS(frames []framebroker.EncodedFrame, w io.Writer) error {
	if len(frames)==0 { return errors.New("no encoded frames") }
	first:=frames[0]
	codec:=strings.ToUpper(first.Codec)
	if codec=="HEVC" { codec="H265" }
	if codec!="H264" && codec!="H265" { return fmt.Errorf("unsupported live codec %q",first.Codec) }
	clock:=first.ClockRate
	if clock<=0 { clock=90000 }

	mux:=&tsMuxer{w:w,cc:make(map[uint16]byte),codec:codec}
	if err:=mux.writeTables(); err!=nil { return err }

	firstTS:=first.Timestamp
	for i,frame:=range frames {
		if strings.ToUpper(frame.Codec)!=codec && !(codec=="H265" && strings.ToUpper(frame.Codec)=="HEVC") {
			return errors.New("codec changed within live segment")
		}
		payload:=frame.Data
		if len(payload)==0 { continue }
		if frame.Keyframe && len(frame.Bootstrap)>0 {
			extra:=0
			for _,nal:=range frame.Bootstrap { extra+=4+len(nal) }
			combined:=make([]byte,0,extra+len(payload))
			for _,nal:=range frame.Bootstrap {
				if len(nal)==0 { continue }
				combined=append(combined,0,0,0,1)
				combined=append(combined,nal...)
			}
			combined=append(combined,payload...)
			payload=combined
		}
		if frame.Keyframe && i>0 {
			if err:=mux.writeTables(); err!=nil { return err }
		}
		delta:=uint32(frame.Timestamp-firstTS)
		pts:=uint64(delta)*90000/uint64(clock)
		if err:=mux.writePES(payload,pts); err!=nil { return err }
	}
	return nil
}

func readFrameIndex(path string) ([]media.FrameIndexEntry,error) {
	f,err:=os.Open(path)
	if err!=nil { return nil,err }
	defer f.Close()

	header:=make([]byte,media.FrameIndexHeaderSize)
	if _,err:=io.ReadFull(f,header); err!=nil { return nil,err }
	if string(header[:4])!="NVFI" || header[4]!=1 || int(header[5])!=media.FrameIndexRecordSize {
		return nil,errors.New("unsupported frame index format")
	}

	var out []media.FrameIndexEntry
	record:=make([]byte,media.FrameIndexRecordSize)
	for {
		_,err:=io.ReadFull(f,record)
		if err==io.EOF { break }
		if err==io.ErrUnexpectedEOF { return nil,errors.New("truncated frame index") }
		if err!=nil { return nil,err }

		rawOffset:=binary.BigEndian.Uint64(record[0:8])
		if rawOffset>uint64(math.MaxInt64) { return nil,errors.New("frame offset overflow") }
		length:=binary.BigEndian.Uint32(record[8:12])
		if length==0 { return nil,errors.New("invalid frame index length") }
		out=append(out,media.FrameIndexEntry{
			Offset:int64(rawOffset),
			Length:int(length),
			Timestamp:binary.BigEndian.Uint32(record[12:16]),
			Keyframe:record[16]&1!=0,
		})
	}
	return out,nil
}

func safeStoragePath(root,relative string) (string,error) {
	clean:=filepath.Clean(filepath.FromSlash(relative))
	if clean=="." || filepath.IsAbs(clean) || strings.HasPrefix(clean,"..") { return "",os.ErrPermission }
	absolute:=filepath.Join(root,clean)
	rel,err:=filepath.Rel(root,absolute)
	if err!=nil || strings.HasPrefix(rel,"..") { return "",os.ErrPermission }
	return absolute,nil
}

func (m *tsMuxer) writeTables() error {
	pat:=buildPAT()
	pmt:=buildPMT(m.codec)
	if err:=m.writeSection(pidPAT,pat); err!=nil { return err }
	return m.writeSection(pidPMT,pmt)
}

func (m *tsMuxer) writeSection(pid uint16,section []byte) error {
	payload:=append([]byte{0x00},section...)
	if len(payload)>184 { return errors.New("PSI section too large") }
	packet:=make([]byte,188)
	for i:=range packet { packet[i]=0xff }
	packet[0]=0x47
	packet[1]=0x40|byte(pid>>8)&0x1f
	packet[2]=byte(pid)
	packet[3]=0x10|(m.cc[pid]&0x0f)
	m.cc[pid]=(m.cc[pid]+1)&0x0f
	copy(packet[4:],payload)
	_,err:=m.w.Write(packet)
	return err
}

func (m *tsMuxer) writePES(es []byte,pts uint64) error {
	pes:=make([]byte,14+len(es))
	copy(pes[:4],[]byte{0x00,0x00,0x01,0xe0})
	pes[4],pes[5]=0,0
	pes[6]=0x80
	pes[7]=0x80
	pes[8]=0x05
	writePTS(pes[9:14],pts)
	copy(pes[14:],es)

	first:=true
	for len(pes)>0 {
		packet:=make([]byte,188)
		for i:=range packet { packet[i]=0xff }
		packet[0]=0x47
		packet[1]=byte(pidVideo>>8)&0x1f
		if first { packet[1]|=0x40 }
		packet[2]=byte(pidVideo&0xff)
		cc:=m.cc[pidVideo]&0x0f
		m.cc[pidVideo]=(m.cc[pidVideo]+1)&0x0f

		if first {
			packet[3]=0x30|cc
			adaptLen:=7
			if len(pes)<176 {
				adaptLen=183-len(pes)
				if adaptLen<7 { adaptLen=7 }
			}
			packet[4]=byte(adaptLen)
			packet[5]=0x10
			writePCR(packet[6:12],pts)
			for i:=12;i<5+adaptLen;i++ { packet[i]=0xff }
			start:=5+adaptLen
			capacity:=188-start
			n:=minInt(capacity,len(pes))
			copy(packet[start:start+n],pes[:n])
			pes=pes[n:]
			first=false
			if _,err:=m.w.Write(packet); err!=nil { return err }
			continue
		}

		if len(pes)>=184 {
			packet[3]=0x10|cc
			copy(packet[4:],pes[:184])
			pes=pes[184:]
		} else {
			n:=len(pes)
			adaptLen:=183-n
			packet[3]=0x30|cc
			packet[4]=byte(adaptLen)
			if adaptLen>0 {
				packet[5]=0
				for i:=6;i<5+adaptLen;i++ { packet[i]=0xff }
			}
			start:=5+adaptLen
			copy(packet[start:],pes)
			pes=nil
		}
		if _,err:=m.w.Write(packet); err!=nil { return err }
	}
	return nil
}

func buildPAT() []byte {
	section:=[]byte{
		0x00,0xb0,0x0d,
		0x00,0x01,
		0xc1,0x00,0x00,
		0x00,0x01,
		0xf0,0x00,
	}
	crc:=mpegCRC32(section)
	return append(section,byte(crc>>24),byte(crc>>16),byte(crc>>8),byte(crc))
}

func buildPMT(codec string) []byte {
	streamType:=byte(0x1b)
	if strings.ToUpper(codec)=="H265" { streamType=0x24 }
	section:=[]byte{
		0x02,0xb0,0x12,
		0x00,0x01,
		0xc1,0x00,0x00,
		0xe1,0x00,
		0xf0,0x00,
		streamType,0xe1,0x00,0xf0,0x00,
	}
	crc:=mpegCRC32(section)
	return append(section,byte(crc>>24),byte(crc>>16),byte(crc>>8),byte(crc))
}

func writePTS(dst []byte,pts uint64) {
	pts&=(1<<33)-1
	dst[0]=byte(0x21|((pts>>29)&0x0e))
	dst[1]=byte(pts>>22)
	dst[2]=byte(((pts>>14)&0xfe)|1)
	dst[3]=byte(pts>>7)
	dst[4]=byte((pts<<1)|1)
}

func writePCR(dst []byte,pcr uint64) {
	base:=pcr&((1<<33)-1)
	dst[0]=byte(base>>25)
	dst[1]=byte(base>>17)
	dst[2]=byte(base>>9)
	dst[3]=byte(base>>1)
	dst[4]=byte((base&1)<<7)|0x7e
	dst[5]=0
}

func mpegCRC32(data []byte) uint32 {
	crc:=uint32(0xffffffff)
	for _,b:=range data {
		crc^=uint32(b)<<24
		for i:=0;i<8;i++ {
			if crc&0x80000000!=0 { crc=(crc<<1)^0x04c11db7 } else { crc<<=1 }
		}
	}
	return crc
}

func minInt(a,b int) int { if a<b { return a }; return b }
