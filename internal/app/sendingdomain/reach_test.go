package sendingdomain

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

const reachTarget = "https://www.acme.com"

// closedAddr is a local port nothing listens on, so dialing it is refused.
func closedAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

// reachFixture serves the domain's http and https sides from local servers; nil means nothing listens.
func reachFixture(t *testing.T, plain, secure http.HandlerFunc, trusted bool) *httpReach {
	t.Helper()
	plainAddr, secureAddr := closedAddr(t), closedAddr(t)
	tr := &http.Transport{TLSClientConfig: &tls.Config{}}
	if plain != nil {
		srv := httptest.NewServer(plain)
		t.Cleanup(srv.Close)
		plainAddr = srv.Listener.Addr().String()
	}
	if secure != nil {
		srv := httptest.NewTLSServer(secure)
		t.Cleanup(srv.Close)
		secureAddr = srv.Listener.Addr().String()
		if trusted {
			pool := x509.NewCertPool()
			pool.AddCert(srv.Certificate())
			tr.TLSClientConfig.RootCAs = pool
		}
	}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		_, port, _ := net.SplitHostPort(addr)
		to := plainAddr
		if port == "443" {
			to = secureAddr
		}
		return (&net.Dialer{}).DialContext(ctx, network, to)
	}
	return &httpReach{transport: tr}
}

func redirectTo(loc string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, loc, http.StatusFound) }
}

func TestReachProbeNamesWhatAVisitorGets(t *testing.T) {
	// httptest's certificate is issued for example.com.
	const domain = "example.com"
	notFound := func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) }
	trackingNotFound := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(TrackingServiceHeader, "tracking")
		w.WriteHeader(http.StatusNotFound)
	}
	upgrade := redirectTo("https://example.com/")
	// Tracking's own answers carry its header: a stale cached target, a miss under the right name, a lookup outage.
	trackingStale := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(TrackingServiceHeader, "tracking")
		http.Redirect(w, r, "https://old.acme.com/", http.StatusFound)
	}
	trackingMiss := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(TrackingServiceHeader, "tracking")
		w.Header().Set(TrackingHostHeader, "example.com")
		w.WriteHeader(http.StatusNotFound)
	}
	badGateway := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Server", "nginx")
		w.WriteHeader(http.StatusBadGateway)
	}
	trackingDown := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(TrackingServiceHeader, "tracking")
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	cases := []struct {
		name          string
		plain, secure http.HandlerFunc
		trusted       bool
		status        models.RedirectReachStatus
		hint          models.RedirectReachHint
	}{
		{"both schemes redirect", redirectTo(reachTarget), redirectTo(reachTarget + "/"), true, models.RedirectReachOK, models.RedirectHintNone},
		{"a proxy upgrades to https first", upgrade, redirectTo(reachTarget), true, models.RedirectReachOK, models.RedirectHintNone},
		{"a proxy answers itself", notFound, notFound, true, models.RedirectReachNotReaching, models.RedirectHintNotRouted},
		{"tracking gets another hostname", trackingNotFound, trackingNotFound, true, models.RedirectReachNotReaching, models.RedirectHintHostHeader},
		{"it redirects elsewhere", redirectTo("https://parked.example.net/"), redirectTo(reachTarget), true, models.RedirectReachNotReaching, models.RedirectHintWrongTarget},
		{"https has no valid certificate", redirectTo(reachTarget), redirectTo(reachTarget), false, models.RedirectReachHTTPSError, models.RedirectHintCertificate},
		{"nothing on port 80", nil, redirectTo(reachTarget), true, models.RedirectReachNotReaching, models.RedirectHintNoListener},
		{"nothing on port 443", redirectTo(reachTarget), nil, true, models.RedirectReachHTTPSError, models.RedirectHintNoListener},
		{"nothing listens on either port", nil, nil, true, models.RedirectReachNotReaching, models.RedirectHintNoListener},
		{"tracking still redirects to the old target", trackingStale, trackingStale, true, models.RedirectReachOK, models.RedirectHintNone},
		{"tracking has not picked the redirect up", trackingMiss, trackingMiss, true, models.RedirectReachUnreachable, models.RedirectHintSettling},
		{"tracking could not look it up", trackingDown, trackingDown, true, models.RedirectReachUnreachable, models.RedirectHintNone},
		{"the proxy's upstream is down", badGateway, badGateway, true, models.RedirectReachUnreachable, models.RedirectHintNone},
		{"https works while http settles", trackingMiss, redirectTo(reachTarget), true, models.RedirectReachUnreachable, models.RedirectHintSettling},
		{"http only upgrades to a bad certificate", upgrade, redirectTo(reachTarget), false, models.RedirectReachHTTPSError, models.RedirectHintCertificate},
		{"nothing on port 80 and a bad certificate", nil, redirectTo(reachTarget), false, models.RedirectReachNotReaching, models.RedirectHintNoListener},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := reachFixture(t, c.plain, c.secure, c.trusted).Probe(context.Background(), domain, reachTarget)
			if got.Status != c.status || got.Hint != c.hint {
				t.Fatalf("got %s/%s (%s), want %s/%s", got.Status, got.Hint, got.Detail, c.status, c.hint)
			}
			if got.Status != models.RedirectReachOK && got.Detail == "" {
				t.Fatal("a failed check says nothing about what it saw")
			}
		})
	}
}

func TestReachDoesNotBlameARedirectItCouldNotOpen(t *testing.T) {
	// A server that cannot open its own public address times out rather than being refused.
	p := &httpReach{transport: &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("i/o timeout")
	}}}
	got := p.Probe(context.Background(), "example.com", reachTarget)
	if got.Status != models.RedirectReachUnreachable || got.Detail == "" {
		t.Fatalf("an unanswered check = %+v", got)
	}
}

func TestSameTargetIgnoresOnlyWhatAVisitorWouldNot(t *testing.T) {
	for _, pair := range [][2]string{{"https://acme.com", "https://ACME.com/"}, {"https://acme.com/a/", "https://acme.com/a"}} {
		if !sameTarget(pair[0], pair[1]) {
			t.Errorf("%q and %q differ", pair[0], pair[1])
		}
	}
	for _, pair := range [][2]string{{"https://acme.com", "http://acme.com"}, {"https://acme.com/a", "https://acme.com/b"}, {"https://acme.com/?x=1", "https://acme.com/"}} {
		if sameTarget(pair[0], pair[1]) {
			t.Errorf("%q and %q match", pair[0], pair[1])
		}
	}
}

func TestReachNamesTheProxyThatAnsweredInstead(t *testing.T) {
	notFound := func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) }
	nginx := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Server", "nginx/1.25.3")
		w.WriteHeader(http.StatusNotFound)
	}
	got := reachFixture(t, notFound, notFound, true).Probe(context.Background(), "example.com", reachTarget)
	if got.Proxy != "traefik" || !strings.Contains(got.Detail, "Traefik") {
		t.Fatalf("Traefik's 404 was not recognised: %+v", got)
	}
	if got = reachFixture(t, nginx, nginx, true).Probe(context.Background(), "example.com", reachTarget); got.Proxy != "nginx" {
		t.Fatalf("nginx was not recognised: %+v", got)
	}

	// Traefik's placeholder certificate names it on https even when http redirects fine.
	srv := httptest.NewUnstartedServer(redirectTo(reachTarget))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{selfSigned(t, "TRAEFIK DEFAULT CERT")}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	plain := httptest.NewServer(redirectTo(reachTarget))
	t.Cleanup(plain.Close)
	p := &httpReach{transport: &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		to := plain.Listener.Addr().String()
		if _, port, _ := net.SplitHostPort(addr); port == "443" {
			to = srv.Listener.Addr().String()
		}
		return (&net.Dialer{}).DialContext(ctx, network, to)
	}}}
	if got = p.Probe(context.Background(), "example.com", reachTarget); got.Hint != models.RedirectHintCertificate || got.Proxy != "traefik" {
		t.Fatalf("Traefik's default certificate was not recognised: %+v", got)
	}
	caddy := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "Caddy")
		http.Redirect(w, r, reachTarget, http.StatusFound)
	}
	if got = reachFixture(t, caddy, caddy, true).Probe(context.Background(), "example.com", reachTarget); got.Status != models.RedirectReachOK || got.Proxy != "" {
		t.Fatalf("a working redirect names a proxy: %+v", got)
	}
}

func selfSigned(t *testing.T, cn string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func TestReachReadsCloudflareRefusalsAsADNSFix(t *testing.T) {
	cloudflare := func(status int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Server", "cloudflare")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}
	}
	// The browser page carries the code in its feedback script; a plain client gets one line.
	prohibited := cloudflare(http.StatusForbidden, `<title>DNS points to prohibited IP | example.com | Cloudflare</title><script>a={event:"feedback clicked",properties:{errorCode: 1000 }}</script>`)
	crossUser := cloudflare(http.StatusConflict, "error code: 1014")
	other := cloudflare(http.StatusForbidden, "error code: 1020")
	cases := []struct {
		name   string
		plain  http.HandlerFunc
		hint   models.RedirectReachHint
		detail string
	}{
		{"root A records at Cloudflare's addresses", prohibited, models.RedirectHintEdgeRecord, "error 1000"},
		{"a name Cloudflare has not activated", crossUser, models.RedirectHintEdgeInactive, "error 1014"},
		{"a Cloudflare refusal with no DNS fix", other, models.RedirectHintNotRouted, "Cloudflare"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := reachFixture(t, c.plain, c.plain, false).Probe(context.Background(), "example.com", reachTarget)
			if got.Status != models.RedirectReachNotReaching || got.Hint != c.hint || !strings.Contains(got.Detail, c.detail) || got.Proxy != "cloudflare" {
				t.Fatalf("got %+v", got)
			}
		})
	}
}
