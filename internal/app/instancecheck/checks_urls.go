package instancecheck

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

const (
	docsAddresses = "/development/configuration/#addresses"
	docsSSO       = "/development/accounts-and-access/#single-sign-on"
	docsRealtime  = "/development/instance-health/#realtime"
	docsDelivery  = "/guides/deliverability/"
)

func urlChecks() []check {
	return []check{
		{id: "app_url_unset", run: checkAppURLUnset},
		{id: "app_url_insecure", run: checkAppURLInsecure},
		{id: "app_url_host_mismatch", run: checkAppURLHostMismatch},
		{id: "cors_missing_origin", run: checkCORSMissingOrigin},
		{id: "api_public_url_unset_oidc", run: checkAPIPublicURLUnsetOIDC},
		{id: "oidc_discovery_failed", run: checkOIDCDiscoveryFailed},
		{id: "websocket_unreachable", run: checkWebsocketUnreachable},
		{id: "tracking_domain_unreachable", run: checkTrackingDomainUnreachable},
		{id: "app_origin_wildcard", run: checkAppOriginWildcard},
	}
}

func checkAppURLUnset(ctx context.Context, d Deps, in Input) *Finding {
	if appURLConfigured() {
		return nil
	}
	return result(CategoryURLs, SeverityError, "APP_URL is not set",
		"APP_URL is not set, so password reset, invitation and setup links are being built against https://app.warmbly.com. "+
			"Those links go to the hosted service, not to your instance, and a reset token in one of them leaves your deployment. "+
			"Set APP_URL to your dashboard origin.",
		docsAddresses)
}

func checkAppURLInsecure(ctx context.Context, d Deps, in Input) *Finding {
	raw := appURL()
	if !appURLConfigured() {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || isLoopbackHost(u.Hostname()) {
		return nil
	}
	return result(CategoryURLs, SeverityWarning, "The dashboard is not behind HTTPS",
		fmt.Sprintf("APP_URL is %s. Browsers refuse WebAuthn outside a secure context, so passkeys are disabled, "+
			"and session cookies are sent in the clear. Put the dashboard behind HTTPS.", raw),
		docsAddresses)
}

func checkAppURLHostMismatch(ctx context.Context, d Deps, in Input) *Finding {
	reached := hostOnly(in.Host)
	configured := hostOf(appURL())
	// Skip on loopback: a local stack legitimately reaches the API on one port
	// and the dashboard on another.
	if reached == "" || configured == "" || !appURLConfigured() || isLoopbackHost(reached) {
		return nil
	}
	if strings.EqualFold(reached, configured) {
		return nil
	}
	return result(CategoryURLs, SeverityWarning, "APP_URL does not match this host",
		fmt.Sprintf("You reached this panel on %s but APP_URL is %s. "+
			"Emailed links are built from APP_URL, so they will point somewhere other than where people actually reach this instance.",
			reached, appURL()),
		docsAddresses)
}

func checkCORSMissingOrigin(ctx context.Context, d Deps, in Input) *Finding {
	allowed := runtimeOf(d).CORSOrigins
	if len(allowed) == 0 {
		allowed = splitList(env("CORS_ALLOW_ORIGINS"))
	}
	// An empty list means the backend derived one at boot and did not hand it
	// over; there is nothing to compare against, so this check cannot run.
	if len(allowed) == 0 {
		return nil
	}

	wanted := []string{}
	if appURLConfigured() {
		wanted = append(wanted, strings.TrimRight(appURL(), "/"))
	}
	if in.Origin != "" {
		wanted = append(wanted, strings.TrimRight(in.Origin, "/"))
	}

	for _, origin := range wanted {
		if origin == "" || containsOrigin(allowed, origin) {
			continue
		}
		return result(CategoryURLs, SeverityWarning, "An origin is missing from the CORS allowlist",
			fmt.Sprintf("%s is not in the allowed CORS origins, so the browser will block its API calls. "+
				"Add it to CORS_ALLOW_ORIGINS.", origin),
			docsAddresses)
	}
	return nil
}

func checkAPIPublicURLUnsetOIDC(ctx context.Context, d Deps, in Input) *Finding {
	if env("OIDC_ISSUER_URL") == "" {
		return nil
	}
	if env("API_PUBLIC_URL") != "" || env("OIDC_REDIRECT_URL") != "" {
		return nil
	}
	return result(CategoryURLs, SeverityError, "OIDC has no redirect URL",
		"OIDC is configured but there is no redirect URL: API_PUBLIC_URL is empty and OIDC_REDIRECT_URL is not set, "+
			"so the OIDC login path is disabled. Set API_PUBLIC_URL to this backend's public base.",
		docsSSO)
}

func checkOIDCDiscoveryFailed(ctx context.Context, d Deps, in Input) *Finding {
	rt := runtimeOf(d)
	if rt.OIDCDiscoveryErr == "" {
		return nil
	}
	return result(CategoryURLs, SeverityError, "OIDC discovery failed",
		fmt.Sprintf("Discovery against %s failed at boot, so the single sign-on button is not shown: %s.",
			env("OIDC_ISSUER_URL"), rt.OIDCDiscoveryErr),
		docsSSO)
}

func checkWebsocketUnreachable(ctx context.Context, d Deps, in Input) *Finding {
	raw := runtimeOf(d).WebsocketURL
	if raw == "" {
		raw = env("WEBSOCKET_URL")
	}
	health := realtimeHealthURL(raw)
	if health == "" {
		return nil
	}
	if probeHTTP(ctx, health) {
		return nil
	}
	// Running out of budget is not evidence of a down service.
	if ctx.Err() != nil {
		return nil
	}
	return result(CategoryURLs, SeverityWarning, "The realtime service is not reachable",
		fmt.Sprintf("The realtime service is not reachable at %s, so the dashboard will not update live "+
			"and presence will be empty.", raw),
		docsRealtime)
}

func checkTrackingDomainUnreachable(ctx context.Context, d Deps, in Input) *Finding {
	domain := env("TRACKING_DOMAIN")
	if domain == "" {
		return nil
	}
	// Outbound mail always builds https:// links, so that is the probe that
	// matters; http is tried second so a local sink is not reported as down.
	for _, candidate := range trackingProbeURLs(domain) {
		if probeHTTP(ctx, candidate) {
			return nil
		}
	}
	// Running out of budget is not evidence of a down service.
	if ctx.Err() != nil {
		return nil
	}
	return result(CategoryURLs, SeverityWarning, "The tracking domain did not answer",
		fmt.Sprintf("%s did not answer, so open pixels and click links in campaign mail will not record. "+
			"Recipients still receive the mail.", domain),
		docsDelivery)
}

func checkAppOriginWildcard(ctx context.Context, d Deps, in Input) *Finding {
	if env("APP_ORIGIN") != "" {
		return nil
	}
	return result(CategoryURLs, SeverityInfo, "APP_ORIGIN is not set",
		"APP_ORIGIN is not set, so the mailbox OAuth callback page posts the authorization code back to the dashboard "+
			"with a wildcard target origin. Set APP_ORIGIN to your dashboard origin.",
		docsAddresses)
}

// realtimeHealthURL mirrors wsHealthURL in cmd/backend/main.go.
func realtimeHealthURL(wsURI string) string {
	if wsURI == "" {
		return ""
	}
	u, err := url.Parse(wsURI)
	if err != nil || u.Host == "" {
		return ""
	}
	if u.Scheme == "wss" {
		u.Scheme = "https"
	} else {
		u.Scheme = "http"
	}
	u.Path = "/health"
	u.RawQuery = ""
	return u.String()
}

func trackingProbeURLs(domain string) []string {
	if strings.Contains(domain, "://") {
		return []string{strings.TrimRight(domain, "/") + "/health"}
	}
	domain = strings.TrimRight(domain, "/")
	return []string{"https://" + domain + "/health", "http://" + domain + "/health"}
}

func splitList(raw string) []string {
	out := []string{}
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func containsOrigin(list []string, origin string) bool {
	for _, item := range list {
		if item == "*" || strings.EqualFold(strings.TrimRight(item, "/"), origin) {
			return true
		}
	}
	return false
}
