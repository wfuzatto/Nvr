package rtsp

import (
	"encoding/binary"
	"errors"
)

type RTPPacket struct {
	Marker      bool
	PayloadType uint8
	Sequence    uint16
	Timestamp   uint32
	SSRC        uint32
	Payload     []byte
}

func ParseRTP(payload []byte) (RTPPacket, error) {
	if len(payload) < 12 { return RTPPacket{}, errors.New("RTP packet too short") }
	if payload[0]>>6 != 2 { return RTPPacket{}, errors.New("unsupported RTP version") }

	padding := payload[0]&0x20 != 0
	extension := payload[0]&0x10 != 0
	csrcCount := int(payload[0] & 0x0f)
	offset := 12 + csrcCount*4
	if offset > len(payload) { return RTPPacket{}, errors.New("invalid RTP CSRC list") }

	if extension {
		if offset+4 > len(payload) { return RTPPacket{}, errors.New("invalid RTP extension") }
		words := int(binary.BigEndian.Uint16(payload[offset+2 : offset+4]))
		offset += 4 + words*4
		if offset > len(payload) { return RTPPacket{}, errors.New("invalid RTP extension length") }
	}

	end := len(payload)
	if padding {
		pad := int(payload[len(payload)-1])
		if pad == 0 || pad > end-offset { return RTPPacket{}, errors.New("invalid RTP padding") }
		end -= pad
	}
	if offset > end { return RTPPacket{}, errors.New("invalid RTP payload offset") }

	return RTPPacket{
		Marker:      payload[1]&0x80 != 0,
		PayloadType: payload[1] & 0x7f,
		Sequence:    binary.BigEndian.Uint16(payload[2:4]),
		Timestamp:   binary.BigEndian.Uint32(payload[4:8]),
		SSRC:        binary.BigEndian.Uint32(payload[8:12]),
		Payload:     payload[offset:end],
	}, nil
}
