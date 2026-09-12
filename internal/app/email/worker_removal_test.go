package email

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/worker"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// stubRemovalRepo answers the handful of calls the disable and disconnect
// paths make. Embedding the interface keeps the stub to those; anything else
// panics loudly rather than passing silently.
type stubRemovalRepo struct {
	repository.EmailRepository

	trace *[]string

	account     *models.Email
	getErr      *errx.Error
	workerID    *uuid.UUID
	workerErr   *errx.Error
	updateErr   *errx.Error
	deleteErr   *errx.Error
	statusSet   []string
	refunded    []float64
	deleteCalls int
	workerCalls int
}

func (s *stubRemovalRepo) record(step string) {
	if s.trace != nil {
		*s.trace = append(*s.trace, step)
	}
}

func (s *stubRemovalRepo) Update(ctx context.Context, orgID, emailAccountID string, udata *models.UpdateEmail) (*models.Email, *errx.Error) {
	if udata.Status != nil {
		s.statusSet = append(s.statusSet, *udata.Status)
	}
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	// Deliberately mirrors the real repository: its RETURNING list carries the
	// mailbox as the dashboard sees it and no worker assignment, so reading
	// WorkerID off this row would find nothing.
	id, _ := uuid.Parse(emailAccountID)
	status := "inactive"
	if udata.Status != nil {
		status = *udata.Status
	}
	return &models.Email{ID: id, Status: status}, nil
}

func (s *stubRemovalRepo) GetByID(ctx context.Context, emailAccountID uuid.UUID) (*models.Email, *errx.Error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.account, nil
}

func (s *stubRemovalRepo) GetWorkerID(ctx context.Context, emailAccountID uuid.UUID) (*uuid.UUID, *errx.Error) {
	s.workerCalls++
	return s.workerID, s.workerErr
}

func (s *stubRemovalRepo) GetSMTPCredentials(ctx context.Context, emailAccountID uuid.UUID) (*repository.SMTPCredentials, *errx.Error) {
	return &repository.SMTPCredentials{SMTPHost: "smtp.test.local", SMTPPort: 587, IMAPHost: "imap.test.local", IMAPPort: 993}, nil
}

func (s *stubRemovalRepo) Delete(ctx context.Context, userID, emailAccountID string, workerLoadRefund float64) *errx.Error {
	s.deleteCalls++
	s.refunded = append(s.refunded, workerLoadRefund)
	s.record("delete")
	return s.deleteErr
}

// stubEventPublisher records what was shipped to workers.
type stubEventPublisher struct {
	events.Publisher

	trace *[]string

	removeErr error
	removed   []workerRemoval
	added     []uuid.UUID
}

var errBusDown = errors.New("bus down")

type workerRemoval struct {
	workerID uuid.UUID
	userID   string
	emailID  string
}

func (p *stubEventPublisher) PublishRemoveEmail(ctx context.Context, workerID uuid.UUID, remove *models.RemoveWorkerEmail) error {
	if p.trace != nil {
		*p.trace = append(*p.trace, "remove")
	}
	p.removed = append(p.removed, workerRemoval{workerID: workerID, userID: remove.UserID, emailID: remove.EmailID})
	return p.removeErr
}

func (p *stubEventPublisher) PublishAddEmail(ctx context.Context, workerID uuid.UUID, email *models.AddWorkerEmail) error {
	if p.trace != nil {
		*p.trace = append(*p.trace, "add")
	}
	p.added = append(p.added, email.ID)
	return nil
}

type removalFixture struct {
	svc     *emailService
	repo    *stubRemovalRepo
	pub     *stubEventPublisher
	assign  *stubAssignment
	trace   []string
	user    uuid.UUID
	org     uuid.UUID
	mailbox uuid.UUID
	worker  uuid.UUID
}

func newRemovalFixture(t *testing.T) *removalFixture {
	t.Helper()
	f := &removalFixture{user: uuid.New(), org: uuid.New(), mailbox: uuid.New(), worker: uuid.New()}
	f.repo = &stubRemovalRepo{
		trace:    &f.trace,
		workerID: &f.worker,
		account: &models.Email{
			ID:             f.mailbox,
			UserID:         f.user.String(),
			OrganizationID: &f.org,
			WorkerID:       &f.worker,
			Email:          "box@test.local",
			Provider:       "smtp_imap",
			Status:         "active",
		},
	}
	f.pub = &stubEventPublisher{trace: &f.trace}
	f.assign = &stubAssignment{live: true}
	f.svc = &emailService{emailRepository: f.repo, publisher: f.pub, workerAssignment: f.assign}
	return f
}

// The defect: a mailbox switched off in the dashboard kept syncing on its old
// schedule, because Update wrote the row and told nobody.
func TestDisablingAMailboxTellsTheWorkerToDropIt(t *testing.T) {
	f := newRemovalFixture(t)
	inactive := "inactive"

	if _, xerr := f.svc.Update(context.Background(), f.org.String(), f.user.String(), f.mailbox.String(), &models.UpdateEmail{Status: &inactive}); xerr != nil {
		t.Fatalf("update: %v", xerr)
	}

	if len(f.pub.removed) != 1 {
		t.Fatalf("published %d removals, want 1: the worker keeps syncing a disabled mailbox until it restarts", len(f.pub.removed))
	}
	got := f.pub.removed[0]
	if got.workerID != f.worker {
		t.Errorf("removal sent to worker %s, want %s", got.workerID, f.worker)
	}
	if got.emailID != f.mailbox.String() || got.userID != f.user.String() {
		t.Errorf("removal carried user=%s email=%s, want user=%s email=%s", got.userID, got.emailID, f.user, f.mailbox)
	}
	// The assignment has to be asked for on its own: the row Update returns
	// carries no worker_id, which is what made the consumer's removal
	// unreachable the first time.
	if f.repo.workerCalls != 1 {
		t.Errorf("asked for the assignment %d times, want 1", f.repo.workerCalls)
	}
	if len(f.pub.added) != 0 {
		t.Errorf("a disabled mailbox was shipped back to a worker: %v", f.pub.added)
	}
}

// A revoked mailbox is just as unusable as a disabled one, and the status
// column carries both.
func TestRevokingAMailboxAlsoDropsItFromTheWorker(t *testing.T) {
	f := newRemovalFixture(t)
	revoked := "revoked"

	if _, xerr := f.svc.Update(context.Background(), f.org.String(), f.user.String(), f.mailbox.String(), &models.UpdateEmail{Status: &revoked}); xerr != nil {
		t.Fatalf("update: %v", xerr)
	}
	if len(f.pub.removed) != 1 {
		t.Fatalf("published %d removals for a revoked mailbox, want 1", len(f.pub.removed))
	}
}

// Re-enabling is the other half of the same silence: without this the mailbox
// waits on the reconciler's next pass, up to five minutes, before it syncs or
// sends again.
func TestReenablingAMailboxShipsItBackToItsWorker(t *testing.T) {
	f := newRemovalFixture(t)
	active := "active"

	if _, xerr := f.svc.Update(context.Background(), f.org.String(), f.user.String(), f.mailbox.String(), &models.UpdateEmail{Status: &active}); xerr != nil {
		t.Fatalf("update: %v", xerr)
	}

	if len(f.pub.added) != 1 || f.pub.added[0] != f.mailbox {
		t.Fatalf("shipped %v back to the worker, want one %s", f.pub.added, f.mailbox)
	}
	if len(f.pub.removed) != 0 {
		t.Errorf("a re-enabled mailbox was also removed: %v", f.pub.removed)
	}
}

// Every other PATCH (a signature, a daily cap, a tag) must leave the worker
// alone: re-shipping decrypted credentials on every keystroke is not free.
func TestAPatchThatLeavesTheStatusAloneDoesNotTouchTheWorker(t *testing.T) {
	f := newRemovalFixture(t)
	name := "New name"

	if _, xerr := f.svc.Update(context.Background(), f.org.String(), f.user.String(), f.mailbox.String(), &models.UpdateEmail{Name: &name}); xerr != nil {
		t.Fatalf("update: %v", xerr)
	}
	if len(f.pub.removed) != 0 || len(f.pub.added) != 0 {
		t.Errorf("a plain edit reached the worker: %d removals, %d loads", len(f.pub.removed), len(f.pub.added))
	}
}

// A status write that failed means the mailbox is still active, so nothing
// should be told to drop it.
func TestAFailedStatusWriteNeverReachesTheWorker(t *testing.T) {
	f := newRemovalFixture(t)
	f.repo.updateErr = errx.InternalError()
	inactive := "inactive"

	if _, xerr := f.svc.Update(context.Background(), f.org.String(), f.user.String(), f.mailbox.String(), &models.UpdateEmail{Status: &inactive}); xerr == nil {
		t.Fatal("a failed status write was reported as success")
	}
	if len(f.pub.removed) != 0 || f.repo.workerCalls != 0 {
		t.Errorf("acted on a failed status write: %d removals, %d assignment lookups", len(f.pub.removed), f.repo.workerCalls)
	}
}

// Disabling stays best-effort: the status is written, no load path ships a
// mailbox that is not active, and the customer's edit must still succeed.
func TestDisablingSucceedsEvenWhenTheBusIsDown(t *testing.T) {
	f := newRemovalFixture(t)
	f.pub.removeErr = errBusDown
	inactive := "inactive"

	if _, xerr := f.svc.Update(context.Background(), f.org.String(), f.user.String(), f.mailbox.String(), &models.UpdateEmail{Status: &inactive}); xerr != nil {
		t.Fatalf("a bus failure blocked the status change: %v", xerr)
	}
	if len(f.pub.removed) != 1 {
		t.Errorf("published %d removals, want 1 attempt", len(f.pub.removed))
	}
}

// Deleting is the path with no safety net: once the row is gone there is no
// assignment left to read and no reconciler that can repair a missed removal.
func TestDeleteTellsTheWorkerBeforeTheRowGoes(t *testing.T) {
	f := newRemovalFixture(t)

	if xerr := f.svc.Delete(context.Background(), f.user.String(), f.mailbox.String()); xerr != nil {
		t.Fatalf("delete: %v", xerr)
	}

	if len(f.pub.removed) != 1 {
		t.Fatalf("published %d removals, want 1: the worker keeps syncing an account that no longer exists", len(f.pub.removed))
	}
	if f.pub.removed[0].workerID != f.worker || f.pub.removed[0].emailID != f.mailbox.String() {
		t.Errorf("removal = %+v, want worker %s and mailbox %s", f.pub.removed[0], f.worker, f.mailbox)
	}
	if len(f.trace) != 2 || f.trace[0] != "remove" || f.trace[1] != "delete" {
		t.Errorf("order was %v, want the removal published before the row is deleted", f.trace)
	}
	if f.repo.deleteCalls != 1 {
		t.Errorf("delete called %d times, want 1", f.repo.deleteCalls)
	}
}

// Reliable, not best-effort: a removal that could not be sent leaves the
// mailbox in place so the customer can try again, rather than stranding it on
// a worker forever.
func TestDeleteKeepsTheMailboxWhenTheWorkerCannotBeTold(t *testing.T) {
	f := newRemovalFixture(t)
	f.pub.removeErr = errBusDown

	xerr := f.svc.Delete(context.Background(), f.user.String(), f.mailbox.String())
	if xerr == nil {
		t.Fatal("the mailbox was deleted without the worker ever being told")
	}
	if xerr.Code != errx.ServiceUnavailable {
		t.Errorf("error code = %d, want 503 so the client knows to retry", xerr.Code)
	}
	if f.repo.deleteCalls != 0 {
		t.Errorf("the row was deleted %d times after the removal failed, want 0", f.repo.deleteCalls)
	}
	if len(f.repo.refunded) != 0 {
		t.Errorf("worker capacity was refunded for a mailbox that still exists (%v)", f.repo.refunded)
	}
}

// The assignment lookup failing is the same situation: we do not know which
// worker to tell.
func TestDeleteKeepsTheMailboxWhenTheAssignmentCannotBeRead(t *testing.T) {
	f := newRemovalFixture(t)
	f.repo.workerErr = errx.InternalError()

	if xerr := f.svc.Delete(context.Background(), f.user.String(), f.mailbox.String()); xerr == nil {
		t.Fatal("the mailbox was deleted on an unreadable assignment")
	}
	if f.repo.deleteCalls != 0 {
		t.Errorf("the row was deleted %d times, want 0", f.repo.deleteCalls)
	}
}

// A mailbox on no worker has nothing to remove, and must still delete.
func TestDeleteWithoutAWorkerStillRemovesTheRow(t *testing.T) {
	f := newRemovalFixture(t)
	f.repo.workerID = nil
	f.repo.account.WorkerID = nil

	if xerr := f.svc.Delete(context.Background(), f.user.String(), f.mailbox.String()); xerr != nil {
		t.Fatalf("delete: %v", xerr)
	}
	if len(f.pub.removed) != 0 {
		t.Errorf("published %d removals for a mailbox on no worker, want 0", len(f.pub.removed))
	}
	if f.repo.deleteCalls != 1 {
		t.Errorf("delete called %d times, want 1", f.repo.deleteCalls)
	}
}

// The worker keeps counting a deleted mailbox against its capacity otherwise:
// the foreign key nulls worker_id and nothing refunds the load. The refund
// rides inside the delete, so it cannot be left half-applied against a row that
// no longer says which worker was charged.
func TestDeleteGivesTheWorkerItsCapacityBack(t *testing.T) {
	f := newRemovalFixture(t)

	if xerr := f.svc.Delete(context.Background(), f.user.String(), f.mailbox.String()); xerr != nil {
		t.Fatalf("delete: %v", xerr)
	}
	if len(f.repo.refunded) != 1 || f.repo.refunded[0] != worker.MailboxWeight("smtp_imap", false) {
		t.Errorf("refunds handed to the delete = %v, want one smtp_imap weight", f.repo.refunded)
	}
}

// A warming Gmail mailbox is charged less than a cold SMTP one, and has to be
// refunded what it was actually charged.
func TestDeleteRefundsTheWeightTheMailboxWasChargedAt(t *testing.T) {
	f := newRemovalFixture(t)
	warming := time.Now()
	f.repo.account.Provider = "gmail"
	f.repo.account.Warmup = &warming

	if xerr := f.svc.Delete(context.Background(), f.user.String(), f.mailbox.String()); xerr != nil {
		t.Fatalf("delete: %v", xerr)
	}
	if len(f.repo.refunded) != 1 || f.repo.refunded[0] != worker.MailboxWeight("gmail", true) {
		t.Errorf("refund = %v, want the warmup weight", f.repo.refunded)
	}
}

// The removal must never be reachable for a mailbox the caller does not own:
// the lookup that finds it is unscoped, so ownership is checked here.
func TestDeleteRefusesAMailboxTheCallerDoesNotOwn(t *testing.T) {
	f := newRemovalFixture(t)
	f.repo.account.UserID = uuid.New().String()

	xerr := f.svc.Delete(context.Background(), f.user.String(), f.mailbox.String())
	if xerr != errx.ErrNotFound {
		t.Fatalf("error = %v, want not found", xerr)
	}
	if len(f.pub.removed) != 0 || f.repo.deleteCalls != 0 {
		t.Errorf("acted on someone else's mailbox: %d removals, %d deletes", len(f.pub.removed), f.repo.deleteCalls)
	}
}

// Owner ids arriving in different letter case are the same owner; Postgres
// compares them as uuids and so does this.
func TestDeleteAcceptsTheOwnerInAnyCase(t *testing.T) {
	f := newRemovalFixture(t)
	f.repo.account.UserID = uuidUpper(f.user)

	if xerr := f.svc.Delete(context.Background(), f.user.String(), f.mailbox.String()); xerr != nil {
		t.Fatalf("the owner was refused their own mailbox: %v", xerr)
	}
}

func TestDeleteRejectsAMalformedID(t *testing.T) {
	f := newRemovalFixture(t)

	if xerr := f.svc.Delete(context.Background(), f.user.String(), "not-a-uuid"); xerr != errx.ErrUuid {
		t.Fatalf("error = %v, want a uuid error", xerr)
	}
	if f.repo.deleteCalls != 0 {
		t.Errorf("a malformed id reached the delete (%d calls)", f.repo.deleteCalls)
	}
}

// Deleting a mailbox that is already gone is a 404, not a removal published
// into the void.
func TestDeleteOfAMissingMailboxPublishesNothing(t *testing.T) {
	f := newRemovalFixture(t)
	f.repo.getErr = errx.ErrNotFound

	if xerr := f.svc.Delete(context.Background(), f.user.String(), f.mailbox.String()); xerr != errx.ErrNotFound {
		t.Fatalf("error = %v, want not found", xerr)
	}
	if len(f.pub.removed) != 0 || f.repo.deleteCalls != 0 {
		t.Errorf("acted on a missing mailbox: %d removals, %d deletes", len(f.pub.removed), f.repo.deleteCalls)
	}
}

// The service is built without a publisher in jobs and reduced deployments;
// deleting must still work there.
func TestDeleteWithNoPublisherWired(t *testing.T) {
	f := newRemovalFixture(t)
	f.svc.publisher = nil

	if xerr := f.svc.Delete(context.Background(), f.user.String(), f.mailbox.String()); xerr != nil {
		t.Fatalf("delete: %v", xerr)
	}
	if f.repo.deleteCalls != 1 {
		t.Errorf("delete called %d times, want 1", f.repo.deleteCalls)
	}
	if f.repo.workerCalls != 0 {
		t.Errorf("looked up the assignment %d times with no publisher wired, want 0", f.repo.workerCalls)
	}
}

// uuidUpper renders an id the way a caller that upper-cases its ids would.
func uuidUpper(id uuid.UUID) string {
	out := []rune(id.String())
	for i, r := range out {
		if r >= 'a' && r <= 'f' {
			out[i] = r - 32
		}
	}
	return string(out)
}

// A delete that fails after the removal was published leaves a mailbox that is
// still active but no longer loaded anywhere. It goes straight back on rather
// than waiting minutes for the reconciler.
func TestAFailedDeletePutsTheMailboxBackOnItsWorker(t *testing.T) {
	f := newRemovalFixture(t)
	f.repo.deleteErr = errx.InternalError()

	if xerr := f.svc.Delete(context.Background(), f.user.String(), f.mailbox.String()); xerr == nil {
		t.Fatal("a failed delete was reported as success")
	}
	if len(f.pub.added) != 1 || f.pub.added[0] != f.mailbox {
		t.Errorf("shipped %v back to a worker, want one %s", f.pub.added, f.mailbox)
	}
}
