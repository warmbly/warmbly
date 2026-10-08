package tasks

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type warmupRecoveryRepository interface {
	repository.OutboundAdmissionRepository
	repository.WarmupDispatchRepository
	RecoverUnstartedWarmupDispatches(context.Context, time.Time, int) (int, error)
}

type warmupRecoveryFixture struct {
	*partnerRoutingFixture
	r            warmupRecoveryRepository
	task, worker uuid.UUID
	nonce        uuid.UUID
	before       time.Time
}

func newWarmupRecoveryFixture(t *testing.T) *warmupRecoveryFixture {
	t.Helper()
	f := &warmupRecoveryFixture{
		partnerRoutingFixture: newPartnerRoutingFixture(t),
		task:                  uuid.New(), worker: uuid.New(), nonce: uuid.New(),
		before: time.Now().Add(-10 * time.Minute),
	}
	f.r = repository.NewTaskRepository(f.pool).(warmupRecoveryRepository)
	f.exec(t, `INSERT INTO fleet_nodes(id,role,active,last_seen_at,warmup_send_protocol) VALUES($1,'worker',true,NOW(),2)`, f.worker)
	f.exec(t, `INSERT INTO workers(id) VALUES($1)`, f.worker)
	f.exec(t, `UPDATE email_accounts SET worker_id=$2,warmup=NOW()-INTERVAL '30 days',warmup_pool_type='free',send_recovery_hold=true,send_recovery_reason='unknown',send_recovery_task_id=$3 WHERE id=$1`, f.sender.ID, f.worker, f.task)
	f.exec(t, `INSERT INTO tasks(id,email_account_id,task_type,status,message_id,completed_at,send_reserved_at,send_result_state,send_executor_nonce,send_executor_worker,send_recipients)
		VALUES($1,$2,'warmup','completed','',NOW()-INTERVAL '1 hour',NOW()-INTERVAL '1 hour','unknown',$3,$4,ARRAY[$5])`, f.task, f.sender.ID, f.nonce, f.worker, "pick-"+f.atWorkspace.String()[:8]+"@acme.test")
	f.exec(t, `INSERT INTO warmup_tasks(task_id,lineage_version,dispatch_nonce,dispatch_worker_id) VALUES($1,1,$2,$3)`, f.task, f.nonce, f.worker)
	f.exec(t, `INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id) VALUES($1,$2,$3,$4)`, uuid.New(), f.task, f.sender.ID, f.atWorkspace)
	f.exec(t, `INSERT INTO warmup_statistics(email_account_id,date,emails_sent,emails_replied,target_volume) VALUES($1,CURRENT_DATE,1,0,8)`, f.sender.ID)
	t.Cleanup(func() {
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM warmup_received WHERE sender_account_id=$1`, f.sender.ID)
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM fleet_nodes WHERE id=$1`, f.worker)
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM workers WHERE id=$1`, f.worker)
	})
	return f
}

func (f *warmupRecoveryFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(t.Context(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func (f *warmupRecoveryFixture) checkRetired(t *testing.T) {
	t.Helper()
	var status, state string
	var released, applied, held bool
	var sent, tokens int
	if err := f.pool.QueryRow(t.Context(), `SELECT t.status,t.send_result_state,t.send_released_at IS NOT NULL,t.send_result_applied_at IS NOT NULL,ea.send_recovery_hold,
		(SELECT emails_sent FROM warmup_statistics WHERE email_account_id=ea.id AND date=CURRENT_DATE),(SELECT count(*) FROM warmup_tokens WHERE task_id=t.id)
		FROM tasks t JOIN email_accounts ea ON ea.id=t.email_account_id WHERE t.id=$1`, f.task).Scan(&status, &state, &released, &applied, &held, &sent, &tokens); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" || state != "failed" || !released || !applied || held || sent != 0 || tokens != 0 {
		t.Fatalf("status=%s state=%s released=%t applied=%t held=%t sent=%d tokens=%d", status, state, released, applied, held, sent, tokens)
	}
	for _, inspect := range []func(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*repository.WarmupDispatchState, error){f.r.InspectOutbound, f.r.InspectWarmupDispatch} {
		state, err := inspect(t.Context(), f.task, f.sender.ID, f.worker)
		if err != nil || state.State != "denied" {
			t.Fatalf("retired command became sendable: state=%+v err=%v", state, err)
		}
	}
	begin, err := f.r.BeginOutbound(t.Context(), f.task, f.sender.ID, f.worker, f.nonce)
	if !errors.Is(err, pgx.ErrNoRows) && (err != nil || begin == nil || begin.State != "denied") {
		t.Fatalf("late command executed: state=%+v err=%v", begin, err)
	}
}

func TestLiveWarmupRecoveryRetiresExistingUnstartedDispatchOnce(t *testing.T) {
	f := newWarmupRecoveryFixture(t)
	pending := uuid.New()
	f.exec(t, `INSERT INTO tasks(id,email_account_id,task_type,status,message_id,scheduled_at) VALUES($1,$2,'warmup','pending','',NOW()+INTERVAL '1 hour')`, pending, f.sender.ID)
	f.exec(t, `INSERT INTO warmup_tasks(task_id) VALUES($1)`, pending)
	var wg sync.WaitGroup
	var mu sync.Mutex
	total := 0
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := f.r.RecoverUnstartedWarmupDispatches(t.Context(), f.before, 100)
			if err != nil {
				t.Error(err)
			}
			mu.Lock()
			total += n
			mu.Unlock()
		}()
	}
	wg.Wait()
	if total != 1 {
		t.Fatalf("concurrent reconcilers retired %d commands", total)
	}
	f.checkRetired(t)
	var pendingCount int
	if err := f.pool.QueryRow(t.Context(), `SELECT count(*) FROM tasks WHERE id=$1 AND status='pending'`, pending).Scan(&pendingCount); err != nil || pendingCount != 1 {
		t.Fatalf("existing next task lost: count=%d err=%v", pendingCount, err)
	}
	called := false
	if err := f.r.(repository.SendResultRecovery).ApplySendResult(t.Context(), models.SendEmailResult{TaskID: f.task, Error: &models.EmailSendError{Code: "UNKNOWN"}}, func(context.Context) error { called = true; return nil }); err != nil || called {
		t.Fatalf("late preparation failure reapplied: called=%t err=%v", called, err)
	}
	f.checkRetired(t)
}

func TestLiveWarmupRecoveryHandlesPartialReservations(t *testing.T) {
	for _, tc := range []struct{ name, update string }{
		{"only dispatch nonce", `UPDATE tasks SET send_executor_nonce=NULL,send_executor_worker=NULL WHERE id=$1`},
		{"only executor nonce", `UPDATE warmup_tasks SET dispatch_nonce=NULL,dispatch_worker_id=NULL WHERE task_id=$1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newWarmupRecoveryFixture(t)
			f.exec(t, tc.update, f.task)
			n, err := f.r.RecoverUnstartedWarmupDispatches(t.Context(), f.before, 100)
			if err != nil || n != 1 {
				t.Fatalf("partial reservation recovery: n=%d err=%v", n, err)
			}
			f.checkRetired(t)
		})
	}
}

func TestLiveWarmupRecoveryPreservesExecutionAndReceiptEvidence(t *testing.T) {
	for _, tc := range []struct{ name, update string }{
		{"outbound started", `UPDATE tasks SET send_executor_started_at=NOW() WHERE id=$1`},
		{"warmup started", `UPDATE warmup_tasks SET dispatch_started_at=NOW() WHERE task_id=$1`},
		{"outbound result", `UPDATE tasks SET send_executor_result='{"success":true}' WHERE id=$1`},
		{"warmup result", `UPDATE warmup_tasks SET dispatch_result='{"success":true}' WHERE task_id=$1`},
		{"confirmed send", `UPDATE warmup_tokens SET sent_message_id='<confirmed@provider.test>' WHERE task_id=$1`},
		{"unknown provider evidence", `UPDATE tasks SET send_result_evidence='{"disposition":"unknown"}' WHERE id=$1`},
		{"applied result", `UPDATE tasks SET send_result_applied_at=NOW() WHERE id=$1`},
		{"recent reservation", `UPDATE tasks SET send_reserved_at=NOW() WHERE id=$1`},
		{"legacy ungated command", `WITH ungated AS(UPDATE tasks SET send_executor_nonce=NULL WHERE id=$1) UPDATE warmup_tasks SET dispatch_nonce=NULL WHERE task_id=$1`},
		{"outbound attempt", `INSERT INTO outbound_attempts(nonce,task_id,email_account_id,provider,attempted_at,recipient_count) SELECT send_executor_nonce,id,email_account_id,'smtp_imap',NOW(),1 FROM tasks WHERE id=$1`},
		{"confirmed receipt", `INSERT INTO warmup_received(email_account_id,internal_id,sender_account_id,message_id,task_id) SELECT recipient_account_id,gen_random_uuid(),sender_account_id,'<confirmed@provider.test>',task_id FROM warmup_tokens WHERE task_id=$1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newWarmupRecoveryFixture(t)
			f.exec(t, tc.update, f.task)
			n, err := f.r.RecoverUnstartedWarmupDispatches(t.Context(), f.before, 100)
			if err != nil || n != 0 {
				t.Fatalf("unsafe recovery: n=%d err=%v", n, err)
			}
			var held bool
			var sent int
			if err := f.pool.QueryRow(t.Context(), `SELECT send_recovery_hold,(SELECT emails_sent FROM warmup_statistics WHERE email_account_id=$1 AND date=CURRENT_DATE) FROM email_accounts WHERE id=$1`, f.sender.ID).Scan(&held, &sent); err != nil || !held || sent != 1 {
				t.Fatalf("held=%t sent=%d err=%v", held, sent, err)
			}
		})
	}
}

func TestLiveWarmupRecoveryKeepsOtherUnknownAndRestrictiveHolds(t *testing.T) {
	for _, reason := range []string{"unknown", "conflict", "authentication", "permanent"} {
		t.Run(reason, func(t *testing.T) {
			f := newWarmupRecoveryFixture(t)
			other := uuid.New()
			f.exec(t, `INSERT INTO tasks(id,email_account_id,task_type,status,message_id,send_result_state) VALUES($1,$2,'warmup','completed','','unknown')`, other, f.sender.ID)
			f.exec(t, `UPDATE email_accounts SET send_recovery_reason=$2 WHERE id=$1`, f.sender.ID, reason)
			n, err := f.r.RecoverUnstartedWarmupDispatches(t.Context(), f.before, 100)
			if err != nil || n != 1 {
				t.Fatalf("n=%d err=%v", n, err)
			}
			var held bool
			var gotReason string
			var heldTask uuid.UUID
			if err := f.pool.QueryRow(t.Context(), `SELECT send_recovery_hold,send_recovery_reason,send_recovery_task_id FROM email_accounts WHERE id=$1`, f.sender.ID).Scan(&held, &gotReason, &heldTask); err != nil {
				t.Fatal(err)
			}
			if !held || gotReason != reason || reason == "unknown" && heldTask != other || reason != "unknown" && heldTask != f.task {
				t.Fatalf("held=%t reason=%s task=%s", held, gotReason, heldTask)
			}
		})
	}
}

func TestLiveWarmupRecoveryAndBeginCannotBothWin(t *testing.T) {
	for i := range 20 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			f := newWarmupRecoveryFixture(t)
			var wg sync.WaitGroup
			var recovered int
			var began *repository.WarmupDispatchState
			var recoveryErr, beginErr error
			wg.Add(2)
			go func() {
				defer wg.Done()
				recovered, recoveryErr = f.r.RecoverUnstartedWarmupDispatches(t.Context(), f.before, 100)
			}()
			go func() {
				defer wg.Done()
				began, beginErr = f.r.BeginOutbound(t.Context(), f.task, f.sender.ID, f.worker, f.nonce)
			}()
			wg.Wait()
			if recoveryErr != nil || beginErr != nil && !errors.Is(beginErr, pgx.ErrNoRows) {
				t.Fatalf("recovery=%v begin=%v state=%+v", recoveryErr, beginErr, began)
			}
			if recovered == 1 {
				if beginErr == nil && (began == nil || began.State != "denied") {
					t.Fatalf("retired dispatch executed: %+v", began)
				}
			} else if began == nil || began.State != "execute" {
				t.Fatalf("neither recovery nor begin won: recovered=%d began=%+v", recovered, began)
			}
		})
	}
}
