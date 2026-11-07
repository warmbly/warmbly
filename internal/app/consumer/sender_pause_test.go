package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestSenderLimitsSpareTheLead(t *testing.T) {
	now := time.Now()
	reset := now.Add(8 * time.Hour)
	for _, tc := range []struct {
		name   string
		code   string
		f      *errx.SendFailure
		spared bool
	}{
		{"gmail smtp daily limit", string(errx.MailErrorCodeSendRejected), &errx.SendFailure{Protocol: "smtp", Status: 550, EnhancedStatus: "5.4.5", Stage: "mail from", Scope: "mailbox", Disposition: errx.SendPermanent, ObservedAt: now}, true},
		{"daily limit from a release that called it a recipient refusal", string(errx.MailErrorCodeRecipientRejected), &errx.SendFailure{Protocol: "smtp", Status: 550, EnhancedStatus: "5.4.5", Stage: "rcpt to", Scope: "recipient", Disposition: errx.SendPermanent, ObservedAt: now}, true},
		{"zoho unusual activity", string(errx.MailErrorCodeSendRejected), &errx.SendFailure{Protocol: "smtp", Status: 550, EnhancedStatus: "5.4.6", Stage: "mail from", Scope: "mailbox", Disposition: errx.SendPermanent, ObservedAt: now}, true},
		{"gmail api quota", string(errx.MailErrorCodeSendingTooFast), &errx.SendFailure{Provider: "google", Protocol: "http", Status: 429, Cause: "rateLimitExceeded", Scope: "mailbox", Disposition: errx.SendThrottle, ObservedAt: now, RetryAt: &reset}, true},
		{"graph throttle", string(errx.MailErrorCodeSendingTooFast), &errx.SendFailure{Provider: "microsoft", Protocol: "http", Status: 429, Scope: "mailbox", Disposition: errx.SendThrottle, ObservedAt: now}, true},
		{"legacy throttle without evidence", string(errx.MailErrorCodeSendingTooFast), nil, true},
		{"smtp throttle after the recipients", string(errx.MailErrorCodeServerUnreachable), &errx.SendFailure{Protocol: "smtp", Status: 451, EnhancedStatus: "4.7.0", Stage: "message accept", Scope: "mailbox", Disposition: errx.SendRetry, ObservedAt: now}, false},
		{"unknown recipient", string(errx.MailErrorCodeRecipientRejected), &errx.SendFailure{Protocol: "smtp", Status: 550, EnhancedStatus: "5.1.1", Stage: "rcpt to", Scope: "recipient", Disposition: errx.SendPermanent, ObservedAt: now}, false},
		{"greylisting", string(errx.MailErrorCodeServerUnreachable), &errx.SendFailure{Protocol: "smtp", Status: 450, EnhancedStatus: "4.2.0", Stage: "rcpt to", Scope: "recipient", Disposition: errx.SendRetry, ObservedAt: now}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, code := sendFailureReason(models.SendEmailResult{Error: &models.EmailSendError{Code: tc.code, Message: "refused", Failure: tc.f}})
			if (code == models.SenderPausedCode) != tc.spared {
				t.Fatalf("code=%s, spared=%t", code, tc.spared)
			}
		})
	}
}

func TestLiveCampaignDailyLimitChargesNoAttempt(t *testing.T) {
	s, handle := liveWarmupService(t)
	f := newSendResultFixture(t, handle)
	s.TaskRepo = repository.NewTaskRepository(handle.Pool)
	s.CampaignRepo = repository.NewCampaignRepostory(handle)
	s.CampaignProgressRepo = repository.NewCampaignProgressRepository(handle.Pool)
	s.CampaignLogRepo = repository.NewCampaignLogRepository(handle)
	s.ContactRepo = repository.NewContactRepostory(handle)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		task := f.stampSend(t, s)
		result := models.SendEmailResult{TaskID: task, SentAt: time.Now(), Error: &models.EmailSendError{
			Code: string(errx.MailErrorCodeSendRejected), Message: "The SMTP server refused the sender (SMTP mail from status 550); enhanced status 5.4.5",
			Failure: &errx.SendFailure{Provider: "smtp", Protocol: "smtp", Status: 550, EnhancedStatus: "5.4.5", Cause: errx.SendCauseSendingLimit, Stage: "mail from", Scope: "mailbox", Disposition: errx.SendPermanent, ObservedAt: time.Now()},
		}}
		if err := s.HandleEmailFailed(ctx, result); err != nil {
			t.Fatal(err)
		}
		var attempts int
		var dispatched *time.Time
		if err := handle.QueryRow(ctx, `SELECT send_attempts,dispatched_at FROM campaign_contact_progress WHERE campaign_id=$1 AND contact_id=$2 AND sequence_id=$3`, f.campaign, f.contact, f.step).Scan(&attempts, &dispatched); err != nil {
			t.Fatal(err)
		}
		if attempts != 0 || dispatched != nil {
			t.Fatalf("a daily sending limit cost the lead: attempts=%d dispatched=%v", attempts, dispatched)
		}
		var hold bool
		var reason *string
		var until *time.Time
		if err := handle.QueryRow(ctx, `SELECT ea.send_recovery_hold,ea.send_cooldown_reason,ea.send_cooldown_until FROM email_accounts ea JOIN tasks t ON t.email_account_id=ea.id WHERE t.id=$1`, task).Scan(&hold, &reason, &until); err != nil {
			t.Fatal(err)
		}
		if hold || reason == nil || *reason != string(errx.SendPauseDailyLimit) || until == nil || time.Until(*until) < 20*time.Hour {
			t.Fatalf("daily limit left hold=%t reason=%v until=%v", hold, reason, until)
		}
		// The next attempt is a fresh day for the fixture.
		if _, err := handle.Exec(ctx, `UPDATE email_accounts SET send_cooldown_until=NULL FROM tasks t WHERE t.id=$1 AND t.email_account_id=email_accounts.id`, task); err != nil {
			t.Fatal(err)
		}
	}
}
