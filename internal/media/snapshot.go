package media

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func FetchSnapshot(ctx context.Context, rawURL string, timeout time.Duration) ([]byte, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" { return nil, "", errors.New("invalid snapshot URL") }
	if u.Scheme != "http" && u.Scheme != "https" { return nil, "", errors.New("snapshot URL must use http or https") }

	username, password := "", ""
	if u.User != nil {
		username = u.User.Username()
		password, _ = u.User.Password()
		u.User = nil
	}
	if timeout <= 0 { timeout = 5 * time.Second }

	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 { return errors.New("too many snapshot redirects") }
			return nil
		},
	}

	resp, err := doSnapshotRequest(ctx, client, u, username, password, "")
	if err != nil { return nil, "", err }

	if resp.StatusCode == http.StatusUnauthorized && username != "" {
		challenge := resp.Header.Get("WWW-Authenticate")
		_ = resp.Body.Close()
		if digest := extractDigestChallenge(challenge); digest != "" {
			auth, err := buildHTTPDigestAuthorization(digest, username, password, http.MethodGet, u.RequestURI())
			if err != nil { return nil, "", err }
			resp, err = doSnapshotRequest(ctx, client, u, "", "", auth)
			if err != nil { return nil, "", err }
		} else {
			return nil, "", errors.New("snapshot authentication rejected")
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("snapshot returned HTTP %d", resp.StatusCode)
	}

	const maxSnapshot = int64(12 << 20)
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSnapshot+1))
	if err != nil { return nil, "", err }
	if int64(len(body)) > maxSnapshot { return nil, "", errors.New("snapshot exceeds 12 MiB") }

	contentType := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0]))
	if contentType == "" { contentType = http.DetectContentType(body) }
	if !strings.HasPrefix(contentType, "image/") {
		return nil, "", fmt.Errorf("snapshot returned non-image content type %q", contentType)
	}
	return body, contentType, nil
}

func doSnapshotRequest(ctx context.Context, client *http.Client, u *url.URL, username, password, authorization string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil { return nil, err }
	req.Header.Set("User-Agent", "NVR/0.2")
	req.Header.Set("Accept", "image/jpeg,image/*;q=0.9,*/*;q=0.1")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	} else if username != "" {
		req.SetBasicAuth(username, password)
	}
	return client.Do(req)
}

func extractDigestChallenge(raw string) string {
	lower := strings.ToLower(raw)
	index := strings.Index(lower, "digest ")
	if index < 0 { return "" }
	return strings.TrimSpace(raw[index:])
}

func buildHTTPDigestAuthorization(challenge, username, password, method, uri string) (string, error) {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(challenge)), "digest ") {
		return "", errors.New("not a Digest challenge")
	}
	params := parseHTTPDigestParams(strings.TrimSpace(challenge[len("Digest "):]))
	realm, nonce := params["realm"], params["nonce"]
	if realm == "" || nonce == "" { return "", errors.New("invalid HTTP Digest challenge") }

	algorithm := strings.ToUpper(params["algorithm"])
	if algorithm == "" { algorithm = "MD5" }
	if algorithm != "MD5" && algorithm != "MD5-SESS" {
		return "", fmt.Errorf("unsupported HTTP Digest algorithm %q", algorithm)
	}

	cnonceBytes := make([]byte, 8)
	if _, err := rand.Read(cnonceBytes); err != nil { return "", err }
	cnonce := hex.EncodeToString(cnonceBytes)
	nc := "00000001"
	qop := chooseHTTPQOP(params["qop"])

	ha1 := md5String(username + ":" + realm + ":" + password)
	if algorithm == "MD5-SESS" {
		ha1 = md5String(ha1 + ":" + nonce + ":" + cnonce)
	}
	ha2 := md5String(method + ":" + uri)

	var response string
	if qop != "" {
		response = md5String(ha1 + ":" + nonce + ":" + nc + ":" + cnonce + ":" + qop + ":" + ha2)
	} else {
		response = md5String(ha1 + ":" + nonce + ":" + ha2)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Digest username=%q, realm=%q, nonce=%q, uri=%q, response=%q", username, realm, nonce, uri, response)
	if opaque := params["opaque"]; opaque != "" { fmt.Fprintf(&b, ", opaque=%q", opaque) }
	if qop != "" { fmt.Fprintf(&b, ", qop=%s, nc=%s, cnonce=%q", qop, nc, cnonce) }
	if algorithm != "" { fmt.Fprintf(&b, ", algorithm=%s", algorithm) }
	return b.String(), nil
}

func parseHTTPDigestParams(raw string) map[string]string {
	out := make(map[string]string)
	var token strings.Builder
	inQuote := false
	flush := func() {
		part := strings.TrimSpace(token.String())
		token.Reset()
		if part == "" { return }
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 { return }
		key := strings.ToLower(strings.TrimSpace(kv[0]))
		value := strings.Trim(strings.TrimSpace(kv[1]), "\"")
		out[key] = value
	}
	for _, r := range raw {
		switch r {
		case '"':
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

func chooseHTTPQOP(raw string) string {
	for _, item := range strings.Split(raw, ",") {
		if strings.EqualFold(strings.TrimSpace(item), "auth") { return "auth" }
	}
	return ""
}

func md5String(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}
