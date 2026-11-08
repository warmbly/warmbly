package config

import (
	"context"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
)

// PortalURLHost is PORTAL_DOMAIN as a host with its port: the portal service's shared address, where connect
// links and reports are built for a workspace with no agency portal domain. Empty when unset, and then no
// link is built anywhere.
func PortalURLHost() string {
	return NormalizeTrackingHost(os.Getenv("PORTAL_DOMAIN"))
}

// PortalBaseURL is the shared portal address as an origin; empty when PORTAL_DOMAIN is unset.
func PortalBaseURL() string {
	host := PortalURLHost()
	if host == "" {
		return ""
	}
	return publicScheme(host) + "://" + host
}

// PortalOriginOn is the origin the portal service answers on for host; an empty host is the shared address.
func PortalOriginOn(host string) string {
	host = NormalizeTrackingHost(host)
	if host == "" {
		return PortalBaseURL()
	}
	return publicScheme(host) + "://" + host
}

// PortalCNAMETarget is the host an agency portal domain is CNAMEd to: PORTAL_DOMAIN without its port, empty when unset.
func PortalCNAMETarget() string {
	return hostWithoutPort(PortalURLHost())
}

// portalOrigins answers whether a host is a verified agency portal domain; unset in processes that serve none.
var portalOrigins atomic.Pointer[func(host string) bool]

// SetPortalHosts installs the verified portal lookup the origin checks consult.
func SetPortalHosts(verified func(host string) bool) {
	if verified == nil {
		portalOrigins.Store(nil)
		return
	}
	portalOrigins.Store(&verified)
}

// PortalOriginHost is the host of an https origin with no port, path or credentials; empty for anything else.
func PortalOriginHost(value string) string {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Port() != "" || u.User != nil ||
		u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || u.Opaque != "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if !strings.Contains(host, ".") || "https://"+host != strings.ToLower(value) {
		return ""
	}
	return host
}

// PortalOrigin is value when it is the origin of a verified agency portal domain, else empty.
func PortalOrigin(value string) string {
	check := portalOrigins.Load()
	if check == nil {
		return ""
	}
	host := PortalOriginHost(value)
	if host == "" || host == PortalCNAMETarget() || host == FormsHostname() || !(*check)(host) {
		return ""
	}
	return "https://" + host
}

// TrustedOrigin is an operator-configured dashboard origin or a verified portal origin.
func TrustedOrigin(value string) string {
	if origin := DashboardOrigin(value); origin != "" {
		return origin
	}
	return PortalOrigin(value)
}

// OperatorOriginFromContext is the request's dashboard origin only when the operator configured it, never a portal.
func OperatorOriginFromContext(ctx context.Context) string {
	origin, _ := ctx.Value(dashboardOriginKey{}).(string)
	return DashboardOrigin(origin)
}
