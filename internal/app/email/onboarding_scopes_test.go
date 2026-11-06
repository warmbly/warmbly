package email

import (
	"context"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
)

const (
	scopeFullMail = "https://mail.google.com/"
	scopeModify   = "https://www.googleapis.com/auth/gmail.modify"
	scopeSettings = "https://www.googleapis.com/auth/gmail.settings.basic"
)

func googleScopes(t *testing.T) []string {
	t.Helper()
	return config.GoogleOauth2Inbox("https://api.example.test").Scopes
}

// A new Google mailbox needs full mail access, which is what its warmup engagement signs in to IMAP with.
func TestNewGoogleConnectRefusesWithoutFullMailAccess(t *testing.T) {
	xerr := checkGrantedScopes(context.Background(), models.InboxProviderGoogle, requiredScopes(googleScopes(t), false),
		tokenWithScope(scopeModify+" "+scopeSettings))
	if xerr == nil {
		t.Fatal("a new connect without full mail access was accepted")
	}
	if !strings.Contains(xerr.Message, "permanently delete all your email from Gmail") {
		t.Errorf("the error should name the unticked permission as the consent screen does, got %q", xerr.Message)
	}
	if xerr := checkGrantedScopes(context.Background(), models.InboxProviderGoogle, requiredScopes(googleScopes(t), false),
		tokenWithScope(scopeFullMail+" "+scopeSettings)); xerr != nil {
		t.Fatalf("the full consent was refused: %v", xerr)
	}
}

// A reconnect asks for full mail access but keeps a mailbox that only grants gmail.modify, on the API path.
func TestGoogleReauthAcceptsTheOldGrant(t *testing.T) {
	want := requiredScopes(googleScopes(t), true)
	for _, granted := range []string{scopeModify + " " + scopeSettings, scopeFullMail + " " + scopeSettings} {
		if xerr := checkGrantedScopes(context.Background(), models.InboxProviderGoogle, want, tokenWithScope(granted)); xerr != nil {
			t.Fatalf("reauth granting %q was refused: %v", granted, xerr)
		}
	}
	if xerr := checkGrantedScopes(context.Background(), models.InboxProviderGoogle, want, tokenWithScope(scopeSettings)); xerr == nil {
		t.Fatal("a reauth with no mail access at all was accepted")
	}
}

func TestGoogleConsentAsksForFullMailAccess(t *testing.T) {
	scopes := googleScopes(t)
	if len(scopes) != 2 || scopes[0] != scopeFullMail || scopes[1] != scopeSettings {
		t.Fatalf("Google mailbox consent = %v", scopes)
	}
	if got := requiredScopes(scopes, true); got[0] != scopeModify || scopes[0] != scopeFullMail {
		t.Fatalf("reauth floor = %v and must not rewrite the request %v", got, scopes)
	}
}

func TestDelegationScopesAreUnchanged(t *testing.T) {
	want := []string{scopeModify, scopeSettings, config.GoogleDirectoryScope}
	if strings.Join(config.GoogleDelegationScopes, " ") != strings.Join(want, " ") {
		t.Fatalf("whole-domain scopes moved to %v; administrators authorized %v", config.GoogleDelegationScopes, want)
	}
}
