package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// stubSyncStateRepo accepts what the worker relays.
type stubSyncStateRepo struct {
	repository.EmailSyncStateRepository
	put   *models.SyncState
	saved *models.SyncState
}

func (s *stubSyncStateRepo) Get(context.Context, uuid.UUID) (*models.SyncState, error) {
	return s.saved, nil
}

func (s *stubSyncStateRepo) Put(_ context.Context, _, _ uuid.UUID, st *models.SyncState) error {
	s.put = st
	return nil
}

// stubErrorRepo records which codes were resolved and how far back.
type stubErrorRepo struct {
	repository.EmailAccountErrorRepository
	resolvedCodes  []string
	resolvedBy     string
	resolvedBefore time.Time
	calls          int
}

func (s *stubErrorRepo) ResolveByCodesBefore(_ context.Context, _ uuid.UUID, codes []string, before time.Time, by string) *errx.Error {
	s.calls++
	s.resolvedCodes = append(s.resolvedCodes, codes...)
	s.resolvedBefore = before
	s.resolvedBy = by
	return nil
}

// Nothing but a credential reconnect ever resolved an error row, so a mailbox
// whose server was briefly unreachable kept a red "needs attention" for good
// and never returned to healthy. A relayed sync state means the worker
// reached the server and finished a pass, which is the only evidence we get
// that the outage is over.
func TestSyncStateClearsTransientMailErrors(t *testing.T) {
	errRepo := &stubErrorRepo{}
	s := &JobsService{
		EmailSyncStateRepository:    &stubSyncStateRepo{},
		EmailAccountErrorRepository: errRepo,
	}
	emailID := uuid.New()

	syncedAt := time.Now()
	if err := s.HandleSyncState(context.Background(), &models.JobEventSyncState{
		UserID:  uuid.New(),
		EmailID: emailID,
		State:   models.SyncState{BackfillStatus: models.SyncBackfillComplete, LastSyncedAt: &syncedAt},
	}); err != nil {
		t.Fatalf("HandleSyncState: %v", err)
	}

	if !errRepo.resolvedBefore.Equal(syncedAt) {
		t.Errorf("resolved errors raised before %v, want the pass's own timestamp %v", errRepo.resolvedBefore, syncedAt)
	}

	if errRepo.calls != 1 {
		t.Fatalf("resolved errors %d times, want once per relayed state", errRepo.calls)
	}
	want := map[string]bool{"SERVER_UNREACHABLE": true, "CONNECTION_LOST": true, "RESOURCE_NOT_FOUND": true, "IMAP_UNKNOWN": true}
	for _, code := range errRepo.resolvedCodes {
		if !want[code] {
			t.Errorf("resolved %q, which a completed sync does not disprove", code)
		}
		delete(want, code)
	}
	for code := range want {
		t.Errorf("%q was left unresolved after a successful sync", code)
	}
}

// A completed sync says nothing about credentials, domain authentication or a
// fair-use deactivation. Clearing those would hide a problem the mailbox's
// owner still has to fix.
func TestSyncStateLeavesActionableErrorsAlone(t *testing.T) {
	errRepo := &stubErrorRepo{}
	s := &JobsService{
		EmailSyncStateRepository:    &stubSyncStateRepo{},
		EmailAccountErrorRepository: errRepo,
	}

	syncedAt := time.Now()
	if err := s.HandleSyncState(context.Background(), &models.JobEventSyncState{
		UserID:  uuid.New(),
		EmailID: uuid.New(),
		State:   models.SyncState{LastSyncedAt: &syncedAt},
	}); err != nil {
		t.Fatalf("HandleSyncState: %v", err)
	}

	for _, code := range errRepo.resolvedCodes {
		switch code {
		case "INVALID_CREDENTIALS", "AUTHENTICATION_FAILED", "DOMAIN_AUTH_REJECTED", "SYNC_FLOOD", "SYNC_FAIR_USE":
			t.Errorf("a successful sync cleared %q, which it does not disprove", code)
		}
	}
	if errRepo.resolvedBy != "sync recovered" {
		t.Errorf("resolvedBy = %q, want it to say what cleared the error", errRepo.resolvedBy)
	}
}

// The bus redelivers: JetStream is configured with MaxDeliver and no
// MaxAckPending, so a stale "the sync succeeded" can arrive after a newer
// failure. Resolution is bounded to errors raised before the pass ran, and a
// state carrying no timestamp cannot be bounded, so it resolves nothing
// rather than clearing a failure it knows nothing about.
func TestSyncStateWithoutATimestampResolvesNothing(t *testing.T) {
	errRepo := &stubErrorRepo{}
	s := &JobsService{
		EmailSyncStateRepository:    &stubSyncStateRepo{},
		EmailAccountErrorRepository: errRepo,
	}

	if err := s.HandleSyncState(context.Background(), &models.JobEventSyncState{
		UserID:  uuid.New(),
		EmailID: uuid.New(),
		State:   models.SyncState{},
	}); err != nil {
		t.Fatalf("HandleSyncState: %v", err)
	}
	if errRepo.calls != 0 {
		t.Errorf("resolved errors %d times from a state with no timestamp to bound it by", errRepo.calls)
	}
}
