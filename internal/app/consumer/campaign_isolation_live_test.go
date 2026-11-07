package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveCampaignIsolationLateResultUsesNormalEffects(t *testing.T) {
	for _, success := range []bool{true, false} {
		t.Run(map[bool]string{true: "sent", false: "failed"}[success], func(t *testing.T) {
			handle := liveDB(t)
			s := liveJobsService(handle)
			f := newSendResultFixture(t, handle)
			task := f.stampSend(t, s)
			ctx := t.Context()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := handle.Pool.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec(`UPDATE tasks SET send_reserved_at=NOW()-INTERVAL '1 hour',send_executor_started_at=NOW()-INTERVAL '1 hour',send_executor_nonce=$2,send_result_state='unknown' WHERE id=$1`, task, uuid.New())
			exec(`UPDATE email_accounts SET send_recovery_hold=true,send_recovery_reason='unknown',send_recovery_task_id=$2 WHERE id=$1`, f.mailbox, task)
			n, err := s.TaskRepo.(interface {
				IsolateUnknownCampaignSends(context.Context, time.Time, int) (int, error)
			}).IsolateUnknownCampaignSends(ctx, time.Now().Add(-10*time.Minute), 100)
			if err != nil || n != 1 {
				t.Fatalf("isolation: %d %v", n, err)
			}
			result := models.SendEmailResult{TaskID: task, Success: success, MessageID: "<late@example.test>", SentAt: time.Now()}
			if !success {
				result.Error = &models.EmailSendError{Code: "UNSUPPORTED", Failure: &errx.SendFailure{Disposition: errx.SendRetry}}
			}
			for range 2 {
				if success {
					err = s.HandleEmailSent(ctx, result)
				} else {
					err = s.HandleEmailFailed(ctx, result)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			var pending, sent, fenced, applied bool
			var attempts, daily int
			if err := handle.Pool.QueryRow(ctx, `SELECT cl.send_outcome_pending_at IS NOT NULL,p.sent_at IS NOT NULL,p.send_attempts,
				t.status='cancelled' AND t.campaign_send_isolated_at IS NOT NULL,t.send_result_applied_at IS NOT NULL,
				COALESCE((SELECT SUM(emails_sent)::int FROM campaign_daily_sends WHERE campaign_id=p.campaign_id),0)
				FROM campaign_contact_progress p JOIN tasks t ON t.id=$1
				JOIN campaign_leads cl ON cl.campaign_id=p.campaign_id AND cl.contact_id=p.contact_id
				WHERE p.campaign_id=$2 AND p.contact_id=$3`, task, f.campaign, f.contact).Scan(&pending, &sent, &attempts, &fenced, &applied, &daily); err != nil {
				t.Fatal(err)
			}
			if pending || !fenced || !applied || sent != success || attempts != map[bool]int{true: 0, false: 1}[success] || daily != map[bool]int{true: 1, false: 0}[success] {
				t.Fatalf("late result: pending=%v fenced=%v applied=%v sent=%v attempts=%d daily=%d", pending, fenced, applied, sent, attempts, daily)
			}
			attributed, err := s.CampaignProgressRepo.CampaignContactSentFromAccount(ctx, f.campaign, f.contact, f.mailbox)
			if err != nil || attributed != success {
				t.Fatalf("reply sender attribution=%v, want %v: %v", attributed, success, err)
			}
		})
	}
}
