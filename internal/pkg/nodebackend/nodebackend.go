// Package nodebackend picks the control-plane address a fleet node calls: the private one when it answers, else the configured public one.
package nodebackend

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Names RenderEnv writes and a node reads; an older node never reads them and keeps the public address.
const (
	EnvInternalURL   = "WARMBLY_BACKEND_INTERNAL_URL"
	EnvInternalCAB64 = "WARMBLY_BACKEND_INTERNAL_CA_B64"
)

const (
	// ProbeTimeout bounds the startup check and each private dial, so an unreachable private address costs seconds.
	ProbeTimeout = 3 * time.Second
	// ConnMaxAge bounds how long a private connection is reused, so new backend tasks behind the name receive traffic.
	ConnMaxAge = 2 * time.Minute
	// FallbackAfter consecutive dial failures send everything to the public address for FallbackFor.
	FallbackAfter = 3
	FallbackFor   = 5 * time.Minute
)

// Backend is the private control-plane address a node switched to; the zero value keeps the public address.
type Backend struct {
	url       string
	transport *originTransport
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

// Transport reaches the private address with its pinned roots and falls back to the public one; nil for the zero value.
func (b Backend) Transport() http.RoundTripper {
	if b.transport == nil {
		return nil
	}
	return b.transport
}

// Client is an http.Client on Transport with the given timeout.
func (b Backend) Client(timeout time.Duration) *http.Client {
	return &http.Client{Transport: b.Transport(), Timeout: timeout}
}

// Resolve probes the private address the control plane rendered and returns it when /health answers over the pinned roots.
func Resolve(ctx context.Context, getenv func(string) string, public string, logf func(string, ...any)) Backend {
	raw := strings.TrimRight(strings.TrimSpace(getenv(EnvInternalURL)), "/")
	if raw == "" {
		return Backend{}
	}
	b, err := New(raw, strings.TrimSpace(getenv(EnvInternalCAB64)), public, logf)
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

// New builds a Backend for privateURL trusting only the base64 PEM bundle caB64 there (system roots when empty), falling back to publicURL.
func New(privateURL, caB64, publicURL string, logf func(string, ...any)) (Backend, error) {
	private, err := parseOrigin(privateURL)
	if err != nil {
		return Backend{}, err
	}
	var public *url.URL
	if strings.TrimSpace(publicURL) != "" {
		if public, err = parseOrigin(publicURL); err != nil {
			return Backend{}, fmt.Errorf("public address: %w", err)
		}
	}
	var roots *x509.CertPool
	if caB64 != "" {
		if roots, err = decodeRoots(caB64); err != nil {
			return Backend{}, err
		}
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	t, err := newOriginTransport(private, public, roots, logf)
	if err != nil {
		return Backend{}, err
	}
	return Backend{url: strings.TrimRight(private.String(), "/"), transport: t}, nil
}

func parseOrigin(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("not an http(s) URL")
	}
	return u, nil
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

// probe asks the private address alone; a fallback answer must not count as reaching it.
func (b Backend) probe(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.url+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := b.transport.current().RoundTrip(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health answered %d", resp.StatusCode)
	}
	return nil
}

// dialError marks a failure before any byte of the request was written: dial, DNS or TLS handshake.
type dialError struct{ err error }

func (e dialError) Error() string { return e.err.Error() }
func (e dialError) Unwrap() error { return e.err }

type generation struct {
	rt   *http.Transport
	born time.Time
}

// originTransport sends the private origin through pinned roots and recycled connections, and anything else, or a failed dial, to the public side.
type originTransport struct {
	private  *url.URL
	public   *url.URL
	roots    *x509.CertPool
	template *http.Transport
	gen      atomic.Pointer[generation]
	logf     func(string, ...any)

	// Replaced in tests.
	now      func() time.Time
	dial     func(ctx context.Context, network, addr string) (net.Conn, error)
	fallback http.RoundTripper

	mu          sync.Mutex
	failures    int
	publicUntil time.Time
	onPublic    bool
}

func newOriginTransport(private, public *url.URL, roots *x509.CertPool, logf func(string, ...any)) (*originTransport, error) {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("default transport is not *http.Transport")
	}
	t := &originTransport{
		private:  private,
		public:   public,
		roots:    roots,
		logf:     logf,
		now:      time.Now,
		dial:     (&net.Dialer{Timeout: ProbeTimeout, KeepAlive: 30 * time.Second}).DialContext,
		fallback: http.DefaultTransport,
	}
	tmpl := base.Clone()
	// A private address never goes through a proxy.
	tmpl.Proxy = nil
	// HTTP/1.1 only: one multiplexed connection would pin a node to a single backend task.
	tmpl.ForceAttemptHTTP2 = false
	tmpl.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	// Every node client shares this transport, and one host takes all of them.
	tmpl.MaxIdleConns = 512
	tmpl.MaxIdleConnsPerHost = 512
	tmpl.DialContext = t.dialPlain
	tmpl.DialTLSContext = t.dialTLS
	t.template = tmpl
	t.gen.Store(&generation{rt: tmpl.Clone(), born: t.now()})
	return t, nil
}

func (t *originTransport) dialPlain(ctx context.Context, network, addr string) (net.Conn, error) {
	conn, err := t.dial(ctx, network, addr)
	if err != nil {
		return nil, dialError{err}
	}
	return conn, nil
}

func (t *originTransport) dialTLS(ctx context.Context, network, addr string) (net.Conn, error) {
	raw, err := t.dial(ctx, network, addr)
	if err != nil {
		return nil, dialError{err}
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	conn := tls.Client(raw, &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: t.roots, ServerName: host})
	hctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	if err := conn.HandshakeContext(hctx); err != nil {
		_ = raw.Close()
		return nil, dialError{err}
	}
	return conn, nil
}

// current is the live connection pool; past ConnMaxAge a fresh one takes over, so new connections re-resolve the name.
func (t *originTransport) current() *http.Transport {
	g := t.gen.Load()
	now := t.now()
	if now.Sub(g.born) < ConnMaxAge {
		return g.rt
	}
	next := &generation{rt: t.template.Clone(), born: now}
	if t.gen.CompareAndSwap(g, next) {
		// Connections still in flight return to the old pool and close after its idle timeout.
		g.rt.CloseIdleConnections()
		return next.rt
	}
	return t.gen.Load().rt
}

func (t *originTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !sameOrigin(req.URL, t.private) {
		return t.fallback.RoundTrip(req)
	}
	if t.public != nil && t.usePublic() {
		return t.toPublic(req, false)
	}
	retry := t.public != nil && (req.Body == nil || req.Body == http.NoBody || req.GetBody != nil)
	// The transport may retry internally on a fresh dial, so a final dial error alone does not prove nothing was sent.
	var wrote atomic.Bool
	traced := req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
		WroteHeaders: func() { wrote.Store(true) },
	}))
	resp, err := t.current().RoundTrip(traced)
	if err == nil {
		t.succeeded()
		return resp, nil
	}
	var de dialError
	if !errors.As(err, &de) {
		return nil, err
	}
	t.failed(err)
	if !retry || wrote.Load() {
		return nil, err
	}
	return t.toPublic(req, true)
}

func sameOrigin(u, origin *url.URL) bool {
	return strings.EqualFold(u.Scheme, origin.Scheme) && strings.EqualFold(u.Host, origin.Host)
}

func (t *originTransport) usePublic() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.now().Before(t.publicUntil)
}

func (t *originTransport) succeeded() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failures = 0
	if t.onPublic {
		t.onPublic = false
		t.logf("control plane: private address %s answers again; leaving the public address", t.private.Host)
	}
}

func (t *originTransport) failed(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failures++
	if t.failures < FallbackAfter || t.public == nil {
		return
	}
	t.failures = 0
	t.publicUntil = t.now().Add(FallbackFor)
	if !t.onPublic {
		t.onPublic = true
		t.logf("control plane: private address %s failed %d dials in a row (%v); using the public address for %s", t.private.Host, FallbackAfter, err, FallbackFor)
	}
}

// toPublic sends req to the public origin; resend is a retry after a private attempt, which needs a fresh body.
func (t *originTransport) toPublic(req *http.Request, resend bool) (*http.Response, error) {
	out := req.Clone(req.Context())
	out.Host = ""
	out.URL.Scheme = t.public.Scheme
	out.URL.Host = t.public.Host
	out.URL.Path = t.public.Path + strings.TrimPrefix(req.URL.Path, t.private.Path)
	if req.URL.RawPath != "" {
		out.URL.RawPath = t.public.EscapedPath() + strings.TrimPrefix(req.URL.RawPath, t.private.EscapedPath())
	}
	// A first send during a public window keeps the caller's body, so the transport closes it.
	if resend && req.Body != nil && req.Body != http.NoBody && req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		out.Body = body
	}
	return t.fallback.RoundTrip(out)
}
