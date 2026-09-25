package config

import (
	"context"
	"net"
	"os"
	"strings"
)

// TrackingHost is the host open pixels and click links are built from, taken
// from TRACKING_DOMAIN, and the CNAME target a customer's own tracking
// subdomain has to point at.
//
// The template helpers used to fall back to a hardcoded track.warmbly.com,
// which is the same class of bug AppBaseURL fixed: a self-hosted install mailed
// its recipients links pointing at someone else's tracking service, so every
// tracked link in every campaign was dead and no open or click was ever
// recorded. There is deliberately no fallback now — an install with no tracking
// host sends untracked mail rather than mail nobody can click.
//
// The value is normalized to a bare host[:port]. A scheme, path or trailing dot
// pasted into the setting is stripped instead of producing links like
// https://https://t.example.com/c/<id>.
func TrackingHost() string {
	return NormalizeTrackingHost(os.Getenv("TRACKING_DOMAIN"))
}

// HydrateTrackingHost resolves TRACKING_DOMAIN through the full config chain
// (environment first, then AWS Parameter Store where that is enabled) and
// publishes it back into the process environment.
//
// Everything that reads the tracking host — TrackingHost here, the instance
// config screen, the instance health probe — reads the environment, so a
// deployment that keeps the value in Parameter Store would otherwise show and
// use two different answers. Returns the normalized host, empty when the
// deployment has none.
func (c *Config) HydrateTrackingHost(ctx context.Context) string {
	if v := TrackingHost(); v != "" {
		return v
	}
	raw, err := c.GetStringRaw(ctx, "TRACKING_DOMAIN", "tracking_domain")
	if err != nil || strings.TrimSpace(raw) == "" {
		return ""
	}
	os.Setenv("TRACKING_DOMAIN", raw)
	return TrackingHost()
}

// TrackingHostname is TrackingHost without its port: the name that has to exist
// in DNS. Development runs on localhost:3000, where the port is part of the URL
// but never part of a lookup.
func TrackingHostname() string {
	return hostWithoutPort(TrackingHost())
}

// DefaultTrackingLabel is the subdomain offered for a domain's tracking host.
// "link" reads as an ordinary link in a recipient's status bar; "track" does not.
const DefaultTrackingLabel = "link"

// DefaultTrackingHost is the tracking host offered for a sending domain.
func DefaultTrackingHost(domain string) string { return DefaultTrackingLabel + "." + domain }

// NormalizeTrackingHost reduces anything a human might paste (a full URL, a
// trailing dot, mixed case, surrounding space) to the bare host[:port] the rest
// of the system stores and compares.
func NormalizeTrackingHost(raw string) string {
	v := strings.TrimSpace(raw)
	if i := strings.Index(v, "://"); i >= 0 {
		v = v[i+3:]
	}
	if i := strings.IndexAny(v, "/?#"); i >= 0 {
		v = v[:i]
	}
	// Credentials in a pasted URL (user@host) are not part of the host.
	if i := strings.LastIndex(v, "@"); i >= 0 {
		v = v[i+1:]
	}
	v = strings.ToLower(strings.TrimSpace(v))
	// A trailing dot is a valid fully-qualified name in DNS but never matches
	// what customers type, so it is stripped on both sides of every compare.
	return strings.TrimSuffix(v, ".")
}

// TrackingURL builds an absolute tracking URL for a host and path. The scheme
// is https except where it cannot be: a loopback or explicitly-ported host is
// the local/dev tracking service, and forcing https there produced pixels no
// browser could load.
func TrackingURL(host, path string) string {
	host = NormalizeTrackingHost(host)
	if host == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return trackingScheme(host) + "://" + host + path
}

func trackingScheme(host string) string {
	name, port := host, ""
	if h, p, err := net.SplitHostPort(host); err == nil {
		name, port = h, p
	}
	if port != "" && port != "443" {
		return "http"
	}
	if name == "localhost" || strings.HasSuffix(name, ".localhost") {
		return "http"
	}
	if ip := net.ParseIP(strings.Trim(name, "[]")); ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
		return "http"
	}
	return "https"
}

func hostWithoutPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return strings.Trim(host, "[]")
}
