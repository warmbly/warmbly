// Package servetls adds an opt-in TLS listener beside a service's plain HTTP one; unset, nothing changes.
package servetls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Mode is where the TLS listener's certificate comes from.
type Mode string

const (
	ModeOff        Mode = ""
	ModeSelfSigned Mode = "self-signed"
	ModeSupplied   Mode = "supplied"
)

// Config is the listener shape read from the environment.
type Config struct {
	Mode             Mode
	Port             int
	CertPEM          string
	CertFile         string
	KeyPEM           string
	KeyFile          string
	HTTPLoopbackOnly bool
}

// FromEnv reads SERVE_TLS, SERVE_TLS_PORT, SERVE_TLS_CERT(_FILE), SERVE_TLS_KEY(_FILE) and SERVE_HTTP_LOOPBACK_ONLY.
func FromEnv(getenv func(string) string) (Config, error) {
	get := func(k string) string { return strings.TrimSpace(getenv(k)) }
	var c Config
	if raw := get("SERVE_HTTP_LOOPBACK_ONLY"); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("SERVE_HTTP_LOOPBACK_ONLY must be true or false, got %q", raw)
		}
		c.HTTPLoopbackOnly = v
	}
	switch mode := Mode(strings.ToLower(get("SERVE_TLS"))); mode {
	case ModeOff, "off", "false":
		if c.HTTPLoopbackOnly {
			return Config{}, errors.New("SERVE_HTTP_LOOPBACK_ONLY=true needs SERVE_TLS, or nothing outside the container can reach the service")
		}
		return c, nil
	case ModeSelfSigned, ModeSupplied:
		c.Mode = mode
	default:
		return Config{}, fmt.Errorf("SERVE_TLS must be self-signed or supplied, got %q", mode)
	}
	port, err := strconv.Atoi(get("SERVE_TLS_PORT"))
	if err != nil || port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("SERVE_TLS=%s needs SERVE_TLS_PORT set to a port number", c.Mode)
	}
	c.Port = port
	if c.Mode == ModeSelfSigned {
		return c, nil
	}
	c.CertPEM, c.CertFile = get("SERVE_TLS_CERT"), get("SERVE_TLS_CERT_FILE")
	c.KeyPEM, c.KeyFile = get("SERVE_TLS_KEY"), get("SERVE_TLS_KEY_FILE")
	if (c.CertPEM == "") == (c.CertFile == "") {
		return Config{}, errors.New("SERVE_TLS=supplied needs exactly one of SERVE_TLS_CERT and SERVE_TLS_CERT_FILE")
	}
	if (c.KeyPEM == "") == (c.KeyFile == "") {
		return Config{}, errors.New("SERVE_TLS=supplied needs exactly one of SERVE_TLS_KEY and SERVE_TLS_KEY_FILE")
	}
	return c, nil
}

// Enabled reports whether a TLS listener is configured.
func (c Config) Enabled() bool { return c.Mode != ModeOff }

// PlainAddr is the plain listener's address, moved to loopback when confined.
func (c Config) PlainAddr(addr string) string {
	if !c.HTTPLoopbackOnly {
		return addr
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		port = strings.TrimPrefix(addr, ":")
	}
	return net.JoinHostPort("127.0.0.1", port)
}

// TLSAddr is the TLS listener's address, on every interface.
func (c Config) TLSAddr() string { return ":" + strconv.Itoa(c.Port) }

// TLSConfig is the server configuration for the TLS listener: TLS 1.2 or later.
func (c Config) TLSConfig(now time.Time) (*tls.Config, error) {
	var (
		cert tls.Certificate
		err  error
	)
	switch c.Mode {
	case ModeSupplied:
		cert, err = c.supplied()
	case ModeSelfSigned:
		cert, err = SelfSigned(now)
	default:
		return nil, errors.New("servetls: TLS is not enabled")
	}
	if err != nil {
		return nil, fmt.Errorf("servetls: %w", err)
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}, nil
}

func (c Config) supplied() (tls.Certificate, error) {
	certPEM, err := pemValue(c.CertPEM, c.CertFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("reading the certificate: %w", err)
	}
	keyPEM, err := pemValue(c.KeyPEM, c.KeyFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("reading the key: %w", err)
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, errors.New("the supplied certificate and key are not a matching PEM pair")
	}
	return cert, nil
}

func pemValue(inline, file string) ([]byte, error) {
	if inline != "" {
		return []byte(inline), nil
	}
	return os.ReadFile(file)
}

// SelfSigned makes an in-memory ECDSA P-256 certificate; a load balancer does not validate its targets'.
func SelfSigned(now time.Time) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "localhost"},
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(1, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, nil
}

// Expiry is when the served certificate stops being valid, or zero when TLS is off.
func Expiry(cfg *tls.Config) time.Time {
	if cfg == nil || len(cfg.Certificates) == 0 {
		return time.Time{}
	}
	cert := cfg.Certificates[0]
	if cert.Leaf != nil {
		return cert.Leaf.NotAfter
	}
	if len(cert.Certificate) == 0 {
		return time.Time{}
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return time.Time{}
	}
	return leaf.NotAfter
}

// Servers builds the plain server and, when enabled, the TLS server, both on one handler.
func (c Config) Servers(addr string, handler http.Handler) (plain, secure *http.Server, err error) {
	plain = &http.Server{Addr: c.PlainAddr(addr), Handler: handler}
	if !c.Enabled() {
		return plain, nil, nil
	}
	tlsConfig, err := c.TLSConfig(time.Now())
	if err != nil {
		return nil, nil, err
	}
	secure = &http.Server{Addr: c.TLSAddr(), Handler: handler, TLSConfig: tlsConfig}
	return plain, secure, nil
}

// ListenAndServe runs a server built by Servers, choosing TLS when it carries a TLS config.
func ListenAndServe(srv *http.Server) error {
	if srv.TLSConfig != nil {
		return srv.ListenAndServeTLS("", "")
	}
	return srv.ListenAndServe()
}

// ClientRoots is the only pool a client trusts for one upstream, from inline PEM or a file; nil means the system roots.
func ClientRoots(inline, file string) (*x509.CertPool, error) {
	if inline == "" && file == "" {
		return nil, nil
	}
	data, err := pemValue(inline, file)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, errors.New("no PEM certificate found")
	}
	return pool, nil
}
