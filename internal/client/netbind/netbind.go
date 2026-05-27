// Package netbind constructs outbound dialers with an optional source IP.
//
// Use cases:
//   - Self-hoster on a single-IP VPS: no env var set, default route is used.
//   - Hosted Warmbly on a multi-IP box: WORKER_BIND_IP set per process so
//     SMTP/IMAP/HTTPS egress leaves from the assigned IP.
//   - Multi-egress per process (future): callers pass an explicit *net.TCPAddr
//     overriding the env-var fallback.
package netbind

import (
	"crypto/tls"
	"net"
	"os"
	"sync"
	"time"
)

const (
	envBindIP      = "WORKER_BIND_IP"
	defaultTimeout = 10 * time.Second
)

var (
	envOnce sync.Once
	envIP   *net.TCPAddr
)

// FromEnv returns the *net.TCPAddr derived from the WORKER_BIND_IP env var, or
// nil if unset/invalid. The lookup is cached for the lifetime of the process.
func FromEnv() *net.TCPAddr {
	envOnce.Do(func() {
		raw := os.Getenv(envBindIP)
		if raw == "" {
			return
		}
		ip := net.ParseIP(raw)
		if ip == nil {
			return
		}
		envIP = &net.TCPAddr{IP: ip}
	})
	return envIP
}

// Dialer returns a *net.Dialer bound to the given local address. If local is
// nil, the env var is consulted. If still unset, the OS default route is used.
func Dialer(local *net.TCPAddr) *net.Dialer {
	if local == nil {
		local = FromEnv()
	}
	return &net.Dialer{
		Timeout:   defaultTimeout,
		LocalAddr: local,
	}
}

// TLSDialer mirrors Dialer for TLS connections (IMAP, HTTPS).
func TLSDialer(local *net.TCPAddr, cfg *tls.Config) *tls.Dialer {
	return &tls.Dialer{
		NetDialer: Dialer(local),
		Config:    cfg,
	}
}
