package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/encryptedkeys"
	"github.com/warmbly/warmbly/internal/infrastructure/kms"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestLiveSyncTruthDependentEventsPrioritizeArrival(t *testing.T) {
	for _, kind := range []models.JobEventType{models.JobEventTypeEmailUpdate, models.JobEventTypeFolderUpdate, models.JobEventTypeRemoveEmail} {
		t.Run(string(kind), func(t *testing.T) {
			s, d := liveWarmupService(t)
			f := newWarmupFixture(t, d)
			key, err := kms.NewLocal(make([]byte, 32))
			if err != nil {
				t.Fatal(err)
			}
			r := repository.NewDurableEmailMessageMapRepository(d, cipher.NewService(key, nil, encryptedkeys.NewPostgres(d)))
			s.ArrivalOutbox = r
			s.InitEvents()
			for i := 0; i < 41; i++ {
				e := f.arrival("<"+uuid.NewString()+"@test.local>", nil)
				e.Message.Subject = "ordinary customer question"
				mapping := repository.EmailMessageData{UserID: e.UserID.String(), EmailID: e.Message.EmailID.String(), ID: e.Message.ID.String(), MessageID: e.Message.MessageID, ThreadID: e.Message.ThreadID}
				if err := r.AdmitArrival(t.Context(), mapping, &repository.PendingArrival{Arrival: e}); err != nil {
					t.Fatal(err)
				}
				if i != 40 {
					continue
				}
				var body any
				switch kind {
				case models.JobEventTypeEmailUpdate:
					body = &models.JobEventEmailUpdate{UserID: e.UserID, EmailID: e.Message.EmailID, ID: e.Message.ID, Flags: []string{"\\Seen"}, UID: 99, Mailbox: 2, ModSeq: 5}
				case models.JobEventTypeFolderUpdate:
					body = &models.JobEventFolderUpdate{UserID: e.UserID, EmailID: e.Message.EmailID, ID: e.Message.ID, Folder: models.FolderArchive}
				case models.JobEventTypeRemoveEmail:
					body = &models.JobEventRemoveEmail{UserID: e.UserID, EmailID: e.Message.EmailID, ID: e.Message.ID}
				}
				// JSON-decoded bodies use the same registered handlers as typed Kafka events.
				raw, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				var decoded map[string]any
				if err := json.Unmarshal(raw, &decoded); err != nil {
					t.Fatal(err)
				}
				event := &models.JobEvent{Type: kind, Body: decoded}
				if err := s.HandleEvent(t.Context(), event); err != nil {
					t.Fatalf("dependent event blocked by unrelated backlog: %v", err)
				}
				if err := s.HandleEvent(t.Context(), event); err != nil {
					t.Fatalf("dependent replay failed: %v", err)
				}
				message, err := s.UniboxRepository.GetForSync(t.Context(), e.UserID, e.Message.EmailID, e.Message.ID)
				if kind == models.JobEventTypeRemoveEmail {
					if !errors.Is(err, repository.ErrEmailNotFound) {
						t.Fatalf("removal did not follow arrival: %v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				} else if kind == models.JobEventTypeFolderUpdate && message.Folder != models.FolderArchive {
					t.Fatal("folder change lost after arrival")
				} else if kind == models.JobEventTypeEmailUpdate && (message.UID != 99 || !message.Seen) {
					t.Fatal("flags/provider update lost after arrival")
				}
			}
			var left int
			if err := d.QueryRow(t.Context(), `SELECT count(*) FROM sync_arrival_outbox WHERE email_id=$1`, f.partner).Scan(&left); err != nil || left != 40 {
				t.Fatalf("unrelated arrivals lost: %d %v", left, err)
			}
		})
	}
}

func TestLiveSyncTruthRemovalWaitsForArrivalAndLostStageAcknowledgement(t *testing.T) {
	for _, lostAck := range []bool{false, true} {
		for _, kind := range []string{"ordinary", "confirmed warmup", "unconfirmed warmup"} {
			name := kind + "/before ingestion"
			if lostAck {
				name = kind + "/lost stage acknowledgement"
			}
			t.Run(name, func(t *testing.T) {
				s, d := liveWarmupService(t)
				f := newWarmupFixture(t, d)
				ctx := t.Context()
				key, err := kms.NewLocal(make([]byte, 32))
				if err != nil {
					t.Fatal(err)
				}
				r := repository.NewDurableEmailMessageMapRepository(d, cipher.NewService(key, nil, encryptedkeys.NewPostgres(d)))
				s.ArrivalOutbox = r
				s.InitEvents()
				e := f.arrival("<"+uuid.NewString()+"@test.local>", nil)
				e.Message.Subject = "ordinary customer question"
				var token uuid.UUID
				if kind != "ordinary" {
					token = f.mintToken(t, s.WarmupRepo)
					if _, err := d.Exec(ctx, `INSERT INTO warmup_tasks(task_id,lineage_version,subject,max_turns) VALUES($1,1,$2,3)`, f.task, f.subject); err != nil {
						t.Fatal(err)
					}
					e = f.arrival(f.sentMsgID, []string{config.WarmupVerifyHeader + ":" + token.String()})
					if kind == "confirmed warmup" {
						if err := s.HandleEmailSent(ctx, models.SendEmailResult{TaskID: f.task, Success: true, MessageID: f.sentMsgID}); err != nil {
							t.Fatal(err)
						}
					}
				}
				mapping := repository.EmailMessageData{UserID: e.UserID.String(), EmailID: e.Message.EmailID.String(), ID: e.Message.ID.String(), MessageID: e.Message.MessageID}
				if err := r.AdmitArrival(ctx, mapping, &repository.PendingArrival{Arrival: e}); err != nil {
					t.Fatal(err)
				}
				if lostAck {
					lost := errors.New("arrival handled but stage acknowledgement lost")
					if err := r.DeliverArrivals(ctx, func(ctx context.Context, kind models.JobEventType, body any) error {
						if err := s.deliverArrivalEvent(ctx, kind, body); err != nil {
							return err
						}
						return lost
					}); !errors.Is(err, lost) {
						t.Fatalf("stage loss fixture: %v", err)
					}
				}
				remove := &models.JobEventRemoveEmail{UserID: e.UserID, EmailID: e.Message.EmailID, ID: e.Message.ID}
				if err := s.HandleRemoveEmail(ctx, remove); !errors.Is(err, ErrSyncArrivalPending) {
					t.Fatalf("removal overtook pending original arrival: %v", err)
				}
				var visible int
				if err := d.QueryRow(ctx, `SELECT count(*) FROM unibox_emails WHERE id=$1`, e.Message.ID).Scan(&visible); err != nil {
					t.Fatal(err)
				}
				if lostAck && kind == "ordinary" && visible != 1 {
					t.Fatal("blocked removal deleted already-ingested mail")
				}
				if _, err := d.Exec(ctx, `UPDATE sync_arrival_outbox SET retry_at=now() WHERE id=$1`, e.Message.ID); err != nil {
					t.Fatal(err)
				}
				fresh, _ := liveWarmupService(t)
				fresh.ArrivalOutbox = r
				fresh.InitEvents()
				if err := r.DeliverArrivals(ctx, fresh.deliverArrivalEvent); err != nil {
					t.Fatal(err)
				}
				if pending, err := r.HasPendingArrival(ctx, e.UserID, e.Message.EmailID, e.Message.ID); err != nil || pending {
					t.Fatalf("arrival did not finish: %v %v", pending, err)
				}
				unowned := *remove
				unowned.UserID = f.senderUser
				if err := fresh.HandleRemoveEmail(ctx, &unowned); err != nil {
					t.Fatalf("unowned removal: %v", err)
				}
				if kind == "ordinary" {
					if _, err := fresh.UniboxRepository.GetForSync(ctx, e.UserID, e.Message.EmailID, e.Message.ID); err != nil {
						t.Fatalf("unowned removal deleted owned row: %v", err)
					}
				}
				if err := fresh.HandleRemoveEmail(ctx, remove); err != nil {
					t.Fatal(err)
				}
				if err := r.DeliverArrivals(ctx, fresh.deliverArrivalEvent); err != nil {
					t.Fatal(err)
				}
				if err := fresh.HandleRemoveEmail(ctx, remove); err != nil {
					t.Fatalf("removal retry: %v", err)
				}
				// Recovery updates to a retained map must not resurrect hidden or deleted mail.
				if err := fresh.HandleUpdateEmail(ctx, &models.JobEventEmailUpdate{UserID: e.UserID, EmailID: e.Message.EmailID, ID: e.Message.ID, UID: 42, Mailbox: 9, ModSeq: 100, Flags: []string{models.FlagSeen}}); err != nil {
					t.Fatal(err)
				}
				for _, table := range []string{"unibox_emails", "unibox_pending_emails", "sync_arrival_outbox"} {
					var rows int
					if err := d.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE id=$1", e.Message.ID).Scan(&rows); err != nil || rows != 0 {
						t.Fatalf("%s retained or recreated mail: %d %v", table, rows, err)
					}
				}
				known, err := r.Get(ctx, e.UserID, e.Message.EmailID, e.Message.MessageID)
				if err != nil || known == nil || known.ID != e.Message.ID.String() {
					t.Fatalf("original mapping lost: %+v %v", known, err)
				}
				if kind != "ordinary" {
					var receipts int
					want := 0
					if kind == "confirmed warmup" {
						want = 1
					}
					if err := d.QueryRow(ctx, `SELECT count(*) FROM warmup_received WHERE sender_account_id=$1 AND email_account_id=$2`, f.sender, f.partner).Scan(&receipts); err != nil || receipts != want {
						t.Fatalf("warmup authority changed: %d want=%d err=%v", receipts, want, err)
					}
					stored, err := fresh.WarmupRepo.FindWarmupToken(ctx, token)
					if err != nil || stored == nil || (stored.ConsumedAt != nil) != (want == 1) {
						t.Fatalf("warmup token authority changed: %+v %v", stored, err)
					}
				}
			})
		}
	}
}

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
