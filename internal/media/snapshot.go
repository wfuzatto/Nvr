package media

import (
	"context"
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil { return nil, "", err }
	req.Header.Set("User-Agent", "NVR/0.2")
	if username != "" { req.SetBasicAuth(username, password) }

	resp, err := client.Do(req)
	if err != nil { return nil, "", err }
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("snapshot returned HTTP %d", resp.StatusCode)
	}

	const maxSnapshot = int64(12 << 20)
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSnapshot+1))
	if err != nil { return nil, "", err }
	if int64(len(body)) > maxSnapshot { return nil, "", errors.New("snapshot exceeds 12 MiB") }

	contentType := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0]))
	if contentType == "" {
		contentType = http.DetectContentType(body)
	}
	if !strings.HasPrefix(contentType, "image/") {
		return nil, "", fmt.Errorf("snapshot returned non-image content type %q", contentType)
	}
	return body, contentType, nil
}
