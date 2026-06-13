// Package safehttp provides an HTTP client hardened against SSRF for requests to
// user-supplied URLs (webhook deliveries, the HTTP-request automation action,
// integration action webhooks).
//
// The literal-IP check most apps ship is not enough: a hostname like evil.com
// can have a DNS A-record pointing at 169.254.169.254 (cloud metadata), 10.x, or
// 127.0.0.1, and DNS rebinding can flip a host from public at validation time to
// private at fetch time. So the guard runs at DIAL time: it resolves the host,
// refuses to connect if ANY resolved address is non-public, and dials the
// validated IP directly (no second lookup), which closes the rebinding window.
// TLS still uses the original hostname for SNI/cert verification, so HTTPS is
// unaffected. Redirects are re-validated and capped.
package safehttp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// ErrBlockedAddress is returned when a request targets a non-public address.
var ErrBlockedAddress = errors.New("destination address is not publicly routable")

// allowUnsafe mirrors the existing WARMBLY_ALLOW_UNSAFE_WEBHOOK_URLS escape hatch
// so local/self-hosted development can reach private hosts.
func allowUnsafe() bool {
	return strings.EqualFold(os.Getenv("WARMBLY_ALLOW_UNSAFE_WEBHOOK_URLS"), "true")
}

// IsBlockedIP reports whether an IP must never be dialed for a user-supplied URL:
// loopback, RFC1918 private, IPv6 ULA, link-local (includes the 169.254.169.254
// cloud metadata endpoint), multicast, unspecified, carrier-grade NAT, and the
// 0.0.0.0/8 "this network" range.
func IsBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		// 0.0.0.0/8 "this network" and 100.64.0.0/10 carrier-grade NAT.
		if v4[0] == 0 {
			return true
		}
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return true
		}
	}
	return false
}

// safeDialContext resolves addr's host, blocks the connection if any resolved IP
// is non-public, and dials the validated IP directly so no rebinding can occur
// between validation and connect.
func safeDialContext(dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if allowUnsafe() {
			return dialer.DialContext(ctx, network, addr)
		}
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, ErrBlockedAddress
		}
		// Fail closed: one private answer in a mixed result set is enough to block,
		// so an attacker can't slip a private IP past us alongside a public one.
		for _, ip := range ips {
			if IsBlockedIP(ip) {
				log.Warn().Str("host", host).Str("ip", ip.String()).Msg("safehttp: blocked SSRF attempt to non-public address")
				return nil, ErrBlockedAddress
			}
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
}

// Client returns an SSRF-hardened *http.Client with the given overall timeout.
func Client(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext:           safeDialContext(dialer),
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			// The dialer re-checks the IP on every hop; also keep the scheme safe.
			if !allowUnsafe() && req.URL.Scheme != "https" {
				return fmt.Errorf("insecure redirect to %s", req.URL.Scheme)
			}
			return nil
		},
	}
}
