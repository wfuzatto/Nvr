package rtsp

import (
	"encoding/binary"
	"testing"
)

func TestParseRTP(t *testing.T) {
	raw := make([]byte, 15)
	raw[0] = 0x80
	raw[1] = 0x80 | 96
	binary.BigEndian.PutUint16(raw[2:4], 123)
	binary.BigEndian.PutUint32(raw[4:8], 456)
	binary.BigEndian.PutUint32(raw[8:12], 789)
	copy(raw[12:], []byte{1,2,3})

	p, err := ParseRTP(raw)
	if err != nil { t.Fatal(err) }
	if !p.Marker || p.PayloadType != 96 || p.Sequence != 123 || p.Timestamp != 456 || p.SSRC != 789 {
		t.Fatalf("unexpected RTP header: %+v", p)
	}
	if len(p.Payload) != 3 { t.Fatalf("payload len=%d", len(p.Payload)) }
}
