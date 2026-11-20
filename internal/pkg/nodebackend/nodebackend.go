// Package nodebackend picks the control-plane address a fleet node calls: the private one when it answers, else the configured public one.
package nodebackend

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Names RenderEnv writes and a node reads; an older node never reads them and keeps the public address.
const (
	EnvInternalURL   = "WARMBLY_BACKEND_INTERNAL_URL"
	EnvInternalCAB64 = "WARMBLY_BACKEND_INTERNAL_CA_B64"
)

// ProbeTimeout bounds the startup check, so an unreachable private address costs a node seconds, not its boot.
const ProbeTimeout = 3 * time.Second

// Backend is the private control-plane address a node switched to; the zero value keeps the public address.
type Backend struct {
	url       string
	transport http.RoundTripper
}

// Private reports whether the node calls the private address.
func (b Backend) Private() bool { return b.url != "" }

// BaseURL is the private address when one was chosen, else public.
func (b Backend) BaseURL(public string) string {
	if b.url != "" {
		return b.url
	}
	return public
}

// Transport reaches the private address with its pinned roots; nil (the default transport) for the zero value.
func (b Backend) Transport() http.RoundTripper { return b.transport }

// Client is an http.Client on Transport with the given timeout.
func (b Backend) Client(timeout time.Duration) *http.Client {
	return &http.Client{Transport: b.transport, Timeout: timeout}
}

// Resolve probes the private address the control plane rendered and returns it when /health answers over the pinned roots.
func Resolve(ctx context.Context, getenv func(string) string, logf func(string, ...any)) Backend {
	raw := strings.TrimRight(strings.TrimSpace(getenv(EnvInternalURL)), "/")
	if raw == "" {
		return Backend{}
	}
	b, err := New(raw, strings.TrimSpace(getenv(EnvInternalCAB64)))
	if err == nil {
		err = b.probe(ctx)
	}
	if err != nil {
		logf("control plane: private address %s unusable (%v); using the public address", raw, err)
		return Backend{}
	}
	logf("control plane: using the private address %s", raw)
	return b
}

// New builds a Backend for rawURL trusting only the base64 PEM bundle caB64 for that origin, or the system roots when it is empty.
func New(rawURL, caB64 string) (Backend, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return Backend{}, errors.New("not an http(s) URL")
	}
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return Backend{}, errors.New("default transport is not *http.Transport")
	}
	pinned := base.Clone()
	// Every node client shares this transport, and one host takes all of them.
	pinned.MaxIdleConns = 512
	pinned.MaxIdleConnsPerHost = 512
	if caB64 != "" {
		roots, err := decodeRoots(caB64)
		if err != nil {
			return Backend{}, err
		}
		pinned.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	}
	return Backend{
		url:       strings.TrimRight(u.String(), "/"),
		transport: originTransport{scheme: u.Scheme, host: u.Host, pinned: pinned},
	}, nil
}

func decodeRoots(caB64 string) (*x509.CertPool, error) {
	pem, err := base64.StdEncoding.DecodeString(caB64)
	if err != nil {
		return nil, fmt.Errorf("%s is not base64", EnvInternalCAB64)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("%s holds no PEM certificate", EnvInternalCAB64)
	}
	return roots, nil
}

func (b Backend) probe(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.url+"/health", nil)
	if err != nil {
		return err
	}
	client := &http.Client{Transport: b.transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health answered %d", resp.StatusCode)
	}
	return nil
}

// originTransport sends only the private origin through the pinned roots; anything else takes the default transport.
type originTransport struct {
	scheme, host string
	pinned       http.RoundTripper
}

func (t originTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.EqualFold(req.URL.Scheme, t.scheme) && strings.EqualFold(req.URL.Host, t.host) {
		return t.pinned.RoundTrip(req)
	}
	return http.DefaultTransport.RoundTrip(req)
}
