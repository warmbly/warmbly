package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/infrastructure/encryptedkeys"
	"github.com/warmbly/warmbly/internal/infrastructure/kms"
	"github.com/warmbly/warmbly/internal/models"
)

func arrivalLiveCipher(t *testing.T, d *db.DB) cipher.CipherService {
	t.Helper()
	k, err := kms.NewLocal(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	return cipher.NewService(k, nil, encryptedkeys.NewPostgres(d))
}

func arrivalLivePayload(f *uniboxFolderFixture, key string) (EmailMessageData, *PendingArrival) {
	id := uuid.New()
	now := time.Now().UTC()
	return EmailMessageData{UserID: f.user.String(), EmailID: f.mailbox.String(), MessageID: key, ID: id.String(), ThreadID: key}, &PendingArrival{Arrival: &models.JobEventNewEmail{UserID: f.user, Message: &models.EmailMessageStoreData{ID: id, EmailID: f.mailbox, MessageID: key, ThreadID: key, Folder: models.FolderInbox, Subject: "encrypted durable arrival", BodyText: "protected content", InternalDate: now, SentDate: now, CreatedAt: now, UpdatedAt: now}}}
}

func dueArrival(t *testing.T, d *db.DB, f *uniboxFolderFixture) {
	t.Helper()
	if _, err := d.Exec(context.Background(), `UPDATE sync_arrival_outbox SET retry_at=now(),locked_until=NULL WHERE email_id=$1`, f.mailbox); err != nil {
		t.Fatal(err)
	}
}

func TestLiveSyncArrivalRestartAndAmbiguousAdmission(t *testing.T) {
	d := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, d.Pool)
	ctx := context.Background()
	c := arrivalLiveCipher(t, d)
	r := NewDurableEmailMessageMapRepository(d, c)
	if _, err := d.Exec(ctx, `INSERT INTO email_sync_state(email_id,user_id,backfill_status,backfill_synced) VALUES($1,$2,'complete',42)`, f.mailbox, f.user); err != nil {
		t.Fatal(err)
	}
	data, p := arrivalLivePayload(f, "restart")
	if err := r.AdmitArrival(ctx, data, p); err != nil {
		t.Fatal(err)
	}
	stats, err := r.ArrivalBacklog(ctx)
	if err != nil || stats.Pending != 1 || stats.Oldest == nil {
		t.Fatalf("missing retained backlog evidence: %+v %v", stats, err)
	}
	var payload string
	if err := d.QueryRow(ctx, `SELECT payload FROM sync_arrival_outbox WHERE email_id=$1`, f.mailbox).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, p.Arrival.Message.Subject) || strings.Contains(payload, "protected content") {
		t.Fatal("outbox plaintext persisted")
	}
	other := newUniboxFolderFixture(t, d.Pool)
	otherCipher, err := c.Cipher(ctx, other.org)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := otherCipher.Decrypt(ctx, payload); err == nil {
		t.Fatal("another organization decrypted pending mail")
	}
	// Fresh repository and key cache after admission, before delivery.
	r = NewDurableEmailMessageMapRepository(d, arrivalLiveCipher(t, d))
	original := p.Arrival.Message.ID
	p.Arrival.Message.ID = uuid.New()
	data.ID = p.Arrival.Message.ID.String()
	if err := r.AdmitArrival(ctx, data, p); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(ctx, data); err != nil {
		t.Fatal(err)
	}
	if err := r.Del(ctx, f.user, f.mailbox, data.MessageID, p.Arrival.Message.ID); err != nil {
		t.Fatal(err)
	}
	known, err := r.Get(ctx, f.user, f.mailbox, data.MessageID)
	if err != nil || known == nil || known.ID != original.String() {
		t.Fatalf("canonical ID changed: %v %v", known, err)
	}
	if pending, err := r.HasPendingArrival(ctx, f.user, f.mailbox, original); err != nil || !pending {
		t.Fatalf("pending=%v err=%v", pending, err)
	}
	if pending, err := r.HasPendingArrival(ctx, other.user, f.mailbox, original); err != nil || pending {
		t.Fatal("pending marker leaked ownership")
	}
	unibox := NewUniboxRepository(d)
	calls := 0
	lost := errors.New("consumer result lost after insert")
	handle := func(ctx context.Context, kind models.JobEventType, body any) error {
		if kind != models.JobEventTypeNewEmail {
			t.Fatal(kind)
		}
		e := body.(*models.JobEventNewEmail)
		if e.Message.ID != original || e.UserID != f.user {
			t.Fatal("replay changed identity")
		}
		if err := unibox.CreateEntry(ctx, e.UserID, e.Message); err != nil {
			return err
		}
		calls++
		if calls == 1 {
			return lost
		}
		return nil
	}
	if err := r.DeliverArrivals(ctx, handle); !errors.Is(err, lost) {
		t.Fatalf("expected retained delivery: %v", err)
	}
	dueArrival(t, d, f)
	r = NewDurableEmailMessageMapRepository(d, arrivalLiveCipher(t, d))
	if err := r.DeliverArrivals(ctx, handle); err != nil {
		t.Fatal(err)
	}
	if err := r.DeliverArrivals(ctx, handle); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := d.QueryRow(ctx, `SELECT count(*) FROM unibox_emails WHERE email_id=$1 AND id=$2`, f.mailbox, original).Scan(&count); err != nil || count != 1 || calls != 2 {
		t.Fatalf("count=%d calls=%d err=%v", count, calls, err)
	}
	if pending, err := r.HasPendingArrival(ctx, f.user, f.mailbox, original); err != nil || pending {
		t.Fatalf("pending not retired: %v", err)
	}
	if err := r.AdmitArrival(ctx, data, p); !errors.Is(err, ErrArrivalAdmissionUnconfirmed) {
		t.Fatalf("completed delivery without retained proof was acknowledged: %v", err)
	}
	stats, err = r.ArrivalBacklog(ctx)
	if err != nil || stats.Pending != 0 || stats.Oldest != nil {
		t.Fatalf("delivered queue claimed a backlog: %+v %v", stats, err)
	}
}

func TestLiveSyncArrivalReportStagesOwnershipAndLegacyMaps(t *testing.T) {
	d := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, d.Pool)
	ctx := context.Background()
	r := NewDurableEmailMessageMapRepository(d, arrivalLiveCipher(t, d))
	data, p := arrivalLivePayload(f, "reports")
	p.Bounce = &models.JobEventInboundBounce{UserID: f.user, EmailID: f.mailbox, OriginalMessageID: "<send@test.local>"}
	p.Complaint = &models.JobEventInboundComplaint{UserID: f.user, EmailID: f.mailbox, OriginalMessageID: "<send@test.local>"}
	wrong := uuid.New()
	data.UserID = wrong.String()
	p.Arrival.UserID = wrong
	p.Bounce.UserID = wrong
	p.Complaint.UserID = wrong
	if err := r.AdmitArrival(ctx, data, p); err == nil {
		t.Fatal("another user admitted mail")
	}
	data.UserID = f.user.String()
	p.Arrival.UserID = f.user
	p.Bounce.UserID = f.user
	p.Complaint.UserID = f.user
	p.Bounce.EmailID = uuid.New()
	if err := r.AdmitArrival(ctx, data, p); err == nil {
		t.Fatal("cross-mailbox report accepted")
	}
	p.Bounce.EmailID = f.mailbox
	if err := r.AdmitArrival(ctx, data, p); err != nil {
		t.Fatal(err)
	}
	var seen []models.JobEventType
	failed := errors.New("report handler unavailable")
	handle := func(_ context.Context, kind models.JobEventType, _ any) error {
		seen = append(seen, kind)
		if kind == models.JobEventTypeInboundBounce && len(seen) == 2 {
			return failed
		}
		return nil
	}
	if err := r.DeliverArrivals(ctx, handle); !errors.Is(err, failed) {
		t.Fatal(err)
	}
	if pending, err := r.HasPendingArrival(ctx, f.user, f.mailbox, p.Arrival.Message.ID); err != nil || pending {
		t.Fatal("completed arrival confused with pending report")
	}
	dueArrival(t, d, f)
	if err := NewDurableEmailMessageMapRepository(d, arrivalLiveCipher(t, d)).DeliverArrivals(ctx, handle); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 4 || seen[0] != models.JobEventTypeNewEmail || seen[1] != models.JobEventTypeInboundBounce || seen[2] != models.JobEventTypeInboundBounce || seen[3] != models.JobEventTypeInboundComplaint {
		t.Fatalf("stages=%v", seen)
	}
	// Completed and legacy mappings are not guessed to be missing arrivals.
	legacy, lp := arrivalLivePayload(f, "legacy")
	if err := r.Add(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	lp.Arrival.Message.ID = uuid.New()
	retry := legacy
	retry.ID = lp.Arrival.Message.ID.String()
	if err := r.AdmitArrival(ctx, retry, lp); !errors.Is(err, ErrArrivalAdmissionUnconfirmed) {
		t.Fatalf("legacy mapping claimed durable admission: %v", err)
	}
	known, err := r.Get(ctx, f.user, f.mailbox, legacy.MessageID)
	if err != nil || known.ID != legacy.ID {
		t.Fatal("legacy mapping replaced")
	}
	if err := r.DeliverArrivals(ctx, func(context.Context, models.JobEventType, any) error {
		t.Fatal("legacy or completed mapping replayed")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestLiveSyncArrivalAtomicRollbackDeletedMailboxAndLease(t *testing.T) {
	d := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, d.Pool)
	ctx := context.Background()
	r := NewDurableEmailMessageMapRepository(d, arrivalLiveCipher(t, d))
	data, p := arrivalLivePayload(f, "atomic")
	// Force only the outbox insert to fail, without disturbing ordinary mappings.
	if _, err := d.Exec(ctx, `ALTER TABLE sync_arrival_outbox ADD CONSTRAINT sync_arrival_test_reject CHECK (message_id<>'atomic')`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = d.Exec(ctx, `ALTER TABLE sync_arrival_outbox DROP CONSTRAINT IF EXISTS sync_arrival_test_reject`)
	})
	if err := r.AdmitArrival(ctx, data, p); err == nil {
		t.Fatal("expected outbox insertion failure")
	}
	if known, err := r.Get(ctx, f.user, f.mailbox, data.MessageID); err != nil || known != nil {
		t.Fatal("map escaped rolled-back admission")
	}
	if _, err := d.Exec(ctx, `ALTER TABLE sync_arrival_outbox DROP CONSTRAINT sync_arrival_test_reject`); err != nil {
		t.Fatal(err)
	}
	if err := r.AdmitArrival(ctx, data, p); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(ctx, `UPDATE sync_arrival_outbox SET locked_until=now()+interval '1 minute' WHERE email_id=$1`, f.mailbox); err != nil {
		t.Fatal(err)
	}
	if err := r.DeliverArrivals(ctx, func(context.Context, models.JobEventType, any) error { t.Fatal("active lease replayed"); return nil }); err != nil {
		t.Fatal(err)
	}
	dueArrival(t, d, f)
	// A process lost while leased becomes replayable, including on a one-connection self-host.
	config := d.Pool.Config()
	config.MaxConns = 1
	config.MinConns = 0
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	single := &db.DB{Pool: pool}
	sr := NewDurableEmailMessageMapRepository(single, arrivalLiveCipher(t, single))
	if err := sr.DeliverArrivals(ctx, func(ctx context.Context, _ models.JobEventType, _ any) error {
		var n int
		return single.QueryRow(ctx, "SELECT 1").Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	data, p = arrivalLivePayload(f, "single-connection")
	if err := sr.AdmitArrival(ctx, data, p); err != nil {
		t.Fatal(err)
	}
	if err := sr.DeliverArrivals(ctx, func(context.Context, models.JobEventType, any) error { return nil }); err != nil {
		t.Fatal(err)
	}
	data, p = arrivalLivePayload(f, "deleted")
	if err := r.AdmitArrival(ctx, data, p); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(ctx, `DELETE FROM email_accounts WHERE id=$1`, f.mailbox); err != nil {
		t.Fatal(err)
	}
	if pending, err := r.HasPendingArrival(ctx, f.user, f.mailbox, p.Arrival.Message.ID); err != nil || pending {
		t.Fatal("deleted mailbox retained pending arrival")
	}
	var n int
	if err := d.QueryRow(ctx, `SELECT count(*) FROM sync_arrival_outbox WHERE email_id=$1`, f.mailbox).Scan(&n); err != nil || n != 0 {
		t.Fatal("deleted mailbox retained queue")
	}
}
