package worker

import (
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/app/worker/wmail"
	"github.com/warmbly/warmbly/internal/errx"
)

// recordSendAttempt + recordSendLatency + recordSendOutcome are the
// integration points the send hot path calls. They classify a
// wmail.SendResult into the right Record* shim so the rolling 1m
// counters feed the QuarantineEvaluator's band classification with the
// granularity that matters (bounce vs complaint vs auth vs rate limit).

func (s *WorkerService) recordSendAttempt() {
	s.RecordSendAttempt()
}

func (s *WorkerService) recordSendLatency(d time.Duration) {
	s.RecordSMTPLatency(int32(d.Milliseconds()))
}

func (s *WorkerService) recordSendOutcome(result *wmail.SendResult) {
	if result == nil {
		return
	}
	if result.Success {
		s.RecordSendSuccess()
		return
	}
	if result.Error == nil {
		return
	}
	switch result.Error.Code {
	case errx.MailErrorCodeAuthenticationFailed,
		errx.MailErrorCodeAuthorizationFailed,
		errx.MailErrorCodeInvalidCredentials,
		errx.MailErrorCodeGoogleAuth:
		s.RecordAuthError()
	case errx.MailErrorCodeRateLimitExceeded,
		errx.MailErrorCodeSendingTooFast,
		errx.MailErrorCodeQuotaExceeded:
		s.RecordRateLimitError()
	case errx.MailErrorCodeRecipientRejected,
		errx.MailErrorCodeAccountSuspended:
		s.RecordBounceHard()
	case errx.MailErrorCodeServerUnreachable,
		errx.MailErrorCodeConnectionLost:
		s.RecordBounceSoft()
	default:
		// Best-effort classification on free-text — keeps the signal
		// useful even when the error code is generic.
		msg := strings.ToLower(result.Error.Message)
		switch {
		case strings.Contains(msg, "bounce") || strings.Contains(msg, "rejected"):
			s.RecordBounceHard()
		case strings.Contains(msg, "rate limit") || strings.Contains(msg, "throttle"):
			s.RecordRateLimitError()
		case strings.Contains(msg, "auth"):
			s.RecordAuthError()
		default:
			s.RecordBounceSoft()
		}
	}
}
