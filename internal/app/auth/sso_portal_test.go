package auth

import (
	"context"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
)

// A browser sign-in returns to the verified portal it began on; any other origin returns to APP_URL.
func TestSSOReturnsOnlyToTheVerifiedPortalItBeganOn(t *testing.T) {
	s := trustedSvc(t, config.LoginCodeNewDevice)
	s.WireFederatedProvider(models.IdentityProviderGoogle, stubProvider{name: "google"})
	t.Setenv("APP_URL", "https://app.acme.test")
	t.Setenv("APP_ORIGIN", "")
	t.Setenv("CORS_ALLOW_ORIGINS", "")
	config.SetPortalHosts(func(host string) bool { return host == "portal.agency.test" })
	t.Cleanup(func() { config.SetPortalHosts(nil) })

	for origin, want := range map[string]string{
		"https://portal.agency.test": "https://portal.agency.test",
		"https://app.acme.test":      "",
		"https://attacker.test":      "",
	} {
		ctx := config.WithDashboardOrigin(context.Background(), origin)
		redirect, err := s.SSOBegin(ctx, models.IdentityProviderGoogle)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		state := pathState(redirect.URL)
		if got := s.SSOReturnOrigin(context.Background(), state); got != want {
			t.Fatalf("origin %q returned to %q, want %q", origin, got, want)
		}
	}
	// A portal that stops verifying no longer receives the sign-in.
	ctx := config.WithDashboardOrigin(context.Background(), "https://portal.agency.test")
	redirect, _ := s.SSOBegin(ctx, models.IdentityProviderGoogle)
	config.SetPortalHosts(func(string) bool { return false })
	if got := s.SSOReturnOrigin(context.Background(), pathState(redirect.URL)); got != "" {
		t.Fatalf("unverified portal still returned to: %q", got)
	}
}

// pathState reads the state stubProvider puts at the end of its authorization URL.
func pathState(authURL string) string { return authURL[strings.LastIndex(authURL, "/")+1:] }
