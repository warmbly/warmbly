package auth

import (
	"context"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/idtoken"
)

type stubProvider struct{ name string }

func (s stubProvider) AuthCodeURL(state, nonce, verifier string) string {
	return "https://provider/" + state
}
func (s stubProvider) Exchange(ctx context.Context, code, verifier, nonce string) (*idtoken.Claims, error) {
	return nil, nil
}
func (s stubProvider) Issuer() string       { return "https://" + s.name }
func (s stubProvider) ProviderName() string { return s.name }

// The login screen renders whatever this returns, so a provider that was never
// wired must never appear: that is exactly how the Google button came to exist
// with no flow behind it.
func TestFederatedProvidersListsOnlyWhatIsWired(t *testing.T) {
	s := &authService{}
	if got := s.FederatedProviders(); len(got) != 0 {
		t.Fatalf("an unconfigured deployment advertised %v", got)
	}

	s.WireFederatedProvider(models.IdentityProviderOIDC, stubProvider{name: "oidc"})
	s.WireFederatedProvider(models.IdentityProviderGoogle, stubProvider{name: "google"})

	got := s.FederatedProviders()
	if len(got) != 2 || got[0] != models.IdentityProviderGoogle || got[1] != models.IdentityProviderOIDC {
		t.Fatalf("got %v, want google before oidc", got)
	}
}

func TestWireFederatedProviderIgnoresNothing(t *testing.T) {
	s := &authService{}
	s.WireFederatedProvider(models.IdentityProviderGoogle, nil)
	s.WireFederatedProvider("", stubProvider{name: "google"})

	if got := s.FederatedProviders(); len(got) != 0 {
		t.Fatalf("got %v, want nothing wired", got)
	}
}

// A handoff that could be collected by any browser holding the code is a
// forwardable login: run the flow against your own provider account, send
// someone the resulting link, and they land in your workspace. The binding is
// what makes the code useless anywhere but the browser that began the flow, so
// an absent one must never be treated as "no binding required".
func TestSSOExchangeRefusesAnUnboundCollection(t *testing.T) {
	s := &authService{}
	for _, binding := range []string{"", "   "} {
		if _, err := s.SSOExchange(context.Background(), "handoff-code", strings.TrimSpace(binding)); err == nil {
			t.Fatalf("a handoff collected with binding %q was accepted", binding)
		} else if err.Identifier != "sso_wrong_browser" {
			t.Fatalf("got %q, want the sso_wrong_browser refusal", err.Identifier)
		}
	}
}

func TestSSOBeginRefusesAnUnconfiguredProvider(t *testing.T) {
	s := &authService{}
	if _, err := s.SSOBegin(context.Background(), models.IdentityProviderGoogle, models.OrgAcquisition{}); err == nil {
		t.Fatal("expected a refusal for a provider this deployment has no client for")
	}
}

// The account security page names what someone signed in with, so a Google
// login must not be recorded as an email one.
func TestSessionProviderNamesTheRealMethod(t *testing.T) {
	tests := map[string]string{
		models.IdentityProviderGoogle: token.AuthProviderGoogle,
		models.IdentityProviderApple:  token.AuthProviderApple,
		models.IdentityProviderOIDC:   token.AuthProviderOIDC,
	}
	for provider, want := range tests {
		if got := sessionProvider(provider); got != want {
			t.Errorf("sessionProvider(%q) = %q, want %q", provider, got, want)
		}
	}
}
