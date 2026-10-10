package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// A refused warmup send is reported until a later send goes out.
func TestLiveLastWarmupSendFailure(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx := context.Background()
	f := newWarmupUsageFixture(t, pool)
	repo := &warmupRepository{db: pool}
	since := time.Now().Add(-24 * time.Hour)

	failed := func(at time.Time, message string) {
		id := uuid.New()
		f.exec(`INSERT INTO tasks (id, task_type, email_account_id, status, message_id, updated_at)
		        VALUES ($1, 'warmup', $2, 'failed', '', $3)`, id, f.account, at)
		f.exec(`INSERT INTO task_failures (task_id, title, message) VALUES ($1, 'Send failed', $2)`, id, message)
	}

	if got, err := repo.LastWarmupSendFailure(ctx, f.account, since); err != nil || got != nil {
		t.Fatalf("no failures: got %+v, %v; want nil", got, err)
	}

	failed(time.Now().Add(-3*time.Hour), "older refusal")
	failed(time.Now().Add(-2*time.Hour), "dial smtp.example.test:465: connection refused")
	got, err := repo.LastWarmupSendFailure(ctx, f.account, since)
	if err != nil || got == nil || got.Message != "dial smtp.example.test:465: connection refused" {
		t.Fatalf("got %+v, %v; want the newest refusal", got, err)
	}

	// A dead-lettered dispatch is the platform's, not the server's answer.
	dead := uuid.New()
	f.exec(`INSERT INTO tasks (id, task_type, email_account_id, status, message_id, updated_at)
	        VALUES ($1, 'warmup', $2, 'dead_lettered', '', NOW())`, dead, f.account)
	f.exec(`INSERT INTO task_failures (task_id, title, message) VALUES ($1, 'Send failed', 'worker offline')`, dead)
	if got, _ := repo.LastWarmupSendFailure(ctx, f.account, since); got == nil || got.Message == "worker offline" {
		t.Fatalf("got %+v; a dead-lettered task must not be reported", got)
	}

	if got, _ := repo.LastWarmupSendFailure(ctx, f.account, time.Now().Add(-time.Hour)); got != nil {
		t.Fatalf("got %+v; a refusal before the window must not be reported", got)
	}

	// A later send that was only dispatched has not proved the server takes mail.
	later := uuid.New()
	f.exec(`INSERT INTO tasks (id, task_type, email_account_id, status, message_id, completed_at)
	        VALUES ($1, 'warmup', $2, 'completed', '', NOW() - interval '1 hour')`, later, f.account)
	f.exec(`INSERT INTO warmup_tokens (token, task_id, sender_account_id, recipient_account_id, conversation_turn)
	        VALUES ($1, $2, $3, $3, 0)`, uuid.New(), later, f.account)
	if got, _ := repo.LastWarmupSendFailure(ctx, f.account, since); got == nil {
		t.Fatal("a dispatched send with no delivery must not clear the report")
	}

	// Its delivery does.
	f.exec(`UPDATE warmup_tokens SET sent_message_id = '<delivered@example.test>' WHERE task_id = $1`, later)
	if got, err := repo.LastWarmupSendFailure(ctx, f.account, since); err != nil || got != nil {
		t.Fatalf("after a delivered send: got %+v, %v; want nil", got, err)
	}
}

func TestLiveDispatchRetryUpgradePreservesOldTasksAndNewMailboxes(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx, now := t.Context(), time.Now().UTC().Truncate(time.Microsecond)
	f := newWarmupUsageFixture(t, pool)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	apply := func(file string) {
		t.Helper()
		sql, err := os.ReadFile("../infrastructure/db/migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		exec(string(sql))
	}
	// Recreate the v0.6.39 schema inside a transaction; never commit the rollback.
	for _, file := range []string{"000277_task_dispatch_retry.down.sql", "000276_mailbox_behavior_defaults.down.sql", "000275_unibox_forward_thread.down.sql"} {
		apply(file)
	}
	old, uncertain, nonce := uuid.New(), uuid.New(), uuid.New()
	exec(`UPDATE email_accounts SET status='active',warmup=NOW(),test_mode='legacy',send_recovery_hold=true,
	    send_recovery_reason='unknown',send_recovery_task_id=$2 WHERE id=$1`, f.account, uncertain)
	exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id,scheduled_at)
	    VALUES($1,'warmup',$2,'pending','',$3)`, old, f.account, now.Add(-2*time.Hour))
	exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id,send_reserved_at,send_result_state,send_executor_nonce,send_executor_started_at)
	    VALUES($1,'warmup',$2,'completed','',$3,'unknown',$4,$3)`, uncertain, f.account, now.Add(-time.Hour), nonce)
	for _, file := range []string{"000275_unibox_forward_thread.up.sql", "000276_mailbox_behavior_defaults.up.sql", "000277_task_dispatch_retry.up.sql"} {
		apply(file)
	}
	var retries int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM tasks WHERE id IN($1,$2) AND dispatch_retry_at IS NOT NULL`, old, uncertain).Scan(&retries); err != nil || retries != 0 {
		t.Fatalf("upgrade unexpectedly delayed existing tasks: retries=%d err=%v", retries, err)
	}
	newAccount, newer := uuid.New(), uuid.New()
	exec(`INSERT INTO email_accounts(id,user_id,organization_id,email,name,signature_plain,signature_html,provider,status,warmup,test_mode)
	    VALUES($1,$2,$3,$4,'Upgrade test','','','smtp_imap','active',NOW(),'legacy')`, newAccount, f.user, f.org, "upgrade-"+newAccount.String()+"@example.test")
	exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id,scheduled_at)
	    VALUES($1,'warmup',$2,'pending','',$3)`, newer, newAccount, now.Add(-time.Minute))
	checkDue := func(want uuid.UUID) {
		t.Helper()
		var next uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM tasks WHERE id IN($1,$2,$3) AND status='pending'
		    AND GREATEST(scheduled_at,COALESCE(dispatch_retry_at,scheduled_at))<=NOW()
		    ORDER BY GREATEST(scheduled_at,COALESCE(dispatch_retry_at,scheduled_at)),id LIMIT 1`, old, newer, uncertain).Scan(&next); err != nil || next != want {
			t.Fatalf("next due task=%v, want=%v err=%v", next, want, err)
		}
	}
	checkDue(old)
	exec(`UPDATE tasks SET dispatch_retry_at=$2 WHERE id=$1`, old, now.Add(5*time.Minute))
	checkDue(newer)
	var held bool
	var state string
	var savedNonce uuid.UUID
	var started time.Time
	if err := tx.QueryRow(ctx, `SELECT e.send_recovery_hold,t.send_result_state,t.send_executor_nonce,t.send_executor_started_at
	    FROM tasks t JOIN email_accounts e ON e.id=t.email_account_id WHERE t.id=$1`, uncertain).Scan(&held, &state, &savedNonce, &started); err != nil || !held || state != "unknown" || savedNonce != nonce || !started.Equal(now.Add(-time.Hour)) {
		t.Fatalf("upgrade changed send protections: held=%v state=%s nonce=%v started=%v err=%v", held, state, savedNonce, started, err)
	}
}

func TestLivePersistentMailboxLoadingFailure(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx, now := context.Background(), time.Now().UTC().Truncate(time.Microsecond)
	f := newWarmupUsageFixture(t, pool)
	f.exec(`UPDATE email_accounts SET status='active',warmup=NOW()-INTERVAL '7 days',test_mode='legacy' WHERE id=$1`, f.account)
	repo := &warmupRepository{db: pool}
	since := now.Add(-24 * time.Hour)
	failed := func(at time.Time, message string) uuid.UUID {
		id := uuid.New()
		f.exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id,updated_at) VALUES($1,'warmup',$2,'failed','',$3)`, id, f.account, at)
		f.exec(`INSERT INTO task_failures(task_id,title,message) VALUES($1,'Send failed',$2)`, id, message)
		return id
	}
	assertVisible := func(want bool) {
		t.Helper()
		got, err := repo.LastWarmupSendFailure(ctx, f.account, since)
		if err != nil || (got != nil) != want {
			t.Fatalf("failure=%+v error=%v want visible=%v", got, err, want)
		}
		incidents, err := repo.ListMailboxLoadingIncidents(ctx, now)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, v := range incidents {
			if v.AccountID == f.account {
				found = true
				if v.WorkerID != nil {
					t.Fatal("unassigned worker must remain nil")
				}
			}
		}
		if found != want {
			t.Fatalf("persistent incident=%v want %v", found, want)
		}
	}
	failed(now.Add(-2*time.Hour), models.MailboxNotLoadedPrefix)
	assertVisible(false)
	failed(now.Add(-10*time.Minute), models.MailboxNotLoadedPrefix+", nothing sent.")
	assertVisible(false)
	failed(now.Add(-75*time.Minute), models.MailboxNotLoadedPrefix)
	failed(now.Add(-40*time.Minute), models.MailboxNotLoadedPrefix)
	assertVisible(true)
	future := failed(now.Add(10*time.Minute), models.MailboxNotLoadedPrefix)
	assertVisible(true)
	f.exec(`DELETE FROM tasks WHERE id=$1`, future)
	future = failed(now.Add(10*time.Minute), "Future provider failure.")
	assertVisible(true)
	f.exec(`DELETE FROM tasks WHERE id=$1`, future)
	assertVisible(true)
	got, err := repo.LastWarmupSendFailure(ctx, f.account, since)
	if err != nil || got.Kind != models.WarmupFailureLoading || got.FirstFailureAt == nil || !got.FirstFailureAt.Equal(now.Add(-2*time.Hour)) {
		t.Fatalf("missing incident classification: %+v %v", got, err)
	}
	for _, change := range []string{"warmup_paused_at=NOW()", "status='inactive'", "test_mode='off'", "test_mode='diagnostic',test_send_enabled=false"} {
		f.exec(`UPDATE email_accounts SET `+change+` WHERE id=$1`, f.account)
		assertVisible(false)
		f.exec(`UPDATE email_accounts SET warmup_paused_at=NULL,status='active',test_mode=NULL WHERE id=$1`, f.account)
	}
	f.exec(`UPDATE tasks SET updated_at=updated_at-INTERVAL '2 hours' WHERE email_account_id=$1 AND status='failed'`, f.account)
	assertVisible(false)
	f.exec(`UPDATE tasks SET updated_at=updated_at+INTERVAL '2 hours' WHERE email_account_id=$1 AND status='failed'`, f.account)
	later := uuid.New()
	f.exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id,completed_at) VALUES($1,'warmup',$2,'completed','',$3)`, later, f.account, now.Add(-5*time.Minute))
	f.exec(`INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id,conversation_turn) VALUES($1,$2,$3,$3,0)`, uuid.New(), later, f.account)
	assertVisible(true)
	f.exec(`UPDATE warmup_tokens SET sent_message_id='<confirmed@example.test>' WHERE task_id=$1`, later)
	assertVisible(false)
	failed(now.Add(-time.Minute), models.MailboxNotLoadedPrefix)
	assertVisible(false)
	failed(now, "Provider sign-in was refused.")
	got, err = repo.LastWarmupSendFailure(ctx, f.account, since)
	if err != nil || got == nil || got.Kind != "" || got.Message != "Provider sign-in was refused." {
		t.Fatalf("new provider error must show immediately: %+v %v", got, err)
	}
}

func TestLiveLoadingIncidentNonLoadingBoundary(t *testing.T) {
	_, pool := liveContactDB(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, tc := range []struct {
		name     string
		boundary time.Duration
		first    time.Duration
	}{
		{"before incident", -3 * time.Hour, -2 * time.Hour},
		{"same timestamp", -80 * time.Minute, -40 * time.Minute},
		{"between failures", -79 * time.Minute, -40 * time.Minute},
		{"future failure", 10 * time.Minute, -2 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newWarmupUsageFixture(t, pool)
			failed := func(offset time.Duration, message string) {
				id := uuid.New()
				f.exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id,updated_at) VALUES($1,'warmup',$2,'failed','',$3)`, id, f.account, now.Add(offset))
				f.exec(`INSERT INTO task_failures(task_id,title,message) VALUES($1,'private',$2)`, id, message)
			}
			for _, offset := range []time.Duration{-2 * time.Hour, -80 * time.Minute, -40 * time.Minute, -5 * time.Minute} {
				failed(offset, models.MailboxNotLoadedPrefix)
			}
			failed(tc.boundary, "Other provider failure")
			var first time.Time
			if err := pool.QueryRow(t.Context(), `SELECT first_at FROM (`+warmupSendFailuresSQL+`) failures`, f.account, mailboxLoadingPattern, now).Scan(&first); err != nil {
				t.Fatal(err)
			}
			if !first.Equal(now.Add(tc.first)) {
				t.Fatalf("first failure=%v want=%v", first, now.Add(tc.first))
			}
		})
	}
}

func TestLiveDispatchRetryDoesNotStarveNewerTask(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx, now := context.Background(), time.Now().UTC().Truncate(time.Microsecond)
	f := newWarmupUsageFixture(t, pool)
	f.exec(`UPDATE email_accounts SET status='active',warmup=NOW()-INTERVAL '7 days',test_mode='legacy' WHERE id=$1`, f.account)
	old, newer := uuid.New(), uuid.New()
	f.exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id,scheduled_at) VALUES($1,'warmup',$3,'pending','',$4),($2,'warmup',$3,'pending','',$5)`, old, newer, f.account, now.Add(-24*time.Hour), now.Add(-2*time.Hour))
	repo := &taskRepository{db: pool}
	if err := repo.RecordDispatchAttempt(ctx, old, true, now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	ids, err := repo.ListDuePendingTaskIDs(ctx, 10000)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, id := range ids {
		if id == old {
			t.Fatal("retrying oldest task still consumes due batch")
		}
		if id == newer {
			found = true
		}
	}
	if !found {
		t.Fatal("newer task not reachable")
	}
	incidents, err := repo.ListOverdueWarmupDispatches(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, v := range incidents {
		if v.AccountID == f.account {
			found = true
		}
	}
	if !found {
		t.Fatal("overdue task absent from monitor")
	}
	var scheduled time.Time
	var status string
	if err := pool.QueryRow(ctx, `SELECT scheduled_at,status::text FROM tasks WHERE id=$1`, old).Scan(&scheduled, &status); err != nil || status != "pending" || !scheduled.Equal(now.Add(-24*time.Hour)) {
		t.Fatalf("retry changed intended schedule or send state: %v %s %v", scheduled, status, err)
	}
	f.exec(`UPDATE tasks SET dispatch_retry_at=NOW()-INTERVAL '1 second' WHERE id=$1`, old)
	ids, err = repo.ListDuePendingTaskIDs(ctx, 10000)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, id := range ids {
		if id == old {
			found = true
		}
	}
	if !found {
		t.Fatal("old task never becomes retryable")
	}
	f.exec(`UPDATE tasks SET status='completed',send_result_state='unknown',send_executor_started_at=NOW() WHERE id=$1`, old)
	if err := repo.RecordDispatchAttempt(ctx, old, true, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var retry time.Time
	if err := pool.QueryRow(ctx, `SELECT dispatch_retry_at,status::text FROM tasks WHERE id=$1`, old).Scan(&retry, &status); err != nil || retry.After(now) || status != "completed" {
		t.Fatalf("started/uncertain send changed: %v %s %v", retry, status, err)
	}
}
