// Package whdomain implements the per-OAuth-app allowed-webhook-domain model:
// normalizing the entries an app declares, and matching a webhook URL's host
// against them. A leading-dot entry (".acme.com") is subdomain-inclusive and
// also matches the apex; a bare entry ("acme.com") matches that exact host only.
//
// Kept dependency-free and standalone so the OAuth service (validation on write)
// and the webhook handler + delivery worker (enforcement on create/deliver) can
// all share one matcher without an import cycle.
package whdomain

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Normalize validates and canonicalizes a single allowed-domain entry to a
// lowercase host, preserving a leading dot. It accepts a bare host
// ("acme.com"), a leading-dot host (".acme.com"), or a full URL
// ("https://acme.com") and reduces it to the host. It rejects empty values,
// wildcards other than the leading dot, embedded paths/ports/credentials, bare
// IP literals, and single-label hosts (which would be dangerously broad).
func Normalize(raw string) (string, error) {
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "" {
		return "", fmt.Errorf("empty domain")
	}
	if strings.Contains(s, "*") {
		return "", fmt.Errorf("use a leading dot for subdomains (.example.com), not a wildcard: %s", raw)
	}
	dot := strings.HasPrefix(s, ".")
	if dot {
		s = s[1:]
	}
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil || u.Hostname() == "" {
			return "", fmt.Errorf("invalid domain: %s", raw)
		}
		s = u.Hostname()
	}
	// Strip anything past the host that slipped in.
	if i := strings.IndexAny(s, "/:?# "); i >= 0 {
		s = s[:i]
	}
	s = strings.Trim(s, ".")
	if s == "" {
		return "", fmt.Errorf("invalid domain: %s", raw)
	}
	if net.ParseIP(s) != nil {
		return "", fmt.Errorf("provide a domain, not an IP address: %s", raw)
	}
	if !strings.Contains(s, ".") {
		return "", fmt.Errorf("a webhook domain must be fully qualified (e.g. hooks.acme.com): %s", raw)
	}
	if dot {
		return "." + s, nil
	}
	return s, nil
}

// NormalizeList normalizes every entry and de-duplicates, preserving order.
func NormalizeList(entries []string) ([]string, error) {
	out := make([]string, 0, len(entries))
	seen := map[string]bool{}
	for _, e := range entries {
		if strings.TrimSpace(e) == "" {
			continue
		}
		n, err := Normalize(e)
		if err != nil {
			return nil, err
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out, nil
}

// HostAllowed reports whether host is permitted by the allowlist. Matching is
// case-insensitive on the host. A leading-dot entry matches the apex and any
// subdomain; a bare entry matches the exact host only. An empty allowlist denies
// everything (an app with no declared domains cannot register webhooks).
func HostAllowed(host string, domains []string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" {
		return false
	}
	for _, d := range domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" {
			continue
		}
		if strings.HasPrefix(d, ".") {
			base := d[1:]
			if host == base || strings.HasSuffix(host, "."+base) {
				return true
			}
			continue
		}
		if host == d {
			return true
		}
	}
	return false
}
