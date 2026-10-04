package rtsp

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestParseH264VideoTrack(t *testing.T) {
	sps := base64.StdEncoding.EncodeToString([]byte{0x67,1,2})
	pps := base64.StdEncoding.EncodeToString([]byte{0x68,3,4})
	sdp := "v=0\r\n" +
		"m=video 0 RTP/AVP 96\r\n" +
		"a=rtpmap:96 H264/90000\r\n" +
		"a=fmtp:96 packetization-mode=1; sprop-parameter-sets=" + sps + "," + pps + "\r\n" +
		"a=control:trackID=1\r\n"

	track, err := parseVideoTrack(sdp, "rtsp://10.0.0.1/live/")
	if err != nil { t.Fatal(err) }
	if track.Codec != "H264" || track.PayloadType != 96 || track.ClockRate != 90000 {
		t.Fatalf("unexpected track: %+v", track)
	}
	if !strings.Contains(track.Control, "trackID=1") { t.Fatalf("control=%q", track.Control) }
	if len(track.Bootstrap) != 2 { t.Fatalf("bootstrap=%d", len(track.Bootstrap)) }
}

func TestParseH265VideoTrack(t *testing.T) {
	sdp := "v=0\r\nm=video 0 RTP/AVP 98\r\na=rtpmap:98 H265/90000\r\na=control:stream=0\r\n"
	track, err := parseVideoTrack(sdp, "rtsp://camera/live")
	if err != nil { t.Fatal(err) }
	if track.Codec != "H265" { t.Fatalf("codec=%q", track.Codec) }
}
