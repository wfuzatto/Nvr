package rtsp

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type VideoTrack struct {
	Codec       string
	PayloadType uint8
	ClockRate   int
	Control     string
	Bootstrap   [][]byte
}

type Session struct {
	conn          net.Conn
	reader        *bufio.Reader
	writeMu       sync.Mutex
	cseq          int
	baseURL       string
	username      string
	password      string
	authChallenge string
	sessionID     string
	track         VideoTrack
	readTimeout   time.Duration
	done          chan struct{}
	closeOnce     sync.Once
}

func OpenSession(ctx context.Context, rawURL string, readTimeout time.Duration) (*Session, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" { return nil, errors.New("invalid RTSP URL") }
	if u.Scheme != "rtsp" && u.Scheme != "rtsps" { return nil, errors.New("unsupported RTSP scheme") }
	if readTimeout <= 0 { readTimeout = 15 * time.Second }

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		if u.Scheme == "rtsps" { port = "322" } else { port = "554" }
	}
	target := net.JoinHostPort(host, port)
	dialer := &net.Dialer{Timeout: 5 * time.Second}

	var conn net.Conn
	if u.Scheme == "rtsps" {
		conn, err = (&tls.Dialer{
			NetDialer: dialer,
			Config: &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12},
		}).DialContext(ctx, "tcp", target)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", target)
	}
	if err != nil { return nil, fmt.Errorf("connect %s: %w", target, err) }

	requestURL := *u
	username, password := "", ""
	if u.User != nil {
		username = u.User.Username()
		password, _ = u.User.Password()
		requestURL.User = nil
	}
	s := &Session{
		conn: conn, reader: bufio.NewReaderSize(conn, 256*1024),
		cseq: 1, baseURL: requestURL.String(),
		username: username, password: password,
		readTimeout: readTimeout, done: make(chan struct{}),
	}

	fail := func(err error) (*Session, error) {
		_ = s.Close()
		return nil, err
	}

	describe, err := s.request("DESCRIBE", s.baseURL, map[string]string{"Accept":"application/sdp"})
	if err != nil { return fail(err) }
	if describe.code == 401 {
		s.authChallenge = describe.header.Get("WWW-Authenticate")
		if s.username == "" { return fail(errors.New("camera requires RTSP credentials")) }
		describe, err = s.request("DESCRIBE", s.baseURL, map[string]string{"Accept":"application/sdp"})
		if err != nil { return fail(err) }
	}
	if describe.code < 200 || describe.code >= 300 {
		return fail(fmt.Errorf("RTSP DESCRIBE returned %d %s", describe.code, describe.status))
	}

	base := describe.header.Get("Content-Base")
	if base == "" { base = s.baseURL }
	track, err := parseVideoTrack(string(describe.body), base)
	if err != nil { return fail(err) }
	s.track = track

	setupHeaders := map[string]string{
		"Transport": "RTP/AVP/TCP;unicast;interleaved=0-1",
	}
	setup, err := s.request("SETUP", track.Control, setupHeaders)
	if err != nil { return fail(err) }
	if setup.code == 401 {
		s.authChallenge = setup.header.Get("WWW-Authenticate")
		setup, err = s.request("SETUP", track.Control, setupHeaders)
		if err != nil { return fail(err) }
	}
	if setup.code < 200 || setup.code >= 300 {
		return fail(fmt.Errorf("RTSP SETUP returned %d %s", setup.code, setup.status))
	}
	s.sessionID = strings.TrimSpace(strings.SplitN(setup.header.Get("Session"), ";", 2)[0])
	if s.sessionID == "" { return fail(errors.New("RTSP SETUP returned no Session header")) }

	play, err := s.request("PLAY", s.baseURL, map[string]string{"Session":s.sessionID})
	if err != nil { return fail(err) }
	if play.code == 401 {
		s.authChallenge = play.header.Get("WWW-Authenticate")
		play, err = s.request("PLAY", s.baseURL, map[string]string{"Session":s.sessionID})
		if err != nil { return fail(err) }
	}
	if play.code < 200 || play.code >= 300 {
		return fail(fmt.Errorf("RTSP PLAY returned %d %s", play.code, play.status))
	}

	go s.keepaliveLoop()
	return s, nil
}

func (s *Session) Track() VideoTrack {
	out := s.track
	out.Bootstrap = cloneNALs(s.track.Bootstrap)
	return out
}

func (s *Session) ReadRTP() (RTPPacket, error) {
	for {
		if err := s.conn.SetReadDeadline(time.Now().Add(s.readTimeout)); err != nil {
			return RTPPacket{}, err
		}
		first, err := s.reader.ReadByte()
		if err != nil { return RTPPacket{}, err }

		if first == '$' {
			channel, err := s.reader.ReadByte()
			if err != nil { return RTPPacket{}, err }
			var lenBuf [2]byte
			if _, err := io.ReadFull(s.reader, lenBuf[:]); err != nil { return RTPPacket{}, err }
			length := int(lenBuf[0])<<8 | int(lenBuf[1])
			if length <= 0 || length > 4<<20 { return RTPPacket{}, errors.New("invalid RTSP interleaved frame length") }
			payload := make([]byte, length)
			if _, err := io.ReadFull(s.reader, payload); err != nil { return RTPPacket{}, err }
			if channel != 0 { continue }
			packet, err := ParseRTP(payload)
			if err != nil { continue }
			return packet, nil
		}

		if first == '\r' || first == '\n' { continue }
		if err := s.reader.UnreadByte(); err != nil { return RTPPacket{}, err }
		if _, err := readResponse(s.reader); err != nil {
			return RTPPacket{}, fmt.Errorf("read asynchronous RTSP response: %w", err)
		}
	}
}

func (s *Session) Close() error {
	var err error
	s.closeOnce.Do(func() {
		close(s.done)
		if s.sessionID != "" {
			_ = s.writeOnly("TEARDOWN", s.baseURL, map[string]string{"Session":s.sessionID})
		}
		err = s.conn.Close()
	})
	return err
}

func (s *Session) request(method, resource string, headers map[string]string) (response, error) {
	cseq, auth, err := s.writeRequest(method, resource, headers)
	if err != nil { return response{}, err }
	_ = cseq
	_ = auth
	if err := s.conn.SetReadDeadline(time.Now().Add(6 * time.Second)); err != nil {
		return response{}, err
	}
	return readResponse(s.reader)
}

func (s *Session) writeOnly(method, resource string, headers map[string]string) error {
	_, _, err := s.writeRequest(method, resource, headers)
	return err
}

func (s *Session) writeRequest(method, resource string, headers map[string]string) (int, string, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	cseq := s.cseq
	s.cseq++
	auth := ""
	if s.authChallenge != "" && s.username != "" {
		var err error
		auth, err = buildAuthorization(s.authChallenge, s.username, s.password, method, resource)
		if err != nil { return 0, "", err }
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s %s RTSP/1.0\r\n", method, resource)
	fmt.Fprintf(&b, "CSeq: %d\r\n", cseq)
	b.WriteString("User-Agent: NVR/0.2\r\n")
	for key, value := range headers {
		if value != "" { fmt.Fprintf(&b, "%s: %s\r\n", key, value) }
	}
	if auth != "" { fmt.Fprintf(&b, "Authorization: %s\r\n", auth) }
	b.WriteString("\r\n")

	if deadline, ok := contextDeadline(headers); ok {
		_ = s.conn.SetWriteDeadline(deadline)
	} else {
		_ = s.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	}
	_, err := io.WriteString(s.conn, b.String())
	if err != nil { return 0, "", fmt.Errorf("send RTSP %s: %w", method, err) }
	return cseq, auth, nil
}

func (s *Session) keepaliveLoop() {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			headers := map[string]string{}
			if s.sessionID != "" { headers["Session"] = s.sessionID }
			if err := s.writeOnly("OPTIONS", s.baseURL, headers); err != nil { return }
		}
	}
}

func parseVideoTrack(sdp, base string) (VideoTrack, error) {
	type candidate struct {
		media string
		payload int
		codec string
		clock int
		control string
		fmtp string
	}
	var tracks []*candidate
	var current *candidate

	lines := strings.Split(strings.ReplaceAll(sdp, "\r\n", "\n"), "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "m=") {
			fields := strings.Fields(strings.TrimPrefix(line, "m="))
			current = nil
			if len(fields) >= 4 {
				payload, err := strconv.Atoi(fields[3])
				if err == nil {
					current = &candidate{media: strings.ToLower(fields[0]), payload: payload, clock: 90000}
					tracks = append(tracks, current)
				}
			}
			continue
		}
		if current == nil { continue }

		if strings.HasPrefix(line, "a=rtpmap:") {
			value := strings.TrimPrefix(line, "a=rtpmap:")
			parts := strings.Fields(value)
			if len(parts) >= 2 {
				pt, err := strconv.Atoi(parts[0])
				if err == nil && pt == current.payload {
					codecParts := strings.Split(parts[1], "/")
					current.codec = strings.ToUpper(codecParts[0])
					if len(codecParts) > 1 {
						if clock, err := strconv.Atoi(codecParts[1]); err == nil { current.clock = clock }
					}
				}
			}
		} else if strings.HasPrefix(line, "a=fmtp:") {
			value := strings.TrimPrefix(line, "a=fmtp:")
			parts := strings.SplitN(value, " ", 2)
			if len(parts) == 2 {
				if pt, err := strconv.Atoi(parts[0]); err == nil && pt == current.payload { current.fmtp = parts[1] }
			}
		} else if strings.HasPrefix(line, "a=control:") {
			current.control = strings.TrimSpace(strings.TrimPrefix(line, "a=control:"))
		}
	}

	for _, track := range tracks {
		if track.media != "video" { continue }
		codec := strings.ToUpper(track.codec)
		if codec == "HEVC" { codec = "H265" }
		if codec != "H264" && codec != "H265" { continue }
		if track.payload < 0 || track.payload > 127 { continue }
		control, err := resolveControl(base, track.control)
		if err != nil { continue }
		return VideoTrack{
			Codec: codec, PayloadType: uint8(track.payload), ClockRate: track.clock,
			Control: control, Bootstrap: parseBootstrap(codec, track.fmtp),
		}, nil
	}
	return VideoTrack{}, errors.New("no supported H264/H265 video track in SDP")
}

func resolveControl(base, control string) (string, error) {
	if control == "" || control == "*" { return "", errors.New("video track has no control URI") }
	if strings.HasPrefix(strings.ToLower(control), "rtsp://") || strings.HasPrefix(strings.ToLower(control), "rtsps://") {
		return control, nil
	}
	baseURL, err := url.Parse(base)
	if err != nil { return "", err }
	if !strings.HasSuffix(baseURL.Path, "/") {
		baseURL.Path += "/"
	}
	ref, err := url.Parse(control)
	if err != nil { return "", err }
	return baseURL.ResolveReference(ref).String(), nil
}

func parseBootstrap(codec, fmtp string) [][]byte {
	params := make(map[string]string)
	for _, part := range strings.Split(fmtp, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 { params[strings.ToLower(strings.TrimSpace(kv[0]))] = strings.TrimSpace(kv[1]) }
	}

	var encoded []string
	if codec == "H264" {
		if raw := params["sprop-parameter-sets"]; raw != "" { encoded = strings.Split(raw, ",") }
	} else {
		for _, key := range []string{"sprop-vps","sprop-sps","sprop-pps"} {
			if raw := params[key]; raw != "" { encoded = append(encoded, raw) }
		}
	}

	var out [][]byte
	for _, raw := range encoded {
		nal, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
		if err == nil && len(nal) > 0 { out = append(out, nal) }
	}
	return out
}

func cloneNALs(input [][]byte) [][]byte {
	out := make([][]byte, 0, len(input))
	for _, nal := range input { out = append(out, append([]byte(nil), nal...)) }
	return out
}

// contextDeadline exists only to keep the write path free of global mutable deadlines.
func contextDeadline(_ map[string]string) (time.Time, bool) { return time.Time{}, false }
