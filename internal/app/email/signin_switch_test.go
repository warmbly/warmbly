package email

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/warmbly/warmbly/internal/app/worker"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func TestRecordedScopesIsWhatTheProviderGranted(t *testing.T) {
	tok := (&oauth2.Token{AccessToken: "a"}).WithExtra(map[string]any{
		"scope": "https://mail.google.com/ openid https://mail.google.com/  email",
	})
	if got := recordedScopes(tok); !slices.Equal(got, []string{"email", "https://mail.google.com/", "openid"}) {
		t.Fatalf("scopes = %v", got)
	}
	if got := recordedScopes(&oauth2.Token{AccessToken: "a"}); got != nil {
		t.Fatalf("a token naming no scope recorded %v, want unknown", got)
	}
	if full := models.FullMailAccess(recordedScopes(tok)); full == nil || !*full {
		t.Fatal("full mail access not read from the recorded scopes")
	}
	if models.FullMailAccess(nil) != nil {
		t.Fatal("unrecorded scopes read as known")
	}
}

func TestFinishReauthRecordsTheNewConsentsScopes(t *testing.T) {
	addr := "ava" + "@acme.io"
	svc, repo, _, sess := reauthFixture("gmail", addr)
	tok := (&oauth2.Token{AccessToken: "fresh", RefreshToken: "renewed"}).WithExtra(map[string]any{
		"scope": "https://www.googleapis.com/auth/gmail.modify https://mail.google.com/",
	})
	if _, xerr := svc.finishReauth(context.Background(), sess, models.InboxProviderGoogle, tok, &inboxOwner{Email: addr}); xerr != nil {
		t.Fatal(xerr)
	}
	if !slices.Equal(repo.wroteScopes, []string{"https://mail.google.com/", "https://www.googleapis.com/auth/gmail.modify"}) {
		t.Fatalf("scopes written = %v", repo.wroteScopes)
	}
}

func TestVendorReconnectRecordsTheConsentsScopes(t *testing.T) {
	svc, repo, org := reconnectFixture("gmail", models.MailAuthDelegated, "google_workspace")
	// Switched off by hand, so the reload after the convert does not put it to work.
	repo.account.Status = "inactive"
	target, kind, xerr := svc.vendorReconnectTarget(context.Background(), org, repo.account.ID, repo.account.Email)
	if xerr != nil {
		t.Fatal(xerr)
	}
	tok := (&oauth2.Token{AccessToken: "a", RefreshToken: "r"}).WithExtra(map[string]any{"scope": models.GoogleFullMailScope})
	sess := &models.VendorConsentState{OrganizationID: org, UserID: uuid.New(), Email: repo.account.Email}
	_, _ = svc.vendorReconnect(context.Background(), sess, target, kind, tok, &inboxOwner{Email: repo.account.Email})
	if repo.converted != 1 || !slices.Equal(repo.scopes, []string{models.GoogleFullMailScope}) {
		t.Fatalf("converted %d with scopes %v", repo.converted, repo.scopes)
	}
}

// switchRepo is one Google mailbox an app password may take over; conversions are counted.
type switchRepo struct {
	stubReauthRepo
	converted int
}

func (r *switchRepo) FindInOrganization(_ context.Context, orgID uuid.UUID, _ string) (*models.EmailRef, *errx.Error) {
	if orgID != *r.account.OrganizationID {
		return nil, nil
	}
	return &models.EmailRef{ID: r.account.ID, Provider: r.account.Provider, Status: r.account.Status, AuthMethod: r.account.AuthMethod}, nil
}

func (r *switchRepo) ConvertGoogleToAppPassword(context.Context, uuid.UUID, uuid.UUID, *models.SmtpImap, string) (bool, *errx.Error) {
	r.converted++
	return true, nil
}

// noValidators is a fleet with no worker able to try the credentials.
type noValidators struct{ worker.WorkerAssignmentService }

func (noValidators) ValidationWorkers(context.Context, uuid.UUID, *uuid.UUID) ([]models.Worker, error) {
	return nil, nil
}

// The app password is tried against Google from a worker first; a switch that cannot be verified changes nothing.
func TestSwitchToAppPasswordVerifiesBeforeItConverts(t *testing.T) {
	for _, auth := range []string{models.MailAuthOAuth, models.MailAuthDelegated} {
		org := uuid.New()
		repo := &switchRepo{stubReauthRepo: stubReauthRepo{account: &models.Email{
			ID: uuid.New(), OrganizationID: &org, Email: "ava" + "@acme.io", Provider: "gmail", AuthMethod: auth, Status: "active",
		}}}
		svc := &emailService{emailRepository: repo, workerAssignment: noValidators{}}
		if _, xerr := svc.SwitchToAppPassword(context.Background(), &org, repo.account.ID, "abcd efgh ijkl mnop"); xerr != errx.ErrEmailOnboardNoWorker {
			t.Fatalf("%s: unverifiable switch = %v", auth, xerr)
		}
		if repo.converted != 0 {
			t.Fatalf("%s: converted without verifying the password", auth)
		}
		if _, xerr := svc.SwitchToAppPassword(context.Background(), &org, repo.account.ID, "short"); xerr != ErrAppPasswordShape || repo.converted != 0 {
			t.Fatalf("%s: malformed password = %v, converted %d", auth, xerr, repo.converted)
		}
	}

	org := uuid.New()
	repo := &switchRepo{stubReauthRepo: stubReauthRepo{account: &models.Email{
		ID: uuid.New(), OrganizationID: &org, Email: "ava" + "@acme.io", Provider: "smtp_imap", AuthMethod: "app_password", Status: "active",
	}}}
	svc := &emailService{emailRepository: repo, workerAssignment: noValidators{}}
	if _, xerr := svc.SwitchToAppPassword(context.Background(), &org, repo.account.ID, "abcdefghijklmnop"); xerr != ErrNotGoogleSignin || repo.converted != 0 {
		t.Fatalf("a mailbox already on an app password = %v, converted %d", xerr, repo.converted)
	}
}
