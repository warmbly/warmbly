package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/config"
)

func verifiedPortals(t *testing.T, hosts ...string) {
	t.Helper()
	set := map[string]bool{}
	for _, h := range hosts {
		set[h] = true
	}
	config.SetPortalHosts(func(host string) bool { return set[host] })
	t.Cleanup(func() { config.SetPortalHosts(nil) })
}

// A mailbox sign-in returns to the verified portal it began on, and never to any other origin.
func TestMailboxCallbackReturnsOnlyToVerifiedPortal(t *testing.T) {
	t.Setenv("APP_URL", "https://app.warmbly.com")
	t.Setenv("APP_ORIGIN", "")
	t.Setenv("CORS_ALLOW_ORIGINS", "")
	verifiedPortals(t, "portal.agency.test")
	for origin, want := range map[string]string{
		"https://portal.agency.test":      "https://portal.agency.test",
		"https://unverified.agency.test":  "",
		"http://portal.agency.test":       "",
		"https://portal.agency.test:8443": "",
	} {
		h := &Handler{EmailService: &returnOriginService{origin: origin}}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/addresses/google/callback?code=c&state=w.nonce", nil)
		h.renderOAuthCallback(c, "gmail")
		if !strings.Contains(w.Body.String(), `var origin = "`+want+`"`) {
			t.Fatalf("origin %q: page did not target %q: %s", origin, want, w.Body.String())
		}
	}
}

func TestIntegrationCallbackReturnsOnlyToVerifiedPortal(t *testing.T) {
	t.Setenv("APP_URL", "https://app.acme.io")
	t.Setenv("APP_ORIGIN", "")
	t.Setenv("CORS_ALLOW_ORIGINS", "")
	verifiedPortals(t, "portal.agency.test")
	for origin, want := range map[string]string{"https://portal.agency.test": "https://portal.agency.test", "https://attacker.test": ""} {
		h := &Handler{IntegrationService: &integrationReturnOriginService{origin: origin}}
		w := serve(t, h.IntegrationOAuthCallback, "/cb?state=s&code=c")
		if !strings.Contains(w.Body.String(), `var origin = "`+want+`"`) {
			t.Fatalf("origin %q page = %s", origin, w.Body.String())
		}
	}
}

func TestCaptchaFrameIsFramedOnlyByAVerifiedPortal(t *testing.T) {
	t.Setenv("CAPTCHA_PROVIDER", "turnstile")
	t.Setenv("TURNSTILE_SITE_KEY", "site-key")
	verifiedPortals(t, "portal.agency.test")
	h := &Handler{}
	for _, origin := range []string{"https://attacker.test", "", "http://portal.agency.test"} {
		w := serve(t, h.CaptchaFrame, "/v1/auth/captcha-frame?origin="+origin)
		if w.Code != http.StatusBadRequest || strings.Contains(w.Body.String(), "turnstile") {
			t.Fatalf("origin %q served the challenge: %d", origin, w.Code)
		}
	}
	w := serve(t, h.CaptchaFrame, "/v1/auth/captcha-frame?origin=https://portal.agency.test")
	csp := w.Header().Get("Content-Security-Policy")
	if w.Code != http.StatusOK || !strings.Contains(csp, "frame-ancestors https://portal.agency.test;") || w.Header().Get("X-Frame-Options") != "" {
		t.Fatalf("verified portal frame = %d csp=%q", w.Code, csp)
	}
	if !strings.Contains(w.Body.String(), `postMessage(m,O)`) || !strings.Contains(w.Body.String(), `"https://portal.agency.test"`) {
		t.Fatal("the token must be posted to the portal origin only")
	}
}

func TestEmailLinkOriginKeepsOperatorOrigins(t *testing.T) {
	t.Setenv("APP_URL", "https://app.acme.io")
	t.Setenv("APP_ORIGIN", "")
	t.Setenv("CORS_ALLOW_ORIGINS", "")
	verifiedPortals(t, "portal.agency.test")
	h := &Handler{}
	ctx := config.WithDashboardOrigin(context.Background(), "https://app.acme.io")
	if got := h.emailLinkOrigin(ctx, [16]byte{}); got != "https://app.acme.io" {
		t.Fatalf("operator origin = %q", got)
	}
	// Without a portal registry nothing proves the portal serves the workspace.
	ctx = config.WithDashboardOrigin(context.Background(), "https://portal.agency.test")
	if got := h.emailLinkOrigin(ctx, [16]byte{}); got != "" {
		t.Fatalf("unproven portal origin = %q", got)
	}
}
