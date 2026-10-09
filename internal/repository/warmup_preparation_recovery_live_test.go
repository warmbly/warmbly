package repository

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func preparationRecoveryFixture(t *testing.T) (*poolLinkFixture, *taskRepository, uuid.UUID, uuid.UUID) {
	t.Helper()
	f, r := lineageFixture(t)
	ctx := t.Context()
	worker := uuid.New()
	queries := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO fleet_nodes(id,role,active,last_seen_at,warmup_send_protocol) VALUES($1,'worker',true,NOW(),1)`, []any{worker}},
		{`INSERT INTO workers(id) VALUES($1)`, []any{worker}},
		{`UPDATE email_accounts SET worker_id=$1,test_mode='legacy' WHERE id=$2`, []any{worker, f.sender}},
		{`INSERT INTO warmup_tasks(task_id,target_account_id,lineage_version) VALUES($1,$2,1)`, []any{f.task, f.recipient}},
		{`UPDATE tasks SET status='active' WHERE id=$1`, []any{f.task}},
	}
	for _, q := range queries {
		if _, err := f.pool.Exec(ctx, q.query, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM fleet_nodes WHERE id=$1`, worker) })
	f.token(t, "", "Preparation recovery", false)
	nonce, err := r.AuthorizeWarmupDispatch(ctx, f.task, f.sender, worker)
	if err != nil {
		t.Fatal(err)
	}
	return f, r, worker, nonce
}

func TestLiveWarmupRecoversOldPreparationResultAfterDeferral(t *testing.T) {
	f, r, worker, nonce := preparationRecoveryFixture(t)
	ctx := t.Context()
	if err := r.DeferWarmupDispatch(ctx, f.task, f.sender, worker, nonce, time.Now()); err != nil {
		t.Fatal(err)
	}
	result := models.SendEmailResult{TaskID: f.task, SentAt: time.Now().UTC(), Error: &models.EmailSendError{Failure: &errx.SendFailure{
		Protocol: "internal", Stage: "prepare", Disposition: errx.SendRetry, Scope: "mailbox", ObservedAt: time.Now().UTC(),
	}}}
	// Reproduce the released backend's result write after the task was requeued.
	apply := func(ctx context.Context) error {
		return f.warmup.FailWarmupSend(ctx, f.sender, f.task, result.SentAt, "Send not published", "Worker authority unavailable")
	}
	if err := r.ApplySendResult(ctx, result, apply); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE email_accounts SET send_cooldown_until=NULL WHERE id=$1`, f.sender); err != nil {
		t.Fatal(err)
	}
	if claimed, err := r.ClaimWarmupTask(ctx, f.task, time.Now().Add(time.Second)); err != nil || !claimed {
		t.Fatalf("claim=%v error=%v", claimed, err)
	}
	f.token(t, "", "Preparation recovery", false)
	if _, err := r.AuthorizeWarmupDispatch(ctx, f.task, f.sender, worker); err != nil {
		t.Fatal(err)
	}
	if err := r.ApplySendResult(ctx, result, apply); err != nil {
		t.Fatal(err)
	}
	var reason string
	if err := f.pool.QueryRow(ctx, `SELECT send_recovery_reason FROM email_accounts WHERE id=$1`, f.sender).Scan(&reason); err != nil || reason != "conflict" {
		t.Fatalf("old backend race not reproduced: reason=%s error=%v", reason, err)
	}
	var wg sync.WaitGroup
	var recovered atomic.Int32
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			retired, err := r.retireUnstartedWarmupDispatch(ctx, f.task, f.sender, time.Now().Add(time.Second))
			if err != nil {
				t.Error(err)
			} else if retired {
				recovered.Add(1)
			}
		}()
	}
	wg.Wait()
	if recovered.Load() != 1 {
		t.Fatalf("never-published command retired %d times", recovered.Load())
	}
	var held bool
	var status string
	var count, tokens int
	if err := f.pool.QueryRow(ctx, `SELECT ea.send_recovery_hold,t.status,ws.emails_sent,
		(SELECT count(*) FROM warmup_tokens WHERE task_id=t.id)
		FROM tasks t JOIN email_accounts ea ON ea.id=t.email_account_id JOIN warmup_statistics ws ON ws.email_account_id=ea.id AND ws.date=CURRENT_DATE
		WHERE t.id=$1`, f.task).Scan(&held, &status, &count, &tokens); err != nil || held || status != "cancelled" || count != 0 || tokens != 0 {
		t.Fatalf("recovery held=%v status=%s sent=%d tokens=%d error=%v", held, status, count, tokens, err)
	}
	state, err := r.InspectWarmupDispatch(ctx, f.task, f.sender, worker)
	if err != nil || state.State != "denied" {
		t.Fatalf("retired command replay: %+v %v", state, err)
	}
}

func TestLiveWarmupPreparationRecoveryRetainsExecutionAndProviderEvidence(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
	}{
		{"worker began", `UPDATE warmup_tasks SET dispatch_started_at=NOW() WHERE task_id=$1`},
		{"executor began", `UPDATE tasks SET send_executor_started_at=NOW() WHERE id=$1`},
		{"outbound nonce", `UPDATE tasks SET send_executor_nonce=gen_random_uuid() WHERE id=$1`},
		{"outbound result", `UPDATE tasks SET send_executor_result='{"success":false}' WHERE id=$1`},
		{"worker result", `UPDATE warmup_tasks SET dispatch_result='{"success":false}' WHERE task_id=$1`},
		{"reserved outbound", `UPDATE tasks SET send_reserved_at=NOW()-INTERVAL '2 hours' WHERE id=$1`},
		{"provider confirmed", `UPDATE warmup_tokens SET sent_message_id='accepted@example.test' WHERE task_id=$1`},
		{"outbound attempt", `INSERT INTO outbound_attempts(nonce,task_id,email_account_id,provider,attempted_at,recipient_count) SELECT gen_random_uuid(),id,email_account_id,'smtp_imap',NOW(),1 FROM tasks WHERE id=$1`},
		{"confirmed receipt", `INSERT INTO warmup_received(email_account_id,internal_id,sender_account_id,message_id,task_id) SELECT recipient_account_id,gen_random_uuid(),sender_account_id,'confirmed@example.test',task_id FROM warmup_tokens WHERE task_id=$1`},
		{"provider failure", `UPDATE tasks SET send_result_evidence=jsonb_set(send_result_evidence,'{protocol}','"smtp"') WHERE id=$1`},
		{"current preparation", `UPDATE tasks SET send_result_applied_at=NOW() WHERE id=$1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r, _, _ := preparationRecoveryFixture(t)
			ctx := t.Context()
			if _, err := f.pool.Exec(ctx, `UPDATE tasks SET completed_at=NOW()-INTERVAL '1 hour',
				send_result_applied_at=NOW()-INTERVAL '2 hours',send_released_at=NOW()-INTERVAL '2 hours',
				send_result_evidence='{"protocol":"internal","stage":"prepare","disposition":"retry","scope":"mailbox"}' WHERE id=$1`, f.task); err != nil {
				t.Fatal(err)
			}
			if _, err := f.pool.Exec(ctx, tc.query, f.task); err != nil {
				t.Fatal(err)
			}
			if retired, err := r.retireUnstartedWarmupDispatch(ctx, f.task, f.sender, time.Now()); err != nil || retired {
				t.Fatalf("protected send retired=%v error=%v", retired, err)
			}
		})
	}
}
