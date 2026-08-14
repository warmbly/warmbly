package confenge

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/pkg/safehttp"
)

// FeedFetcher loads a feed from HTTPS (production) or file:// (dev/tests).
// Remote fetches use the SSRF-hardened client and optional host allowlist.
type FeedFetcher struct {
	AllowedHosts []string
	Token        string
	MaxBytes     int64
	HTTPClient   *http.Client
	AllowFile    bool
	RequireHTTPS bool
}

// Fetch returns raw payload bytes from uri.
// Supported schemes: https, http (dev only via WARMBLY_ALLOW_UNSAFE_WEBHOOK_URLS), file.
func (f *FeedFetcher) Fetch(ctx context.Context, uri string) ([]byte, error) {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return nil, fmt.Errorf("feed URI is required")
	}
	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("invalid feed URI: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "file":
		if !f.AllowFile {
			return nil, fmt.Errorf("file feed URLs are disabled in this environment")
		}
		return f.fetchFile(u)
	case "https", "http":
		if f.RequireHTTPS && strings.ToLower(u.Scheme) != "https" {
			return nil, fmt.Errorf("feed URL must use https in this environment")
		}
		return f.fetchHTTP(ctx, u)
	default:
		return nil, fmt.Errorf("unsupported feed scheme %q", u.Scheme)
	}
}

func (f *FeedFetcher) fetchFile(u *url.URL) ([]byte, error) {
	path := u.Path
	if path == "" {
		path = u.Opaque
	}
	// file:///absolute/path
	if strings.HasPrefix(u.String(), "file://") && path == "" {
		return nil, fmt.Errorf("empty file path")
	}
	data, err := readFileLimited(path, f.maxBytes())
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (f *FeedFetcher) fetchHTTP(ctx context.Context, u *url.URL) ([]byte, error) {
	host := strings.ToLower(u.Hostname())
	if f.RequireHTTPS && len(f.AllowedHosts) == 0 {
		return nil, fmt.Errorf("feed host allowlist is required in this environment")
	}
	if len(f.AllowedHosts) > 0 && !hostAllowed(host, f.AllowedHosts) {
		return nil, fmt.Errorf("feed host %q is not in allowlist", host)
	}
	client := f.HTTPClient
	if client == nil {
		client = safehttp.Client(30 * time.Second)
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if f.Token != "" {
		req.Header.Set("Authorization", "Bearer "+f.Token)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := clientCopy.Do(req)
	if err != nil {
		return nil, fmt.Errorf("feed fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("feed fetch HTTP %d", resp.StatusCode)
	}
	maxBytes := f.maxBytes()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read feed response: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("payload exceeds max size of %d bytes", maxBytes)
	}
	return data, nil
}

func (f *FeedFetcher) maxBytes() int64 {
	if f.MaxBytes > 0 {
		return f.MaxBytes
	}
	return DefaultMaxPayloadBytes
}

func hostAllowed(host string, allow []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, a := range allow {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" {
			continue
		}
		if host == a || strings.HasSuffix(host, "."+a) {
			return true
		}
	}
	return false
}
