package repository

import (
	"context"
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
