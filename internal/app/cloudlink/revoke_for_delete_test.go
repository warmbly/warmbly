package cloudlink

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/email"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// stubLinkRepo implements only the local calls used by revocation.
type stubLinkRepo struct {
	repository.CloudLinkRepository

	link    *models.CloudLink
	linkErr error

	mailbox    *models.CloudLinkMailbox
	mailboxErr error

	unenrolled  []uuid.UUID
	unenrollErr error
}

func (r *stubLinkRepo) Get(context.Context, *uuid.UUID) (*models.CloudLink, error) {
	return r.link, r.linkErr
}

func (r *stubLinkRepo) GetByInstance(context.Context, uuid.UUID) (*models.CloudLink, error) {
	return r.link, r.linkErr
}

func (r *stubLinkRepo) WithReconciliationLock(_ context.Context, fn func() error) error { return fn() }
func (r *stubLinkRepo) SetDisconnectPending(context.Context, uuid.UUID) error {
	r.link.DisconnectPending = true
	return nil
}
func (r *stubLinkRepo) BeginRemoval(context.Context, uuid.UUID) error {
	r.mailbox.EnrollmentState = "pending_remove"
	return nil
}
func (r *stubLinkRepo) CarryStanding(context.Context, uuid.UUID, *models.WarmupHealthInfo) error {
	return nil
}

func (r *stubLinkRepo) GetByAccount(context.Context, uuid.UUID) (*models.CloudLinkMailbox, error) {
	return r.mailbox, r.mailboxErr
}

func (r *stubLinkRepo) Unenroll(_ context.Context, accountID uuid.UUID) error {
	r.unenrolled = append(r.unenrolled, accountID)
	return r.unenrollErr
}

// stubEmails answers the ownership check.
type stubEmails struct {
	repository.EmailRepository

	account *models.Email
}

func (s stubEmails) GetByID(context.Context, uuid.UUID) (*models.Email, *errx.Error) {
	return s.account, nil
}

type revokeFixture struct {
	svc     *service
	repo    *stubLinkRepo
	org     uuid.UUID
	account uuid.UUID
	// deletes records remote deletion paths.
	deletes *[]string
}

// newRevokeFixture serves the requested cloud response.
func newRevokeFixture(t *testing.T, cloudStatus int) *revokeFixture {
	t.Helper()
	org, account := uuid.New(), uuid.New()
	deletes := &[]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			*deletes = append(*deletes, r.URL.Path)
		}
		w.WriteHeader(cloudStatus)
		switch {
		case cloudStatus == http.StatusNotFound:
			// A missing remote mailbox means revocation already succeeded.
			_, _ = w.Write([]byte(`{"code":"pool_link_mailbox_not_found","message":"That mailbox is not enrolled."}`))
		case cloudStatus >= 400:
			_, _ = w.Write([]byte(`{"code":"internal_error","message":"nope"}`))
		}
	}))
	t.Cleanup(srv.Close)

	repo := &stubLinkRepo{
		link:    &models.CloudLink{CloudURL: srv.URL, Token: "t"},
		mailbox: &models.CloudLinkMailbox{EmailAccountID: account, RemoteID: account},
	}
	svc := &service{
		repo:   repo,
		emails: stubEmails{account: &models.Email{ID: account, OrganizationID: &org}},
		tokens: map[uuid.UUID]cachedToken{},
	}
	return &revokeFixture{svc: svc, repo: repo, org: org, account: account, deletes: deletes}
}

// A successful answer guarantees remote deletion preceded local deletion.
func TestRevokeForDeleteTakesTheMailboxOffTheCloudBeforeTheLocalRow(t *testing.T) {
	f := newRevokeFixture(t, http.StatusNoContent)

	if xerr := f.svc.RevokeForDelete(context.Background(), f.org, f.account); xerr != nil {
		t.Fatalf("RevokeForDelete: %v", xerr)
	}
	if len(*f.deletes) != 1 || (*f.deletes)[0] != "/v1/pool-link/instance/mailboxes/"+f.account.String() {
		t.Fatalf("cloud deletes = %v, want the mailbox's own path", *f.deletes)
	}
	if len(f.repo.unenrolled) != 1 || f.repo.unenrolled[0] != f.account {
		t.Fatalf("local rows dropped = %v, want one for %s", f.repo.unenrolled, f.account)
	}
}

// Remote refusal must preserve the local enrollment for retry.
func TestRevokeForDeleteRefusesWhenTheCloudDoes(t *testing.T) {
	f := newRevokeFixture(t, http.StatusInternalServerError)

	if xerr := f.svc.RevokeForDelete(context.Background(), f.org, f.account); xerr == nil {
		t.Fatal("a refused cloud delete was reported as a revocation")
	}
	if len(f.repo.unenrolled) != 0 {
		t.Errorf("the local row was dropped although the cloud still holds the mailbox: %v", f.repo.unenrolled)
	}
}

// An unreadable link may still own a remote credential.
func TestRevokeForDeleteRefusesAnUnreadableLink(t *testing.T) {
	f := newRevokeFixture(t, http.StatusNoContent)
	f.repo.link, f.repo.linkErr = nil, errors.New("cannot decrypt the instance token")

	if xerr := f.svc.RevokeForDelete(context.Background(), f.org, f.account); xerr == nil {
		t.Fatal("an unreadable link was read as a mailbox the cloud does not hold")
	}
	if len(*f.deletes) != 0 || len(f.repo.unenrolled) != 0 {
		t.Errorf("acted on an unreadable link: deletes=%v unenrolled=%v", *f.deletes, f.repo.unenrolled)
	}
}

// A missing remote mailbox makes a repeated revocation idempotent.
func TestRevokeForDeleteToleratesAMailboxTheCloudHasAlreadyDropped(t *testing.T) {
	f := newRevokeFixture(t, http.StatusNotFound)

	if xerr := f.svc.RevokeForDelete(context.Background(), f.org, f.account); xerr != nil {
		t.Fatalf("a mailbox the cloud no longer holds was refused: %v", xerr)
	}
}

// A mailbox that was never enrolled requires no remote call.
func TestRevokeForDeleteIsANoopWithNothingToRevoke(t *testing.T) {
	f := newRevokeFixture(t, http.StatusNoContent)
	f.repo.mailbox = nil

	if xerr := f.svc.RevokeForDelete(context.Background(), f.org, f.account); xerr != nil {
		t.Fatalf("RevokeForDelete: %v", xerr)
	}
	if len(*f.deletes) != 0 {
		t.Errorf("called the cloud anyway: %v", *f.deletes)
	}
}

// An enrollment without its link cannot prove that the cloud credential is gone.
func TestRevokeForDeleteRefusesAnEnrollmentWithoutALink(t *testing.T) {
	f := newRevokeFixture(t, http.StatusNoContent)
	f.repo.link = nil

	if xerr := f.svc.RevokeForDelete(context.Background(), f.org, f.account); xerr == nil {
		t.Fatal("an enrollment without a link was reported as revoked")
	}
	if len(*f.deletes) != 0 || len(f.repo.unenrolled) != 0 {
		t.Errorf("acted without a link: deletes=%v unenrolled=%v", *f.deletes, f.repo.unenrolled)
	}
}

// Local cleanup failure does not undo confirmed remote revocation.
func TestRevokeForDeleteRetainsRetryWhenTheLocalRowCannotBeDropped(t *testing.T) {
	f := newRevokeFixture(t, http.StatusNoContent)
	f.repo.unenrollErr = errors.New("db down")

	if xerr := f.svc.RevokeForDelete(context.Background(), f.org, f.account); xerr == nil {
		t.Fatal("cleanup failure must retain the pending removal")
	}
	if len(*f.deletes) != 1 {
		t.Fatalf("cloud deletes = %v, want one", *f.deletes)
	}
}

// A mailbox from another workspace is refused before anything is asked of the cloud.
func TestRevokeForDeleteRefusesAForeignMailbox(t *testing.T) {
	f := newRevokeFixture(t, http.StatusNoContent)
	other := uuid.New()
	f.svc.emails = stubEmails{account: &models.Email{ID: f.account, OrganizationID: &other}}

	if xerr := f.svc.RevokeForDelete(context.Background(), f.org, f.account); xerr == nil {
		t.Fatal("revoked an enrollment for a mailbox in another workspace")
	}
	if len(*f.deletes) != 0 {
		t.Errorf("called the cloud for a foreign mailbox: %v", *f.deletes)
	}
}

// stubEmailDeletes records any call the revocation makes into the mailbox
// service, which is what would make it recursive.
type stubEmailDeletes struct {
	email.EmailService

	deletes  *[]string
	onDelete func()
}

func (s stubEmailDeletes) Delete(context.Context, string, string) *errx.Error {
	*s.deletes = append(*s.deletes, "delete")
	if s.onDelete != nil {
		s.onDelete()
	}
	return nil
}

// RevokeForDelete is the leaf that keeps the managed delete from recursing, so
// it must not call back into the mailbox service at all.
func TestRevokeForDeleteDoesNotCallIntoTheMailboxDelete(t *testing.T) {
	f := newRevokeFixture(t, http.StatusNoContent)
	calls := &[]string{}
	f.svc.emailSvc = stubEmailDeletes{deletes: calls}

	if xerr := f.svc.RevokeForDelete(context.Background(), f.org, f.account); xerr != nil {
		t.Fatalf("RevokeForDelete: %v", xerr)
	}
	if len(*calls) != 0 {
		t.Fatalf("the revocation called into the mailbox service: %v", *calls)
	}
}

// Unenrolling a managed mirror releases the cloud link and then deletes the
// mirror. The delete revokes too, so the cloud answers the repeat call with
// pool_link_mailbox_not_found, which is tolerated: neither call loops.
func TestUnenrollReleasesTheCloudBeforeDeletingAManagedMirror(t *testing.T) {
	f := newRevokeFixture(t, http.StatusNoContent)
	deletes := &[]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*deletes = append(*deletes, r.URL.Path)
		if len(*deletes) == 1 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"pool_link_mailbox_not_found","message":"That mailbox is not enrolled."}`))
	}))
	t.Cleanup(srv.Close)
	f.repo.link.CloudURL = srv.URL
	f.deletes = deletes

	calls := &[]string{}
	f.svc.emailSvc = stubEmailDeletes{
		deletes:  calls,
		onDelete: func() { _ = f.svc.RevokeForDelete(context.Background(), f.org, f.account) },
	}
	f.repo.mailbox.Managed = true

	if xerr := f.svc.Unenroll(context.Background(), f.org, f.account); xerr != nil {
		t.Fatalf("Unenroll: %v", xerr)
	}
	if len(*calls) != 1 {
		t.Fatalf("the mirror was deleted %d times, want 1", len(*calls))
	}
	if len(*deletes) != 2 || (*deletes)[0] != (*deletes)[1] {
		t.Fatalf("cloud deletes = %v, want the mailbox path twice", *deletes)
	}
}
