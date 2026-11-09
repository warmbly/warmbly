package email

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"golang.org/x/oauth2"
)

// stubReauthRepo serves one mailbox and records the reconnect writes.
type stubReauthRepo struct {
	repository.EmailRepository

	account       *models.Email
	storedRefresh string
	updateErr     *errx.Error

	wroteAccess  string
	wroteRefresh string
	wroteScopes  []string
	updated      *models.UpdateEmail
}

// GetSyncSkipFolders answers the loader's skip-list read with an empty list.
func (s *stubReauthRepo) GetSyncSkipFolders(context.Context, uuid.UUID) ([]string, *errx.Error) {
	return nil, nil
}

func (s *stubReauthRepo) GetByID(ctx context.Context, emailAccountID uuid.UUID) (*models.Email, *errx.Error) {
	return s.account, nil
}

func (s *stubReauthRepo) Get(ctx context.Context, orgID, emailAccountID string) (*models.Email, *errx.Error) {
	// The real org-scoped Get does not select user_id or organization_id;
	// mimic that so a caller depending on them fails here too (it did once).
	partial := *s.account
	partial.UserID = ""
	partial.OrganizationID = nil
	return &partial, nil
}

func (s *stubReauthRepo) GetOAuthCredentials(ctx context.Context, emailAccountID uuid.UUID) (*repository.OAuthCredentials, *errx.Error) {
	return &repository.OAuthCredentials{RefreshToken: s.storedRefresh}, nil
}

func (s *stubReauthRepo) RefreshBoxToken(ctx context.Context, id uuid.UUID, accessToken, refreshToken string, expiresAt time.Time) error {
	s.wroteAccess = accessToken
	s.wroteRefresh = refreshToken
	return nil
}

func (s *stubReauthRepo) ReplaceBoxToken(ctx context.Context, id uuid.UUID, accessToken, refreshToken string, expiresAt time.Time, scopes []string, appID *uuid.UUID) error {
	s.wroteScopes = scopes
	return s.RefreshBoxToken(ctx, id, accessToken, refreshToken, expiresAt)
}

func (s *stubReauthRepo) Update(ctx context.Context, userID, emailAccountID string, udata *models.UpdateEmail) (*models.Email, *errx.Error) {
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	s.updated = udata
	return s.account, nil
}

// stubErrorsRepo records which error codes a reconnect resolved.
type stubErrorsRepo struct {
	repository.EmailAccountErrorRepository

	resolved []string
}

func (s *stubErrorsRepo) ResolveByCodes(ctx context.Context, accountID uuid.UUID, codes []string, resolvedBy string) *errx.Error {
	s.resolved = append(s.resolved, codes...)
	return nil
}

func reauthFixture(provider, email string) (*emailService, *stubReauthRepo, *stubErrorsRepo, *models.EmailOnboardingState) {
	org := uuid.New()
	accountID := uuid.New()
	repo := &stubReauthRepo{
		account: &models.Email{
			ID:             accountID,
			UserID:         uuid.NewString(),
			OrganizationID: &org,
			Email:          email,
			Provider:       provider,
			Status:         "inactive",
		},
		storedRefresh: "stored-refresh",
	}
	errs := &stubErrorsRepo{}
	svc := &emailService{emailRepository: repo, accountErrors: errs}
	sess := &models.EmailOnboardingState{
		UserID:         repo.account.UserID,
		OrganizationID: &org,
		Provider:       provider,
		EmailAccountID: &accountID,
	}
	return svc, repo, errs, sess
}

func TestFinishReauth_WrongAccountIsRefused(t *testing.T) {
	svc, repo, _, sess := reauthFixture("gmail", "owner@example.com")

	tok := &oauth2.Token{AccessToken: "new-access", RefreshToken: "new-refresh"}
	_, xerr := svc.finishReauth(context.Background(), sess, models.InboxProviderGoogle, tok, &inboxOwner{Email: "somebody-else@example.com"})
	if xerr != errx.ErrEmailReauthWrongAccount {
		t.Fatalf("expected ErrEmailReauthWrongAccount, got %v", xerr)
	}
	if repo.wroteAccess != "" || repo.updated != nil {
		t.Fatalf("a refused reauth must write nothing (access %q, update %v)", repo.wroteAccess, repo.updated)
	}
}

func TestFinishReauth_UpdatesTokensResolvesErrorsAndReactivates(t *testing.T) {
	svc, repo, errs, sess := reauthFixture("gmail", "owner@example.com")

	tok := &oauth2.Token{AccessToken: "new-access", RefreshToken: "new-refresh"}
	// The consent address matches case-insensitively, as addresses do.
	if _, xerr := svc.finishReauth(context.Background(), sess, models.InboxProviderGoogle, tok, &inboxOwner{Email: "Owner@Example.com"}); xerr != nil {
		t.Fatalf("finishReauth: %v", xerr)
	}
	if repo.wroteAccess != "new-access" || repo.wroteRefresh != "new-refresh" {
		t.Fatalf("tokens not written: access %q refresh %q", repo.wroteAccess, repo.wroteRefresh)
	}
	if repo.updated == nil || repo.updated.Status == nil || *repo.updated.Status != "active" {
		t.Fatalf("reauth must reactivate the mailbox, got %+v", repo.updated)
	}
	want := map[string]bool{}
	for _, c := range errx.CredentialMailErrorCodes {
		want[string(c)] = true
	}
	for _, c := range errs.resolved {
		delete(want, c)
	}
	if len(errs.resolved) == 0 || len(want) != 0 {
		t.Fatalf("credential errors not resolved: got %v", errs.resolved)
	}
}

func TestFinishReauth_KeepsStoredRefreshTokenWhenProviderOmitsIt(t *testing.T) {
	svc, repo, _, sess := reauthFixture("gmail", "owner@example.com")

	tok := &oauth2.Token{AccessToken: "new-access"} // no refresh token on repeat consent
	if _, xerr := svc.finishReauth(context.Background(), sess, models.InboxProviderGoogle, tok, &inboxOwner{Email: "owner@example.com"}); xerr != nil {
		t.Fatalf("finishReauth: %v", xerr)
	}
	if repo.wroteRefresh != "stored-refresh" {
		t.Fatalf("stored refresh token must be kept, got %q", repo.wroteRefresh)
	}
}

func TestFinishReauth_RefusesWhenNoRefreshTokenAnywhere(t *testing.T) {
	svc, repo, _, sess := reauthFixture("gmail", "owner@example.com")
	repo.storedRefresh = ""

	tok := &oauth2.Token{AccessToken: "new-access"} // provider omitted it, nothing stored
	_, xerr := svc.finishReauth(context.Background(), sess, models.InboxProviderGoogle, tok, &inboxOwner{Email: "owner@example.com"})
	if xerr != errx.ErrEmailReauthNoRefreshToken {
		t.Fatalf("expected ErrEmailReauthNoRefreshToken, got %v", xerr)
	}
	if repo.wroteAccess != "" {
		t.Fatalf("must not seal an empty refresh token over the stored row")
	}
}

func TestFinishReauth_KeepsErrorsWhenReactivationFails(t *testing.T) {
	svc, repo, errs, sess := reauthFixture("gmail", "owner@example.com")
	repo.updateErr = errx.InternalError()

	tok := &oauth2.Token{AccessToken: "new-access", RefreshToken: "new-refresh"}
	_, xerr := svc.finishReauth(context.Background(), sess, models.InboxProviderGoogle, tok, &inboxOwner{Email: "owner@example.com"})
	if xerr == nil {
		t.Fatal("expected the failed reactivation to surface")
	}
	// The banner (and its reconnect button) must survive a failed reactivation.
	if len(errs.resolved) != 0 {
		t.Fatalf("errors must stay unresolved when reactivation fails, resolved %v", errs.resolved)
	}
}

func TestOAuthReauth_RefusesSMTPIMAPMailboxes(t *testing.T) {
	svc, repo, _, _ := reauthFixture("smtp_imap", "owner@example.com")

	_, xerr := svc.OAuthReauth(context.Background(), repo.account.UserID, repo.account.OrganizationID, repo.account.ID, "")
	if xerr != errx.ErrEmailReauthProvider {
		t.Fatalf("expected ErrEmailReauthProvider, got %v", xerr)
	}
}

func TestUpdateSMTPIMAPCredentials_RefusesOAuthMailboxes(t *testing.T) {
	svc, repo, _, _ := reauthFixture("gmail", "owner@example.com")

	creds := &models.SmtpImap{
		SMTP: &models.Service{Host: "smtp.example.com", Port: 587},
		IMAP: &models.Service{Host: "imap.example.com", Port: 993},
	}
	_, xerr := svc.UpdateSMTPIMAPCredentials(context.Background(), repo.account.OrganizationID, repo.account.ID, creds)
	if xerr != errx.ErrEmailReauthOAuthOnly {
		t.Fatalf("expected ErrEmailReauthOAuthOnly, got %v", xerr)
	}
}
