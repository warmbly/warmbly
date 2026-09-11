package tasks

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/scheduler"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

// Issue #401: rotation chose a mailbox per SEND, so a six-step sequence reached
// one contact from three different addresses. Rotation belongs to the lead, not
// to the step: the mailbox that sends a contact's first email sends the rest of
// their sequence, and rotation spreads NEW leads across the pool.
//
// Skipped unless WARMBLY_TEST_DB is set:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/tasks/ -run Live -v

type stickyFixture struct {
	pool      *pgxpool.Pool
	user, org uuid.UUID
	mailboxes []uuid.UUID
	campaign  uuid.UUID
	steps     []uuid.UUID
	leads     []uuid.UUID
	svc       *tasksService
	sender    *attributingSender
}

// newStickyFixture builds a round-robin campaign over two mailboxes with a
// chained multi-step sequence and no waits, so consecutive ticks walk one lead
// through its whole sequence and rotation is the only thing choosing mailboxes.
func newStickyFixture(t *testing.T, steps, leads int) *stickyFixture {
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

	f := &stickyFixture{
		pool: pool, user: uuid.New(), org: uuid.New(), campaign: uuid.New(),
		mailboxes: []uuid.UUID{uuid.New(), uuid.New()},
		sender:    &attributingSender{},
	}
	for i := 0; i < steps; i++ {
		f.steps = append(f.steps, uuid.New())
	}

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	exec(`INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Live', 'Sticky')`,
		f.user, "sticky-"+f.user.String()[:8]+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Live Sticky', $2, $3)`,
		f.org, "sticky-"+f.org.String()[:8], f.user)
	for _, mb := range f.mailboxes {
		exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name,
		          signature_plain, signature_html, provider, status, campaign_limit, min_wait_time, timezone)
		      VALUES ($1, $2, $3, $4, 'Live', '', '', 'smtp_imap', 'active', 50, 0, 'UTC')`,
			mb, f.user, f.org, "sticky-"+mb.String()[:8]+"@test.local")
	}
	exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, status,
	          daily_limit, timezone, days, start_time, end_time, rotation_mode, updated_at, created_at)
	      VALUES ($1, $2, $3, 'Live Sticky', '', 'active', 50, 'UTC', 127, '00:00', '23:59',
	              'round_robin', NOW(), NOW())`, f.campaign, f.user, f.org)

	// Each step connects to the next with an unconditional branch; the last one
	// ends the flow. Nothing waits, so every step is due the moment the one
	// before it was sent.
	for i, id := range f.steps {
		conditions := `{"branches":[]}`
		if i+1 < len(f.steps) {
			conditions = `{"branches":[{"branch_id":"b` + string(rune('1'+i)) + `","target_step_id":"` + f.steps[i+1].String() + `"}]}`
		}
		exec(`INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
		          body_plain, body_html, wait_after, position, conditions, kind)
		      VALUES ($1, $2, $3, $4, 'Hi', 'Hello', '<p>Hello</p>', 0, $5, $6::jsonb, 'email')`,
			id, f.campaign, f.org, "Step "+string(rune('1'+i)), i+1, conditions)
	}
	for i := 0; i < leads; i++ {
		id := uuid.New()
		f.leads = append(f.leads, id)
		exec(`INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields, verification_status, created_at)
		      VALUES ($1, $2, $3, $4, 'Live', 'Contact', '', '', '{}', 'valid', NOW() + make_interval(secs => $5))`,
			id, f.user, f.org, "sticky-lead-"+id.String()[:8]+"@test.local", float64(i))
		exec(`INSERT INTO campaign_leads (campaign_id, contact_id, position) VALUES ($1, $2, $3)`, f.campaign, id, i)
	}

	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM task_failures WHERE task_id IN (SELECT task_id FROM campaign_tasks WHERE campaign_id = $1)`, f.campaign},
			{`DELETE FROM task_execution_keys WHERE task_id IN (SELECT task_id FROM campaign_tasks WHERE campaign_id = $1)`, f.campaign},
			{`DELETE FROM tasks WHERE id IN (SELECT task_id FROM campaign_tasks WHERE campaign_id = $1)`, f.campaign},
			{`DELETE FROM campaign_tasks WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM campaign_logs WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM campaign_daily_sends WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM campaign_contact_progress WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM campaign_leads WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM sequences WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM campaigns WHERE id = $1`, f.campaign},
			{`DELETE FROM email_accounts WHERE organization_id = $1`, f.org},
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
	progressRepo := repository.NewCampaignProgressRepository(pool)
	taskRepo := repository.NewTaskRepository(pool)
	f.svc = &tasksService{
		tasksClient:          noopTaskScheduler{},
		scheduler:            scheduler.NewSchedulerService(taskRepo, repository.NewWarmupRepository(pool), progressRepo, emailRepo, campaignRepo, contactRepo, logRepo),
		cipherService:        noopCipher{},
		emailSender:          f.sender,
		taskRepo:             taskRepo,
		campaignProgressRepo: progressRepo,
		emailRepo:            emailRepo,
		campaignRepo:         campaignRepo,
		contactRepo:          contactRepo,
		campaignLogRepo:      logRepo,
		trackedLinkRepo:      repository.NewTrackedLinkRepository(pool),
	}
	return f
}

// tick runs one real campaign tick, seeded the way the chain seeds it.
func (f *stickyFixture) tick(t *testing.T) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx,
		`UPDATE tasks SET status = 'cancelled' WHERE status = 'pending' AND id IN (
			SELECT task_id FROM campaign_tasks WHERE campaign_id = $1)`, f.campaign); err != nil {
		t.Fatalf("clear pending tasks: %v", err)
	}
	taskID := uuid.New()
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO tasks (id, task_type, email_account_id, status, message_id, scheduled_at, created_at, updated_at)
		VALUES ($1, 'campaign', $2, 'pending', '', NOW(), NOW(), NOW())`, taskID, f.mailboxes[0]); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO campaign_tasks (task_id, campaign_id) VALUES ($1, $2)`,
		taskID, f.campaign); err != nil {
		t.Fatalf("create campaign task: %v", err)
	}
	if xerr := f.svc.HandleCampaignTask(&proto.ProcessTask{TaskId: taskID.String()}); xerr != nil {
		t.Fatalf("campaign tick: %v", xerr)
	}
	return taskID
}

// sendsByLead maps each contact to the mailboxes its steps were sent from, in
// order, read from the tasks that actually dispatched them.
func (f *stickyFixture) sendsByLead(t *testing.T) map[uuid.UUID][]uuid.UUID {
	t.Helper()
	rows, err := f.pool.Query(context.Background(), `
		SELECT ct.contact_id, t.email_account_id
		FROM campaign_tasks ct
		JOIN tasks t ON t.id = ct.task_id
		WHERE ct.campaign_id = $1 AND ct.contact_id IS NOT NULL AND t.status = 'completed'
		ORDER BY t.created_at ASC`, f.campaign)
	if err != nil {
		t.Fatalf("read sends: %v", err)
	}
	defer rows.Close()
	out := map[uuid.UUID][]uuid.UUID{}
	for rows.Next() {
		var contact, mailbox uuid.UUID
		if err := rows.Scan(&contact, &mailbox); err != nil {
			t.Fatalf("scan sends: %v", err)
		}
		out[contact] = append(out[contact], mailbox)
	}
	return out
}

func (f *stickyFixture) boundSender(t *testing.T, contact uuid.UUID) *uuid.UUID {
	t.Helper()
	var id *uuid.UUID
	if err := f.pool.QueryRow(context.Background(),
		`SELECT email_account_id FROM campaign_leads WHERE campaign_id = $1 AND contact_id = $2`,
		f.campaign, contact).Scan(&id); err != nil {
		t.Fatalf("read lead sender: %v", err)
	}
	return id
}

// TestLiveSequenceKeepsOneSenderPerLead is issue #401: every step a contact
// receives comes from the mailbox that sent them the first one, while rotation
// still spreads the two leads across the two mailboxes.
func TestLiveSequenceKeepsOneSenderPerLead(t *testing.T) {
	f := newStickyFixture(t, 3, 2)

	// Six ticks: three steps for each of the two leads.
	for i := 0; i < 6; i++ {
		f.tick(t)
	}

	sends := f.sendsByLead(t)
	if len(sends) != 2 {
		t.Fatalf("expected both leads to be emailed, got sends for %d", len(sends))
	}
	used := map[uuid.UUID]bool{}
	for _, contact := range f.leads {
		got := sends[contact]
		if len(got) != 3 {
			t.Fatalf("lead %s received %d steps, want 3", contact, len(got))
		}
		for _, mb := range got[1:] {
			if mb != got[0] {
				t.Fatalf("lead %s heard from %v: a sequence must stay on the mailbox that sent its first email", contact, got)
			}
		}
		if bound := f.boundSender(t, contact); bound == nil || *bound != got[0] {
			t.Fatalf("lead %s sent from %s but is recorded against %v", contact, got[0], bound)
		}
		used[got[0]] = true
	}
	if len(used) != 2 {
		t.Fatalf("both leads were started on the same mailbox; rotation must still spread NEW leads")
	}
}

// TestLiveLeadMovesOffAMailboxThatCanNoLongerSend: staying on one address is a
// rule while the mailbox can send. A mailbox that is disconnected, or taken off
// the campaign, is not coming back, and a lead left waiting for it would go
// silent mid-sequence — so it moves, and the activity log says so.
func TestLiveLeadMovesOffAMailboxThatCanNoLongerSend(t *testing.T) {
	f := newStickyFixture(t, 2, 1)

	f.tick(t)
	first := f.sendsByLead(t)[f.leads[0]]
	if len(first) != 1 {
		t.Fatalf("first step did not send: %v", first)
	}
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE email_accounts SET status = 'inactive' WHERE id = $1`, first[0]); err != nil {
		t.Fatalf("disconnect mailbox: %v", err)
	}

	f.tick(t)
	got := f.sendsByLead(t)[f.leads[0]]
	if len(got) != 2 {
		t.Fatalf("the follow-up did not send after its mailbox was disconnected: %v", got)
	}
	if got[1] == got[0] {
		t.Fatalf("the follow-up left from the disconnected mailbox %s", got[0])
	}
	if bound := f.boundSender(t, f.leads[0]); bound == nil || *bound != got[1] {
		t.Fatalf("the lead is recorded against %v, want its new mailbox %s", bound, got[1])
	}
	var logged int
	if err := f.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM campaign_logs WHERE campaign_id = $1 AND event_type = 'sender_reassigned'`,
		f.campaign).Scan(&logged); err != nil {
		t.Fatalf("read logs: %v", err)
	}
	if logged != 1 {
		t.Fatalf("moving a lead to another mailbox wrote %d activity lines, want 1", logged)
	}
}

// TestLiveABusySenderHoldsItsOwnLeadOnly: a mailbox that has used its budget is
// coming back tomorrow, so its leads wait rather than switching address. The
// leads behind them must NOT wait with them — that would drop a campaign's
// whole throughput to the first mailbox that fills up.
func TestLiveABusySenderHoldsItsOwnLeadOnly(t *testing.T) {
	f := newStickyFixture(t, 2, 2)

	f.tick(t)
	first := f.sendsByLead(t)[f.leads[0]]
	if len(first) != 1 {
		t.Fatalf("first step did not send: %v", first)
	}
	// That mailbox has now sent its one email for the day.
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE email_accounts SET campaign_limit = 1 WHERE id = $1`, first[0]); err != nil {
		t.Fatalf("cap mailbox: %v", err)
	}

	f.tick(t)
	sends := f.sendsByLead(t)
	if got := sends[f.leads[0]]; len(got) != 1 {
		t.Fatalf("lead 1 was emailed again from %v while its own mailbox was out of budget", got)
	}
	got := sends[f.leads[1]]
	if len(got) != 1 {
		t.Fatalf("lead 2 did not send while a free mailbox was available: %v", got)
	}
	if got[0] == first[0] {
		t.Fatalf("lead 2 sent from the mailbox that has no budget left")
	}
}

// TestLiveBoundLeadInItsMailboxGapDoesNotHoldTheQueue is the same rule for
// spacing rather than budget (issue #437). A lead bound to a mailbox that sent
// a moment ago has to wait out that mailbox's minimum gap; the leads behind it,
// which another mailbox can take right now, must not wait with it.
func TestLiveBoundLeadInItsMailboxGapDoesNotHoldTheQueue(t *testing.T) {
	f := newStickyFixture(t, 2, 2)

	f.tick(t)
	first := f.sendsByLead(t)[f.leads[0]]
	if len(first) != 1 {
		t.Fatalf("first step did not send: %v", first)
	}

	// An hour of spacing on every mailbox. Lead 1 is bound to one that has just
	// sent, so its follow-up cannot go for an hour; lead 2 has no mailbox yet
	// and the other one has never sent.
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE email_accounts SET min_wait_time = 3600 WHERE organization_id = $1`, f.org); err != nil {
		t.Fatalf("widen the gap: %v", err)
	}

	f.tick(t)
	sends := f.sendsByLead(t)
	if got := sends[f.leads[0]]; len(got) != 1 {
		t.Fatalf("lead 1 was emailed again from %v inside its own mailbox's minimum gap", got)
	}
	got := sends[f.leads[1]]
	if len(got) != 1 {
		t.Fatalf("lead 2 did not send while the other mailbox was free and idle: %v", got)
	}
	if got[0] == first[0] {
		t.Fatalf("lead 2 sent from the mailbox that is inside its minimum gap")
	}
}
