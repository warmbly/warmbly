package servetls

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestFromEnv(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr string
	}{
		{name: "unset is plain", env: nil, want: Config{}},
		{name: "off is plain", env: map[string]string{"SERVE_TLS": "off"}, want: Config{}},
		{name: "self-signed", env: map[string]string{"SERVE_TLS": "self-signed", "SERVE_TLS_PORT": "8443"}, want: Config{Mode: ModeSelfSigned, Port: 8443}},
		{name: "loopback", env: map[string]string{"SERVE_TLS": "Self-Signed", "SERVE_TLS_PORT": "8443", "SERVE_HTTP_LOOPBACK_ONLY": "true"}, want: Config{Mode: ModeSelfSigned, Port: 8443, HTTPLoopbackOnly: true}},
		{name: "supplied files", env: map[string]string{"SERVE_TLS": "supplied", "SERVE_TLS_PORT": "443", "SERVE_TLS_CERT_FILE": "/c", "SERVE_TLS_KEY_FILE": "/k"}, want: Config{Mode: ModeSupplied, Port: 443, CertFile: "/c", KeyFile: "/k"}},
		{name: "supplied inline", env: map[string]string{"SERVE_TLS": "supplied", "SERVE_TLS_PORT": "443", "SERVE_TLS_CERT": "c", "SERVE_TLS_KEY": "k"}, want: Config{Mode: ModeSupplied, Port: 443, CertPEM: "c", KeyPEM: "k"}},
		{name: "supplied needs a key", env: map[string]string{"SERVE_TLS": "supplied", "SERVE_TLS_PORT": "443", "SERVE_TLS_CERT_FILE": "/c"}, wantErr: "SERVE_TLS_KEY"},
		{name: "supplied refuses two certs", env: map[string]string{"SERVE_TLS": "supplied", "SERVE_TLS_PORT": "443", "SERVE_TLS_CERT": "c", "SERVE_TLS_CERT_FILE": "/c", "SERVE_TLS_KEY": "k"}, wantErr: "exactly one of SERVE_TLS_CERT"},
		{name: "port required", env: map[string]string{"SERVE_TLS": "self-signed"}, wantErr: "SERVE_TLS_PORT"},
		{name: "port in range", env: map[string]string{"SERVE_TLS": "self-signed", "SERVE_TLS_PORT": "70000"}, wantErr: "SERVE_TLS_PORT"},
		{name: "unknown mode", env: map[string]string{"SERVE_TLS": "yes"}, wantErr: "SERVE_TLS must be"},
		{name: "loopback without tls", env: map[string]string{"SERVE_HTTP_LOOPBACK_ONLY": "true"}, wantErr: "needs SERVE_TLS"},
		{name: "loopback not a bool", env: map[string]string{"SERVE_HTTP_LOOPBACK_ONLY": "maybe"}, wantErr: "true or false"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := FromEnv(envOf(tc.env))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want one mentioning %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestPlainAddr(t *testing.T) {
	if got := (Config{}).PlainAddr("0.0.0.0:8080"); got != "0.0.0.0:8080" {
		t.Fatalf("open plain addr moved: %s", got)
	}
	confined := Config{Mode: ModeSelfSigned, Port: 8443, HTTPLoopbackOnly: true}
	for in, want := range map[string]string{"0.0.0.0:8080": "127.0.0.1:8080", ":8090": "127.0.0.1:8090", "8090": "127.0.0.1:8090"} {
		if got := confined.PlainAddr(in); got != want {
			t.Fatalf("PlainAddr(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSelfSignedTLSConfig(t *testing.T) {
	cfg, err := Config{Mode: ModeSelfSigned, Port: 8443}.TLSConfig(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Fatalf("MinVersion = %x, want TLS 1.2", cfg.MinVersion)
	}
	leaf := cfg.Certificates[0].Leaf
	if _, ok := leaf.PublicKey.(*ecdsa.PublicKey); !ok || leaf.PublicKey.(*ecdsa.PublicKey).Curve != elliptic.P256() {
		t.Fatalf("certificate key is %v, want ECDSA P-256", leaf.PublicKeyAlgorithm)
	}
	if exp := Expiry(cfg); !exp.After(time.Now().AddDate(0, 11, 0)) {
		t.Fatalf("certificate expires too soon: %s", exp)
	}
}

// internalPair is what an operator supplies: a self-signed, non-CA certificate naming the internal host.
func internalPair(t *testing.T, host string) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(7),
		Subject:               pkix.Name{CommonName: host},
		DNSNames:              []string{host},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

func TestSuppliedTLSConfig(t *testing.T) {
	certPEM, keyPEM := internalPair(t, "backend.warmbly.internal")
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]Config{
		"inline": {Mode: ModeSupplied, Port: 8443, CertPEM: string(certPEM), KeyPEM: string(keyPEM)},
		"files":  {Mode: ModeSupplied, Port: 8443, CertFile: certFile, KeyFile: keyFile},
		"mixed":  {Mode: ModeSupplied, Port: 8443, CertPEM: string(certPEM), KeyFile: keyFile},
	} {
		cfg, err := c.TLSConfig(time.Now())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if Expiry(cfg).IsZero() {
			t.Fatalf("%s: no expiry read from the supplied certificate", name)
		}
	}
	otherCert, _ := internalPair(t, "backend.warmbly.internal")
	if _, err := (Config{Mode: ModeSupplied, Port: 8443, CertPEM: string(otherCert), KeyPEM: string(keyPEM)}).TLSConfig(time.Now()); err == nil {
		t.Fatal("a certificate that does not match its key was accepted")
	}
	if _, err := (Config{Mode: ModeSupplied, Port: 8443, CertFile: filepath.Join(dir, "missing"), KeyFile: keyFile}).TLSConfig(time.Now()); err == nil {
		t.Fatal("a missing certificate file was accepted")
	}
}

func serveTLS(t *testing.T, c Config) string {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") })
	_, secure, err := c.Servers(":0", handler)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = secure.ServeTLS(ln, "", "") }()
	t.Cleanup(func() { _ = secure.Close() })
	return ln.Addr().String()
}

func get(client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	return string(body), err
}

func TestServersPlainOnly(t *testing.T) {
	plain, secure, err := Config{}.Servers(":8080", http.NotFoundHandler())
	if err != nil || plain == nil || secure != nil || plain.Addr != ":8080" {
		t.Fatalf("plain-only Servers = %v, %v, %v", plain, secure, err)
	}
}

func TestTLSListenerRefusesOldVersions(t *testing.T) {
	addr := serveTLS(t, Config{Mode: ModeSelfSigned, Port: 8443})
	old := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MaxVersion: tls.VersionTLS11}}}
	if _, err := get(old, "https://"+addr+"/"); err == nil {
		t.Fatal("TLS 1.1 handshake was accepted")
	}
	modern := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	if body, err := get(modern, "https://"+addr+"/"); err != nil || body != "ok" {
		t.Fatalf("TLS 1.2+ request = %q, %v", body, err)
	}
}

func TestPinnedClientVerifiesTheSuppliedCertificate(t *testing.T) {
	const host = "backend.warmbly.internal"
	certPEM, keyPEM := internalPair(t, host)
	addr := serveTLS(t, Config{Mode: ModeSupplied, Port: 8443, CertPEM: string(certPEM), KeyPEM: string(keyPEM)})
	_, port, _ := net.SplitHostPort(addr)

	client := func(roots *x509.CertPool) *http.Client {
		dialer := &net.Dialer{}
		return &http.Client{Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, addr)
			},
		}}
	}
	roots, err := ClientRoots(string(certPEM), "")
	if err != nil {
		t.Fatal(err)
	}
	if body, err := get(client(roots), "https://"+host+":"+port+"/"); err != nil || body != "ok" {
		t.Fatalf("pinned request = %q, %v", body, err)
	}
	if _, err := get(client(roots), "https://other.warmbly.internal:"+port+"/"); err == nil {
		t.Fatal("a certificate for another host name was accepted")
	}
	stranger, _ := internalPair(t, host)
	strangerRoots, err := ClientRoots(string(stranger), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := get(client(strangerRoots), "https://"+host+":"+port+"/"); err == nil {
		t.Fatal("a server certificate the client does not trust was accepted")
	}
}

func TestClientRoots(t *testing.T) {
	if pool, err := ClientRoots("", ""); pool != nil || err != nil {
		t.Fatalf("unset roots = %v, %v; want the system roots", pool, err)
	}
	if _, err := ClientRoots("not pem", ""); err == nil {
		t.Fatal("a value with no certificate was accepted")
	}
}
