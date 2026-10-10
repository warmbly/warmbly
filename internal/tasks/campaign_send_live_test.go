package tasks

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/scheduler"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

// End-to-end checks of the campaign send path (issue #169) against a real
// Postgres. Skipped unless WARMBLY_TEST_DB is set:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/tasks/ -run Live -v
//
// These drive the real HandleCampaignTask over the real scheduler, routing and
// repositories, with only the bus and KMS stubbed, because the bug being fixed
// lives exactly in the ordering between "the command is published" and "the
// progress row is written". A stubbed repository proves nothing about it.

// breakingProgressRepo wraps the real repository and fails RecordEmailSent on
// demand, which is precisely the "progress write is best-effort" failure the
// issue is about. Everything else passes straight through to Postgres.
type breakingProgressRepo struct {
	repository.CampaignProgressRepository
	breakStamp bool
}

type rejectingCampaignTrackingRepo struct {
	repository.TaskRepository
}

func (r rejectingCampaignTrackingRepo) UpdateCampaignTaskTracking(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return errors.New("simulated durable tracking write failure")
}

func TestLiveCampaignSendRequiresDurableIntentBeforeReservation(t *testing.T) {
	f := newSendFixture(t)
	id := uuid.New()
	at := time.Now().Add(-time.Minute)
	if ok, err := f.svc.taskRepo.CreateTaskWithLock(t.Context(), &Task{ID: id, TaskType: "campaign", EmailAccountID: f.mailbox, Status: "pending", ScheduledAt: &at}, &CampaignTask{TaskID: id, CampaignID: &f.campaign}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	original := f.svc.taskRepo
	f.svc.taskRepo = rejectingCampaignTrackingRepo{TaskRepository: original}
	if xerr := f.svc.HandleCampaignTask(&proto.ProcessTask{TaskId: id.String()}); xerr == nil {
		t.Fatal("failed durable intent write allowed execution")
	}
	if f.sender.count() != 0 {
		t.Fatal("send dispatched without durable send intent")
	}
	var dispatched int
	if err := f.pool.QueryRow(t.Context(), `SELECT COUNT(*) FROM campaign_contact_progress WHERE campaign_id=$1 AND dispatched_at IS NOT NULL`, f.campaign).Scan(&dispatched); err != nil || dispatched != 0 {
		t.Fatal("send reserved without durable intent", dispatched, err)
	}
	ct, err := original.GetCampaignTask(t.Context(), id)
	if err != nil || ct.DispatchIntent != repository.CampaignDispatchWakeup {
		t.Fatal("unsent wakeup intent changed", ct, err)
	}
	f.svc.taskRepo = original
	if xerr := f.svc.HandleCampaignTask(&proto.ProcessTask{TaskId: id.String()}); xerr != nil {
		t.Fatal(xerr)
	}
	ct, err = original.GetCampaignTask(t.Context(), id)
	if err != nil || ct.DispatchIntent != repository.CampaignDispatchSend || f.sender.count() != 1 {
		t.Fatal("durable send intent not recorded before successful retry", ct, err)
	}
}

func (b *breakingProgressRepo) RecordEmailSent(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) error {
	if b.breakStamp {
		return errors.New("simulated transient database error on the progress write")
	}
	return b.CampaignProgressRepository.RecordEmailSent(ctx, campaignID, contactID, sequenceID)
}

type sendFixture struct {
	pool                      *pgxpool.Pool
	user, org, mailbox        uuid.UUID
	campaign, step            uuid.UUID
	leadA, leadB              uuid.UUID
	svc                       *tasksService
	sender                    *recordingSender
	progress                  *breakingProgressRepo
	realProgress              repository.CampaignProgressRepository
	emailAAddr, emailBAddress string
}

func newSendFixture(t *testing.T) *sendFixture {
	t.Helper()
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	ctx := context.Background()
	handle, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { handle.Pool.Close() })
	pool := handle.Pool

	f := &sendFixture{
		pool: pool, user: uuid.New(), org: uuid.New(), mailbox: uuid.New(),
		campaign: uuid.New(), step: uuid.New(), leadA: uuid.New(), leadB: uuid.New(),
	}
	f.emailAAddr = "lead-a-" + f.leadA.String()[:8] + "@test.local"
	f.emailBAddress = "lead-b-" + f.leadB.String()[:8] + "@test.local"

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	exec(`INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Live', 'Test')`,
		f.user, "live-"+f.user.String()[:8]+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Live Send', $2, $3)`,
		f.org, "live-"+f.org.String()[:8], f.user)
	// A mailbox with no min-gap, so nothing but the reservation decides whether
	// a send happens. Worker assignment lives in the stubbed sender.
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name,
	          signature_plain, signature_html, provider, status, campaign_limit, min_wait_time, timezone)
	      VALUES ($1, $2, $3, $4, 'Live', '', '', 'smtp_imap', 'active', 50, 0, 'UTC')`,
		f.mailbox, f.user, f.org, "live-"+f.mailbox.String()[:8]+"@test.local")
	exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, status,
	          daily_limit, timezone, days, start_time, end_time, rotation_mode, updated_at, created_at)
	      VALUES ($1, $2, $3, 'Live Send', '', 'active', 50, 'UTC', 127, '00:00', '23:59',
	              'least_recently_used', NOW(), NOW())`, f.campaign, f.user, f.org)
	exec(`INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
	          body_plain, body_html, wait_after, position, kind)
	      VALUES ($1, $2, $3, 'Step 1', 'Hi', 'Hello', '<p>Hello</p>', 0, 0, 'email')`, f.step, f.campaign, f.org)
	for i, c := range []struct {
		id    uuid.UUID
		email string
	}{{f.leadA, f.emailAAddr}, {f.leadB, f.emailBAddress}} {
		exec(`INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields, verification_status, created_at)
		      VALUES ($1, $2, $3, $4, 'Live', 'Contact', '', '', '{}', 'valid', NOW() + make_interval(secs => $5))`,
			c.id, f.user, f.org, c.email, float64(i))
		exec(`INSERT INTO campaign_leads (campaign_id, contact_id, position) VALUES ($1, $2, $3)`, f.campaign, c.id, i)
	}

	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM task_failures WHERE task_id IN (SELECT id FROM tasks WHERE email_account_id = $1)`, f.mailbox},
			{`DELETE FROM task_execution_keys WHERE task_id IN (SELECT id FROM tasks WHERE email_account_id = $1)`, f.mailbox},
			{`DELETE FROM campaign_tasks WHERE task_id IN (SELECT id FROM tasks WHERE email_account_id = $1)`, f.mailbox},
			{`DELETE FROM tasks WHERE email_account_id = $1`, f.mailbox},
			{`DELETE FROM campaign_logs WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM campaign_daily_sends WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM campaign_contact_progress WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM campaign_leads WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM sequences WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM campaigns WHERE id = $1`, f.campaign},
			{`DELETE FROM email_accounts WHERE id = $1`, f.mailbox},
			{`DELETE FROM contacts WHERE organization_id = $1`, f.org},
			{`DELETE FROM organizations WHERE id = $1`, f.org},
			{`DELETE FROM users WHERE id = $1`, f.user},
		} {
			if _, err := pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})

	enc, err := encrypt.NewEncrypter([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("encrypter: %v", err)
	}
	emailRepo := repository.NewEmailRepostory(handle, enc)
	campaignRepo := repository.NewCampaignRepostory(handle)
	contactRepo := repository.NewContactRepostory(handle)
	logRepo := repository.NewCampaignLogRepository(handle)
	f.realProgress = repository.NewCampaignProgressRepository(pool)
	f.progress = &breakingProgressRepo{CampaignProgressRepository: f.realProgress}
	taskRepo := repository.NewTaskRepository(pool)
	f.sender = &recordingSender{}
	f.svc = &tasksService{
		tasksClient:          noopTaskScheduler{},
		scheduler:            scheduler.NewSchedulerService(taskRepo, repository.NewWarmupRepository(pool), f.progress, emailRepo, campaignRepo, contactRepo, logRepo),
		cipherService:        noopCipher{},
		emailSender:          f.sender,
		taskRepo:             taskRepo,
		campaignProgressRepo: f.progress,
		emailRepo:            emailRepo,
		campaignRepo:         campaignRepo,
		contactRepo:          contactRepo,
		campaignLogRepo:      logRepo,
		trackedLinkRepo:      repository.NewTrackedLinkRepository(pool),
	}
	return f
}

// newTask writes one due, pending campaign task the way the chain does. It
// bypasses CreateTaskWithLock's one-pending-task guard, because a test that
// races two ticks needs two.
func (f *sendFixture) newTask(t *testing.T) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	taskID := uuid.New()
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO tasks (id, task_type, email_account_id, status, message_id, scheduled_at, created_at, updated_at)
		VALUES ($1, 'campaign', $2, 'pending', '', NOW(), NOW(), NOW())`, taskID, f.mailbox); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO campaign_tasks (task_id, campaign_id) VALUES ($1, $2)`,
		taskID, f.campaign); err != nil {
		t.Fatalf("create campaign task: %v", err)
	}
	return taskID
}

// tick runs the real handler on a fresh due task, exactly as the scheduler
// callback does. Each tick leaves a successor behind, so they are cancelled
// first to keep every tick in the test deliberate.
func (f *sendFixture) tick(t *testing.T) uuid.UUID {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE tasks SET status = 'cancelled' WHERE status = 'pending' AND id IN (
			SELECT task_id FROM campaign_tasks WHERE campaign_id = $1)`, f.campaign); err != nil {
		t.Fatalf("clear pending tasks: %v", err)
	}
	taskID := f.newTask(t)
	if xerr := f.svc.HandleCampaignTask(&proto.ProcessTask{TaskId: taskID.String()}); xerr != nil {
		t.Fatalf("campaign tick: %v", xerr)
	}
	return taskID
}

type progressRow struct {
	sentAt, dispatchedAt *time.Time
	attempts             int
}

func (f *sendFixture) progressFor(t *testing.T, contact uuid.UUID) *progressRow {
	t.Helper()
	var r progressRow
	err := f.pool.QueryRow(context.Background(), `SELECT sent_at, dispatched_at, send_attempts
		FROM campaign_contact_progress WHERE campaign_id = $1 AND contact_id = $2 AND sequence_id = $3`,
		f.campaign, contact, f.step).Scan(&r.sentAt, &r.dispatchedAt, &r.attempts)
	if err != nil {
		return nil
	}
	return &r
}

// TestLiveCampaignTickReservesBeforeItDispatches is the ordering the whole fix
// rests on: when the tick hands a send to the bus, the durable record of that
// attempt is already committed.
func TestLiveCampaignTickReservesBeforeItDispatches(t *testing.T) {
	f := newSendFixture(t)

	f.tick(t)
	if f.sender.count() != 1 {
		t.Fatalf("the tick dispatched %d sends, want 1", f.sender.count())
	}
	row := f.progressFor(t, f.leadA)
	if row == nil || row.dispatchedAt == nil {
		t.Fatalf("the send went out without a reservation: %+v", row)
	}
	if row.sentAt == nil {
		t.Fatal("a healthy tick should also stamp sent_at")
	}
	var sent, newLeads int
	if err := f.pool.QueryRow(context.Background(), `SELECT emails_sent, new_leads_started FROM campaign_daily_sends
		WHERE campaign_id = $1 AND send_date = CURRENT_DATE`, f.campaign).Scan(&sent, &newLeads); err != nil {
		t.Fatalf("read daily counters: %v", err)
	}
	if sent != 1 || newLeads != 1 {
		t.Fatalf("daily counters = %d/%d, want 1/1", sent, newLeads)
	}
}

// TestLiveLostProgressWriteDoesNotResend is issue #169 end to end: the send is
// dispatched, the progress write fails, and the next tick must serve the OTHER
// lead rather than emailing the first one twice.
func TestLiveLostProgressWriteDoesNotResend(t *testing.T) {
	f := newSendFixture(t)

	f.progress.breakStamp = true
	f.tick(t)
	if f.sender.count() != 1 {
		t.Fatalf("first tick dispatched %d sends, want 1", f.sender.count())
	}
	row := f.progressFor(t, f.leadA)
	if row == nil || row.dispatchedAt == nil {
		t.Fatalf("no reservation survived the failed stamp: %+v", row)
	}
	if row.sentAt != nil {
		t.Fatal("precondition: the stamp was supposed to fail")
	}
	// The failure is visible, not swallowed.
	var logged int
	if err := f.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM campaign_logs
		WHERE campaign_id = $1 AND metadata->>'code' = 'PROGRESS_WRITE_FAILED'`, f.campaign).Scan(&logged); err != nil {
		t.Fatal(err)
	}
	if logged != 1 {
		t.Fatalf("a failed progress write produced %d activity log entries, want 1", logged)
	}

	// Next tick: lead A must not be served again.
	f.progress.breakStamp = false
	f.tick(t)
	if f.sender.count() != 2 {
		t.Fatalf("second tick dispatched a total of %d sends, want 2", f.sender.count())
	}
	if rowB := f.progressFor(t, f.leadB); rowB == nil || rowB.dispatchedAt == nil {
		t.Fatalf("the second tick did not serve the other lead: %+v", rowB)
	}
	if again := f.progressFor(t, f.leadA); again.sentAt != nil {
		t.Fatal("lead A was served a second time (this is issue #169)")
	}

	// And with both leads dispatched, a third tick sends nothing at all.
	f.tick(t)
	if f.sender.count() != 2 {
		t.Fatalf("a third tick sent %d in total; every lead was already served", f.sender.count())
	}
}

// TestLiveUndispatchableSendKeepsItsStepRoutable covers the other side: when
// the command provably never left, the reservation must be given back so the
// lead is retried rather than silently dropped.
func TestLiveUndispatchableSendKeepsItsStepRoutable(t *testing.T) {
	f := newSendFixture(t)

	f.sender.setFail(ErrWorkerOffline)
	f.tick(t)
	if f.sender.count() != 0 {
		t.Fatal("nothing should have been published")
	}
	if row := f.progressFor(t, f.leadA); row != nil && row.dispatchedAt != nil {
		t.Fatalf("a send that never left kept its reservation: %+v", row)
	}
	var sent int
	if err := f.pool.QueryRow(context.Background(), `SELECT COALESCE(emails_sent, 0) FROM campaign_daily_sends
		WHERE campaign_id = $1 AND send_date = CURRENT_DATE`, f.campaign).Scan(&sent); err != nil && err.Error() != "no rows in result set" {
		t.Fatalf("read daily counters: %v", err)
	}
	if sent != 0 {
		t.Fatalf("emails_sent = %d for a send that never left", sent)
	}

	// The lead is still first in line once the worker is back.
	f.sender.setFail(nil)
	f.tick(t)
	if f.sender.count() != 1 {
		t.Fatalf("the retried send did not go out (%d dispatched)", f.sender.count())
	}
	if row := f.progressFor(t, f.leadA); row == nil || row.sentAt == nil {
		t.Fatalf("lead A was not served on the retry: %+v", row)
	}
}

// TestLiveAmbiguousDispatchKeepsItsReservation covers the one failure that is
// NOT safe to retry: the publish call itself failed, so the bus may have taken
// the command. That reservation has to stand until an outcome arrives.
func TestLiveAmbiguousDispatchKeepsItsReservation(t *testing.T) {
	f := newSendFixture(t)

	f.sender.setFail(ErrSendDispatchUnknown)
	f.tick(t)
	row := f.progressFor(t, f.leadA)
	if row == nil || row.dispatchedAt == nil {
		t.Fatalf("an ambiguous dispatch gave its reservation back: %+v", row)
	}
	if row.sentAt != nil {
		t.Fatal("an ambiguous dispatch must not be stamped sent")
	}

	// The next tick serves the other lead, never this one.
	f.sender.setFail(nil)
	f.tick(t)
	if f.sender.count() != 1 {
		t.Fatalf("second tick dispatched %d sends, want 1", f.sender.count())
	}
	if rowB := f.progressFor(t, f.leadB); rowB == nil || rowB.sentAt == nil {
		t.Fatalf("the second tick did not serve the other lead: %+v", rowB)
	}
}

// TestLiveConcurrentTicksSendOnce covers two ticks racing on the same pair,
// which is the other way one recipient got two copies.
func TestLiveConcurrentTicksSendOnce(t *testing.T) {
	f := newSendFixture(t)
	ctx := context.Background()

	// One lead only, so both ticks must pick the SAME (contact, step) pair.
	if _, err := f.pool.Exec(ctx, `DELETE FROM campaign_leads WHERE campaign_id = $1 AND contact_id = $2`,
		f.campaign, f.leadB); err != nil {
		t.Fatalf("drop lead B: %v", err)
	}

	// Both ticks are created up front, so both see the same routing state.
	var taskIDs []uuid.UUID
	for i := 0; i < 2; i++ {
		taskIDs = append(taskIDs, f.newTask(t))
	}
	var wg sync.WaitGroup
	for _, id := range taskIDs {
		wg.Add(1)
		go func(id uuid.UUID) {
			defer wg.Done()
			_ = f.svc.HandleCampaignTask(&proto.ProcessTask{TaskId: id.String()})
		}(id)
	}
	wg.Wait()

	// Whatever the interleaving, no contact may be dispatched twice and the
	// day may not be charged more than the number of sends.
	var rows, dispatched, sent int
	if err := f.pool.QueryRow(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE dispatched_at IS NOT NULL), COUNT(*) FILTER (WHERE sent_at IS NOT NULL)
		FROM campaign_contact_progress WHERE campaign_id = $1`, f.campaign).Scan(&rows, &dispatched, &sent); err != nil {
		t.Fatal(err)
	}
	if dispatched != f.sender.count() {
		t.Fatalf("%d sends went out but %d steps are reserved", f.sender.count(), dispatched)
	}
	var counted int
	if err := f.pool.QueryRow(ctx, `SELECT COALESCE(emails_sent, 0) FROM campaign_daily_sends
		WHERE campaign_id = $1 AND send_date = CURRENT_DATE`, f.campaign).Scan(&counted); err != nil {
		t.Fatalf("read daily counters: %v", err)
	}
	if counted != f.sender.count() {
		t.Fatalf("the day counted %d sends but %d went out", counted, f.sender.count())
	}
	// The single lead may only have been emailed once, whichever tick won.
	if f.sender.count() != 1 || dispatched != 1 || rows != 1 {
		t.Fatalf("one lead, two ticks: sends=%d reserved=%d rows=%d, want 1/1/1", f.sender.count(), dispatched, rows)
	}
	// ...and the losing tick ended as a skipped duplicate, not as a send.
	var skipped int
	if err := f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE email_account_id = $1 AND status = 'skipped_duplicate'`, f.mailbox).Scan(&skipped); err != nil {
		t.Fatal(err)
	}
	t.Logf("sends=%d reserved=%d sent=%d skipped_duplicate=%d", f.sender.count(), dispatched, sent, skipped)
}
