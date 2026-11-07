package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

var syncTestGeneration = uuid.New()

// stubSyncStateRepo accepts what the worker relays.
type stubSyncStateRepo struct {
	repository.EmailSyncStateRepository
	put              *models.SyncState
	saved            *models.SyncState
	getErr           error
	reportWorker     uuid.UUID
	reportGeneration uuid.UUID
	reportAccepted   bool
}

func (s *stubSyncStateRepo) Get(context.Context, uuid.UUID) (*models.SyncState, error) {
	return s.saved, s.getErr
}

func (s *stubSyncStateRepo) Put(_ context.Context, _, _ uuid.UUID, st *models.SyncState) error {
	s.put = st
	return nil
}

func (s *stubSyncStateRepo) PutFromWorker(ctx context.Context, _, _, workerID, generation uuid.UUID, st *models.SyncState, before func(context.Context, *repository.SyncStatePrior) error) (bool, *repository.SyncStatePrior, error) {
	s.reportWorker = workerID
	s.reportGeneration = generation
	prior := repository.PriorOf(s.saved)
	if s.reportAccepted {
		if before != nil {
			if err := before(ctx, prior); err != nil {
				return false, nil, err
			}
		}
		s.put = st
	}
	return s.reportAccepted, prior, nil
}

type syncStateBus struct {
	calls  int
	types  []string
	status []string
}

func (b *syncStateBus) Publish(_ context.Context, _ string, event any, attrs map[string]string) error {
	b.calls++
	b.types = append(b.types, attrs["event_type"])
	var body struct {
		Status string `json:"status"`
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return err
	}
	b.status = append(b.status, body.Status)
	return nil
}

type syncStateEmailRepo struct {
	repository.EmailRepository
	account *models.Email
}

func (r syncStateEmailRepo) GetByID(context.Context, uuid.UUID) (*models.Email, *errx.Error) {
	return r.account, nil
}

func TestSyncStateChecksReportingWorkerBeforeSaving(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		repo := &stubSyncStateRepo{reportAccepted: accepted}
		errRepo := &stubErrorRepo{}
		bus := &syncStateBus{}
		org := uuid.New()
		s := &JobsService{EmailSyncStateRepository: repo, EmailAccountErrorRepository: errRepo,
			EmailRepository:    syncStateEmailRepo{account: &models.Email{OrganizationID: &org}},
			StreamingPublisher: pubsub.NewStreamingPublisher(bus, removeSyncAudience{org: org})}
		workerID := uuid.New()
		at := time.Now().Add(-time.Minute)
		event := &models.JobEventSyncState{WorkerID: &workerID, ReportGeneration: syncTestGeneration, UserID: uuid.New(), EmailID: uuid.New(),
			State: models.SyncState{TransportGeneration: syncTestGeneration, LastSyncedAt: &at, BackfillStatus: models.SyncBackfillComplete, FoldersSkippedCap: 4}}
		var output bytes.Buffer
		previous := log.Logger
		log.Logger = zerolog.New(&output)
		err := s.HandleSyncState(t.Context(), event)
		log.Logger = previous
		if err != nil {
			t.Fatal(err)
		}
		// A dropped report leaves last_synced_at stale, so it must be logged (#1157).
		if logged := strings.Contains(output.String(), "sync state report dropped") && strings.Contains(output.String(), workerID.String()); logged == accepted {
			t.Fatalf("accepted=%t drop logged=%t: %s", accepted, logged, output.String())
		}
		if repo.reportWorker != workerID || repo.reportGeneration != syncTestGeneration || (repo.put != nil) != accepted {
			t.Fatalf("worker=%s accepted=%t saved=%+v", repo.reportWorker, accepted, repo.put)
		}
		if (bus.calls > 0) != accepted || (errRepo.calls > 0) != accepted {
			t.Fatalf("accepted=%t published=%d recovery=%d", accepted, bus.calls, errRepo.calls)
		}
		if accepted && (len(bus.types) == 0 || bus.types[0] != string(pubsub.EventAccountSyncState) || bus.status[0] != string(models.SyncBackfillComplete)) {
			t.Fatalf("published types=%v status=%v", bus.types, bus.status)
		}
	}
}

// stubErrorRepo records which codes were resolved and how far back.
type stubErrorRepo struct {
	repository.EmailAccountErrorRepository
	resolvedCodes  []string
	resolvedBy     string
	resolvedBefore time.Time
	calls          int
	operation      models.MailTransportOperation
	fail           bool
}

func (s *stubErrorRepo) ResolveByCodesBefore(_ context.Context, _ uuid.UUID, codes []string, before time.Time, by string) *errx.Error {
	s.calls++
	s.resolvedCodes = append(s.resolvedCodes, codes...)
	s.resolvedBefore = before
	s.resolvedBy = by
	if s.fail {
		return errx.InternalError()
	}
	return nil
}

func (s *stubErrorRepo) RecordTransportSuccess(ctx context.Context, id uuid.UUID, operation models.MailTransportOperation, at time.Time, _ *uuid.UUID, _ uuid.UUID) *errx.Error {
	s.operation = operation
	return s.ResolveByCodesBefore(ctx, id, transientMailErrorCodes, at, string(operation)+" recovered")
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
		State:   models.SyncState{TransportGeneration: syncTestGeneration, BackfillStatus: models.SyncBackfillComplete, LastSyncedAt: &syncedAt},
	}); err != nil {
		t.Fatalf("HandleSyncState: %v", err)
	}

	if !errRepo.resolvedBefore.Equal(syncedAt) {
		t.Errorf("resolved errors raised before %v, want the pass's own timestamp %v", errRepo.resolvedBefore, syncedAt)
	}

	if errRepo.calls != 1 {
		t.Fatalf("resolved errors %d times, want once per relayed state", errRepo.calls)
	}
	if errRepo.operation != models.MailTransportSync {
		t.Fatal("sync recovery did not carry explicit operation scope")
	}
	want := map[string]bool{"SERVER_UNREACHABLE": true, "CONNECTION_LOST": true, "RESOURCE_NOT_FOUND": true, "IMAP_UNKNOWN": true, "SENDING_TOO_FAST": true, "QUOTA_EXCEEDED": true}
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
		State:   models.SyncState{TransportGeneration: syncTestGeneration, LastSyncedAt: &syncedAt},
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

func TestSyncStateRejectsIncompleteAndInvalidSuccessClocks(t *testing.T) {
	old := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	zero := time.Time{}
	for _, state := range []models.SyncState{
		{TransportGeneration: syncTestGeneration, LastSyncedAt: &future}, {TransportGeneration: syncTestGeneration, LastSyncedAt: &zero},
		{TransportGeneration: syncTestGeneration, LastSyncedAt: &old, Deferred: 1},
		{TransportGeneration: syncTestGeneration, LastSyncedAt: &old, BackfillCursor: models.SyncCursor{GoogleRecovery: &models.GoogleHistoryRecovery{}}},
	} {
		errRepo := &stubErrorRepo{}
		s := &JobsService{EmailSyncStateRepository: &stubSyncStateRepo{}, EmailAccountErrorRepository: errRepo}
		if err := s.HandleSyncState(t.Context(), &models.JobEventSyncState{EmailID: uuid.New(), State: state}); err != nil {
			t.Fatal(err)
		}
		if errRepo.calls != 0 {
			t.Fatal("incomplete/future state was treated as recovery")
		}
	}
}

func TestSyncStateRecoveryRetriesBeforeStateIsStored(t *testing.T) {
	at := time.Now().Add(-time.Minute)
	older := at.Add(-time.Minute)
	errRepo := &stubErrorRepo{fail: true}
	stateRepo := &stubSyncStateRepo{saved: &models.SyncState{TransportGeneration: syncTestGeneration, LastSyncedAt: &older}}
	s := &JobsService{EmailSyncStateRepository: stateRepo, EmailAccountErrorRepository: errRepo}
	event := &models.JobEventSyncState{EmailID: uuid.New(), State: models.SyncState{TransportGeneration: syncTestGeneration, LastSyncedAt: &at}}
	if err := s.HandleSyncState(t.Context(), event); err == nil {
		t.Fatal("recovery failure was acknowledged")
	}
	if stateRepo.put != nil {
		t.Fatal("state stored before recovery, suppressing an idempotent retry")
	}
	errRepo.fail = false
	if err := s.HandleSyncState(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if errRepo.calls != 2 {
		t.Fatal("recovery failure suppressed retry")
	}
}

func TestSyncStateUnreadablePreviousStateRetriesWithoutRecovery(t *testing.T) {
	at := time.Now().Add(-time.Minute)
	lookupErr := errors.New("previous state unavailable")
	stateRepo := &stubSyncStateRepo{saved: &models.SyncState{LastSyncedAt: &at}, getErr: lookupErr}
	errRepo := &stubErrorRepo{}
	s := &JobsService{EmailSyncStateRepository: stateRepo, EmailAccountErrorRepository: errRepo}
	event := &models.JobEventSyncState{EmailID: uuid.New(), State: models.SyncState{TransportGeneration: syncTestGeneration, LastSyncedAt: &at}}
	if err := s.HandleSyncState(t.Context(), event); !errors.Is(err, lookupErr) {
		t.Fatalf("previous-state failure was not retried: %v", err)
	}
	if stateRepo.put != nil || errRepo.calls != 0 {
		t.Fatal("unreadable previous state acquired recovery or stored the relay")
	}
	stateRepo.getErr = nil
	if err := s.HandleSyncState(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if stateRepo.put == nil || errRepo.calls != 0 {
		t.Fatal("equal-timestamp retry acquired recovery")
	}
	fresh := at.Add(time.Second)
	event.State.LastSyncedAt = &fresh
	if err := s.HandleSyncState(t.Context(), event); err != nil || errRepo.calls != 1 {
		t.Fatal("fresh typed pass did not recover after the lookup recovered", err)
	}
}
