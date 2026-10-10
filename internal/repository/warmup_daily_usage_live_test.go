package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// warmupUsageFixture owns the isolated mailbox ledger used by each test.
type warmupUsageFixture struct {
	user    uuid.UUID
	org     uuid.UUID
	account uuid.UUID
	exec    func(query string, args ...any)
}

func newWarmupUsageFixture(t *testing.T, pool *pgxpool.Pool) warmupUsageFixture {
	t.Helper()
	ctx := context.Background()
	f := warmupUsageFixture{user: uuid.New(), org: uuid.New(), account: uuid.New()}
	f.exec = func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	f.exec(`INSERT INTO users (id, first_name, last_name, email)
	        VALUES ($1, 'Warmup', 'Usage', $2)`, f.user, "warmup-usage-"+uuid.NewString()+"@example.test")
	f.exec(`INSERT INTO organizations (id, name, owner_user_id)
	        VALUES ($1, 'Warmup usage', $2)`, f.org, f.user)
	f.exec(`INSERT INTO email_accounts
	          (id, user_id, organization_id, email, name, signature_plain, signature_html, provider, warmup_max)
	        VALUES ($1, $2, $3, $4, 'Warmup usage', '', '', 'smtp_imap', 40)`,
		f.account, f.user, f.org, "mailbox-"+uuid.NewString()+"@example.test")
	t.Cleanup(func() {
		for _, step := range []struct {
			query string
			arg   uuid.UUID
		}{
			{`DELETE FROM email_accounts WHERE id = $1`, f.account},
			{`DELETE FROM organizations WHERE id = $1`, f.org},
			{`DELETE FROM users WHERE id = $1`, f.user},
		} {
			if _, err := pool.Exec(context.Background(), step.query, step.arg); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	})
	return f
}

// Reported usage must match the scheduler's completed-task cap.
func TestLiveAccountDailyUsageCountsWarmupTasksNotTheStatisticsRow(t *testing.T) {
	handle, pool := liveContactDB(t)
	ctx := context.Background()
	f := newWarmupUsageFixture(t, pool)
	day := time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC)

	// Dispatch counted both sends before one failed.
	f.exec(`INSERT INTO tasks (id, task_type, email_account_id, status, message_id, completed_at)
	        VALUES ($1, 'warmup', $2, 'completed', '', $3)`, uuid.New(), f.account, day)
	f.exec(`INSERT INTO tasks (id, task_type, email_account_id, status, message_id, completed_at)
	        VALUES ($1, 'warmup', $2, 'failed', '', $3)`, uuid.New(), f.account, day)
	f.exec(`INSERT INTO warmup_statistics (email_account_id, date, emails_sent, target_volume)
	        VALUES ($1, DATE($2), 2, 10)`, f.account, day)

	repo := &analyticsRepository{DB: handle}
	usage, xerr := repo.GetAccountDailyUsage(ctx, f.account, day)
	if xerr != nil {
		t.Fatalf("GetAccountDailyUsage: %v", xerr)
	}
	if usage.WarmupSent != 1 {
		t.Fatalf("warmup_sent = %d, want 1: only the delivered send counts, the way the cap counts it", usage.WarmupSent)
	}
	if usage.WarmupLimit != 40 {
		t.Fatalf("warmup_limit = %d, want 40", usage.WarmupLimit)
	}
}

// Daily warmup usage is scoped by task type and date.
func TestLiveAccountDailyUsageScopesWarmupToTypeAndDay(t *testing.T) {
	handle, pool := liveContactDB(t)
	ctx := context.Background()
	f := newWarmupUsageFixture(t, pool)
	day := time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC)

	f.exec(`INSERT INTO tasks (id, task_type, email_account_id, status, message_id, completed_at)
	        VALUES ($1, 'campaign', $2, 'completed', '<c@example.test>', $3)`, uuid.New(), f.account, day)
	f.exec(`INSERT INTO tasks (id, task_type, email_account_id, status, message_id, completed_at)
	        VALUES ($1, 'warmup', $2, 'completed', '', $3)`, uuid.New(), f.account, day.AddDate(0, 0, -1))

	repo := &analyticsRepository{DB: handle}
	usage, xerr := repo.GetAccountDailyUsage(ctx, f.account, day)
	if xerr != nil {
		t.Fatalf("GetAccountDailyUsage: %v", xerr)
	}
	if usage.WarmupSent != 0 {
		t.Fatalf("warmup_sent = %d, want 0", usage.WarmupSent)
	}
	if usage.CampaignSent != 1 {
		t.Fatalf("campaign_sent = %d, want 1", usage.CampaignSent)
	}
}

// Failed sends refund both sent and reply counters taken at dispatch.
func TestLiveGiveBackDailySendReversesADispatchedWarmupSend(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx := context.Background()
	f := newWarmupUsageFixture(t, pool)
	day := time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC)
	repo := &warmupRepository{db: pool}

	newThread, reply := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{newThread, reply} {
		f.exec(`INSERT INTO tasks (id, task_type, email_account_id, status, message_id, completed_at)
		        VALUES ($1, 'warmup', $2, 'completed', '', $3)`, id, f.account, day)
	}
	f.exec(`INSERT INTO warmup_tasks(task_id,warmup_charged_date,warmup_reply_charged) VALUES($1,($2::timestamptz AT TIME ZONE 'UTC')::date,false),($3,($2::timestamptz AT TIME ZONE 'UTC')::date,true)`, newThread, day, reply)
	// Only a nonzero conversation turn consumed the reply counter.
	f.exec(`INSERT INTO warmup_tokens (token, task_id, sender_account_id, recipient_account_id, conversation_turn)
	        VALUES ($1, $2, $3, $3, 0)`, uuid.New(), newThread, f.account)
	f.exec(`INSERT INTO warmup_tokens (token, task_id, sender_account_id, recipient_account_id, conversation_turn)
	        VALUES ($1, $2, $3, $3, 2)`, uuid.New(), reply, f.account)
	f.exec(`INSERT INTO warmup_statistics (email_account_id, date, emails_sent, emails_replied, target_volume)
	        VALUES ($1, DATE($2), 3, 1, 10)`, f.account, day)

	read := func() (sent, replied int) {
		t.Helper()
		if err := pool.QueryRow(ctx, `SELECT emails_sent, emails_replied FROM warmup_statistics
		                              WHERE email_account_id = $1 AND date = DATE($2)`, f.account, day).Scan(&sent, &replied); err != nil {
			t.Fatalf("read statistics: %v", err)
		}
		return sent, replied
	}

	// The new thread: the send comes back, the reply count is untouched.
	if err := repo.FailWarmupSend(ctx, f.account, newThread, day, "Send failed", "refused"); err != nil {
		t.Fatalf("FailWarmupSend: %v", err)
	}
	if sent, replied := read(); sent != 2 || replied != 1 {
		t.Fatalf("after giving back a new thread: sent=%d replied=%d, want 2/1", sent, replied)
	}

	// The reply: both come back, because both were taken at dispatch.
	if err := repo.FailWarmupSend(ctx, f.account, reply, day, "Send failed", "refused"); err != nil {
		t.Fatalf("FailWarmupSend: %v", err)
	}
	if sent, replied := read(); sent != 1 || replied != 0 {
		t.Fatalf("after giving back a reply: sent=%d replied=%d, want 1/0", sent, replied)
	}

	// Duplicate failure delivery must not refund another send.
	if err := repo.FailWarmupSend(ctx, f.account, reply, day, "Send failed", "refused"); err != nil {
		t.Fatalf("FailWarmupSend (repeat): %v", err)
	}
	if sent, replied := read(); sent != 1 || replied != 0 {
		t.Fatalf("a repeated give-back changed the counters: sent=%d replied=%d", sent, replied)
	}
}

// A late failure refunds the original dispatch day only.
func TestLiveGiveBackDailySendLeavesOtherDaysAlone(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx := context.Background()
	f := newWarmupUsageFixture(t, pool)
	counted := time.Date(2026, time.September, 16, 23, 50, 0, 0, time.UTC)
	next := counted.AddDate(0, 0, 1)
	repo := &warmupRepository{db: pool}

	taskID := uuid.New()
	f.exec(`INSERT INTO tasks (id, task_type, email_account_id, status, message_id, completed_at)
	        VALUES ($1, 'warmup', $2, 'completed', '', $3)`, taskID, f.account, counted)
	f.exec(`INSERT INTO warmup_tasks(task_id,warmup_charged_date,warmup_reply_charged) VALUES($1,($2::timestamptz AT TIME ZONE 'UTC')::date,false)`, taskID, counted)
	f.exec(`INSERT INTO warmup_statistics (email_account_id, date, emails_sent, target_volume)
	        VALUES ($1, DATE($2), 5, 10)`, f.account, counted)
	f.exec(`INSERT INTO warmup_statistics (email_account_id, date, emails_sent, target_volume)
	        VALUES ($1, DATE($2), 3, 10)`, f.account, next)

	if err := repo.FailWarmupSend(ctx, f.account, taskID, counted, "Send failed", "refused"); err != nil {
		t.Fatalf("FailWarmupSend: %v", err)
	}
	for _, tc := range []struct {
		day  time.Time
		want int
	}{{counted, 4}, {next, 3}} {
		var sent int
		if err := pool.QueryRow(ctx, `SELECT emails_sent FROM warmup_statistics
		                              WHERE email_account_id = $1 AND date = DATE($2)`, f.account, tc.day).Scan(&sent); err != nil {
			t.Fatalf("read statistics: %v", err)
		}
		if sent != tc.want {
			t.Fatalf("%s emails_sent = %d, want %d", tc.day.Format("2006-01-02"), sent, tc.want)
		}
	}
}
