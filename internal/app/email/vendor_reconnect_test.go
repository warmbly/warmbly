package email

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// convertRepo is one mailbox whose in-place conversion is recorded, or refused.
type convertRepo struct {
	stubReauthRepo
	converted int
	refuse    bool
	scopes    []string
}

func (r *convertRepo) ConvertToGoogleOAuth(_ context.Context, orgID, accountID uuid.UUID, access, refresh string, _ time.Time, _ string, scopes []string) (bool, *errx.Error) {
	if r.refuse || orgID != *r.account.OrganizationID || accountID != r.account.ID || access == "" || refresh == "" {
		return false, nil
	}
	r.converted++
	r.scopes = scopes
	return true, nil
}

func reconnectFixture(provider, authMethod, mailHost string) (*emailService, *convertRepo, uuid.UUID) {
	org := uuid.New()
	repo := &convertRepo{stubReauthRepo: stubReauthRepo{account: &models.Email{
		ID: uuid.New(), UserID: uuid.NewString(), OrganizationID: &org, Email: "ava@acme.io",
		Provider: provider, AuthMethod: authMethod, MailHost: mailHost, Status: "active",
	}}}
	return &emailService{emailRepository: repo}, repo, org
}

func TestVendorReconnectTargetIsTheOrganizationsGoogleMailbox(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		provider, auth, host string
		want                 reconnectKind
	}{
		{"gmail", models.MailAuthOAuth, "google_workspace", reconnectRenew},
		{"gmail", models.MailAuthDelegated, "google_workspace", reconnectConvert},
		{"smtp_imap", "app_password", "google_workspace", reconnectConvert},
		{"smtp_imap", "app_password", "gmail", reconnectConvert},
	} {
		svc, repo, org := reconnectFixture(tc.provider, tc.auth, tc.host)
		_, kind, xerr := svc.vendorReconnectTarget(ctx, org, repo.account.ID, "AVA@acme.io")
		if xerr != nil || kind != tc.want {
			t.Fatalf("%s/%s = %v, %v", tc.provider, tc.auth, kind, xerr)
		}
	}

	svc, repo, org := reconnectFixture("gmail", models.MailAuthOAuth, "google_workspace")
	if _, _, xerr := svc.vendorReconnectTarget(ctx, uuid.New(), repo.account.ID, "ava@acme.io"); xerr != errx.ErrNotFound {
		t.Fatalf("another workspace's mailbox = %v", xerr)
	}
	if _, _, xerr := svc.vendorReconnectTarget(ctx, org, repo.account.ID, "leo@acme.io"); xerr != ErrVendorConsentWrongMailbox {
		t.Fatalf("another address = %v", xerr)
	}
	for _, tc := range []struct{ provider, host string }{{"outlook", "microsoft365"}, {"smtp_imap", "other"}} {
		svc, repo, org := reconnectFixture(tc.provider, "", tc.host)
		if _, _, xerr := svc.vendorReconnectTarget(ctx, org, repo.account.ID, "ava@acme.io"); xerr != ErrVendorReconnectNotEligible {
			t.Fatalf("%s on %s = %v", tc.provider, tc.host, xerr)
		}
	}
}

func TestVendorConsentURLRefusesAMailboxOutsideTheOrganization(t *testing.T) {
	t.Setenv("BOX_GOOGLE_CLIENT_ID", "client-1.apps.googleusercontent.com")
	t.Setenv("BOX_GOOGLE_CLIENT_SECRET", "secret")
	t.Setenv("BOX_GOOGLE_OAUTH_CONNECT", "")
	svc, repo, _ := reconnectFixture("gmail", models.MailAuthDelegated, "google_workspace")
	svc.oauthInbox = &config.Oauth2Inbox{Google: config.GoogleOauth2Inbox("https://api.example.test")}
	id := repo.account.ID
	_, xerr := svc.VendorConsentURL(context.Background(), models.VendorConsentRequest{
		OrganizationID: uuid.New(), UserID: uuid.New(), ConnectionID: uuid.New(), Email: "ava@acme.io", EmailAccountID: &id,
	})
	if xerr != errx.ErrNotFound {
		t.Fatalf("a link for another workspace's mailbox = %v", xerr)
	}
}

func TestVendorReconnectConvertsOnlyWhatItChecked(t *testing.T) {
	svc, repo, org := reconnectFixture("gmail", models.MailAuthDelegated, "google_workspace")
	tok := &oauth2.Token{AccessToken: "a", RefreshToken: "r"}
	sess := &models.VendorConsentState{OrganizationID: org, UserID: uuid.New(), Email: "ava@acme.io", EmailAccountID: &repo.account.ID}
	repo.refuse = true
	if _, xerr := svc.vendorReconnect(context.Background(), sess, repo.account, reconnectConvert, tok, &inboxOwner{Email: "ava@acme.io"}); xerr != ErrVendorReconnectNotEligible {
		t.Fatalf("a mailbox the guarded convert refused = %v", xerr)
	}
	if repo.converted != 0 || repo.wroteAccess != "" {
		t.Fatal("the mailbox's sign-in changed after a refusal")
	}
}
