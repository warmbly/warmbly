package jobs

import (
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
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
