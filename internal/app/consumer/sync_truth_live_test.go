package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/infrastructure/encryptedkeys"
	"github.com/warmbly/warmbly/internal/infrastructure/kms"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type failSyncArrivalCreate struct {
	repository.UniboxRepository
	fail bool
}

func (r *failSyncArrivalCreate) CreateEntry(ctx context.Context, user uuid.UUID, message *models.EmailMessageStoreData) error {
	if r.fail {
		r.fail = false
		return errors.New("transient arrival write failure")
	}
	return r.UniboxRepository.CreateEntry(ctx, user, message)
}

func TestLiveSyncTruthPendingArrivalRetainsProviderUpdatesAcrossReload(t *testing.T) {
	for _, kind := range []string{"seen", "folder", "full update"} {
		t.Run(kind, func(t *testing.T) {
			s, d := liveWarmupService(t)
			f := newWarmupFixture(t, d)
			ctx := t.Context()
			key, err := kms.NewLocal(make([]byte, 32))
			if err != nil {
				t.Fatal(err)
			}
			cipherService := cipher.NewService(key, nil, encryptedkeys.NewPostgres(d))
			r := repository.NewDurableEmailMessageMapRepository(d, cipherService)
			s.ArrivalOutbox = r
			s.UniboxRepository = &failSyncArrivalCreate{UniboxRepository: s.UniboxRepository, fail: true}
			e := f.arrival("<"+uuid.NewString()+"@test.local>", nil)
			e.Message.Subject = "ordinary customer question"
			mapping := repository.EmailMessageData{UserID: e.UserID.String(), EmailID: e.Message.EmailID.String(), ID: e.Message.ID.String(), MessageID: e.Message.MessageID}
			if err := r.AdmitArrival(ctx, mapping, &repository.PendingArrival{Arrival: e}); err != nil {
				t.Fatal(err)
			}
			s.InitEvents()
			if err := r.DeliverArrivals(ctx, s.deliverArrivalEvent); err == nil {
				t.Fatal("expected transient arrival write failure")
			}
			apply := func(service *JobsService) error {
				switch kind {
				case "seen":
					return service.HandleFlagsAdd(ctx, &models.JobEventFlags{UserID: e.UserID, EmailID: e.Message.EmailID, ID: e.Message.ID, Flags: []string{models.FlagSeen}})
				case "folder":
					return service.HandleFolderUpdate(ctx, &models.JobEventFolderUpdate{UserID: e.UserID, EmailID: e.Message.EmailID, ID: e.Message.ID, Folder: models.FolderArchive})
				default:
					return service.HandleUpdateEmail(ctx, &models.JobEventEmailUpdate{UserID: e.UserID, EmailID: e.Message.EmailID, ID: e.Message.ID, Flags: []string{models.FlagSeen}, Folder: models.FolderArchive, FolderPath: "Archive", UID: 42, Mailbox: 7})
				}
			}
			if err := apply(s); !errors.Is(err, ErrSyncArrivalPending) {
				t.Fatalf("update was acknowledged ahead of failed arrival: %v", err)
			}
			if _, err := d.Exec(ctx, `UPDATE sync_arrival_outbox SET retry_at=now() WHERE id=$1`, e.Message.ID); err != nil {
				t.Fatal(err)
			}
			fresh, _ := liveWarmupService(t)
			fresh.ArrivalOutbox = repository.NewDurableEmailMessageMapRepository(d, cipherService)
			fresh.InitEvents()
			if err := fresh.ArrivalOutbox.DeliverArrivals(ctx, fresh.deliverArrivalEvent); err != nil {
				t.Fatal(err)
			}
			if err := apply(fresh); err != nil {
				t.Fatal(err)
			}
			message, err := fresh.UniboxRepository.GetForSync(ctx, e.UserID, e.Message.EmailID, e.Message.ID)
			if err != nil {
				t.Fatal(err)
			}
			if kind != "folder" && !message.Seen {
				t.Fatal("provider Seen update lost after original-ID arrival replay")
			}
			if kind != "seen" && message.Folder != models.FolderArchive {
				t.Fatalf("provider folder update lost: %q", message.Folder)
			}
			if kind == "full update" && (message.UID != 42 || message.Mailbox != 7 || message.FolderPath != "Archive") {
				t.Fatalf("provider handles lost: %+v", message)
			}
			if _, err := d.Exec(ctx, `DELETE FROM email_accounts WHERE id=$1`, e.Message.EmailID); err != nil {
				t.Fatal(err)
			}
			if err := apply(fresh); err != nil {
				t.Fatalf("deleted mailbox update retried forever: %v", err)
			}
		})
	}
}

func TestLiveSyncTruthBackgroundReadsKeepUnreadAndStayOwnerScoped(t *testing.T) {
	s, d := liveWarmupService(t)
	f := newWarmupFixture(t, d)
	ctx := t.Context()
	e := f.arrival("<"+uuid.NewString()+"@test.local>", nil)
	if err := s.UniboxRepository.CreateEntry(ctx, e.UserID, e.Message); err != nil {
		t.Fatal(err)
	}
	assertUnread := func() {
		t.Helper()
		var seen bool
		if err := d.QueryRow(ctx, `SELECT seen FROM unibox_emails WHERE id=$1`, e.Message.ID).Scan(&seen); err != nil || seen {
			t.Fatalf("background work marked unread mail seen: %v %v", seen, err)
		}
	}
	for _, owner := range []uuid.UUID{f.senderUser, uuid.New()} {
		if _, err := s.UniboxRepository.GetForSync(ctx, owner, e.Message.EmailID, e.Message.ID); !errors.Is(err, repository.ErrEmailNotFound) {
			t.Fatalf("unowned sync lookup returned a row: %v", err)
		}
	}
	for _, mailbox := range []uuid.UUID{f.sender, uuid.New(), uuid.Nil} {
		if _, err := s.UniboxRepository.GetForSync(ctx, e.UserID, mailbox, e.Message.ID); !errors.Is(err, repository.ErrEmailNotFound) {
			t.Fatalf("wrong-mailbox sync lookup returned a row: %v", err)
		}
	}
	assertUnread()
	if err := s.HandleFlagsAdd(ctx, &models.JobEventFlags{UserID: e.UserID, EmailID: e.Message.EmailID, ID: e.Message.ID, Flags: []string{models.FlagFlagged}}); err != nil {
		t.Fatal(err)
	}
	assertUnread()
	for _, folder := range []string{models.FolderInbox, models.FolderArchive, models.FolderArchive} {
		if err := s.HandleFolderUpdate(ctx, &models.JobEventFolderUpdate{UserID: e.UserID, EmailID: e.Message.EmailID, ID: e.Message.ID, Folder: folder}); err != nil {
			t.Fatal(err)
		}
		assertUnread()
	}
	if err := s.HandleFlagsAdd(ctx, &models.JobEventFlags{UserID: e.UserID, EmailID: e.Message.EmailID, ID: e.Message.ID, Flags: []string{models.FlagSeen}}); err != nil {
		t.Fatal(err)
	}
	message, err := s.UniboxRepository.GetForSync(ctx, e.UserID, e.Message.EmailID, e.Message.ID)
	if err != nil || message == nil || !message.Seen {
		t.Fatalf("real provider Seen flag did not mark read: %v", err)
	}
	if err := s.UniboxRepository.MarkSeen(ctx, e.UserID, e.Message.ID, false); err != nil {
		t.Fatal(err)
	}
	assertUnread()
	message, err = s.UniboxRepository.GetByID(ctx, e.UserID, e.Message.ID)
	if err != nil || message == nil || !message.Seen {
		t.Fatalf("explicit user view no longer marks read: %v", err)
	}
}

func TestLiveSyncTruthLegacyArrivalHasNoPendingGuard(t *testing.T) {
	s, d := liveWarmupService(t)
	f := newWarmupFixture(t, d)
	ctx := t.Context()
	key, err := kms.NewLocal(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	r := repository.NewDurableEmailMessageMapRepository(d, cipher.NewService(key, nil, encryptedkeys.NewPostgres(d)))
	s.ArrivalOutbox = r
	s.UniboxRepository = &failSyncArrivalCreate{UniboxRepository: s.UniboxRepository, fail: true}
	e := f.arrival("<"+uuid.NewString()+"@test.local>", nil)
	e.Message.Subject = "ordinary legacy customer question"
	// Legacy workers admit only the map, then publish NEW_EMAIL separately.
	if err := r.Add(ctx, repository.EmailMessageData{UserID: e.UserID.String(), EmailID: e.Message.EmailID.String(), ID: e.Message.ID.String(), MessageID: e.Message.MessageID}); err != nil {
		t.Fatal(err)
	}
	if err := s.HandleNewEmail(ctx, e); err == nil {
		t.Fatal("expected legacy arrival creation failure")
	}
	if pending, err := r.HasPendingArrival(ctx, e.UserID, e.Message.EmailID, e.Message.ID); err != nil || pending {
		t.Fatalf("legacy map unexpectedly acquired a durable arrival marker: %v %v", pending, err)
	}
	if err := s.HandleFlagsAdd(ctx, &models.JobEventFlags{UserID: e.UserID, EmailID: e.Message.EmailID, ID: e.Message.ID, Flags: []string{models.FlagSeen}}); err != nil {
		t.Fatalf("legacy map alone inferred a pending arrival: %v", err)
	}
	if err := s.HandleFolderUpdate(ctx, &models.JobEventFolderUpdate{UserID: e.UserID, EmailID: e.Message.EmailID, ID: e.Message.ID, Folder: models.FolderArchive}); err != nil {
		t.Fatalf("legacy map alone inferred a pending arrival: %v", err)
	}
	if err := s.HandleNewEmail(ctx, e); err != nil {
		t.Fatal(err)
	}
	message, err := s.UniboxRepository.GetForSync(ctx, e.UserID, e.Message.EmailID, e.Message.ID)
	if err != nil {
		t.Fatal(err)
	}
	if message.Seen || message.Folder != models.FolderInbox {
		t.Fatalf("legacy update ordering must not be reported as protected: %+v", message)
	}
}
