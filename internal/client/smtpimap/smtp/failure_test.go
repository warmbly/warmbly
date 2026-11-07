package smtp

import (
	"io"
	"net/textproto"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/pkg/dsn"
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

// A refusal quotes the server's own reply so the bounce rules can read it; an
// AUTH reply is never quoted.
func TestSendFailureQuotesTheServerReply(t *testing.T) {
	native := &textproto.Error{Code: 550, Msg: "Mailbox unavailable\r\n\x07blocked   using zen.spamhaus.org"}
	got := sendFailure(errx.ErrMailRecipientRejected("refused"), native, "rcpt to", "recipient").Message
	if !strings.Contains(got, "server replied: 550 Mailbox unavailable blocked using zen.spamhaus.org") {
		t.Fatalf("message %q does not quote the reply", got)
	}
	if strings.ContainsRune(got, '\x07') || strings.ContainsRune(got, '\n') {
		t.Fatalf("message %q keeps control characters", got)
	}
	masked := sendFailure(errx.ErrMailRecipientRejected("refused"), &textproto.Error{Code: 550, Msg: "5.1.1 <gone@example.com>: User unknown"}, "rcpt to", "recipient").Message
	if strings.Contains(masked, "gone@example.com") || !strings.Contains(masked, "User unknown") {
		t.Fatalf("message %q keeps the address or drops the reason", masked)
	}
	if dsn.Blame(got) != dsn.CauseReputationBlock {
		t.Fatalf("Blame(%q) = %q, want reputation_block", got, dsn.Blame(got))
	}

	long := &textproto.Error{Code: 550, Msg: strings.Repeat("x", 4*replyTextLimit)}
	if n := len([]rune(sendFailure(errx.ErrMailRecipientRejected("refused"), long, "rcpt to", "recipient").Message)); n > 2*replyTextLimit {
		t.Fatalf("quoted reply is unbounded: %d runes", n)
	}

	auth := &textproto.Error{Code: 535, Msg: "5.7.8 Username and Password not accepted dXNlcjpzZWNyZXQ="}
	if msg := sendFailure(errx.ErrMailInvalidCredentials, auth, "auth", "mailbox").Message; strings.Contains(msg, "dXNlcjpzZWNyZXQ") {
		t.Fatalf("an AUTH reply reached the message: %q", msg)
	}
}
