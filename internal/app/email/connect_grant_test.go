package email

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"golang.org/x/oauth2"
)

// A handed-over sign-in is checked before any provider is called, and only the workspace's own app can redeem it.
func TestRedeemGrantRefusesBeforeCallingTheProvider(t *testing.T) {
	ctx := context.Background()
	org := uuid.New()
	own := &oauth2.Config{ClientID: "acme.apps.googleusercontent.com", Endpoint: oauth2.Endpoint{TokenURL: "https://invalid.example/token"}}

	none := &emailService{oauthInbox: deploymentInbox()}
	none.WireOAuthApps(fakeApps{}, fakePartnership(true))
	if _, _, _, xerr := none.redeemGrant(ctx, org, models.MailboxOAuthGrant{Provider: models.InboxProviderGoogle, RefreshToken: "rt"}); xerr != ErrGrantAppRequired {
		t.Fatalf("without the workspace's own app: %v", xerr)
	}

	s := &emailService{oauthInbox: deploymentInbox()}
	s.WireOAuthApps(fakeApps{cfg: own, id: uuid.New()}, fakePartnership(true))
	cases := map[string]struct {
		g    models.MailboxOAuthGrant
		want *errx.Error
	}{
		"smtp provider": {models.MailboxOAuthGrant{Provider: models.InboxProviderSMTPIMAP, RefreshToken: "rt"}, errx.ErrEmailOnboardProvider},
		"nothing":       {models.MailboxOAuthGrant{Provider: models.InboxProviderGoogle}, ErrGrantInput},
		"both":          {models.MailboxOAuthGrant{Provider: models.InboxProviderGoogle, RefreshToken: "rt", Code: "c"}, ErrGrantInput},
		"code, no uri":  {models.MailboxOAuthGrant{Provider: models.InboxProviderGoogle, Code: "c"}, ErrGrantRedirect},
		"code, http":    {models.MailboxOAuthGrant{Provider: models.InboxProviderGoogle, Code: "c", RedirectURI: "http://acme.com/cb"}, ErrGrantRedirect},
	}
	for name, c := range cases {
		if _, _, _, xerr := s.redeemGrant(ctx, org, c.g); xerr != c.want {
			t.Errorf("%s: %v, want %v", name, xerr, c.want)
		}
	}
}
