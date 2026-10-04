package rtsp

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Result struct {
	StatusCode int `json:"status_code"`
	Status string `json:"status"`
	Server string `json:"server,omitempty"`
	PublicMethods []string `json:"public_methods,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	VideoTracks int `json:"video_tracks"`
	AudioTracks int `json:"audio_tracks"`
	Controls []string `json:"controls,omitempty"`
	SDP string `json:"-"`
	LatencyMS int64 `json:"latency_ms"`
	Authenticated bool `json:"authenticated"`
}

type response struct {
	code int
	status string
	header textproto.MIMEHeader
	body []byte
}

func Probe(ctx context.Context, rawURL string) (Result, error) {
	started := time.Now()
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return Result{}, errors.New("invalid RTSP URL")
	}
	if u.Scheme != "rtsp" && u.Scheme != "rtsps" {
		return Result{}, errors.New("unsupported RTSP scheme")
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		if u.Scheme == "rtsps" { port = "322" } else { port = "554" }
	}
	target := net.JoinHostPort(host, port)

	var conn net.Conn
	dialer := &net.Dialer{Timeout: 4 * time.Second}
	if u.Scheme == "rtsps" {
		conn, err = (&tls.Dialer{
			NetDialer: dialer,
			Config: &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12},
		}).DialContext(ctx, "tcp", target)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", target)
	}
	if err != nil { return Result{}, fmt.Errorf("connect %s: %w", target, err) }
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok { _ = conn.SetDeadline(deadline) }

	reader := bufio.NewReader(conn)
	cseq := 1
	requestURL := *u
	requestURL.User = nil
	resource := requestURL.String()

	options, err := roundTrip(conn, reader, "OPTIONS", resource, cseq, "")
	if err != nil { return Result{}, err }
	cseq++

	result := Result{
		StatusCode: options.code,
		Status: options.status,
		Server: options.header.Get("Server"),
		PublicMethods: splitMethods(options.header.Get("Public")),
	}

	authHeader := ""
	describe, err := roundTrip(conn, reader, "DESCRIBE", resource, cseq, authHeader)
	if err != nil { return Result{}, err }
	cseq++

	if describe.code == 401 {
		if u.User == nil {
			return Result{}, errors.New("camera requires authentication but URL has no credentials")
		}
		password, _ := u.User.Password()
		challenge := describe.header.Get("WWW-Authenticate")
		authHeader, err = buildAuthorization(challenge, u.User.Username(), password, "DESCRIBE", resource)
		if err != nil { return Result{}, err }
		describe, err = roundTrip(conn, reader, "DESCRIBE", resource, cseq, authHeader)
		if err != nil { return Result{}, err }
		cseq++
		result.Authenticated = describe.code >= 200 && describe.code < 300
	}

	result.StatusCode = describe.code
	result.Status = describe.status
	if server := describe.header.Get("Server"); server != "" { result.Server = server }
	result.ContentType = describe.header.Get("Content-Type")
	result.LatencyMS = time.Since(started).Milliseconds()

	if describe.code < 200 || describe.code >= 300 {
		return result, fmt.Errorf("RTSP DESCRIBE returned %d %s", describe.code, describe.status)
	}

	result.SDP = string(describe.body)
	result.VideoTracks, result.AudioTracks, result.Controls = parseSDP(result.SDP)
	return result, nil
}

func roundTrip(conn net.Conn, reader *bufio.Reader, method, resource string, cseq int, authorization string) (response, error) {
	var request strings.Builder
	fmt.Fprintf(&request, "%s %s RTSP/1.0\r\n", method, resource)
	fmt.Fprintf(&request, "CSeq: %d\r\n", cseq)
	request.WriteString("User-Agent: NVR/0.1\r\n")
	if method == "DESCRIBE" { request.WriteString("Accept: application/sdp\r\n") }
	if authorization != "" { fmt.Fprintf(&request, "Authorization: %s\r\n", authorization) }
	request.WriteString("\r\n")

	if _, err := io.WriteString(conn, request.String()); err != nil {
		return response{}, fmt.Errorf("send RTSP %s: %w", method, err)
	}
	return readResponse(reader)
}

func readResponse(reader *bufio.Reader) (response, error) {
	statusLine, err := reader.ReadString('\n')
	if err != nil { return response{}, fmt.Errorf("read RTSP status: %w", err) }
	statusLine = strings.TrimSpace(statusLine)
	parts := strings.SplitN(statusLine, " ", 3)
	if len(parts) < 2 || !strings.HasPrefix(parts[0], "RTSP/") {
		return response{}, fmt.Errorf("invalid RTSP response: %q", statusLine)
	}
	code, err := strconv.Atoi(parts[1])
	if err != nil { return response{}, fmt.Errorf("invalid RTSP status code: %w", err) }
	status := ""
	if len(parts) == 3 { status = parts[2] }

	tp := textproto.NewReader(reader)
	headers, err := tp.ReadMIMEHeader()
	if err != nil { return response{}, fmt.Errorf("read RTSP headers: %w", err) }

	var body []byte
	if rawLen := headers.Get("Content-Length"); rawLen != "" {
		length, err := strconv.Atoi(strings.TrimSpace(rawLen))
		if err != nil || length < 0 || length > 4<<20 {
			return response{}, errors.New("invalid RTSP Content-Length")
		}
		body = make([]byte, length)
		if _, err := io.ReadFull(reader, body); err != nil {
			return response{}, fmt.Errorf("read RTSP body: %w", err)
		}
	}
	return response{code: code, status: status, header: headers, body: body}, nil
}

func buildAuthorization(challenge, username, password, method, resource string) (string, error) {
	challenge = strings.TrimSpace(challenge)
	lower := strings.ToLower(challenge)

	if strings.HasPrefix(lower, "basic") {
		token := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
		return "Basic " + token, nil
	}
	if !strings.HasPrefix(lower, "digest") {
		return "", errors.New("unsupported RTSP authentication challenge")
	}

	params := parseAuthParams(strings.TrimSpace(challenge[len("Digest"):]))
	realm, nonce := params["realm"], params["nonce"]
	if realm == "" || nonce == "" {
		return "", errors.New("invalid RTSP Digest challenge")
	}
	algorithm := strings.ToUpper(params["algorithm"])
	if algorithm != "" && algorithm != "MD5" {
		return "", fmt.Errorf("unsupported RTSP Digest algorithm %q", algorithm)
	}

	ha1 := md5hex(username + ":" + realm + ":" + password)
	ha2 := md5hex(method + ":" + resource)
	response := ""
	qop := chooseQOP(params["qop"])
	nc := "00000001"
	cnonce := fmt.Sprintf("%016x", rand.Uint64())

	if qop != "" {
		response = md5hex(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
	} else {
		response = md5hex(ha1 + ":" + nonce + ":" + ha2)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Digest username=%q, realm=%q, nonce=%q, uri=%q, response=%q", username, realm, nonce, resource, response)
	if opaque := params["opaque"]; opaque != "" { fmt.Fprintf(&b, ", opaque=%q", opaque) }
	if qop != "" { fmt.Fprintf(&b, ", qop=%s, nc=%s, cnonce=%q", qop, nc, cnonce) }
	if algorithm != "" { b.WriteString(", algorithm=MD5") }
	return b.String(), nil
}

func parseAuthParams(raw string) map[string]string {
	out := map[string]string{}
	var token strings.Builder
	inQuote := false
	flush := func() {
		part := strings.TrimSpace(token.String())
		token.Reset()
		if part == "" { return }
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 { return }
		key := strings.ToLower(strings.TrimSpace(kv[0]))
		value := strings.Trim(strings.TrimSpace(kv[1]), "\\\"")
		out[key] = value
	}
	for _, r := range raw {
		switch r {
		case '`':
			inQuote = !inQuote
			token.WriteRune(r)
		case ',':
			if inQuote { token.WriteRune(r) } else { flush() }
		default:
			token.WriteRune(r)
		}
	}
	flush()
	return out
}

func chooseQOP(raw string) string {
	for _, item := range strings.Split(raw, ",") {
		if strings.EqualFold(strings.TrimSpace(item), "auth") { return "auth" }
	}
	return ""
}

func md5hex(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func splitMethods(raw string) []string {
	if strings.TrimSpace(raw) == "" { return nil }
	items := strings.Split(raw, ",")
	out := make([]string, 0, len(items))
	for _, item := range items {
		if value := strings.TrimSpace(item); value != "" { out = append(out, value) }
	}
	return out
}

func parseSDP(sdp string) (video, audio int, controls []string) {
	for _, raw := range strings.Split(strings.ReplaceAll(sdp, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "m=video "):
			video++
		case strings.HasPrefix(line, "m=audio "):
			audio++
		case strings.HasPrefix(line, "a=control:"):
			value := strings.TrimSpace(strings.TrimPrefix(line, "a=control:"))
			if value != "" && value != "*" { controls = append(controls, value) }
		}
	}
	return
}
