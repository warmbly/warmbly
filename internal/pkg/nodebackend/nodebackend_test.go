package nodebackend

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/pkg/servetls"
)

const publicAddr = "https://api.example.com"

func healthy() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

func counting(n *atomic.Int32) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		_, _ = io.WriteString(w, "ok")
	})
}

func caOf(srv *httptest.Server) string {
	return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))
}

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func get(client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

// mustGet reads the body to the end, so the connection goes back to the pool.
func mustGet(t *testing.T, client *http.Client, url string) {
	t.Helper()
	resp, err := get(client, url)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func resolve(t *testing.T, m map[string]string) Backend {
	t.Helper()
	return Resolve(context.Background(), env(m), publicAddr, t.Logf)
}

type clock struct {
	mu sync.Mutex
	at time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

type logs struct {
	mu    sync.Mutex
	lines []string
}

func (l *logs) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *logs) count(substr string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, line := range l.lines {
		if strings.Contains(line, substr) {
			n++
		}
	}
	return n
}

// testBackend is a Backend on a fake clock whose private dials go through dial.
func testBackend(t *testing.T, privateURL, ca, public string, dial func(ctx context.Context, network, addr string) (net.Conn, error)) (Backend, *originTransport, *clock, *logs) {
	t.Helper()
	l := &logs{}
	b, err := New(privateURL, ca, public, l.logf)
	if err != nil {
		t.Fatal(err)
	}
	clk := &clock{at: time.Unix(1_700_000_000, 0)}
	ot := b.transport
	ot.now = clk.now
	ot.dial = dial
	ot.gen.Store(&generation{rt: ot.template.Clone(), born: clk.now()})
	return b, ot, clk, l
}

func dialTo(addr string, n *atomic.Int32) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, _ string) (net.Conn, error) {
		n.Add(1)
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
}

func refused(context.Context, string, string) (net.Conn, error) {
	return nil, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
}

func TestResolveUsesPrivateAddressServedUnderThePinnedCA(t *testing.T) {
	srv := httptest.NewTLSServer(healthy())
	defer srv.Close()

	b := resolve(t, map[string]string{EnvInternalURL: srv.URL + "/", EnvInternalCAB64: caOf(srv)})
	if !b.Private() || b.BaseURL(publicAddr) != srv.URL {
		t.Fatalf("BaseURL = %q, want the private %q", b.BaseURL(publicAddr), srv.URL)
	}
	mustGet(t, b.Client(time.Second), srv.URL+"/health")
}

func TestResolveRefusesACertificateFromAnotherCA(t *testing.T) {
	cert, err := servetls.SelfSigned(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(healthy())
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	defer srv.Close()
	other := httptest.NewTLSServer(healthy())
	defer other.Close()

	b := resolve(t, map[string]string{EnvInternalURL: srv.URL, EnvInternalCAB64: caOf(other)})
	if b.Private() || b.BaseURL(publicAddr) != publicAddr {
		t.Fatal("a server whose certificate the pinned CA did not issue was trusted")
	}
}

func TestResolveKeepsThePublicAddress(t *testing.T) {
	srv := httptest.NewTLSServer(healthy())
	ca := caOf(srv)
	failing := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer failing.Close()
	closed := srv.URL
	srv.Close()

	for name, m := range map[string]map[string]string{
		"unset":       {},
		"unreachable": {EnvInternalURL: closed, EnvInternalCAB64: ca},
		"unhealthy":   {EnvInternalURL: failing.URL, EnvInternalCAB64: caOf(failing)},
		"bad ca":      {EnvInternalURL: failing.URL, EnvInternalCAB64: "%%%"},
		"not a url":   {EnvInternalURL: "backend:8443"},
	} {
		b := resolve(t, m)
		if b.Private() || b.BaseURL(publicAddr) != publicAddr || b.Transport() != nil {
			t.Errorf("%s: switched away from the public address", name)
		}
	}
}

func TestPinnedRootsApplyOnlyToThePrivateOrigin(t *testing.T) {
	private := httptest.NewTLSServer(healthy())
	defer private.Close()
	elsewhere := httptest.NewTLSServer(healthy())
	defer elsewhere.Close()

	b, err := New(private.URL, caOf(private), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	client := b.Client(time.Second)
	mustGet(t, client, private.URL+"/health")
	// Same certificate, other origin: only the system roots apply there, so it is refused.
	if resp, err := get(client, elsewhere.URL+"/health"); err == nil {
		_ = resp.Body.Close()
		t.Fatal("the pinned CA was trusted for an origin it was not pinned to")
	}
}

// Two backend tasks behind one name: a connection outliving ConnMaxAge is replaced, so the second task gets traffic.
func TestConnectionsAreRecycledSoEveryTaskGetsTraffic(t *testing.T) {
	var hitsA, hitsB, dials atomic.Int32
	a := httptest.NewTLSServer(counting(&hitsA))
	defer a.Close()
	b := httptest.NewTLSServer(counting(&hitsB))
	defer b.Close()
	tasks := []string{a.Listener.Addr().String(), b.Listener.Addr().String()}
	// Each new connection resolves to the next task, as successive Cloud Map answers do.
	nextTask := func(ctx context.Context, network, _ string) (net.Conn, error) {
		n := dials.Add(1) - 1
		return (&net.Dialer{}).DialContext(ctx, network, tasks[n%2])
	}
	be, _, clk, _ := testBackend(t, "https://example.com:8443", caOf(a), "", nextTask)
	client := be.Client(time.Second)

	for range 5 {
		mustGet(t, client, "https://example.com:8443/api/v1/internal/x")
	}
	clk.advance(ConnMaxAge - time.Second)
	mustGet(t, client, "https://example.com:8443/api/v1/internal/x")
	if hitsA.Load() != 6 || hitsB.Load() != 0 || dials.Load() != 1 {
		t.Fatalf("before ConnMaxAge: a=%d b=%d dials=%d, want one kept-alive connection to a", hitsA.Load(), hitsB.Load(), dials.Load())
	}
	clk.advance(2 * time.Second)
	for range 5 {
		mustGet(t, client, "https://example.com:8443/api/v1/internal/x")
	}
	if hitsB.Load() != 5 || dials.Load() != 2 {
		t.Fatalf("after ConnMaxAge: b=%d dials=%d, want a fresh connection to b", hitsB.Load(), dials.Load())
	}
}

// A request that never reached the private address is sent once to the public one, origin rewritten and nothing else.
func TestDialFailureResendsTheRequestToThePublicAddress(t *testing.T) {
	type seen struct{ method, uri, header, body string }
	var mu sync.Mutex
	var got []seen
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, seen{r.Method, r.URL.RequestURI(), r.Header.Get("X-Warmbly-Node-Id"), string(body)})
		mu.Unlock()
	}))
	defer public.Close()
	ca := httptest.NewTLSServer(healthy())
	defer ca.Close()
	be, _, _, _ := testBackend(t, "https://example.com:8443", caOf(ca), public.URL, refused)
	client := be.Client(time.Second)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com:8443/api/v1/internal/sync/x?a=1&b=2", nil)
	req.Header.Set("X-Warmbly-Node-Id", "node-1")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET was not resent: %v", err)
	}
	_ = resp.Body.Close()

	req, _ = http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com:8443/api/v1/internal/dek/decrypt", bytes.NewReader([]byte(`{"k":1}`)))
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("POST with a replayable body was not resent: %v", err)
	}
	_ = resp.Body.Close()

	// A body that cannot be replayed is not resent.
	req, _ = http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com:8443/api/v1/internal/x", io.NopCloser(strings.NewReader("once")))
	if resp, err := client.Do(req); err == nil {
		_ = resp.Body.Close()
		t.Fatal("a request without GetBody was resent")
	}

	mu.Lock()
	defer mu.Unlock()
	want := []seen{
		{http.MethodGet, "/api/v1/internal/sync/x?a=1&b=2", "node-1", ""},
		{http.MethodPost, "/api/v1/internal/dek/decrypt", "", `{"k":1}`},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("public side saw %v, want %v", got, want)
	}
}

// Once the request may have reached the backend, the error is returned, never resent.
func TestFailureAfterTheRequestWasWrittenIsNotResent(t *testing.T) {
	var publicHits atomic.Int32
	public := httptest.NewServer(counting(&publicHits))
	defer public.Close()
	private := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer private.Close()
	var dials atomic.Int32
	be, ot, _, _ := testBackend(t, private.URL, caOf(private), public.URL, dialTo(private.Listener.Addr().String(), &dials))

	for range FallbackAfter + 1 {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, private.URL+"/api/v1/internal/x", bytes.NewReader([]byte("send")))
		if resp, err := be.Client(time.Second).Do(req); err == nil {
			_ = resp.Body.Close()
			t.Fatal("a dropped connection read as success")
		}
	}
	if publicHits.Load() != 0 || ot.usePublic() {
		t.Fatalf("a written request reached the public side (%d) or opened a public window", publicHits.Load())
	}
}

// After FallbackAfter dial failures in a row everything goes public for FallbackFor, then the private address is tried again.
func TestRepeatedDialFailuresOpenAPublicWindow(t *testing.T) {
	var publicHits, privateHits, dials atomic.Int32
	public := httptest.NewServer(counting(&publicHits))
	defer public.Close()
	private := httptest.NewTLSServer(counting(&privateHits))
	defer private.Close()
	var down atomic.Bool
	down.Store(true)
	reach := dialTo(private.Listener.Addr().String(), &dials)
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		if down.Load() {
			dials.Add(1)
			return refused(ctx, network, addr)
		}
		return reach(ctx, network, addr)
	}
	be, _, clk, l := testBackend(t, "https://example.com:8443", caOf(private), public.URL, dial)
	client := be.Client(time.Second)
	const u = "https://example.com:8443/api/v1/internal/fleet/heartbeat"

	for range FallbackAfter {
		mustGet(t, client, u)
	}
	if dials.Load() != FallbackAfter || publicHits.Load() != FallbackAfter {
		t.Fatalf("dials=%d public=%d, want each failed dial resent", dials.Load(), publicHits.Load())
	}
	down.Store(false)
	mustGet(t, client, u)
	clk.advance(FallbackFor - time.Second)
	mustGet(t, client, u)
	if dials.Load() != FallbackAfter || publicHits.Load() != FallbackAfter+2 {
		t.Fatalf("inside the window: dials=%d public=%d, want no private attempt", dials.Load(), publicHits.Load())
	}
	clk.advance(2 * time.Second)
	mustGet(t, client, u)
	mustGet(t, client, u)
	if privateHits.Load() != 2 || publicHits.Load() != FallbackAfter+2 {
		t.Fatalf("after the window: private=%d public=%d, want the private address back", privateHits.Load(), publicHits.Load())
	}
	if l.count("using the public address") != 1 || l.count("answers again") != 1 {
		t.Fatalf("each switch is logged once, got %q", l.lines)
	}
}
