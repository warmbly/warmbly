package jobs

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestLiveWarmupGenericCompletionDoesNotProveChargedAccounting(t *testing.T) {
	for _, status := range []string{"completed", "dead_lettered"} {
		t.Run(status, func(t *testing.T) {
			s, h := liveWarmupService(t)
			f := newWarmupFixture(t, h)
			if err := s.WarmupRepo.IncrementDailyCount(t.Context(), f.sender, time.Now()); err != nil {
				t.Fatal(err)
			}
			if _, err := h.Exec(t.Context(), `UPDATE tasks SET status='active',completed_at=NULL,send_reserved_at=NOW(),send_executor_nonce=$2,send_result_state='unknown' WHERE id=$1`, f.task, uuid.New()); err != nil {
				t.Fatal(err)
			}
			if _, err := h.Exec(t.Context(), `INSERT INTO warmup_tasks(task_id,dispatch_nonce) VALUES($1,$2)`, f.task, uuid.New()); err != nil {
				t.Fatal(err)
			}
			if err := s.TaskRepo.UpdateTaskStatus(t.Context(), f.task, "completed"); err != nil {
				t.Fatal(err)
			}
			if status == "dead_lettered" {
				if err := s.TaskRepo.UpdateTaskStatus(t.Context(), f.task, status); err != nil {
					t.Fatal(err)
				}
			}
			result := models.SendEmailResult{TaskID: f.task, Error: &models.EmailSendError{Failure: &errx.SendFailure{Disposition: errx.SendRetry}}, SentAt: time.Now()}
			if err := s.HandleEmailFailed(t.Context(), result); err != nil {
				t.Fatal(err)
			}
			var sent int
			var settled, refunded bool
			if err := h.QueryRow(t.Context(), `SELECT ws.emails_sent,t.send_result_applied_at IS NOT NULL,w.warmup_refunded_at IS NOT NULL FROM tasks t JOIN warmup_tasks w ON w.task_id=t.id JOIN warmup_statistics ws ON ws.email_account_id=t.email_account_id AND ws.date=CURRENT_DATE WHERE t.id=$1`, f.task).Scan(&sent, &settled, &refunded); err != nil {
				t.Fatal(err)
			}
			if sent != 1 || !settled || refunded {
				t.Fatalf("unproven historical accounting: sent=%d settled=%v refunded=%v; want 1/true/false", sent, settled, refunded)
			}
		})
	}
}

func TestLiveAppliedWarmupFailureRepairsOnlyRefundedChargeDeadLetters(t *testing.T) {
	for _, source := range []string{"event", "recorded", "rollback"} {
		for _, proof := range []string{"refunded", "unrefunded", "legacy", "ambiguous"} {
			t.Run(source+"/"+proof, func(t *testing.T) {
				s, h := liveWarmupService(t)
				f := newWarmupFixture(t, h)
				exec := func(sql string, args ...any) {
					t.Helper()
					if _, err := h.Exec(t.Context(), sql, args...); err != nil {
						t.Fatal(err)
					}
				}
				for range 3 {
					if err := s.WarmupRepo.IncrementDailyCount(t.Context(), f.sender, time.Now()); err != nil {
						t.Fatal(err)
					}
				}
				if proof != "legacy" {
					exec(`INSERT INTO warmup_tasks(task_id,warmup_charged_date,warmup_reply_charged) VALUES($1,(NOW() AT TIME ZONE 'UTC')::date,false)`, f.task)
				}
				result := models.SendEmailResult{TaskID: f.task, Error: &models.EmailSendError{Failure: &errx.SendFailure{Disposition: errx.SendRetry, ObservedAt: time.Now().UTC()}}}
				if proof == "unrefunded" {
					exec(`UPDATE tasks SET status='failed',send_result_state='failed',send_result_applied_at=NOW() WHERE id=$1`, f.task)
				} else if err := s.HandleEmailFailed(t.Context(), result); err != nil {
					t.Fatal(err)
				}
				var applied time.Time
				var released, refunded *time.Time
				if err := h.QueryRow(t.Context(), `SELECT t.send_result_applied_at,t.send_released_at,w.warmup_refunded_at FROM tasks t LEFT JOIN warmup_tasks w ON w.task_id=t.id WHERE t.id=$1`, f.task).Scan(&applied, &released, &refunded); err != nil {
					t.Fatal(err)
				}
				if err := s.TaskRepo.UpdateTaskStatus(t.Context(), f.task, "dead_lettered"); err != nil {
					t.Fatal(err)
				}
				exec(`INSERT INTO task_dead_letters(task_id,task_type,payload,last_error,attempts,next_retry_at) VALUES($1,'warmup','{"task_id":"preserved"}','late dispatcher failure',2,NOW())`, f.task)
				exec(`UPDATE email_accounts SET send_recovery_hold=true,send_recovery_reason='authentication',send_recovery_task_id=$2 WHERE id=$1`, f.sender, f.task)
				if proof == "ambiguous" {
					result.Error.Failure.Disposition = errx.SendAmbiguous
				}
				raw, err := json.Marshal(result)
				if err != nil {
					t.Fatal(err)
				}
				exec(`UPDATE tasks SET send_executor_result=$2 WHERE id=$1`, f.task, raw)
				if source == "recorded" {
					reader := s.TaskRepo.(interface {
						ListUnappliedSendResults(context.Context, int) ([]models.SendEmailResult, error)
					})
					results, err := reader.ListUnappliedSendResults(t.Context(), 200)
					if err != nil {
						t.Fatal(err)
					}
					found := false
					for _, stored := range results {
						if stored.TaskID == f.task {
							found = true
							result = stored
						}
					}
					if found != (proof == "refunded") {
						t.Fatalf("recorded terminal repair selection=%v, proof=%s", found, proof)
					}
				}
				callbacks := 0
				if source == "rollback" && proof == "refunded" {
					constraint := "charge_repair_" + uuid.New().String()[:8]
					exec(`ALTER TABLE task_dead_letters ADD CONSTRAINT ` + constraint + ` CHECK (task_id<>'` + f.task.String() + `'::uuid OR status<>'resolved') NOT VALID`)
					t.Cleanup(func() {
						if _, err := h.Exec(context.Background(), `ALTER TABLE task_dead_letters DROP CONSTRAINT IF EXISTS `+constraint); err != nil {
							t.Error(err)
						}
					})
					if err := s.TaskRepo.(repository.SendResultRecovery).ApplySendResult(t.Context(), result, func(context.Context) error { callbacks++; return nil }); err == nil {
						t.Fatal("failed dead-letter write committed terminal repair")
					}
					var status, dlq string
					if err := h.QueryRow(t.Context(), `SELECT t.status,d.status FROM tasks t JOIN task_dead_letters d ON d.task_id=t.id WHERE t.id=$1`, f.task).Scan(&status, &dlq); err != nil || status != "dead_lettered" || dlq != "pending" || callbacks != 0 {
						t.Fatalf("partial terminal repair: status=%s dlq=%s callbacks=%d err=%v", status, dlq, callbacks, err)
					}
					exec(`ALTER TABLE task_dead_letters DROP CONSTRAINT ` + constraint)
				}
				for range 2 {
					if err := s.TaskRepo.(repository.SendResultRecovery).ApplySendResult(t.Context(), result, func(context.Context) error { callbacks++; return nil }); err != nil {
						t.Fatal(err)
					}
				}
				var status, dlq string
				var sent int
				var unchanged, evidence bool
				if err := h.QueryRow(t.Context(), `SELECT t.status,d.status,ws.emails_sent,
					t.send_result_state='failed' AND t.send_result_applied_at=$2 AND t.send_released_at IS NOT DISTINCT FROM $3::timestamptz AND w.warmup_refunded_at IS NOT DISTINCT FROM $4::timestamptz,
					d.payload='{"task_id":"preserved"}'::jsonb AND d.last_error='late dispatcher failure' AND d.attempts=2 AND d.replayed_at IS NULL AND e.send_recovery_hold AND e.send_recovery_reason='authentication' AND e.send_recovery_task_id=t.id
					FROM tasks t LEFT JOIN warmup_tasks w ON w.task_id=t.id JOIN task_dead_letters d ON d.task_id=t.id JOIN email_accounts e ON e.id=t.email_account_id JOIN warmup_statistics ws ON ws.email_account_id=e.id AND ws.date=(NOW() AT TIME ZONE 'UTC')::date WHERE t.id=$1`, f.task, applied, released, refunded).Scan(&status, &dlq, &sent, &unchanged, &evidence); err != nil {
					t.Fatal(err)
				}
				wantStatus, wantDLQ, wantSent := "dead_lettered", "pending", 3
				if proof == "refunded" {
					wantStatus, wantDLQ = "failed", "resolved"
				}
				if proof == "refunded" || proof == "ambiguous" {
					wantSent = 2
				}
				if status != wantStatus || dlq != wantDLQ || sent != wantSent || callbacks != 0 || !unchanged || !evidence {
					t.Fatalf("terminal repair status=%s dlq=%s sent=%d callbacks=%d unchanged=%v evidence=%v", status, dlq, sent, callbacks, unchanged, evidence)
				}
			})
		}
	}
}
