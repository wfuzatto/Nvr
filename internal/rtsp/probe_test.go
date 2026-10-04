package rtsp

import (
	"strings"
	"testing"
)

func TestParseSDP(t *testing.T) {
	sdp := "v=0\r\nm=video 0 RTP/AVP 96\r\na=control:trackID=1\r\nm=audio 0 RTP/AVP 0\r\na=control:trackID=2\r\n"
	video, audio, controls := parseSDP(sdp)
	if video != 1 || audio != 1 { t.Fatalf("video=%d audio=%d", video, audio) }
	if len(controls) != 2 { t.Fatalf("controls=%v", controls) }
}

func TestBasicAuthorization(t *testing.T) {
	got, err := buildAuthorization("Basic realm=\"camera\"", "admin", "secret", "DESCRIBE", "rtsp://camera/stream")
	if err != nil { t.Fatal(err) }
	if !strings.HasPrefix(got, "Basic ") { t.Fatalf("unexpected auth %q", got) }
}

func TestDigestAuthorization(t *testing.T) {
	got, err := buildAuthorization("Digest realm=\"cam\", nonce=\"abc\", qop=\"auth\", algorithm=MD5", "admin", "secret", "DESCRIBE", "rtsp://camera/stream")
	if err != nil { t.Fatal(err) }
	for _, part := range []string{"Digest ", "username=\"admin\"", "response=", "qop=auth"} {
		if !strings.Contains(got, part) { t.Fatalf("missing %q in %q", part, got) }
	}
}
