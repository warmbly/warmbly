package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/advanced"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/warmup"
	"github.com/warmbly/warmbly/internal/infrastructure/encryptedkeys"
	"github.com/warmbly/warmbly/internal/infrastructure/kms"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestLiveSyncArrivalReplayPreservesHiddenWarmupAndReceiptAuthority(t *testing.T) {
	s, d := liveWarmupService(t)
	f := newWarmupFixture(t, d)
	ctx := t.Context()
	f.mintToken(t, s.WarmupRepo)
	k, err := kms.NewLocal(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	r := repository.NewDurableEmailMessageMapRepository(d, cipher.NewService(k, nil, encryptedkeys.NewPostgres(d)))
	e := f.arrival(f.sentMsgID, nil)
	e.UserID = f.senderUser
	e.Message.EmailID = f.sender
	e.Message.Folder = models.FolderSent
	data := repository.EmailMessageData{UserID: e.UserID.String(), EmailID: e.Message.EmailID.String(), MessageID: e.Message.MessageID, ID: e.Message.ID.String(), ThreadID: e.Message.ThreadID}
	if err := r.AdmitArrival(ctx, data, &repository.PendingArrival{Arrival: e}); err != nil {
		t.Fatal(err)
	}
	s.InitEvents()
	lost := errors.New("consumer terminated after handling")
	if err := r.DeliverArrivals(ctx, func(ctx context.Context, kind models.JobEventType, body any) error {
		if err := s.deliverArrivalEvent(ctx, kind, body); err != nil {
			return err
		}
		return lost
	}); !errors.Is(err, lost) {
		t.Fatal(err)
	}
	if _, err := d.Exec(ctx, `UPDATE sync_arrival_outbox SET retry_at=now() WHERE email_id=$1`, f.sender); err != nil {
		t.Fatal(err)
	}
	fresh, _ := liveWarmupService(t)
	fresh.InitEvents()
	if err := r.DeliverArrivals(ctx, fresh.deliverArrivalEvent); err != nil {
		t.Fatal(err)
	}
	var visible, pending, receipts int
	if err := d.QueryRow(ctx, `SELECT count(*) FROM unibox_emails WHERE id=$1`, e.Message.ID).Scan(&visible); err != nil || visible != 0 {
		t.Fatalf("unconfirmed sent copy exposed: %d %v", visible, err)
	}
	if err := d.QueryRow(ctx, `SELECT count(*) FROM unibox_pending_emails WHERE id=$1`, e.Message.ID).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("verification deferral lost original ID: %d %v", pending, err)
	}
	if err := d.QueryRow(ctx, `SELECT count(*) FROM warmup_received WHERE sender_account_id=$1`, f.sender).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatalf("sync invented receipt: %d %v", receipts, err)
	}
	if err := fresh.HandleEmailSent(ctx, models.SendEmailResult{TaskID: f.task, Success: true, MessageID: f.sentMsgID}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(ctx, `UPDATE unibox_pending_emails SET retry_at=now() WHERE email_account_id=$1`, f.sender); err != nil {
		t.Fatal(err)
	}
	if err := fresh.retryPendingWarmupVerification(ctx); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRow(ctx, `SELECT count(*) FROM unibox_emails WHERE id=$1`, e.Message.ID).Scan(&visible); err != nil || visible != 0 {
		t.Fatalf("confirmed warmup exposed: %d %v", visible, err)
	}
	if err := d.QueryRow(ctx, `SELECT count(*) FROM warmup_received WHERE sender_account_id=$1`, f.sender).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatalf("sent copy invented recipient receipt: %d %v", receipts, err)
	}
	other := f.arrival("<"+uuid.NewString()+"@test.local>", nil)
	other.Message.Subject = "ordinary customer question"
	data = repository.EmailMessageData{UserID: other.UserID.String(), EmailID: other.Message.EmailID.String(), MessageID: other.Message.MessageID, ID: other.Message.ID.String()}
	if err := r.AdmitArrival(ctx, data, &repository.PendingArrival{Arrival: other}); err != nil {
		t.Fatal(err)
	}
	if err := r.DeliverArrivals(ctx, fresh.deliverArrivalEvent); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRow(ctx, `SELECT count(*) FROM unibox_emails WHERE id=$1`, other.Message.ID).Scan(&visible); err != nil || visible != 1 {
		t.Fatalf("ordinary arrival missing: %d %v", visible, err)
	}
}

func TestLiveSyncArrivalReportReplayIsIdempotent(t *testing.T) {
	d := liveDB(t)
	f := newSendResultFixture(t, d)
	s := liveJobsService(d)
	ctx := t.Context()
	task := f.stampSend(t, s)
	messageID := "<" + task.String() + "@test.local>"
	if _, err := d.Exec(ctx, `UPDATE tasks SET message_id=$2 WHERE id=$1`, task, messageID); err != nil {
		t.Fatal(err)
	}
	s.UniboxRepository = repository.NewUniboxRepository(d)
	s.EmailRepository = repository.NewEmailRepostory(d, nil)
	s.WarmupRepo = repository.NewWarmupRepository(d.Pool)
	s.AdvancedService = advanced.NewService(repository.NewAdvancedOutreachRepository(d.Pool), s.CampaignRepo, s.EmailRepository, s.TaskRepo, repository.NewContactRepostory(d), s.CampaignProgressRepo, nil, nil, nil, nil, warmup.NewService(s.WarmupRepo))
	s.InitEvents()
	k, err := kms.NewLocal(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	cipherService := cipher.NewService(k, nil, encryptedkeys.NewPostgres(d))
	r := repository.NewDurableEmailMessageMapRepository(d, cipherService)
	id := uuid.New()
	recipient := "lead-" + f.contact.String()[:8] + "@test.local"
	p := &repository.PendingArrival{Arrival: &models.JobEventNewEmail{UserID: f.user, ReportOriginalMessageID: messageID, Message: &models.EmailMessageStoreData{ID: id, EmailID: f.mailbox, MessageID: "report", Folder: models.FolderInbox}}, Bounce: &models.JobEventInboundBounce{UserID: f.user, EmailID: f.mailbox, OriginalMessageID: messageID, FailedRecipient: recipient, Reason: "recipient refused"}, Complaint: &models.JobEventInboundComplaint{UserID: f.user, EmailID: f.mailbox, OriginalMessageID: messageID, ComplainedRecipient: recipient, Provider: "fixture"}}
	data := repository.EmailMessageData{UserID: f.user.String(), EmailID: f.mailbox.String(), MessageID: "report", ID: id.String()}
	if err := r.AdmitArrival(ctx, data, p); err != nil {
		t.Fatal(err)
	}
	lost := errors.New("result lost after report was recorded")
	for _, target := range []models.JobEventType{models.JobEventTypeInboundBounce, models.JobEventTypeInboundComplaint} {
		if err := r.DeliverArrivals(ctx, func(ctx context.Context, kind models.JobEventType, body any) error {
			if err := s.deliverArrivalEvent(ctx, kind, body); err != nil {
				return err
			}
			if kind == target {
				return lost
			}
			return nil
		}); !errors.Is(err, lost) {
			t.Fatal(err)
		}
		if _, err := d.Exec(ctx, `UPDATE sync_arrival_outbox SET retry_at=now() WHERE email_id=$1`, f.mailbox); err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.NewDurableEmailMessageMapRepository(d, cipherService).DeliverArrivals(ctx, s.deliverArrivalEvent); err != nil {
		t.Fatal(err)
	}
	var events, suppressed int
	if err := d.QueryRow(ctx, `SELECT count(*) FROM deliverability_events WHERE organization_id=$1`, f.org).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRow(ctx, `SELECT count(*) FROM suppressed_recipients WHERE organization_id=$1`, f.org).Scan(&suppressed); err != nil {
		t.Fatal(err)
	}
	if events != 2 || suppressed != 1 {
		t.Fatalf("report replay duplicated effects: events=%d suppressed=%d", events, suppressed)
	}
}
