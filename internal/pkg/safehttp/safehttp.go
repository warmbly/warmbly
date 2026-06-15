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

// blockedHostnames are denied before DNS resolution: cloud-metadata service
// names that resolve to link-local IPs (covered by the IP check too, but denying
// the name closes a split-horizon-DNS gap) and bare localhost forms.
var blockedHostnames = map[string]bool{
	"localhost":                  true,
	"metadata.google.internal":   true,
	"metadata.goog":              true,
	"metadata.amazonaws.com":     true,
	"instance-data":              true,
	"instance-data.ec2.internal": true,
}

// isBlockedHostname reports whether a hostname must be refused pre-resolution.
func isBlockedHostname(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[].")
	if host == "" {
		return true
	}
	if blockedHostnames[host] || strings.HasSuffix(host, ".localhost") {
		return true
	}
	return false
}

// IsBlockedIP reports whether an IP must never be dialed for a user-supplied URL:
// loopback, RFC1918 private, IPv6 ULA, link-local (includes the 169.254.169.254
// cloud metadata endpoint), multicast, unspecified, carrier-grade NAT, the
// 0.0.0.0/8 "this network" range, plus documentation/benchmark/reserved ranges.
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
		switch {
		case v4[0] == 0: // 0.0.0.0/8 "this network"
			return true
		case v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127: // 100.64.0.0/10 CGNAT
			return true
		case v4[0] == 192 && v4[1] == 0 && v4[2] == 0: // 192.0.0.0/24 IETF protocol assignments
			return true
		case v4[0] == 192 && v4[1] == 0 && v4[2] == 2: // 192.0.2.0/24 TEST-NET-1
			return true
		case v4[0] == 198 && v4[1] == 51 && v4[2] == 100: // 198.51.100.0/24 TEST-NET-2
			return true
		case v4[0] == 203 && v4[1] == 0 && v4[2] == 113: // 203.0.113.0/24 TEST-NET-3
			return true
		case v4[0] == 192 && v4[1] == 88 && v4[2] == 99: // 192.88.99.0/24 6to4 relay anycast
			return true
		case v4[0] == 198 && (v4[1] == 18 || v4[1] == 19): // 198.18.0.0/15 benchmarking
			return true
		case v4[0] >= 240: // 240.0.0.0/4 reserved + 255.255.255.255 broadcast
			return true
		}
	}
	return false
}

// portAllowed restricts the dial to web ports. Only 443 and 8443 are reachable
// for user-supplied URLs (so a host that passes the IP check still cannot reach
// 22/3306/6379/9200/etc.); the dev flag opens any port for local testing.
func portAllowed(port string) bool {
	if allowUnsafe() {
		return true
	}
	return port == "443" || port == "8443"
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
		if isBlockedHostname(host) {
			log.Warn().Str("host", host).Msg("safehttp: blocked request to a denied hostname")
			return nil, ErrBlockedAddress
		}
		if !portAllowed(port) {
			log.Warn().Str("host", host).Str("port", port).Msg("safehttp: blocked request to a non-web port")
			return nil, ErrBlockedAddress
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
		ResponseHeaderTimeout: 10 * time.Second,
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
