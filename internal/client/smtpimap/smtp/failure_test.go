package smtp

import (
	"io"
	"net/textproto"
	"testing"

	"github.com/warmbly/warmbly/internal/errx"
)

func TestSendFailurePreservesSMTPRefusalAndAmbiguity(t *testing.T) {
	for _, tc := range []struct {
		name, stage, scope, disposition, enhanced string
		status                                    int
		base                                      *errx.MailError
	}{
		{"temporary recipient", "rcpt to", "recipient", errx.SendRetry, "4.2.0", 450, errx.ErrMailServerUnreachable},
		{"permanent recipient", "rcpt to", "recipient", errx.SendPermanent, "5.1.1", 550, errx.ErrMailRecipientRejected("refused")},
		{"sender policy", "mail from", "mailbox", errx.SendPermanent, "5.7.1", 550, errx.ErrMailSendRejected("refused")},
		{"sender authentication", "message accept", "mailbox", errx.SendAuth, "5.7.26", 550, errx.ErrMailDomainAuthRejected},
		{"bad login", "auth", "mailbox", errx.SendAuth, "5.7.8", 535, errx.ErrMailInvalidCredentials},
		{"temporary login refusal", "auth", "mailbox", errx.SendRetry, "4.7.0", 454, errx.ErrMailInvalidCredentials},
		{"lost authentication connection", "auth", "mailbox", errx.SendRetry, "", 0, errx.ErrMailInvalidCredentials},
		{"lost final acceptance", "message accept", "mailbox", errx.SendAmbiguous, "", 0, errx.ErrMailServerUnreachable},
		{"lost body response", "message body", "mailbox", errx.SendAmbiguous, "", 0, errx.ErrMailServerUnreachable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var native error = io.EOF
			if tc.status != 0 {
				native = &textproto.Error{Code: tc.status, Msg: tc.enhanced + " private provider body user@example.test"}
			}
			normalized := sendFailure(tc.base, native, tc.stage, tc.scope)
			got := normalized.Failure
			if got.Status != tc.status || got.EnhancedStatus != tc.enhanced || got.Disposition != tc.disposition || got.Scope != tc.scope || got.Stage != tc.stage || got.Cause != "" {
				t.Fatalf("failure = %+v", got)
			}
			if tc.disposition == errx.SendRetry && normalized.Type == errx.MailErrorCritical {
				t.Fatal("temporary refusal disables authentication")
			}
		})
	}
}
