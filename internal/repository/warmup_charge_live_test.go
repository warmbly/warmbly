package repository

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func newWarmupChargeFixture(t *testing.T, reply bool) (*poolLinkFixture, *taskRepository, uuid.UUID, uuid.UUID) {
	t.Helper()
	f, r := lineageFixture(t)
	ctx := t.Context()
	worker := uuid.New()
	queries := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO fleet_nodes(id,role,active,last_seen_at,warmup_send_protocol) VALUES($1,'worker',true,NOW(),2)`, []any{worker}},
		{`INSERT INTO workers(id) VALUES($1)`, []any{worker}},
		{`UPDATE email_accounts SET worker_id=$1,shared_daily_limit=100,rolling_recipient_limit=100,warmup_max=50 WHERE id=$2`, []any{worker, f.sender}},
		{`INSERT INTO warmup_pool_participants(pool_id,email_account_id) SELECT wp.id,ea.id FROM warmup_pools wp CROSS JOIN email_accounts ea WHERE wp.pool_type='free' AND ea.id IN($1,$2) ON CONFLICT DO NOTHING`, []any{f.sender, f.recipient}},
		{`UPDATE tasks SET status='active' WHERE id=$1`, []any{f.task}},
		{`INSERT INTO warmup_tasks(task_id,target_account_id,lineage_version,subject,scenario_version,rendering_version,max_turns) VALUES($1,$2,1,'Pinned diagnostic','diagnostic-v1','canonical-v1',3)`, []any{f.task, f.recipient}},
	}
	for _, q := range queries {
		if _, err := f.pool.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	if reply {
		parent := uuid.New()
		if _, err := f.pool.Exec(ctx, `INSERT INTO tasks(id,email_account_id,task_type,status,message_id) VALUES($1,$2,'warmup','completed','')`, parent, f.recipient); err != nil {
			t.Fatal(err)
		}
		if _, err := f.pool.Exec(ctx, `UPDATE warmup_tasks SET parent_task_id=$1 WHERE task_id=$2`, parent, f.task); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM fleet_nodes WHERE id=$1`, worker) })
	f.token(t, "", "Pinned diagnostic", false)
	nonce, err := r.AuthorizeWarmupDispatch(ctx, f.task, f.sender, worker)
	if err != nil {
		t.Fatal(err)
	}
	return f, r, worker, nonce
}

func TestLiveWarmupReservationPreservesOriginalChargeDay(t *testing.T) {
	f, r, worker, _ := newWarmupChargeFixture(t, false)
	ctx := t.Context()
	for _, q := range []string{
		`UPDATE tasks SET completed_at=completed_at-INTERVAL '1 day' WHERE id=$1`,
		`UPDATE warmup_tasks SET warmup_charged_date=warmup_charged_date-1 WHERE task_id=$1`,
	} {
		if _, err := f.pool.Exec(ctx, q, f.task); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.pool.Exec(ctx, `UPDATE warmup_statistics SET date=date-1 WHERE email_account_id=$1`, f.sender); err != nil {
		t.Fatal(err)
	}
	if err := f.warmup.IncrementDailyCount(ctx, f.sender, time.Now()); err != nil {
		t.Fatal(err)
	}
	original, err := r.GetTask(ctx, f.task)
	if err != nil {
		t.Fatal(err)
	}
	address := "pl-" + f.recipient.String()[:8] + "@test.local"
	if _, err = r.ReserveOutbound(ctx, OutboundReservation{TaskID: f.task, MailboxID: f.sender, OrganizationID: f.org, WorkerID: worker, Provider: models.InboxProviderSMTPIMAP, Recipients: []string{address}}); err != nil {
		t.Fatal(err)
	}
	reserved, err := r.GetTask(ctx, f.task)
	if err != nil || reserved.CompletedAt == nil || original.CompletedAt == nil || !reserved.CompletedAt.Equal(*original.CompletedAt) {
		t.Fatalf("reservation rewrote the charge timestamp: original=%+v reserved=%+v error=%v", original, reserved, err)
	}
	// Generic completion cannot change the independently recorded charge day.
	if err = r.UpdateTaskStatus(ctx, f.task, "completed"); err != nil {
		t.Fatal(err)
	}
	result := models.SendEmailResult{TaskID: f.task, Error: &models.EmailSendError{Failure: &errx.SendFailure{Disposition: errx.SendRetry}}}
	for range 2 {
		if err = r.ApplySendResult(ctx, result, func(ctx context.Context) error {
			return f.warmup.FailWarmupSend(ctx, f.sender, f.task, time.Now(), "Send failed", "Provider refused")
		}); err != nil {
			t.Fatal(err)
		}
	}
	var yesterday, today int
	if err = f.pool.QueryRow(ctx, `SELECT SUM(emails_sent) FILTER(WHERE date=(NOW() AT TIME ZONE 'UTC')::date-1),SUM(emails_sent) FILTER(WHERE date=(NOW() AT TIME ZONE 'UTC')::date) FROM warmup_statistics WHERE email_account_id=$1`, f.sender).Scan(&yesterday, &today); err != nil {
		t.Fatal(err)
	}
	if yesterday != 0 || today != 1 {
		t.Fatalf("wrong-day refund: yesterday=%d today=%d; want 0/1", yesterday, today)
	}
}

func TestLiveWarmupRefundCASProtectsConcurrentAndRestampedTasks(t *testing.T) {
	f, r, _, _ := newWarmupChargeFixture(t, false)
	ctx := t.Context()
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := f.warmup.FailWarmupSend(ctx, f.sender, f.task, time.Now(), "Send failed", "Provider refused"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	var sent int
	var refunded time.Time
	if err := f.pool.QueryRow(ctx, `SELECT ws.emails_sent,w.warmup_refunded_at FROM warmup_tasks w JOIN tasks t ON t.id=w.task_id JOIN warmup_statistics ws ON ws.email_account_id=t.email_account_id AND ws.date=w.warmup_charged_date WHERE w.task_id=$1`, f.task).Scan(&sent, &refunded); err != nil || sent != 0 {
		t.Fatalf("concurrent refund sent=%d error=%v", sent, err)
	}
	if err := f.warmup.IncrementDailyCount(ctx, f.sender, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateTaskStatus(ctx, f.task, "completed"); err != nil {
		t.Fatal(err)
	}
	if err := f.warmup.FailWarmupSend(ctx, f.sender, f.task, time.Now(), "Send failed", "Duplicate"); err != nil {
		t.Fatal(err)
	}
	var again time.Time
	if err := f.pool.QueryRow(ctx, `SELECT ws.emails_sent,w.warmup_refunded_at FROM warmup_tasks w JOIN tasks t ON t.id=w.task_id JOIN warmup_statistics ws ON ws.email_account_id=t.email_account_id AND ws.date=w.warmup_charged_date WHERE w.task_id=$1`, f.task).Scan(&sent, &again); err != nil || sent != 1 || !again.Equal(refunded) {
		t.Fatalf("restamping refunded another task: sent=%d refunded=%v again=%v error=%v", sent, refunded, again, err)
	}
}

func TestLiveWarmupRefundAndResultRollbackTogether(t *testing.T) {
	f, r, _, _ := newWarmupChargeFixture(t, false)
	ctx := t.Context()
	want := errors.New("later result write failed")
	result := models.SendEmailResult{TaskID: f.task, Error: &models.EmailSendError{Failure: &errx.SendFailure{Disposition: errx.SendRetry}}}
	err := r.ApplySendResult(ctx, result, func(ctx context.Context) error {
		if err := f.warmup.FailWarmupSend(ctx, f.sender, f.task, time.Now(), "Send failed", "Provider refused"); err != nil {
			return err
		}
		return want
	})
	if !errors.Is(err, want) {
		t.Fatal(err)
	}
	var status, state string
	var refunded, applied bool
	var sent int
	if err = f.pool.QueryRow(ctx, `SELECT t.status,t.send_result_state,t.send_result_applied_at IS NOT NULL,w.warmup_refunded_at IS NOT NULL,ws.emails_sent FROM tasks t JOIN warmup_tasks w ON w.task_id=t.id JOIN warmup_statistics ws ON ws.email_account_id=t.email_account_id AND ws.date=w.warmup_charged_date WHERE t.id=$1`, f.task).Scan(&status, &state, &applied, &refunded, &sent); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || state != "unknown" || applied || refunded || sent != 1 {
		t.Fatalf("partial refund committed: status=%s state=%s applied=%v refunded=%v sent=%d", status, state, applied, refunded, sent)
	}
}

func TestLiveWarmupReplyRefundUsesChargedShapeAfterParentDeletion(t *testing.T) {
	f, _, _, _ := newWarmupChargeFixture(t, true)
	ctx := t.Context()
	if _, err := f.pool.Exec(ctx, `DELETE FROM tasks WHERE id=(SELECT parent_task_id FROM warmup_tasks WHERE task_id=$1)`, f.task); err != nil {
		t.Fatal(err)
	}
	var detached, reply bool
	if err := f.pool.QueryRow(ctx, `SELECT parent_task_id IS NULL,warmup_reply_charged FROM warmup_tasks WHERE task_id=$1`, f.task).Scan(&detached, &reply); err != nil || !detached || !reply {
		t.Fatalf("reply proof lost with parent: detached=%v reply=%v error=%v", detached, reply, err)
	}
	if err := f.warmup.FailWarmupSend(ctx, f.sender, f.task, time.Now(), "Send failed", "Provider refused"); err != nil {
		t.Fatal(err)
	}
	var sent, replies int
	if err := f.pool.QueryRow(ctx, `SELECT emails_sent,emails_replied FROM warmup_statistics WHERE email_account_id=$1`, f.sender).Scan(&sent, &replies); err != nil || sent != 0 || replies != 0 {
		t.Fatalf("incorrect reply refund: sent=%d replies=%d error=%v", sent, replies, err)
	}
}

func TestLiveWarmupChargeAndRefundUseUTCInNonUTCSession(t *testing.T) {
	f, _, worker, nonce := newWarmupChargeFixture(t, false)
	ctx := t.Context()
	config, err := pgxpool.ParseConfig(os.Getenv("WARMBLY_TEST_DB"))
	if err != nil {
		t.Fatal(err)
	}
	zone := "Etc/GMT-14"
	if time.Now().UTC().Hour() < 10 {
		zone = "Etc/GMT+12"
	}
	config.ConnConfig.RuntimeParams["timezone"] = zone
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	r := &taskRepository{db: pool}
	warmup := &warmupRepository{db: pool}
	if err = r.DeferWarmupDispatch(ctx, f.task, f.sender, worker, nonce, time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if claimed, err := r.ClaimWarmupTask(ctx, f.task, time.Now()); err != nil || !claimed {
		t.Fatalf("reclaim: claimed=%v error=%v", claimed, err)
	}
	f.token(t, "", "Pinned diagnostic", false)
	if _, err = r.AuthorizeWarmupDispatch(ctx, f.task, f.sender, worker); err != nil {
		t.Fatal(err)
	}
	var differentDay, utcCharge bool
	if err = pool.QueryRow(ctx, `SELECT CURRENT_DATE<>(NOW() AT TIME ZONE 'UTC')::date,warmup_charged_date=(NOW() AT TIME ZONE 'UTC')::date FROM warmup_tasks WHERE task_id=$1`, f.task).Scan(&differentDay, &utcCharge); err != nil || !differentDay || !utcCharge {
		t.Fatalf("charge follows session rather than UTC: differentDay=%v utcCharge=%v error=%v", differentDay, utcCharge, err)
	}
	if err = warmup.IncrementDailyCount(ctx, f.sender, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = warmup.IncrementReplyCount(ctx, f.sender, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = warmup.FailWarmupSend(ctx, f.sender, f.task, time.Now(), "Send failed", "Provider refused"); err != nil {
		t.Fatal(err)
	}
	var utcSent, utcReplies, sessionRows int
	if err = pool.QueryRow(ctx, `SELECT SUM(emails_sent) FILTER(WHERE date=(NOW() AT TIME ZONE 'UTC')::date),SUM(emails_replied) FILTER(WHERE date=(NOW() AT TIME ZONE 'UTC')::date),COUNT(*) FILTER(WHERE date=CURRENT_DATE) FROM warmup_statistics WHERE email_account_id=$1`, f.sender).Scan(&utcSent, &utcReplies, &sessionRows); err != nil || utcSent != 1 || utcReplies != 1 || sessionRows != 0 {
		t.Fatalf("non-UTC refund changed another day: utcSent=%d utcReplies=%d sessionRows=%d error=%v", utcSent, utcReplies, sessionRows, err)
	}
}

func TestLiveWarmupChargeProofRollsBackWhenCounterWriteFails(t *testing.T) {
	f, r, worker, _ := newWarmupChargeFixture(t, false)
	ctx := t.Context()
	queries := []struct {
		sql string
		arg uuid.UUID
	}{
		{`UPDATE warmup_statistics SET emails_sent=2147483647 WHERE email_account_id=$1`, f.sender},
		{`UPDATE warmup_tasks SET dispatch_nonce=NULL,dispatch_worker_id=NULL,warmup_charged_date=NULL,warmup_reply_charged=NULL,warmup_refunded_at=NULL WHERE task_id=$1`, f.task},
		{`UPDATE tasks SET status='active',completed_at=NULL,send_result_state=NULL WHERE id=$1`, f.task},
		{`UPDATE email_accounts SET send_recovery_hold=false,send_recovery_reason=NULL,send_recovery_task_id=NULL WHERE id=$1`, f.sender},
	}
	for _, q := range queries {
		if _, err := f.pool.Exec(ctx, q.sql, q.arg); err != nil {
			t.Fatal(err)
		}
	}
	_, err := r.AuthorizeWarmupDispatch(ctx, f.task, f.sender, worker)
	var sqlError *pgconn.PgError
	if !errors.As(err, &sqlError) || sqlError.Code != "22003" {
		t.Fatalf("expected counter overflow, got %v", err)
	}
	var status string
	var charged, completed, nonce, held bool
	if err = f.pool.QueryRow(ctx, `SELECT t.status,t.completed_at IS NOT NULL,w.warmup_charged_date IS NOT NULL OR w.warmup_reply_charged IS NOT NULL,w.dispatch_nonce IS NOT NULL,ea.send_recovery_hold FROM tasks t JOIN warmup_tasks w ON w.task_id=t.id JOIN email_accounts ea ON ea.id=t.email_account_id WHERE t.id=$1`, f.task).Scan(&status, &completed, &charged, &nonce, &held); err != nil {
		t.Fatal(err)
	}
	if status != "active" || completed || charged || nonce || held {
		t.Fatalf("counter failure committed charge authority: status=%s completed=%v charged=%v nonce=%v held=%v", status, completed, charged, nonce, held)
	}
}

func TestLiveWarmupCancellationPathsRefundOnlyProvenCharge(t *testing.T) {
	for _, path := range []string{"defer", "cancel", "recover"} {
		for _, proven := range []bool{false, true} {
			name := path + "/legacy"
			if proven {
				name = path + "/charged"
			}
			t.Run(name, func(t *testing.T) {
				f, r, worker, nonce := newWarmupChargeFixture(t, false)
				ctx := t.Context()
				if !proven {
					if _, err := f.pool.Exec(ctx, `UPDATE warmup_tasks SET warmup_charged_date=NULL,warmup_reply_charged=NULL WHERE task_id=$1`, f.task); err != nil {
						t.Fatal(err)
					}
				}
				var err error
				switch path {
				case "defer":
					err = r.DeferWarmupDispatch(ctx, f.task, f.sender, worker, nonce, time.Now())
				case "cancel":
					reservation := &OutboundReservation{TaskID: f.task, MailboxID: f.sender, OrganizationID: f.org, WorkerID: worker, Provider: models.InboxProviderSMTPIMAP, Recipients: []string{"pl-" + f.recipient.String()[:8] + "@test.local"}}
					var reserved uuid.UUID
					if reserved, err = r.ReserveOutbound(ctx, *reservation); err == nil {
						err = r.CancelOutbound(ctx, f.task, f.sender, worker, reserved)
					}
				case "recover":
					var retired bool
					retired, err = r.retireUnstartedWarmupDispatch(ctx, f.task, f.sender, time.Now().Add(time.Second))
					if !retired {
						t.Fatalf("unstarted task not retired: %v", err)
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				var sent int
				var refunded bool
				if err = f.pool.QueryRow(ctx, `SELECT ws.emails_sent,w.warmup_refunded_at IS NOT NULL FROM warmup_tasks w JOIN tasks t ON t.id=w.task_id JOIN warmup_statistics ws ON ws.email_account_id=t.email_account_id WHERE t.id=$1`, f.task).Scan(&sent, &refunded); err != nil {
					t.Fatal(err)
				}
				want := 1
				if proven {
					want = 0
				}
				if sent != want || refunded != proven {
					t.Fatalf("refund without proof or skipped valid charge: sent=%d refunded=%v proven=%v", sent, refunded, proven)
				}
			})
		}
	}
}
