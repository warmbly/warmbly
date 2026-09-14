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

	"github.com/warmbly/warmbly/internal/app/advanced"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/scheduler"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

// Live checks of the campaign send path against a real Postgres. Skipped unless
// WARMBLY_TEST_DB is set (same convention as internal/scheduler):
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/tasks/ -run Live -v
//
// What is worth proving here is that recipient suppression cannot be bypassed:
// it is the control that stops mail to an address that unsubscribed, bounced or
// complained, and it used to be skipped in full when the campaign had no
// organization (issue #168). That only holds against the real query paths.

func liveCampaignDB(t *testing.T) *db.DB {
	t.Helper()
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	handle, err := db.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { handle.Pool.Close() })
	return handle
}

// campaignSendFixture is one org/user/mailbox/campaign/step/lead graph, ready
// for a single campaign tick.
type campaignSendFixture struct {
	pool                                        *pgxpool.Pool
	user, org, mailbox, campaign, step, contact uuid.UUID
	contactEmail                                string
}

func newCampaignSendFixture(t *testing.T, pool *pgxpool.Pool) *campaignSendFixture {
	t.Helper()
	ctx := context.Background()
	f := &campaignSendFixture{
		pool: pool, user: uuid.New(), org: uuid.New(), mailbox: uuid.New(),
		campaign: uuid.New(), step: uuid.New(), contact: uuid.New(),
	}
	f.contactEmail = "lead-" + f.contact.String()[:8] + "@test.local"

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}

	exec(`INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Live', 'Test')`,
		f.user, "live-"+f.user.String()[:8]+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Live Test', $2, $3)`,
		f.org, "live-"+f.org.String()[:8], f.user)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name,
	          signature_plain, signature_html, provider, status, campaign_limit, min_wait_time, timezone)
	      VALUES ($1, $2, $3, $4, 'Live', '', '', 'smtp_imap', 'active', 50, 0, 'UTC')`,
		f.mailbox, f.user, f.org, "live-"+f.mailbox.String()[:8]+"@test.local")
	// An always-open window so nothing the test observes comes from scheduling.
	exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, status,
	          daily_limit, timezone, days, start_time, end_time, rotation_mode, updated_at, created_at)
	      VALUES ($1, $2, $3, 'Live Test', '', 'active', 50, 'UTC', 127, '00:00', '23:59',
	              'least_recently_used', NOW(), NOW())`, f.campaign, f.user, f.org)
	exec(`INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
	          body_plain, body_html, wait_after, position, kind)
	      VALUES ($1, $2, $3, 'Step 1', 'Hi', 'Hello', '<p>Hello</p>', 0, 0, 'email')`,
		f.step, f.campaign, f.org)
	exec(`INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields)
	      VALUES ($1, $2, $3, $4, 'Live', 'Contact', '', '', '{}')`,
		f.contact, f.user, f.org, f.contactEmail)
	exec(`INSERT INTO campaign_leads (campaign_id, contact_id, position) VALUES ($1, $2, 0)`, f.campaign, f.contact)

	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM task_execution_keys WHERE task_id IN (SELECT id FROM tasks WHERE email_account_id = $1)`, f.mailbox},
			{`DELETE FROM task_failures WHERE task_id IN (SELECT id FROM tasks WHERE email_account_id = $1)`, f.mailbox},
			{`DELETE FROM campaign_tasks WHERE task_id IN (SELECT id FROM tasks WHERE email_account_id = $1)`, f.mailbox},
			{`DELETE FROM tasks WHERE email_account_id = $1`, f.mailbox},
			{`DELETE FROM campaign_logs WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM campaign_daily_sends WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM campaign_contact_progress WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM campaign_leads WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM sequences WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM suppressed_recipients WHERE organization_id = $1`, f.org},
			{`DELETE FROM campaigns WHERE id = $1`, f.campaign},
			{`DELETE FROM email_accounts WHERE id = $1`, f.mailbox},
			{`DELETE FROM contacts WHERE organization_id = $1`, f.org},
			{`DELETE FROM crm_task_types WHERE organization_id = $1`, f.org},
			{`DELETE FROM organizations WHERE id = $1`, f.org},
			{`DELETE FROM users WHERE id = $1`, f.user},
		} {
			if _, err := pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})
	return f
}

// suppress puts the fixture's lead on the organization's suppression list, the
// state a bounce, complaint or unsubscribe leaves behind.
func (f *campaignSendFixture) suppress(t *testing.T, reason string) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(),
		`INSERT INTO suppressed_recipients (organization_id, email, reason, source)
		 VALUES ($1, $2, $3, 'unsubscribe')`, f.org, f.contactEmail, reason); err != nil {
		t.Fatalf("suppress: %v", err)
	}
}

// dropOrganization detaches the campaign from its workspace, the pre-migration
// state issue #168 reports. campaigns.organization_id is NOT NULL since
// migration 000092, so the constraint is lifted for the duration of the test
// and restored before anything else runs.
func (f *campaignSendFixture) dropOrganization(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `ALTER TABLE campaigns ALTER COLUMN organization_id DROP NOT NULL`); err != nil {
		t.Fatalf("drop not null: %v", err)
	}
	t.Cleanup(func() {
		// Runs before the fixture teardown (LIFO), so the orgless row is gone
		// by the time the constraint comes back.
		if _, err := f.pool.Exec(context.Background(),
			`UPDATE campaigns SET organization_id = $2 WHERE id = $1`, f.campaign, f.org); err != nil {
			t.Errorf("restore campaign org: %v", err)
		}
		if _, err := f.pool.Exec(context.Background(),
			`ALTER TABLE campaigns ALTER COLUMN organization_id SET NOT NULL`); err != nil {
			t.Errorf("restore not null: %v", err)
		}
	})
	if _, err := f.pool.Exec(ctx,
		`UPDATE campaigns SET organization_id = NULL WHERE id = $1`, f.campaign); err != nil {
		t.Fatalf("null out organization: %v", err)
	}
}

// queueTick creates the pending campaign task a scheduler tick would run.
func (f *campaignSendFixture) queueTick(t *testing.T, taskRepo repository.TaskRepository) uuid.UUID {
	t.Helper()
	taskID := uuid.New()
	now := time.Now()
	created, err := taskRepo.CreateTaskWithLock(context.Background(), &repository.Task{
		ID: taskID, TaskType: "campaign", EmailAccountID: f.mailbox, Status: "pending", ScheduledAt: &now,
	}, &repository.CampaignTask{TaskID: taskID, CampaignID: &f.campaign})
	if err != nil || !created {
		t.Fatalf("queue tick: created=%v err=%v", created, err)
	}
	return taskID
}

func (f *campaignSendFixture) sendsRecorded(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM campaign_contact_progress WHERE campaign_id = $1 AND sent_at IS NOT NULL`,
		f.campaign).Scan(&n); err != nil {
		t.Fatalf("count sends: %v", err)
	}
	return n
}

func (f *campaignSendFixture) campaignStatus(t *testing.T) string {
	t.Helper()
	var status string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT status FROM campaigns WHERE id = $1`, f.campaign).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	return status
}

func (f *campaignSendFixture) logEvents(t *testing.T) []string {
	t.Helper()
	rows, err := f.pool.Query(context.Background(),
		`SELECT event_type FROM campaign_logs WHERE campaign_id = $1`, f.campaign)
	if err != nil {
		t.Fatalf("read logs: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			t.Fatalf("scan log: %v", err)
		}
		out = append(out, e)
	}
	return out
}

// noopTaskScheduler stands in for the task queue. The tick writes the successor
// `tasks` row itself; the local dev provider treats the enqueue as a no-op too,
// so nothing about scheduling is being faked away here.
type noopTaskScheduler struct{}

func (noopTaskScheduler) CreateTask(ctx context.Context, taskData *proto.ProcessTask, scheduleTime time.Time) (string, error) {
	return "", nil
}
func (noopTaskScheduler) DeleteTask(ctx context.Context, name string) error { return nil }

// noopCipher stands in for the envelope-encryption warm-up the send path does
// before dispatch. The tick only checks that a DEK resolves; nothing in these
// tests encrypts, so KMS and Redis stay out of the harness.
type noopCipher struct{}

func (noopCipher) Cipher(ctx context.Context, orgID uuid.UUID) (*cipher.Cipher, error) {
	return &cipher.Cipher{}, nil
}

// recordingSender stands in for the worker dispatch: the tick's real side
// effect is the stamped send, and this is what makes it observable without a
// live worker. fail makes Send behave like a dispatch that could not be
// published, which is what the reservation tests need; the mutex is there
// because those tests run ticks concurrently.
type recordingSender struct {
	mu   sync.Mutex
	sent int
	fail error
	// msgs is every message handed over, in order, so a test can assert on
	// what the recipient would actually receive (headers included).
	msgs []EmailMessage
}

func (r *recordingSender) Send(ctx context.Context, taskID uuid.UUID, msg EmailMessage, account models.Email) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return r.fail
	}
	r.sent++
	r.msgs = append(r.msgs, msg)
	return nil
}

// message returns the i-th message handed over.
func (r *recordingSender) message(t *testing.T, i int) EmailMessage {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if i >= len(r.msgs) {
		t.Fatalf("wanted message %d, only %d were sent", i, len(r.msgs))
	}
	return r.msgs[i]
}

// count reads the send tally under the lock.
func (r *recordingSender) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sent
}

// setFail makes every subsequent Send return err (nil restores dispatch).
func (r *recordingSender) setFail(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fail = err
}

// liveCampaignService wires the real repositories the campaign tick uses.
// Everything that reaches outside Postgres (the worker dispatch, KMS, the task
// queue) is stubbed; every gate under test runs against the real queries.
func liveCampaignService(t *testing.T, handle *db.DB, sender EmailSender) *tasksService {
	t.Helper()
	pool := handle.Pool
	enc, err := encrypt.NewEncrypter([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("encrypter: %v", err)
	}

	taskRepo := repository.NewTaskRepository(pool)
	warmupRepo := repository.NewWarmupRepository(pool)
	progressRepo := repository.NewCampaignProgressRepository(pool)
	emailRepo := repository.NewEmailRepostory(handle, enc)
	campaignRepo := repository.NewCampaignRepostory(handle)
	contactRepo := repository.NewContactRepostory(handle)
	logRepo := repository.NewCampaignLogRepository(handle)

	adv := advanced.NewService(
		repository.NewAdvancedOutreachRepository(pool),
		campaignRepo, emailRepo, taskRepo, contactRepo, progressRepo,
		repository.NewCRMRepository(pool),
		repository.NewGroupRepostory(handle, models.Categories),
		repository.NewUniboxRepository(handle),
		nil, nil,
	)

	return &tasksService{
		emailSender:   sender,
		cipherService: noopCipher{},
		tasksClient:   noopTaskScheduler{},
		scheduler: scheduler.NewSchedulerService(
			taskRepo, warmupRepo, progressRepo, emailRepo, campaignRepo, contactRepo, logRepo,
		),
		advanced:             adv,
		taskRepo:             taskRepo,
		warmupRepo:           warmupRepo,
		campaignProgressRepo: progressRepo,
		emailRepo:            emailRepo,
		campaignRepo:         campaignRepo,
		contactRepo:          contactRepo,
		campaignLogRepo:      logRepo,
	}
}

// The regression issue #168 asks for: a campaign with no organization, whose
// recipient is on the suppression list, must not send.
//
// Suppression is enforced twice, and the missing tenant defeats both: routing
// excludes a suppressed lead by joining suppressed_recipients on the campaign's
// organization_id, which matches nothing when that is NULL. The send gate is
// what has to stop it, and that is what this asserts.
func TestLiveOrglessCampaignDoesNotSendToSuppressedRecipient(t *testing.T) {
	handle := liveCampaignDB(t)
	svc := liveCampaignService(t, handle, nil)
	f := newCampaignSendFixture(t, handle.Pool)

	f.suppress(t, "unsubscribed")
	f.dropOrganization(t)

	// Sender resolution is org-scoped too (issue #167), so an orgless campaign
	// now finds no mailboxes before routing is even consulted. That is a second
	// lock on the same door, not the one under test: the tick below still runs
	// the send gate, which is what must refuse.
	_, pair, _, err := svc.scheduler.CalculateNextCampaignTime(context.Background(), f.campaign)
	if pair != nil || !errors.Is(err, scheduler.ErrNoEmailAccounts) {
		t.Fatalf("expected an orgless campaign to resolve no senders, got pair=%v err=%v", pair, err)
	}

	taskID := f.queueTick(t, svc.taskRepo)
	if xerr := svc.HandleCampaignTask(&proto.ProcessTask{TaskId: taskID.String()}); xerr != nil {
		t.Fatalf("campaign tick returned an error: %v", xerr)
	}

	if n := f.sendsRecorded(t); n != 0 {
		t.Fatalf("orgless campaign sent to a suppressed recipient: %d sends recorded", n)
	}
	if got := f.campaignStatus(t); got != "paused" {
		t.Fatalf("campaign status = %q, want paused (fail closed, not silently skipped)", got)
	}
	if events := f.logEvents(t); len(events) == 0 {
		t.Fatal("nothing recorded in the campaign feed: the skipped check has to be visible")
	}
}

// The control: with the organization intact, the same suppressed lead is
// excluded by routing, so no send is even attempted. Without this the test
// above would pass on a build that simply never sends anything.
func TestLiveSuppressedRecipientIsSkipped(t *testing.T) {
	handle := liveCampaignDB(t)
	svc := liveCampaignService(t, handle, nil)
	f := newCampaignSendFixture(t, handle.Pool)

	// Before suppression the lead routes, so anything observed after it is
	// suppression and not a broken fixture.
	if _, pair, _, err := svc.scheduler.CalculateNextCampaignTime(context.Background(), f.campaign); err != nil || pair == nil {
		t.Fatalf("fixture lead should route before suppression, got pair=%v err=%v", pair, err)
	}

	f.suppress(t, "hard bounce")

	taskID := f.queueTick(t, svc.taskRepo)
	if xerr := svc.HandleCampaignTask(&proto.ProcessTask{TaskId: taskID.String()}); xerr != nil {
		t.Fatalf("campaign tick returned an error: %v", xerr)
	}

	if n := f.sendsRecorded(t); n != 0 {
		t.Fatalf("suppressed recipient was mailed: %d sends recorded", n)
	}
	if got := f.campaignStatus(t); got != "completed" {
		t.Fatalf("campaign status = %q, want completed (nothing left to send)", got)
	}
}

// The state the fail-closed gate defends against is no longer reachable through
// the schema: migration 000092 backfilled organization_id and made it NOT NULL.
func TestLiveCampaignRequiresAnOrganization(t *testing.T) {
	handle := liveCampaignDB(t)
	f := newCampaignSendFixture(t, handle.Pool)

	_, err := f.pool.Exec(context.Background(), `
		INSERT INTO campaigns (id, user_id, organization_id, name, description, status,
		    daily_limit, timezone, days, start_time, end_time, updated_at, created_at)
		VALUES (gen_random_uuid(), $1, NULL, 'Orgless', '', 'draft', 50, 'UTC', 127,
		        '00:00', '23:59', NOW(), NOW())`, f.user)
	if err == nil {
		t.Fatal("an orgless campaign was accepted: campaigns.organization_id must be NOT NULL")
	}
}

// The other direction: an ordinary campaign, organization present and nothing
// suppressed, still sends. Without this the fail-closed gate could be blocking
// everything and the tests above would not notice.
func TestLiveHealthyCampaignStillSends(t *testing.T) {
	handle := liveCampaignDB(t)
	sender := &recordingSender{}
	svc := liveCampaignService(t, handle, sender)
	f := newCampaignSendFixture(t, handle.Pool)

	taskID := f.queueTick(t, svc.taskRepo)
	if xerr := svc.HandleCampaignTask(&proto.ProcessTask{TaskId: taskID.String()}); xerr != nil {
		t.Fatalf("campaign tick returned an error: %v", xerr)
	}

	if sender.count() != 1 {
		t.Fatalf("sender saw %d sends, want 1", sender.count())
	}
	if n := f.sendsRecorded(t); n != 1 {
		t.Fatalf("%d sends stamped, want 1", n)
	}
}
