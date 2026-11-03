package dsn

import (
	"strings"
	"testing"
)

func TestReasonCarriesStatusAndDiagnostic(t *testing.T) {
	full := `Final-Recipient: rfc822; [EMAIL]
Action: failed
Status: 5.2.2
Diagnostic-Code: smtp; 552-5.2.2 The email account that you tried to reach is
    over quota and inactive.
Remote-MTA: dns; gmail-smtp-in.l.google.com
`
	policy := "Final-Recipient: rfc822; [EMAIL]\nAction: failed\nStatus: 5.7.1\nDiagnostic-Code: smtp; 550 Service unavailable, Client host blocked using Spamhaus\n"
	for _, tc := range []struct {
		name, body, subject, want string
	}{
		{"folded diagnostic that already carries the status", full, "Delivery Status Notification (Failure)",
			"552-5.2.2 The email account that you tried to reach is over quota and inactive."},
		{"status joined to a diagnostic without one", policy, "Undeliverable",
			"5.7.1 550 Service unavailable, Client host blocked using Spamhaus"},
		{"status alone", "Action: failed\nStatus: 5.1.1\n", "Undeliverable", "5.1.1"},
		{"no status falls back to the subject", "Your message could not be delivered.", "Undeliverable: hello", "Undeliverable: hello"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Parse(tc.body).Reason(tc.subject); got != tc.want {
				t.Fatalf("Reason = %q, want %q", got, tc.want)
			}
		})
	}

	r := Parse(full)
	if r.Status != "5.2.2" || r.Action != "failed" {
		t.Fatalf("status %q action %q", r.Status, r.Action)
	}
	if !Blame(r.Reason("")).AddressIsFine() {
		t.Fatal("a full mailbox must not blame the address")
	}
}

func TestReasonIsBounded(t *testing.T) {
	long := "Status: 5.1.1\nDiagnostic-Code: smtp; 550 5.1.1 " + strings.Repeat("x", 4*ReasonLimit) + "\x07\n"
	got := Parse(long).Reason("")
	if n := len([]rune(got)); n > ReasonLimit {
		t.Fatalf("reason is %d runes, limit %d", n, ReasonLimit)
	}
	if strings.ContainsRune(got, '\x07') {
		t.Fatal("control characters must not reach the reason")
	}
}
