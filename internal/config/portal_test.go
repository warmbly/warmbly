package config

import (
	"context"
	"testing"
)

func withPortals(t *testing.T, hosts ...string) {
	t.Helper()
	set := map[string]bool{}
	for _, h := range hosts {
		set[h] = true
	}
	SetPortalHosts(func(host string) bool { return set[host] })
	t.Cleanup(func() { SetPortalHosts(nil) })
}

func TestPortalAddressComesOnlyFromPortalDomain(t *testing.T) {
	t.Setenv("FORMS_DOMAIN", "forms.example.test")
	t.Setenv("PORTAL_DOMAIN", "")
	for name, got := range map[string]string{
		"target": PortalCNAMETarget(), "host": PortalURLHost(), "base": PortalBaseURL(),
		"origin": PortalOriginOn(""), "report": ReportURLOn("", "tok"),
	} {
		if got != "" {
			t.Fatalf("unset PORTAL_DOMAIN %s = %q, want nothing", name, got)
		}
	}
	t.Setenv("PORTAL_DOMAIN", "https://Portal.Example.test:8443/")
	if got := PortalCNAMETarget(); got != "portal.example.test" {
		t.Fatalf("PORTAL_DOMAIN target = %q", got)
	}
	if got := PortalBaseURL(); got != "https://portal.example.test:8443" {
		t.Fatalf("PORTAL_DOMAIN base = %q", got)
	}
	if got := ReportURLOn("", "tok"); got != "https://portal.example.test:8443/r/tok" {
		t.Fatalf("shared report link = %q", got)
	}
	if got := ReportURLOn("portal.agency.test", "tok"); got != "https://portal.agency.test/r/tok" {
		t.Fatalf("agency report link = %q", got)
	}
	t.Setenv("PORTAL_DOMAIN", "localhost:8095")
	if got := PortalBaseURL(); got != "http://localhost:8095" {
		t.Fatalf("local portal base = %q", got)
	}
}

func TestPortalOriginAcceptsOnlyVerifiedHTTPSOrigins(t *testing.T) {
	t.Setenv("FORMS_DOMAIN", "forms.example.test")
	t.Setenv("PORTAL_DOMAIN", "portal.example.test")
	withPortals(t, "portal.agency.test", "portal.example.test", "forms.example.test")
	if got := PortalOrigin("https://portal.agency.test"); got != "https://portal.agency.test" {
		t.Fatalf("verified portal refused: %q", got)
	}
	for _, bad := range []string{
		"https://evil.test",
		"http://portal.agency.test",
		"https://portal.agency.test:8443",
		"https://portal.agency.test/",
		"https://portal.agency.test/app",
		"https://user@portal.agency.test",
		"https://portal.agency.test?x=1",
		"https://portal.example.test",
		"https://forms.example.test",
		"",
		"null",
	} {
		if got := PortalOrigin(bad); got != "" {
			t.Fatalf("PortalOrigin(%q) = %q, want refused", bad, got)
		}
	}
}

func TestPortalOriginOffWithoutLookup(t *testing.T) {
	SetPortalHosts(nil)
	if got := PortalOrigin("https://portal.agency.test"); got != "" {
		t.Fatalf("no lookup wired yet accepted %q", got)
	}
}

func TestTrustedOriginKeepsConfiguredAndAddsVerifiedPortals(t *testing.T) {
	t.Setenv("APP_URL", "https://app.example.test")
	t.Setenv("CORS_ALLOW_ORIGINS", "")
	t.Setenv("APP_ORIGIN", "")
	withPortals(t, "portal.agency.test")
	for in, want := range map[string]string{
		"https://app.example.test":    "https://app.example.test",
		"https://portal.agency.test":  "https://portal.agency.test",
		"https://attacker.test":       "",
		"https://portal.agency.test.": "",
	} {
		if got := TrustedOrigin(in); got != want {
			t.Fatalf("TrustedOrigin(%q) = %q, want %q", in, got, want)
		}
	}
	ctx := WithDashboardOrigin(context.Background(), "https://portal.agency.test")
	if got := DashboardOriginFromContext(ctx); got != "https://portal.agency.test" {
		t.Fatalf("portal origin dropped from context: %q", got)
	}
	if got := OperatorOriginFromContext(ctx); got != "" {
		t.Fatalf("operator origin includes a portal: %q", got)
	}
	if got := GetPasswordResetURL("t", OperatorOriginFromContext(ctx)); got != "https://app.example.test/auth/reset-password/confirm#session=t" {
		t.Fatalf("reset link = %q", got)
	}
}
