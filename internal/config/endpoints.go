package config

import (
	"context"
	"net"
	"net/url"
	"os"
	"strings"
)

// AppBaseURL is the dashboard origin every emailed link is built from.
//
// This used to be a hardcoded https://app.warmbly.com, which meant a
// self-hosted deployment mailed its users a reset link pointing at someone
// else's dashboard, carrying a live reset token signed with the self-host's own
// AUTH_SECRET. APP_URL is the documented variable; FRONTEND_BASE_URL is the
// older name and stays supported so existing deployments keep working.
//
// An install that set neither is answered from what it did configure, and a
// self-host is never answered with the hosted dashboard: a link nobody can
// open is a support ticket, while a working link to someone else's app is that
// deployment's tokens walking out of it.
func AppBaseURL() string {
	for _, key := range []string{"APP_URL", "FRONTEND_BASE_URL"} {
		if v := strings.TrimRight(strings.TrimSpace(os.Getenv(key)), "/"); v != "" {
			return v
		}
	}
	if v := inferredAppBaseURL(); v != "" {
		return v
	}
	if SelfHosted() {
		return ""
	}
	return "https://app.warmbly.com"
}

// DashboardOrigin allows exact configured origins only, never a CORS wildcard.
func DashboardOrigin(value string) string {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return ""
	}
	allowed := splitCSV(os.Getenv("CORS_ALLOW_ORIGINS"))
	allowed = append(allowed, os.Getenv("APP_ORIGIN"), AppBaseURL())
	for _, candidate := range allowed {
		v, err := url.Parse(strings.TrimSpace(candidate))
		if err == nil && v.User == nil && v.Scheme+"://"+v.Host == value {
			return value
		}
	}
	return ""
}

type dashboardOriginKey struct{}

func PrimaryDashboardOrigin() string {
	if origin := DashboardOrigin(strings.TrimSpace(os.Getenv("APP_ORIGIN"))); origin != "" {
		return origin
	}
	u, err := url.Parse(AppBaseURL())
	if err != nil || u.User != nil {
		return ""
	}
	return DashboardOrigin(u.Scheme + "://" + u.Host)
}

func WithDashboardOrigin(ctx context.Context, origin string) context.Context {
	return context.WithValue(ctx, dashboardOriginKey{}, DashboardOrigin(origin))
}

func DashboardOriginFromContext(ctx context.Context) string {
	origin, _ := ctx.Value(dashboardOriginKey{}).(string)
	return DashboardOrigin(origin)
}

func dashboardBaseURL(origin string) string {
	if trusted := DashboardOrigin(origin); trusted != "" {
		return trusted
	}
	return AppBaseURL()
}

func DashboardBaseURL(ctx context.Context) string {
	return dashboardBaseURL(DashboardOriginFromContext(ctx))
}

// inferredAppBaseURL reconstructs the dashboard origin from the rest of the
// deployment's own configuration. CORS_ALLOW_ORIGINS is exact when it is set
// (the dashboard is the first origin the browser calls the API from);
// PUBLIC_HOST is the installer's one hostname everything else derives from.
func inferredAppBaseURL() string {
	for _, origin := range strings.Split(os.Getenv("CORS_ALLOW_ORIGINS"), ",") {
		origin = strings.TrimRight(strings.TrimSpace(origin), "/")
		if origin == "" || origin == "*" {
			continue
		}
		if u, err := url.Parse(origin); err == nil && u.Scheme != "" && u.Host != "" {
			return u.Scheme + "://" + u.Host
		}
	}
	host := NormalizeTrackingHost(os.Getenv("PUBLIC_HOST"))
	if host == "" {
		return ""
	}
	return publicScheme(host) + "://" + host
}

// WebsocketURL is the realtime gateway clients connect to. It is deployment
// configuration rather than a secret, which is why GET /v1/auth/config serves
// it: a CLI or a developer client cannot otherwise find the socket on a
// self-hosted instance, where the host layout is whatever the operator chose.
func WebsocketURL() string {
	return NormalizeWebsocketURL(os.Getenv("WEBSOCKET_URL"))
}

func NormalizeWebsocketURL(value string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return ""
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" || u.User != nil {
		return ""
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	case "ws", "wss":
	default:
		return ""
	}
	u.Path = strings.TrimRight(u.Path, "/")
	// Normalize the path, not the query string on a legacy reverse-proxy URL.
	switch {
	case strings.HasSuffix(u.Path, "/socket/websocket"):
	case strings.HasSuffix(u.Path, "/socket"):
		u.Path += "/websocket"
	default:
		u.Path += "/socket/websocket"
	}
	u.RawPath = ""
	u.Fragment = ""
	return u.String()
}

func GetPasswordResetURL(sessionToken, origin string) string {
	return dashboardBaseURL(origin) + "/auth/reset-password/confirm?session=" + url.QueryEscape(sessionToken)
}

// GetInviteURL uses a trusted initiating dashboard, or the primary dashboard.
func GetInviteURL(token, origin string) string {
	return dashboardBaseURL(origin) + "/invite?token=" + url.QueryEscape(token)
}

// FormsBaseURL is the origin hosted form pages are served from: FORMS_DOMAIN,
// the host routed to the forms service (cmd/forms). Empty when unset — the
// pages do not live on the API origin, so there is nothing to fall back to,
// and a share link pointing at the wrong process is worse than none.
func FormsBaseURL() string {
	host := NormalizeTrackingHost(os.Getenv("FORMS_DOMAIN"))
	if host == "" {
		return ""
	}
	return publicScheme(host) + "://" + host
}

// publicScheme is https except where TLS cannot be terminated: a loopback or
// private-network host is a development or LAN install. The port is
// deliberately not a signal, because an install can terminate TLS on any port
// and inferring http from one handed an https deployment http:// share links.
func publicScheme(host string) string {
	name := hostWithoutPort(NormalizeTrackingHost(host))
	if name == "localhost" || strings.HasSuffix(name, ".localhost") {
		return "http"
	}
	if ip := net.ParseIP(strings.Trim(name, "[]")); ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
		return "http"
	}
	return "https"
}

// FormsHostname is the bare host this install serves forms on. It is the
// CNAME target a customer points their own forms subdomain at, so it never
// carries a port.
func FormsHostname() string {
	return hostWithoutPort(NormalizeTrackingHost(FormsBaseURL()))
}

// FormsURLHost is the shared host form URLs are built on. Unlike the CNAME
// target it keeps the port, because dropping it points every share link and
// embed on a ported install at nothing.
func FormsURLHost() string {
	return NormalizeTrackingHost(FormsBaseURL())
}

// FormURLOn builds the hosted page URL on a specific host, which is how a
// verified custom forms domain replaces the shared one. An empty host falls
// back to this install's own forms base.
func FormURLOn(host, publicID string) string {
	host = NormalizeTrackingHost(host)
	if host == "" {
		return GetFormURL(publicID)
	}
	return publicScheme(host) + "://" + host + "/f/" + url.PathEscape(publicID)
}

// GetFormURL is the hosted page for one form; empty when no base is known.
func GetFormURL(publicID string) string {
	base := FormsBaseURL()
	if base == "" {
		return ""
	}
	return base + "/f/" + url.PathEscape(publicID)
}
