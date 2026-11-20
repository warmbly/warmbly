package nodebackend

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/pkg/servetls"
)

func healthy() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
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

func resolve(t *testing.T, m map[string]string) Backend {
	t.Helper()
	return Resolve(context.Background(), env(m), t.Logf)
}

func TestResolveUsesPrivateAddressServedUnderThePinnedCA(t *testing.T) {
	srv := httptest.NewTLSServer(healthy())
	defer srv.Close()

	b := resolve(t, map[string]string{EnvInternalURL: srv.URL + "/", EnvInternalCAB64: caOf(srv)})
	if !b.Private() || b.BaseURL("https://api.example.com") != srv.URL {
		t.Fatalf("BaseURL = %q, want the private %q", b.BaseURL("https://api.example.com"), srv.URL)
	}
	resp, err := get(b.Client(time.Second), srv.URL+"/health")
	if err != nil {
		t.Fatalf("the shared client cannot reach the private address: %v", err)
	}
	_ = resp.Body.Close()
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
	if b.Private() || b.BaseURL("https://api.example.com") != "https://api.example.com" {
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
		if b.Private() || b.BaseURL("https://api.example.com") != "https://api.example.com" || b.Transport() != nil {
			t.Errorf("%s: switched away from the public address", name)
		}
	}
}

func TestPinnedRootsApplyOnlyToThePrivateOrigin(t *testing.T) {
	private := httptest.NewTLSServer(healthy())
	defer private.Close()
	elsewhere := httptest.NewTLSServer(healthy())
	defer elsewhere.Close()

	b, err := New(private.URL, caOf(private))
	if err != nil {
		t.Fatal(err)
	}
	client := b.Client(time.Second)
	if resp, err := get(client, private.URL+"/health"); err != nil {
		t.Fatalf("private origin: %v", err)
	} else {
		_ = resp.Body.Close()
	}
	// Same certificate, other origin: only the system roots apply there, so it is refused.
	if resp, err := get(client, elsewhere.URL+"/health"); err == nil {
		_ = resp.Body.Close()
		t.Fatal("the pinned CA was trusted for an origin it was not pinned to")
	}
}
