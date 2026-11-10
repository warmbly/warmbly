package api

import (
	"net"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/config"
)

// tailscaleCGNAT is the 100.64.0.0/10 carrier-grade NAT range Tailscale hands
// out to nodes; MagicDNS A records resolve into it. A developer hitting the API
// over a Tailscale IP from another device shows up with an origin in this range.
var tailscaleCGNAT = mustCIDR("100.64.0.0/10")

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

// devOriginAllowed reports whether a browser Origin should be accepted for CORS
// in non-production mode. It permits the origins a developer realistically
// serves the dashboards from — loopback, RFC1918 LAN addresses, link-local, the
// Tailscale CGNAT range (100.64.0.0/10), and *.ts.net MagicDNS names — on any
// port, so the API "just works" when reached from another device on the tailnet
// or LAN without enumerating every host:port.
//
// It is only wired in non-release builds (see Run). Production keeps the strict
// explicit allowlist and never consults this function.
func devOriginAllowed(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return false
	}

	host := u.Hostname()
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if strings.HasSuffix(host, ".ts.net") {
		return true
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	switch {
	case ip.IsLoopback():
		return true
	case ip.IsPrivate(): // RFC1918 (10/8, 172.16/12, 192.168/16) + IPv6 ULA
		return true
	case ip.IsLinkLocalUnicast(): // 169.254/16, fe80::/10
		return true
	case tailscaleCGNAT.Contains(ip):
		return true
	default:
		return false
	}
}

// portalOriginAllowed accepts the https origin of a verified agency portal domain, and only that.
func portalOriginAllowed(_ *gin.Context, origin string) bool {
	return config.PortalOrigin(origin) != ""
}
