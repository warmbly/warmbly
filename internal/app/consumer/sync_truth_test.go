package jobs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestArrivalWaitStillRetriesWithoutCapturingAnException(t *testing.T) {
	if err := errs.Init(errs.Config{Service: "consumer", Environment: "test"}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	user, mailbox, id := uuid.New(), uuid.New(), uuid.New()
	s := &JobsService{UniboxRepository: &syncTruthLookup{}, ArrivalOutbox: &syncTruthOutbox{user: user, mailbox: mailbox, id: id, pending: true}}
	err := s.HandleUpdateEmail(t.Context(), &models.JobEventEmailUpdate{UserID: user, EmailID: mailbox, ID: id})
	if !errors.Is(err, ErrSyncArrivalPending) {
		t.Fatalf("arrival wait acknowledged instead of retried: %v", err)
	}
	CaptureError(user, mailbox, fmt.Errorf("wrapped: %w", err))
	if output.Len() != 0 {
		t.Fatalf("expected wait captured as an exception: %s", output.String())
	}
	CaptureError(user, mailbox, errors.New("actual repository failure"))
	if !strings.Contains(output.String(), "actual repository failure") {
		t.Fatal("unexpected errors are no longer reported")
	}
}

func TestSyncTruthProgressRelaysDoNotResolveTransportWarnings(t *testing.T) {
	previous := time.Now().Add(-time.Hour)
	fresh := previous.Add(time.Minute)
	for _, tc := range []struct {
		name     string
		stamp    *time.Time
		deferred int
		recovery *models.GoogleHistoryRecovery
		resolved int
	}{
		{"heartbeat", &previous, 0, nil, 0},
		{"older redelivery", nil, 0, nil, 0},
		{"deferred", &fresh, 5, nil, 0},
		{"recovering", &fresh, 0, &models.GoogleHistoryRecovery{HistoryID: 100}, 0},
		{"successful catch-up", &fresh, 0, nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errRepo := &stubErrorRepo{}
			repo := &stubSyncStateRepo{saved: &models.SyncState{LastSyncedAt: &previous}}
			s := &JobsService{EmailSyncStateRepository: repo, EmailAccountErrorRepository: errRepo}
			if err := s.HandleSyncState(t.Context(), &models.JobEventSyncState{UserID: uuid.New(), EmailID: uuid.New(), State: models.SyncState{BackfillStatus: models.SyncBackfillComplete, LastSyncedAt: tc.stamp, Deferred: tc.deferred, BackfillCursor: models.SyncCursor{GoogleRecovery: tc.recovery}}}); err != nil {
				t.Fatal(err)
			}
			if repo.put == nil || errRepo.calls != tc.resolved {
				t.Fatalf("state publication/recovery outcome: saved=%v resolved=%d", repo.put != nil, errRepo.calls)
			}
		})
	}
}

type removeSyncInbox struct {
	repository.UniboxRepository
	deletes int
	err     error
}

func (r *removeSyncInbox) Delete(context.Context, uuid.UUID, uuid.UUID) error {
	r.deletes++
	return r.err
}

type removeSyncBus struct{ deleted int }

type removeSyncWarmup struct {
	repository.WarmupRepository
	lookups int
}

func (r *removeSyncWarmup) GetWarmupReceived(_ context.Context, mailbox, id uuid.UUID) (*repository.WarmupReceived, error) {
	r.lookups++
	return &repository.WarmupReceived{EmailAccountID: mailbox, InternalID: id, MessageID: "<warmup@fake.test>", CreatedAt: time.Now()}, nil
}

func (b *removeSyncBus) Publish(_ context.Context, _ string, data any, _ map[string]string) error {
	if event, ok := data.(*pubsub.EmailInboxEvent); ok && event.EventType == pubsub.EventEmailDeleted {
		b.deleted++
	}
	return nil
}

func TestSyncTruthRemovalWaitsForScopedPendingArrivals(t *testing.T) {
	user, mailbox, id := uuid.New(), uuid.New(), uuid.New()
	lookupErr := errors.New("pending marker unavailable")
	for _, tc := range []struct {
		name                    string
		owner, account, message uuid.UUID
		pending                 bool
		err, want               error
	}{
		{"pending", user, mailbox, id, true, nil, ErrSyncArrivalPending},
		{"marker error", user, mailbox, id, false, lookupErr, lookupErr},
		{"finished or legacy", user, mailbox, id, false, nil, nil},
		{"other user", uuid.New(), mailbox, id, true, nil, nil},
		{"other mailbox", user, uuid.New(), id, true, nil, nil},
		{"other message", user, mailbox, uuid.New(), true, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inbox, bus := &removeSyncInbox{}, &removeSyncBus{}
			s := &JobsService{UniboxRepository: inbox, ArrivalOutbox: &syncTruthOutbox{user: user, mailbox: mailbox, id: id, pending: tc.pending, err: tc.err}, EmailRepository: newEmailAccountRepo{}, StreamingPublisher: pubsub.NewStreamingPublisher(bus)}
			err := s.HandleRemoveEmail(t.Context(), &models.JobEventRemoveEmail{UserID: tc.owner, EmailID: tc.account, ID: tc.message})
			if !errors.Is(err, tc.want) {
				t.Fatalf("removal error=%v want=%v", err, tc.want)
			}
			want := 1
			if tc.want != nil {
				want = 0
			}
			if inbox.deletes != want || bus.deleted != want {
				t.Fatalf("deletes=%d notifications=%d want=%d", inbox.deletes, bus.deleted, want)
			}
		})
	}
}

func TestSyncTruthRemovalDeleteFailureRetriesBeforeNotification(t *testing.T) {
	failure := errors.New("unibox delete unavailable")
	inbox, bus := &removeSyncInbox{err: failure}, &removeSyncBus{}
	warmup := &removeSyncWarmup{}
	s := &JobsService{UniboxRepository: inbox, WarmupRepo: warmup, EmailRepository: newEmailAccountRepo{}, StreamingPublisher: pubsub.NewStreamingPublisher(bus)}
	e := &models.JobEventRemoveEmail{UserID: uuid.New(), EmailID: uuid.New(), ID: uuid.New()}
	if err := s.HandleRemoveEmail(t.Context(), e); !errors.Is(err, failure) {
		t.Fatalf("delete failure acknowledged: %v", err)
	}
	if bus.deleted != 0 {
		t.Fatal("failed deletion published success")
	}
	inbox.err = nil
	if err := s.HandleRemoveEmail(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	if inbox.deletes != 2 || bus.deleted != 1 || warmup.lookups != 2 {
		t.Fatalf("deletes=%d notifications=%d", inbox.deletes, bus.deleted)
	}
}

type syncTruthLookup struct {
	repository.UniboxRepository
	message  *models.EmailMessageStoreData
	reads    int
	lookups  int
	appearOn int
}

func (r *syncTruthLookup) GetByID(context.Context, uuid.UUID, uuid.UUID) (*models.EmailMessageStoreData, error) {
	r.reads++
	if r.message == nil {
		return nil, repository.ErrEmailNotFound
	}
	r.message.Seen = true
	return r.message, nil
}

func (r *syncTruthLookup) GetForSync(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*models.EmailMessageStoreData, error) {
	r.lookups++
	if r.message == nil || (r.appearOn > 0 && r.lookups < r.appearOn) {
		return nil, repository.ErrEmailNotFound
	}
	return r.message, nil
}

func (*syncTruthLookup) UpdatePendingEmail(context.Context, uuid.UUID, uuid.UUID, func(*models.EmailMessageStoreData)) (bool, error) {
	return false, nil
}

func (r *syncTruthLookup) UpdateEntry(_ context.Context, _, _, _ uuid.UUID, update *repository.UpdateUniboxEntry) error {
	if update.Seen != nil {
		r.message.Seen = *update.Seen
	}
	return nil
}

type syncTruthOutbox struct {
	repository.ArrivalOutbox
	user, mailbox, id uuid.UUID
	pending           bool
	err               error
}

func (r *syncTruthOutbox) HasPendingArrival(_ context.Context, user, mailbox, id uuid.UUID) (bool, error) {
	if user != r.user || mailbox != r.mailbox || id != r.id {
		return false, nil
	}
	return r.pending, r.err
}

func TestSyncTruthBackgroundUpdatesNeverViewUnreadMail(t *testing.T) {
	for _, kind := range []string{"flagged", "folder", "same folder"} {
		t.Run(kind, func(t *testing.T) {
			r := &syncTruthLookup{message: &models.EmailMessageStoreData{Seen: false, Folder: models.FolderInbox, ProviderFolder: models.FolderInbox}}
			s := &JobsService{UniboxRepository: r, EmailRepository: warmupInboxEmailRepo{}}
			user, mailbox, id := uuid.New(), uuid.New(), uuid.New()
			var err error
			if kind == "flagged" {
				err = s.HandleFlagsAdd(t.Context(), &models.JobEventFlags{UserID: user, EmailID: mailbox, ID: id, Flags: []string{models.FlagFlagged}})
			} else {
				folder := models.FolderInbox
				if kind == "folder" {
					folder = models.FolderArchive
				}
				err = s.HandleFolderUpdate(t.Context(), &models.JobEventFolderUpdate{UserID: user, EmailID: mailbox, ID: id, Folder: folder})
			}
			if err != nil || r.message.Seen || r.reads != 0 {
				t.Fatalf("background update treated as view: seen=%v reads=%d err=%v", r.message.Seen, r.reads, err)
			}
		})
	}
}

func TestSyncTruthPendingArrivalUpdatesRetryOnlyForOwnedUndeliveredMail(t *testing.T) {
	user, mailbox, id := uuid.New(), uuid.New(), uuid.New()
	for _, kind := range []string{"flags add", "flags remove", "folder", "full update"} {
		t.Run(kind, func(t *testing.T) {
			r := &syncTruthLookup{}
			outbox := &syncTruthOutbox{user: user, mailbox: mailbox, id: id, pending: true}
			s := &JobsService{UniboxRepository: r, ArrivalOutbox: outbox}
			apply := func(owner, box uuid.UUID) error {
				switch kind {
				case "flags add":
					return s.HandleFlagsAdd(t.Context(), &models.JobEventFlags{UserID: owner, EmailID: box, ID: id, Flags: []string{models.FlagSeen}})
				case "flags remove":
					return s.HandleFlagsRemove(t.Context(), &models.JobEventFlags{UserID: owner, EmailID: box, ID: id, Flags: []string{models.FlagSeen}})
				case "folder":
					return s.HandleFolderUpdate(t.Context(), &models.JobEventFolderUpdate{UserID: owner, EmailID: box, ID: id, Folder: models.FolderArchive})
				default:
					return s.HandleUpdateEmail(t.Context(), &models.JobEventEmailUpdate{UserID: owner, EmailID: box, ID: id, Flags: []string{models.FlagSeen}})
				}
			}
			if err := apply(user, mailbox); !errors.Is(err, ErrSyncArrivalPending) {
				t.Fatalf("pending arrival was acknowledged: %v", err)
			}
			if err := apply(uuid.New(), mailbox); err != nil {
				t.Fatalf("unowned update retried: %v", err)
			}
			if err := apply(user, uuid.New()); err != nil {
				t.Fatalf("other-mailbox update retried: %v", err)
			}
			outbox.pending = false
			if err := apply(user, mailbox); err != nil {
				t.Fatalf("deliberately hidden/deleted mail retried: %v", err)
			}
			outbox.err = errors.New("marker unavailable")
			if err := apply(user, mailbox); !errors.Is(err, outbox.err) {
				t.Fatalf("marker failure swallowed: %v", err)
			}
		})
	}
}

func TestSyncTruthArrivalFinishingDuringLookupStillGetsUpdate(t *testing.T) {
	user, mailbox, id := uuid.New(), uuid.New(), uuid.New()
	r := &syncTruthLookup{message: &models.EmailMessageStoreData{EmailID: mailbox}, appearOn: 3}
	s := &JobsService{UniboxRepository: r, EmailRepository: warmupInboxEmailRepo{}, ArrivalOutbox: &syncTruthOutbox{user: user, mailbox: mailbox, id: id}}
	if err := s.HandleFlagsAdd(t.Context(), &models.JobEventFlags{UserID: user, EmailID: mailbox, ID: id, Flags: []string{models.FlagSeen}}); err != nil {
		t.Fatal(err)
	}
	if !r.message.Seen {
		t.Fatal("arrival committed between reads lost the later Seen update")
	}
}
