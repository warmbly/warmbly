package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// followUpFixture is a lead whose first email mailbox A sent earlier today,
// with a threaded second step due now and more mailboxes added per test.
type followUpFixture struct {
	*liveFixture
	contact uuid.UUID
	tz      string
}

func newFollowUpFixture(t *testing.T, pool *pgxpool.Pool, mode models.FollowUpSender, firstSentAgo time.Duration) *followUpFixture {
	t.Helper()
	ctx := context.Background()
	tz := openHoursTimezone(t)
	f := &followUpFixture{liveFixture: newLiveFixture(t, pool, tz), tz: tz}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	if err := pool.QueryRow(ctx, `SELECT contact_id FROM campaign_leads WHERE campaign_id = $1`, f.campaign).Scan(&f.contact); err != nil {
		t.Fatalf("lead: %v", err)
	}
	var first uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM sequences WHERE campaign_id = $1`, f.campaign).Scan(&first); err != nil {
		t.Fatalf("first step: %v", err)
	}
	exec(`UPDATE campaigns SET follow_up_sender = $2 WHERE id = $1`, f.campaign, string(mode))
	second := uuid.New()
	exec(`INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
	          body_plain, body_html, wait_after, position, kind, thread_reply)
	      VALUES ($1, $2, $3, 'Step 2', '', 'Again', '<p>Again</p>', 0, 1, 'email', true)`, second, f.campaign, f.org)
	exec(`UPDATE sequences SET conditions = jsonb_build_object('branches', jsonb_build_array(
	          jsonb_build_object('branch_id', 'live-else', 'target_step_id', $1::text)))
	      WHERE id = $2`, second.String(), first)

	sentAt := earlierToday(firstSentAgo)
	task := uuid.New()
	exec(`INSERT INTO tasks (id, task_type, email_account_id, status, message_id, scheduled_at, completed_at)
	      VALUES ($1, 'campaign', $2, 'completed', $3, $4, $4)`, task, f.mailbox, task.String()+"@test.local", sentAt)
	exec(`INSERT INTO campaign_tasks (task_id, campaign_id, contact_id, sequence_id) VALUES ($1, $2, $3, $4)`,
		task, f.campaign, f.contact, first)
	exec(`INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at, dispatched_at, dispatch_task_id)
	      VALUES ($1, $2, $3, $4, $4, $5)`, f.campaign, f.contact, first, sentAt, task)
	exec(`UPDATE campaign_leads SET email_account_id = $3, sender_assigned_at = $4 WHERE campaign_id = $1 AND contact_id = $2`,
		f.campaign, f.contact, f.mailbox, sentAt)
	return f
}

// addMailbox connects another mailbox to the fixture's org, optionally with a
// campaign send of its own `lastSentAgo` before now.
func (f *followUpFixture) addMailbox(t *testing.T, domain string, limit int, lastSentAgo time.Duration) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	if _, err := f.pool.Exec(ctx, `INSERT INTO email_accounts (id, user_id, organization_id, email, name,
	          signature_plain, signature_html, provider, status, campaign_limit, min_wait_time, timezone)
	      VALUES ($1, $2, $3, $4, 'Other', '', '', 'smtp_imap', 'active', $5, 600, $6)`,
		id, f.user, f.org, "m-"+id.String()[:8]+"@"+domain, limit, f.tz); err != nil {
		t.Fatalf("add mailbox: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = f.pool.Exec(c, `DELETE FROM tasks WHERE email_account_id = $1`, id)
		_, _ = f.pool.Exec(c, `DELETE FROM email_account_daily_plan WHERE email_account_id = $1`, id)
		_, _ = f.pool.Exec(c, `DELETE FROM email_account_behavior WHERE email_account_id = $1`, id)
		_, _ = f.pool.Exec(c, `DELETE FROM email_accounts WHERE id = $1`, id)
	})
	if lastSentAgo > 0 {
		at := earlierToday(lastSentAgo)
		if _, err := f.pool.Exec(ctx, `INSERT INTO tasks (id, task_type, email_account_id, status, message_id, scheduled_at, completed_at)
		      VALUES ($1, 'campaign', $2, 'completed', $3, $4, $4)`, uuid.New(), id, uuid.NewString()+"@test.local", at); err != nil {
			t.Fatalf("mailbox send: %v", err)
		}
	}
	return id
}

func (f *followUpFixture) capFirstMailbox(t *testing.T) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `UPDATE email_accounts SET campaign_limit = 1 WHERE id = $1`, f.mailbox); err != nil {
		t.Fatalf("cap: %v", err)
	}
}

// The default keeps the conversation on its mailbox: a spent mailbox makes
// the follow-up wait rather than move.
func TestLiveSameMailboxFollowUpWaitsForItsSender(t *testing.T) {
	handle, pool := liveDB(t)
	f := newFollowUpFixture(t, pool, models.FollowUpSameMailbox, 2*time.Hour)
	f.capFirstMailbox(t)
	f.addMailbox(t, "other.test", 50, 0)

	_, pair, _, err := liveScheduler(t, handle, pool).CalculateNextCampaignTime(context.Background(), f.campaign)
	if pair != nil || !errors.Is(err, ErrCampaignDeferred) {
		t.Fatalf("pair=%v err=%v, want the follow-up held for its own mailbox", pair, err)
	}
}

// Rotating every step: the follow-up does not wait for the spent mailbox that
// sent the first email, it goes out from another one.
func TestLiveRotatedFollowUpDoesNotWaitForItsLastSender(t *testing.T) {
	handle, pool := liveDB(t)
	f := newFollowUpFixture(t, pool, models.FollowUpRotate, 2*time.Hour)
	f.capFirstMailbox(t)
	other := f.addMailbox(t, "other.test", 50, 0)

	_, pair, accountID, err := liveScheduler(t, handle, pool).CalculateNextCampaignTime(context.Background(), f.campaign)
	if err != nil || pair == nil {
		t.Fatalf("pair=%v err=%v, want a sendable follow-up", pair, err)
	}
	if accountID != other || pair.ContactID != f.contact || pair.IsNewLead {
		t.Fatalf("account=%s contact=%s new=%v, want the follow-up from %s", accountID, pair.ContactID, pair.IsNewLead, other)
	}
}

// With both mailboxes free, rotation would pick the one that sent the first
// email (it has been idle longest), but a rotated follow-up comes from the
// other address.
func TestLiveRotatedFollowUpPrefersAnotherMailbox(t *testing.T) {
	handle, pool := liveDB(t)
	f := newFollowUpFixture(t, pool, models.FollowUpRotate, 3*time.Hour)
	other := f.addMailbox(t, "other.test", 50, time.Hour)

	_, pair, accountID, err := liveScheduler(t, handle, pool).CalculateNextCampaignTime(context.Background(), f.campaign)
	if err != nil || pair == nil {
		t.Fatalf("pair=%v err=%v, want a sendable follow-up", pair, err)
	}
	if accountID != other {
		t.Fatalf("account=%s, want %s rather than the previous sender", accountID, other)
	}
}

// A rotated follow-up still respects the min gap: the only other mailbox sent
// a minute ago, so the lead is not handed to it now.
func TestLiveRotatedFollowUpRespectsTheMinGap(t *testing.T) {
	handle, pool := liveDB(t)
	f := newFollowUpFixture(t, pool, models.FollowUpRotate, 2*time.Hour)
	f.capFirstMailbox(t)
	f.addMailbox(t, "other.test", 50, time.Minute)

	at, pair, _, err := liveScheduler(t, handle, pool).CalculateNextCampaignTime(context.Background(), f.campaign)
	if pair != nil || !errors.Is(err, ErrCampaignDeferred) {
		t.Fatalf("pair=%v err=%v, want a deferral until the gap passes", pair, err)
	}
	if time.Until(at) < 8*time.Minute {
		t.Fatalf("slot %s is inside the other mailbox's 600s gap", at)
	}
}

// Rotating within the domain skips a mailbox on another domain, and waits
// when every mailbox on the lead's domain is spent.
func TestLiveSameDomainFollowUpStaysOnTheDomain(t *testing.T) {
	handle, pool := liveDB(t)
	f := newFollowUpFixture(t, pool, models.FollowUpSameDomain, 2*time.Hour)
	f.capFirstMailbox(t)
	f.addMailbox(t, "other.test", 50, 0)
	sibling := f.addMailbox(t, "test.local", 50, 0)
	s := liveScheduler(t, handle, pool)

	_, pair, accountID, err := s.CalculateNextCampaignTime(context.Background(), f.campaign)
	if err != nil || pair == nil || accountID != sibling {
		t.Fatalf("pair=%v account=%s err=%v, want the follow-up from the same-domain mailbox %s", pair, accountID, err, sibling)
	}

	if _, err := pool.Exec(context.Background(), `UPDATE email_accounts SET campaign_limit = 1 WHERE id = $1`, sibling); err != nil {
		t.Fatalf("cap sibling: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO tasks (id, task_type, email_account_id, status, message_id, scheduled_at, completed_at)
	      VALUES ($1, 'campaign', $2, 'completed', $3, $4, $4)`, uuid.New(), sibling, uuid.NewString()+"@test.local", earlierToday(time.Hour)); err != nil {
		t.Fatalf("spend sibling: %v", err)
	}
	_, pair, _, err = s.CalculateNextCampaignTime(context.Background(), f.campaign)
	if pair != nil || !errors.Is(err, ErrCampaignDeferred) {
		t.Fatalf("pair=%v err=%v, want the lead waiting for its domain, not sent from another", pair, err)
	}
}
