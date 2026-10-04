package rtsp

import (
	"bytes"
	"errors"
	"strings"
	"time"
)

var annexBStartCode = []byte{0, 0, 0, 1}

type AccessUnit struct {
	Codec      string
	Timestamp  uint32
	Keyframe   bool
	ReceivedAt time.Time
	Data       []byte
}

type Depacketizer struct {
	codec      string
	payloadType uint8
	timestamp  uint32
	started     bool
	keyframe    bool
	buf         bytes.Buffer
	sequenceSet bool
	lastSequence uint16
	damaged     bool
	fuOpen      bool
}

func NewDepacketizer(codec string, payloadType uint8) (*Depacketizer, error) {
	codec = strings.ToUpper(codec)
	if codec == "HEVC" { codec = "H265" }
	if codec != "H264" && codec != "H265" {
		return nil, errors.New("unsupported video codec")
	}
	return &Depacketizer{codec: codec, payloadType: payloadType}, nil
}

func (d *Depacketizer) Push(packet RTPPacket) (*AccessUnit, error) {
	if packet.PayloadType != d.payloadType { return nil, nil }
	if len(packet.Payload) == 0 { return nil, nil }

	if !d.started || packet.Timestamp != d.timestamp {
		if d.buf.Len() > 0 {
			d.buf.Reset()
			d.keyframe = false
		}
		d.timestamp = packet.Timestamp
		d.started = true
		d.sequenceSet = false
		d.damaged = false
		d.fuOpen = false
	}
	if d.sequenceSet && packet.Sequence != d.lastSequence+1 {
		d.buf.Reset()
		d.keyframe = false
		d.damaged = true
		d.fuOpen = false
	}
	d.lastSequence = packet.Sequence
	d.sequenceSet = true

	if d.damaged {
		if packet.Marker {
			d.damaged = false
			d.buf.Reset()
			d.keyframe = false
			d.fuOpen = false
		}
		return nil, nil
	}

	var err error
	switch d.codec {
	case "H264":
		err = d.pushH264(packet.Payload)
	case "H265":
		err = d.pushH265(packet.Payload)
	}
	if err != nil {
		d.buf.Reset()
		d.keyframe = false
		d.fuOpen = false
		return nil, err
	}

	if !packet.Marker { return nil, nil }
	if d.buf.Len() == 0 {
		d.keyframe = false
		return nil, nil
	}
	out := AccessUnit{
		Codec: d.codec, Timestamp: d.timestamp, Keyframe: d.keyframe,
		ReceivedAt: time.Now().UTC(), Data: append([]byte(nil), d.buf.Bytes()...),
	}
	d.buf.Reset()
	d.keyframe = false
	d.fuOpen = false
	return &out, nil
}

func (d *Depacketizer) pushH264(payload []byte) error {
	nalType := payload[0] & 0x1f
	switch {
	case nalType >= 1 && nalType <= 23:
		d.appendNAL(payload)
		if nalType == 5 { d.keyframe = true }
		return nil
	case nalType == 24:
		offset := 1
		for offset+2 <= len(payload) {
			size := int(payload[offset])<<8 | int(payload[offset+1])
			offset += 2
			if size <= 0 || offset+size > len(payload) { return errors.New("invalid H264 STAP-A packet") }
			nal := payload[offset : offset+size]
			d.appendNAL(nal)
			if nal[0]&0x1f == 5 { d.keyframe = true }
			offset += size
		}
		if offset != len(payload) { return errors.New("invalid H264 STAP-A tail") }
		return nil
	case nalType == 28:
		if len(payload) < 3 { return errors.New("invalid H264 FU-A packet") }
		fuHeader := payload[1]
		start := fuHeader&0x80 != 0
		end := fuHeader&0x40 != 0
		reconstructedType := fuHeader & 0x1f
		if start {
			d.fuOpen = true
			d.buf.Write(annexBStartCode)
			d.buf.WriteByte((payload[0] & 0xe0) | reconstructedType)
			if reconstructedType == 5 { d.keyframe = true }
		} else if !d.fuOpen {
			return errors.New("H264 FU-A continuation without start")
		}
		d.buf.Write(payload[2:])
		if end { d.fuOpen = false }
		return nil
	default:
		return nil
	}
}

func (d *Depacketizer) pushH265(payload []byte) error {
	if len(payload) < 2 { return errors.New("invalid H265 NAL") }
	nalType := (payload[0] >> 1) & 0x3f

	switch {
	case nalType <= 47:
		d.appendNAL(payload)
		if nalType >= 19 && nalType <= 21 { d.keyframe = true }
		return nil
	case nalType == 48:
		offset := 2
		for offset+2 <= len(payload) {
			size := int(payload[offset])<<8 | int(payload[offset+1])
			offset += 2
			if size <= 0 || offset+size > len(payload) { return errors.New("invalid H265 AP packet") }
			nal := payload[offset : offset+size]
			d.appendNAL(nal)
			t := (nal[0] >> 1) & 0x3f
			if t >= 19 && t <= 21 { d.keyframe = true }
			offset += size
		}
		if offset != len(payload) { return errors.New("invalid H265 AP tail") }
		return nil
	case nalType == 49:
		if len(payload) < 4 { return errors.New("invalid H265 FU packet") }
		fuHeader := payload[2]
		start := fuHeader&0x80 != 0
		end := fuHeader&0x40 != 0
		reconstructedType := fuHeader & 0x3f
		if start {
			d.fuOpen = true
			d.buf.Write(annexBStartCode)
			d.buf.WriteByte((payload[0] & 0x81) | (reconstructedType << 1))
			d.buf.WriteByte(payload[1])
			if reconstructedType >= 19 && reconstructedType <= 21 { d.keyframe = true }
		} else if !d.fuOpen {
			return errors.New("H265 FU continuation without start")
		}
		d.buf.Write(payload[3:])
		if end { d.fuOpen = false }
		return nil
	default:
		return nil
	}
}

func (d *Depacketizer) appendNAL(nal []byte) {
	if len(nal) == 0 { return }
	d.buf.Write(annexBStartCode)
	d.buf.Write(nal)
}
