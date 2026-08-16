package instancecheck

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/app/instanceconfig"
	"github.com/warmbly/warmbly/internal/config"
)

func result(category string, sev Severity, title, message, docs string) *Finding {
	return &Finding{Category: category, Severity: sev, Title: title, Message: message, Docs: docs}
}

func env(key string) string { return strings.TrimSpace(os.Getenv(key)) }

func truthy(key string) bool {
	switch strings.ToLower(env(key)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// devEnv mirrors isDevEnv in cmd/backend/boot.go: an unset APP_ENV is dev.
func devEnv() bool {
	switch strings.ToLower(env("APP_ENV")) {
	case "", "dev", "development", "local":
		return true
	}
	return false
}

func isLoopbackHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if i := strings.LastIndex(host, ":"); i > 0 && !strings.Contains(host[i:], "]") {
		host = host[:i]
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || strings.HasSuffix(host, ".localhost")
}

func isLoopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return false
	}
	return isLoopbackHost(u.Hostname())
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// hostOnly strips a port from a Host header value.
func hostOnly(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if h, _, err := splitHostPort(host); err == nil {
		return h
	}
	return host
}

func splitHostPort(host string) (string, string, error) {
	u, err := url.Parse("//" + host)
	if err != nil || u.Hostname() == "" {
		return "", "", fmt.Errorf("unparseable host")
	}
	return u.Hostname(), u.Port(), nil
}

// registrableDomain is the last two labels of a hostname. Good enough for an
// advisory comparison; it is deliberately not a public-suffix lookup.
func registrableDomain(host string) string {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return host
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

func policy(d Deps) *config.AuthPolicy {
	if d.Policy != nil {
		return d.Policy
	}
	if d.Runtime != nil && d.Runtime.Policy != nil {
		return d.Runtime.Policy
	}
	return config.LoadAuthPolicy(mailDelivers(d))
}

func mailDelivers(d Deps) bool {
	if d.Transport != nil {
		return d.Transport.Delivers
	}
	if d.Runtime != nil && d.Runtime.MailTransportKind != "" {
		return d.Runtime.MailDelivers
	}
	return config.MailTransport() != config.MailTransportLog
}

// appURL is the origin every emailed link is built from, resolved exactly as
// the runtime resolves it.
func appURL() string { return config.AppBaseURL() }

// appURLConfigured reports whether an operator actually set it, as opposed to
// falling back to the hosted dashboard.
func appURLConfigured() bool {
	return env("APP_URL") != "" || env("FRONTEND_BASE_URL") != ""
}

func humanizeDuration(d time.Duration) string {
	if d <= time.Minute {
		return "less than a minute"
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	switch {
	case hours > 0 && minutes > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
}

// probeHTTP treats any response below 500 as reachable: an auth wall still
// proves the service answered. Two attempts, so one dropped packet does not
// become a health warning.
func probeHTTP(ctx context.Context, rawURL string) bool {
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return false
		}
		resp, err := client.Do(req)
		if err == nil {
			code := resp.StatusCode
			_ = resp.Body.Close()
			if code < 500 {
				return true
			}
		}
		if ctx.Err() != nil {
			return false
		}
	}
	return false
}

// runtimeOf returns the injected runtime facts, or an empty set.
func runtimeOf(d Deps) *instanceconfig.Runtime {
	if d.Runtime != nil {
		return d.Runtime
	}
	return &instanceconfig.Runtime{}
}
