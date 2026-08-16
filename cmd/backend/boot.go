package main

import (
	"context"
	"log"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/notify"
)

// insecureDefaults are the working secret values shipped in docker-compose.yml
// and the Makefile so `docker compose up` boots with no .env. They are public
// in this repository, so a deployment still running on one is not protected by
// it at all: a forged AUTH_SECRET JWT is accepted by the realtime service, and
// the local KMS master key unwraps every organization DEK offline.
//
// The map is keyed by env var so the check is exact rather than a heuristic.
var insecureDefaults = map[string]string{
	"AUTH_SECRET":                "local-dev-auth-secret-minimum-32-characters-long",
	"KMS_LOCAL_MASTER_KEY":       "Xr0JA7gqF2POy29a7MRByyqddivTNt8WOyKsOXklazk=",
	"CREDENTIALS_ENCRYPTION_KEY": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	"INTERNAL_API_TOKEN":         "local-dev-internal-token",
	"SECRET_KEY_BASE":            "local-development-secret-key-base-minimum-64-characters-for-phoenix",
}

// checkSecrets refuses to start a deployment that reaches the network while
// still using a published default secret. Local development is exempt: that is
// what the defaults exist for.
//
// ALLOW_INSECURE_DEFAULTS=true downgrades the refusal to a warning, for an
// operator who genuinely wants a throwaway instance on a trusted LAN.
// It returns the offenders so the health page can keep reporting them for as
// long as they are in use, not just once at boot.
func checkSecrets() []string {
	var offenders []string
	for env, def := range insecureDefaults {
		if os.Getenv(env) == def {
			offenders = append(offenders, env)
		}
	}
	if len(offenders) == 0 {
		return nil
	}
	sort.Strings(offenders)

	list := strings.Join(offenders, ", ")
	if isDevEnv() || strings.EqualFold(os.Getenv("ALLOW_INSECURE_DEFAULTS"), "true") {
		log.Printf("Warning: using the published default value for %s. Anyone can read these from the Warmbly repository. Generate real values before this instance is reachable by other people (make gen-key).", list)
		return offenders
	}

	log.Fatalf("Refusing to start: %s still hold the published default value from docker-compose.yml. "+
		"Those defaults are public, so they provide no protection. Generate real values (make gen-key) and put them in .env, "+
		"or set ALLOW_INSECURE_DEFAULTS=true if this instance is genuinely disposable.", list)
	return offenders
}

func isDevEnv() bool {
	env := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	return env == "" || env == "dev" || env == "development" || env == "local"
}

// passkeysUsableFor reports whether WebAuthn can work on this deployment's
// origin. Passkeys require a secure context and an RP ID that is a real
// domain, so a plain-http LAN address fails in the browser with an opaque
// error. Detecting it here lets GET /auth/config hide the button instead.
func passkeysUsableFor(appURL string) bool {
	if appURL == "" {
		return false
	}
	u, err := url.Parse(appURL)
	if err != nil {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	// http is a secure context only on loopback.
	host := u.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || strings.HasSuffix(host, ".localhost")
}

// oidcRedirectURL is where the provider sends the browser back. Explicit
// OIDC_REDIRECT_URL wins; otherwise it derives from the backend's public base,
// which is where the callback handler actually lives.
func oidcRedirectURL() string {
	if v := strings.TrimSpace(os.Getenv("OIDC_REDIRECT_URL")); v != "" {
		return v
	}
	base := strings.TrimRight(os.Getenv("API_PUBLIC_URL"), "/")
	if base == "" {
		return ""
	}
	// The route is registered on /v1, not /api/v1: there is no /api prefix.
	return base + "/v1/auth/oidc/callback"
}

// oauthPublicBaseURL is the base every mailbox-connect redirect_uri is built
// from. It has to be the address the provider will send a browser back to,
// which is API_PUBLIC_URL, the same value oidcRedirectURL derives from and the
// one .env.example already documents as `<API_PUBLIC_URL>/addresses/...`.
//
// It is emphatically NOT API_HOST: that is the listener's bind address, which
// stays 0.0.0.0:8080 in a container. Sent as a redirect_uri it is not even an
// absolute URI, so the provider rejects the request outright.
func oauthPublicBaseURL(bindAddr string) string {
	if v := strings.TrimRight(strings.TrimSpace(os.Getenv("API_PUBLIC_URL")), "/"); v != "" {
		return v
	}
	return browsableBaseFromBindAddr(bindAddr)
}

// browsableBaseFromBindAddr turns a listener address into a URL a browser on
// the same host can actually open, so a stock local install that never set
// API_PUBLIC_URL still produces an absolute redirect_uri.
func browsableBaseFromBindAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	// Already a URL (someone put a real base in API_HOST): keep it as-is.
	if strings.Contains(addr, "://") {
		return strings.TrimRight(addr, "/")
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = addr, ""
	}
	// A wildcard bind is reachable from the host itself as localhost.
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "localhost"
	}
	if port == "" {
		return "http://" + host
	}
	return "http://" + net.JoinHostPort(host, port)
}

// splitList parses a comma-separated env list.
func splitList(v string) []string {
	out := []string{}
	for _, part := range strings.Split(v, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func mailTransportKind(t *notify.Transport) string {
	if t == nil {
		return ""
	}
	return t.Kind
}

// warnDeploymentURLs surfaces the configuration mistakes that silently break
// auth once an operator moves off localhost, each of which previously appeared
// only as a failure in the browser.
func warnDeploymentURLs(ctx context.Context, appURL string) {
	if appURL == "" {
		log.Printf("Warning: APP_URL is not set. Password reset and team invitation emails will link to %s, which is almost certainly not this deployment.", config.AppBaseURL())
		return
	}
	if !passkeysUsableFor(appURL) {
		log.Printf("Warning: APP_URL is %s. Passkeys need a secure context, so they are disabled: browsers refuse WebAuthn on plain http outside localhost, and an IP address cannot be a relying-party ID. Put the dashboard behind HTTPS to enable them.", appURL)
	}
}
