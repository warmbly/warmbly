package email

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// A new Gmail mailbox is refused before any OAuth config is consulted, so an
// instance with no Google client configured still gets the policy answer
// rather than the setup one.
func TestOAuthStart_GmailRefusedUnlessEnabled(t *testing.T) {
	t.Setenv("BOX_GOOGLE_OAUTH_CONNECT", "false")
	org := uuid.New()
	svc := &emailService{}

	_, xerr := svc.OAuthStart(context.Background(), uuid.NewString(), &org, models.InboxProviderGoogle, "", false, "")
	if xerr != errx.ErrEmailOnboardGoogleOAuthDisabled {
		t.Fatalf("expected ErrEmailOnboardGoogleOAuthDisabled, got %v", xerr)
	}
}

// Enabled, the request falls through to the ordinary path; with no client
// configured that is the not-configured error, proving the gate stepped aside.
func TestOAuthStart_GmailAllowedWhenEnabled(t *testing.T) {
	t.Setenv("BOX_GOOGLE_OAUTH_CONNECT", "true")
	t.Setenv("BOX_GOOGLE_CLIENT_ID", "test-client")
	t.Setenv("BOX_GOOGLE_CLIENT_SECRET", "test-secret")
	if !config.GoogleOAuthConnect() {
		t.Fatal("BOX_GOOGLE_OAUTH_CONNECT=true must enable Google sign-in for new mailboxes")
	}
	org := uuid.New()
	svc := &emailService{}

	_, xerr := svc.OAuthStart(context.Background(), uuid.NewString(), &org, models.InboxProviderGoogle, "", false, "")
	if xerr != errx.ErrEmailOnboardGoogleNotConfigured {
		t.Fatalf("expected the gate to step aside (ErrEmailOnboardGoogleNotConfigured), got %v", xerr)
	}
}

// Renewing an existing Gmail mailbox's tokens is never gated: the mailbox is
// already on Google sign-in and this is the only way to keep it working.
func TestOAuthReauth_GmailNotGated(t *testing.T) {
	t.Setenv("BOX_GOOGLE_OAUTH_CONNECT", "")
	svc, repo, _, _ := reauthFixture("gmail", "owner@example.com")

	_, xerr := svc.OAuthReauth(context.Background(), repo.account.UserID, repo.account.OrganizationID, repo.account.ID, "")
	if xerr == errx.ErrEmailOnboardGoogleOAuthDisabled {
		t.Fatal("reauth of an existing Gmail mailbox must not be refused by the new-mailbox gate")
	}
}

// A personal Gmail hint is steered to app passwords before any consent; a Workspace hint passes the gate.
func TestOAuthStart_PersonalGmailRefused(t *testing.T) {
	t.Setenv("BOX_GOOGLE_OAUTH_CONNECT", "true")
	t.Setenv("BOX_GOOGLE_CLIENT_ID", "test-client")
	t.Setenv("BOX_GOOGLE_CLIENT_SECRET", "test-secret")
	org := uuid.New()
	svc := &emailService{}

	for hint, want := range map[string]*errx.Error{
		"someone" + "@gmail.com":          errx.ErrEmailOnboardGooglePersonal,
		" Someone" + "@GoogleMail.com ":   errx.ErrEmailOnboardGooglePersonal,
		"someone" + "@example.com":        errx.ErrEmailOnboardGoogleNotConfigured,
		"someone" + "@gmail.com.evil.org": errx.ErrEmailOnboardGoogleNotConfigured,
		"":                                errx.ErrEmailOnboardGoogleNotConfigured,
	} {
		_, xerr := svc.OAuthStart(context.Background(), uuid.NewString(), &org, models.InboxProviderGoogle, hint, false, "")
		if xerr != want {
			t.Errorf("hint %q: got %v, want %v", hint, xerr, want)
		}
	}
}
