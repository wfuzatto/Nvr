package playback

import "testing"

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
