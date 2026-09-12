package jobs

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Live checks of the send-outcome loop against a real Postgres. Skipped unless
// WARMBLY_TEST_DB is set (same convention as internal/scheduler):
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/app/consumer/ -run Live -v
//
// What is worth proving here is the walk-back itself: that a worker's
// EMAIL_FAILED clears the step the control plane stamped, gives the day's
// counters back, keeps the step routable until the attempt cap, and then drops
// the lead. That only holds against the real queries.

type sendResultFixture struct {
	user, org, mailbox, campaign, step, contact uuid.UUID
}

func newSendResultFixture(t *testing.T, handle *db.DB) *sendResultFixture {
	t.Helper()
	ctx := context.Background()
	pool := handle.Pool
	f := &sendResultFixture{
		user: uuid.New(), org: uuid.New(), mailbox: uuid.New(),
		campaign: uuid.New(), step: uuid.New(), contact: uuid.New(),
	}
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
	      VALUES ($1, $2, $3, $4, 'Live', '', '', 'smtp_imap', 'active', 50, 600, 'UTC')`,
		f.mailbox, f.user, f.org, "live-"+f.mailbox.String()[:8]+"@test.local")
	exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, status,
	          daily_limit, timezone, days, start_time, end_time, rotation_mode, updated_at, created_at)
	      VALUES ($1, $2, $3, 'Live Test', '', 'completed', 50, 'UTC', 127, '00:00', '23:59',
	              'least_recently_used', NOW(), NOW())`, f.campaign, f.user, f.org)
	exec(`INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
	          body_plain, body_html, wait_after, position, kind)
	      VALUES ($1, $2, $3, 'Step 1', 'Hi', 'Hello', '<p>Hello</p>', 0, 1, 'email')`, f.step, f.campaign, f.org)
	exec(`INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields)
	      VALUES ($1, $2, $3, $4, 'Live', 'Contact', '', '', '{}')`,
		f.contact, f.user, f.org, "lead-"+f.contact.String()[:8]+"@test.local")
	exec(`INSERT INTO campaign_leads (campaign_id, contact_id, position) VALUES ($1, $2, 0)`, f.campaign, f.contact)

	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM task_failures WHERE task_id IN (SELECT id FROM tasks WHERE email_account_id = $1)`, f.mailbox},
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
	return f
}

// dispatch does what the backend's campaign tick does around a send: the
// reservation (which also counts the day's send) before the command goes on
// the bus, and nothing after it. The stamp is left to the caller so a test can
// reproduce the crash window the reservation exists for.
func (f *sendResultFixture) dispatch(t *testing.T, s *JobsService) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	taskID := uuid.New()
	now := time.Now()
	created, err := s.TaskRepo.CreateTaskWithLock(ctx, &repository.Task{
		ID: taskID, TaskType: "campaign", EmailAccountID: f.mailbox, Status: "pending", ScheduledAt: &now,
	}, &repository.CampaignTask{TaskID: taskID, CampaignID: &f.campaign})
	if err != nil || !created {
		t.Fatalf("create task: created=%v err=%v", created, err)
	}
	if err := s.TaskRepo.UpdateCampaignTaskTracking(ctx, taskID, f.contact, f.step); err != nil {
		t.Fatalf("tracking: %v", err)
	}
	reserved, err := s.CampaignProgressRepo.ReserveSend(ctx, f.campaign, f.contact, f.step, taskID, uuid.Nil, true)
	if err != nil || !reserved {
		t.Fatalf("reserve send: reserved=%v err=%v", reserved, err)
	}
	return taskID
}

// stampSend is a full tick: the reservation, the dispatch, and the stamp that
// follows it.
func (f *sendResultFixture) stampSend(t *testing.T, s *JobsService) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	taskID := f.dispatch(t, s)
	if err := s.CampaignProgressRepo.RecordEmailSent(ctx, f.campaign, f.contact, f.step); err != nil {
		t.Fatalf("record sent: %v", err)
	}
	if err := s.TaskRepo.UpdateTaskStatusWithLock(ctx, taskID, "completed"); err != nil {
		t.Fatalf("complete task: %v", err)
	}
	return taskID
}

func TestLiveHandleEmailFailedWalksBackAndRetriesUntilCap(t *testing.T) {
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

	f := newSendResultFixture(t, handle)
	s := &JobsService{
		TaskRepo:             repository.NewTaskRepository(handle.Pool),
		CampaignRepo:         repository.NewCampaignRepostory(handle),
		CampaignProgressRepo: repository.NewCampaignProgressRepository(handle.Pool),
		CampaignLogRepo:      repository.NewCampaignLogRepository(handle),
		ContactRepo:          repository.NewContactRepostory(handle),
	}

	var sentAt *time.Time
	var attempts int
	readProgress := func() {
		t.Helper()
		if err := handle.Pool.QueryRow(ctx, `SELECT sent_at, send_attempts FROM campaign_contact_progress
			WHERE campaign_id = $1 AND contact_id = $2 AND sequence_id = $3`, f.campaign, f.contact, f.step).
			Scan(&sentAt, &attempts); err != nil {
			t.Fatalf("read progress: %v", err)
		}
	}
	nextPair := func() *repository.ContactSequencePair {
		t.Helper()
		pairs, _, _, err := s.CampaignProgressRepo.FindRoutedPairs(ctx, f.campaign, "created_at", "asc", "", false, false, nil, 1)
		if err != nil {
			t.Fatalf("next pair: %v", err)
		}
		if len(pairs) == 0 {
			return nil
		}
		return &pairs[0]
	}

	// First failure: the stamped step is walked back, the day's counters give
	// the send back, the task is failed, the completed campaign reopens, and
	// the step is routable again.
	taskID := f.stampSend(t, s)
	readProgress()
	if sentAt == nil {
		t.Fatal("precondition: step should be stamped sent")
	}
	if nextPair() != nil {
		t.Fatal("precondition: a stamped single-step lead has nothing left to route")
	}
	if err := s.HandleEmailFailed(ctx, models.SendEmailResult{
		TaskID: taskID, Success: false,
		Error: &models.EmailSendError{Code: "UNSUPPORTED", Message: "worker could not send"},
	}); err != nil {
		t.Fatalf("handle failed: %v", err)
	}
	readProgress()
	if sentAt != nil || attempts != 1 {
		t.Fatalf("after first failure: sent_at=%v attempts=%d, want NULL/1", sentAt, attempts)
	}
	var taskStatus string
	if err := handle.Pool.QueryRow(ctx, `SELECT status FROM tasks WHERE id = $1`, taskID).Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "failed" {
		t.Fatalf("task status = %s, want failed", taskStatus)
	}
	var sent, newLeads int
	if err := handle.Pool.QueryRow(ctx, `SELECT emails_sent, new_leads_started FROM campaign_daily_sends
		WHERE campaign_id = $1 AND send_date = CURRENT_DATE`, f.campaign).Scan(&sent, &newLeads); err != nil {
		t.Fatal(err)
	}
	if sent != 0 || newLeads != 0 {
		t.Fatalf("daily counters after walk-back: sent=%d new=%d, want 0/0", sent, newLeads)
	}
	var campaignStatus string
	if err := handle.Pool.QueryRow(ctx, `SELECT status FROM campaigns WHERE id = $1`, f.campaign).Scan(&campaignStatus); err != nil {
		t.Fatal(err)
	}
	if campaignStatus != "active" {
		t.Fatalf("campaign status = %s, want active (reopened)", campaignStatus)
	}
	if pair := nextPair(); pair == nil || pair.SequenceID != f.step || !pair.IsNewLead {
		t.Fatalf("after walk-back the step should be routable as a new lead, got %+v", pair)
	}
	var logs int
	if err := handle.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM campaign_logs
		WHERE campaign_id = $1 AND event_type = 'email_failed' AND metadata->>'level' = 'error'`, f.campaign).Scan(&logs); err != nil {
		t.Fatal(err)
	}
	if logs != 1 {
		t.Fatalf("campaign log entries = %d, want 1", logs)
	}

	// A duplicate result for the same task is a no-op.
	if err := s.HandleEmailFailed(ctx, models.SendEmailResult{TaskID: taskID, LegacyErrorMsg: "again"}); err != nil {
		t.Fatalf("duplicate: %v", err)
	}
	readProgress()
	if attempts != 1 {
		t.Fatalf("duplicate result changed attempts to %d", attempts)
	}

	// Keep failing: the step stays routable until the cap, then the lead is
	// dropped and marked failed.
	for i := 2; i <= config.CampaignSendMaxAttempts; i++ {
		id := f.stampSend(t, s)
		if err := s.HandleEmailFailed(ctx, models.SendEmailResult{TaskID: id, LegacyErrorMsg: "still broken"}); err != nil {
			t.Fatalf("attempt %d: %v", i, err)
		}
		readProgress()
		if attempts != i {
			t.Fatalf("attempt %d recorded as %d", i, attempts)
		}
		if i < config.CampaignSendMaxAttempts && nextPair() == nil {
			t.Fatalf("after attempt %d the step should still be routable", i)
		}
	}
	if nextPair() != nil {
		t.Fatal("after the attempt cap the lead should be dropped from routing")
	}
	counts, xerr := s.ContactRepo.CampaignLeadCounts(ctx, f.org.String(), f.campaign.String())
	if xerr != nil {
		t.Fatalf("lead counts: %v", xerr)
	}
	if counts.Failed != 1 || counts.Queued != 0 || counts.Total != 1 {
		t.Fatalf("lead counts = %+v, want failed=1", counts)
	}
}
