package jobs

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
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
