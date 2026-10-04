package rtsp

import "testing"

func TestH264SingleNAL(t *testing.T) {
	d, err := NewDepacketizer("H264", 96)
	if err != nil { t.Fatal(err) }

	au, err := d.Push(RTPPacket{PayloadType:96, Timestamp:100, Marker:true, Payload:[]byte{0x65,1,2,3}})
	if err != nil { t.Fatal(err) }
	if au == nil || !au.Keyframe { t.Fatalf("unexpected access unit: %+v", au) }
	if len(au.Data) != 8 { t.Fatalf("data len=%d", len(au.Data)) }
}

func TestH264FUA(t *testing.T) {
	d, _ := NewDepacketizer("H264", 96)
	if au, err := d.Push(RTPPacket{PayloadType:96, Timestamp:1, Marker:false, Payload:[]byte{0x7c,0x85,1,2}}); err != nil || au != nil {
		t.Fatalf("start err=%v au=%v", err, au)
	}
	au, err := d.Push(RTPPacket{PayloadType:96, Timestamp:1, Marker:true, Payload:[]byte{0x7c,0x45,3,4}})
	if err != nil { t.Fatal(err) }
	if au == nil || !au.Keyframe { t.Fatalf("unexpected access unit: %+v", au) }
}

func TestH265SingleNAL(t *testing.T) {
	d, err := NewDepacketizer("H265", 98)
	if err != nil { t.Fatal(err) }
	// Type 19 IDR_W_RADL.
	au, err := d.Push(RTPPacket{PayloadType:98, Timestamp:9, Marker:true, Payload:[]byte{19<<1,1,2,3}})
	if err != nil { t.Fatal(err) }
	if au == nil || !au.Keyframe { t.Fatalf("unexpected access unit: %+v", au) }
}

func TestPacketLossDropsDamagedAccessUnit(t *testing.T) {
	d, _ := NewDepacketizer("H264", 96)
	if au, err := d.Push(RTPPacket{PayloadType:96, Sequence:10, Timestamp:7, Marker:false, Payload:[]byte{0x7c,0x85,1,2}}); err != nil || au != nil {
		t.Fatalf("start err=%v au=%v", err, au)
	}
	// Sequence 11 was lost. The damaged access unit must not be published.
	au, err := d.Push(RTPPacket{PayloadType:96, Sequence:12, Timestamp:7, Marker:true, Payload:[]byte{0x7c,0x45,3,4}})
	if err != nil { t.Fatal(err) }
	if au != nil { t.Fatalf("damaged AU should be dropped: %+v", au) }

	// Next timestamp starts cleanly.
	au, err = d.Push(RTPPacket{PayloadType:96, Sequence:13, Timestamp:8, Marker:true, Payload:[]byte{0x65,9}})
	if err != nil { t.Fatal(err) }
	if au == nil || !au.Keyframe { t.Fatalf("expected clean keyframe after loss: %+v", au) }
}
