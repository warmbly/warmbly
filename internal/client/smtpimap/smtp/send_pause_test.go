package smtp

import (
	"net/textproto"
	"testing"

	"github.com/warmbly/warmbly/internal/errx"
)

// Real replies from the submission servers customers connect, and the pause each must leave on the mailbox.
func TestSendPauseReadsProviderReplies(t *testing.T) {
	for _, tc := range []struct {
		name, stage, scope string
		base               *errx.MailError
		code               int
		msg                string
		want               errx.SendPause
	}{
		{"gmail daily limit", "mail from", "mailbox", errx.ErrMailSendRejected("refused"), 550,
			"5.4.5 Daily user sending limit exceeded. For more information on Gmail sending limits go to https://support.google.com/a/answer/166852", errx.SendPauseDailyLimit},
		{"gmail relay limit", "message accept", "mailbox", errx.ErrMailSendRejected("refused"), 550,
			"5.4.5 Daily SMTP relay limit exceeded for customer", errx.SendPauseDailyLimit},
		{"gmail daily limit at rcpt", "rcpt to", "mailbox", errx.ErrMailSendRejected("refused"), 550,
			"5.4.5 Daily user sending limit exceeded.", errx.SendPauseDailyLimit},
		{"gmail temporary sending limit", "message accept", "mailbox", errx.ErrMailServerUnreachable, 421,
			"4.7.0 Temporary System Problem, sending limit reached. Try again later", errx.SendPauseDailyLimit},
		{"microsoft send as denied", "message accept", "mailbox", errx.ErrMailSendRejected("refused"), 554,
			"5.2.0 STOREDRV.Submission.Exception:SendAsDeniedException.MapiExceptionSendAsDenied; Failed to process message due to a permanent exception", errx.SendPausePermanent},
		{"microsoft outbound spam", "message accept", "mailbox", errx.ErrMailSendRejected("refused"), 554,
			"5.2.0 STOREDRV.Submission.Exception:OutboundSpamException; WASCL UserAction verdict is not None", errx.SendPauseSenderBlocked},
		{"microsoft submission quota", "message accept", "mailbox", errx.ErrMailSendRejected("refused"), 554,
			"5.2.2 mailbox full; STOREDRV.Submission.Exception:SubmissionQuotaExceededException", errx.SendPauseDailyLimit},
		{"microsoft tenant restricted", "mail from", "mailbox", errx.ErrMailSendRejected("refused"), 550,
			"5.7.708 Service unavailable. Access denied, traffic not accepted from this IP", errx.SendPauseSenderBlocked},
		{"microsoft tenant threshold", "mail from", "mailbox", errx.ErrMailSendRejected("refused"), 550,
			"5.7.705 Access denied, tenant has exceeded threshold", errx.SendPauseSenderBlocked},
		{"microsoft banned sender", "rcpt to", "mailbox", errx.ErrMailSendRejected("refused"), 550,
			"5.1.8 Access denied, bad outbound sender", errx.SendPauseSenderBlocked},
		{"microsoft rate limit", "mail from", "mailbox", errx.ErrMailServerUnreachable, 451,
			"4.7.28 Service unavailable, rate limited", errx.SendPauseRateLimited},
		{"zoho unusual activity", "mail from", "mailbox", errx.ErrMailSendRejected("refused"), 550,
			"5.4.6 Unusual sending activity detected. Please try after sometime.", errx.SendPauseSenderBlocked},
		{"generic sender policy", "mail from", "mailbox", errx.ErrMailSendRejected("refused"), 550,
			"5.7.1 Sender address rejected: not owned by user", errx.SendPausePermanent},
		{"greylisted recipient", "rcpt to", "recipient", errx.ErrMailServerUnreachable, 450,
			"4.2.0 <lead@example.org>: Recipient address rejected: Greylisted, see http://postgrey.schweikert.ch/", errx.SendPauseNone},
		{"greylisted policy", "rcpt to", "recipient", errx.ErrMailServerUnreachable, 450,
			"4.7.1 <lead@example.org>: Recipient address rejected: Try again later", errx.SendPauseNone},
		{"recipient over rate", "rcpt to", "recipient", errx.ErrMailServerUnreachable, 450,
			"4.2.1 The user you are trying to contact is receiving mail at a rate that prevents additional messages from being delivered.", errx.SendPauseNone},
		{"unknown recipient", "rcpt to", "recipient", errx.ErrMailRecipientRejected("refused"), 550,
			"5.1.1 The email account that you tried to reach does not exist.", errx.SendPauseNone},
		{"busy server", "message accept", "mailbox", errx.ErrMailServerUnreachable, 451,
			"4.3.0 Mail server temporarily rejected message.", errx.SendPauseRetry},
		{"closing connection", "rcpt to", "recipient", errx.ErrMailServerUnreachable, 421,
			"4.7.0 Try again later, closing connection.", errx.SendPauseRateLimited},
		{"unauthenticated domain", "message accept", "mailbox", errx.ErrMailDomainAuthRejected, 550,
			"5.7.26 This mail has been blocked because the sender is unauthenticated.", errx.SendPauseDomainAuth},
		{"microsoft dmarc", "message accept", "mailbox", errx.ErrMailDomainAuthRejected, 550,
			"5.7.509 Access denied, sending domain example.com does not pass DMARC verification", errx.SendPauseDomainAuth},
		{"bad password", "auth", "mailbox", errx.ErrMailInvalidCredentials, 535,
			"5.7.8 Username and Password not accepted.", errx.SendPauseAuthentication},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := sendFailure(tc.base, &textproto.Error{Code: tc.code, Msg: tc.msg}, tc.stage, tc.scope)
			if kind := errx.ClassifySendPause(string(got.Code), got.Failure); kind != tc.want {
				t.Fatalf("pause = %q, want %q (failure %+v)", kind, tc.want, got.Failure)
			}
		})
	}
}

// A queued result from a release that read no reply text still classifies on its enhanced status.
func TestSendPauseReadsResultsWithoutReplyCause(t *testing.T) {
	f := &errx.SendFailure{Provider: "smtp", Protocol: "smtp", Status: 550, EnhancedStatus: "5.4.5", Stage: "rcpt to", Scope: "recipient", Disposition: errx.SendPermanent}
	if kind := errx.ClassifySendPause(string(errx.MailErrorCodeRecipientRejected), f); kind != errx.SendPauseDailyLimit || !kind.SparesLead(f) {
		t.Fatalf("pause = %q", kind)
	}
}

func TestSenderRefusalAtRecipientIsNotABounce(t *testing.T) {
	if !senderRefusal(&textproto.Error{Code: 550, Msg: "5.4.5 Daily user sending limit exceeded."}) {
		t.Fatal("daily limit at RCPT read as a recipient refusal")
	}
	if senderRefusal(&textproto.Error{Code: 550, Msg: "5.1.1 <lead@example.org>: Recipient address rejected: User unknown"}) {
		t.Fatal("unknown recipient read as a sender refusal")
	}
}
