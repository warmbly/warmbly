package emailverify

import "testing"

// The verdicts below are the whole of issue #200: a 5xx reply to RCPT TO may be
// about the recipient OR about our probe, and only the first may ever become a
// permanent "invalid" that drops the contact from every campaign.
func TestClassifyPermanentRcpt(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		msg  string
		want probeOutcome
	}{
		// The reported failure: Postfix defers its HELO restriction to RCPT.
		{"postfix defers a non-FQDN helo rejection to rcpt", 504,
			"5.5.2 <localhost>: Helo command rejected: need fully-qualified hostname", probeUnknown},
		{"helo rejected without an enhanced code", 550,
			"Helo command rejected: need fully-qualified hostname", probeUnknown},
		{"relay policy", 554, "5.7.1 <a@b.com>: Relay access denied", probeUnknown},
		{"connecting host blocked", 554,
			"5.7.1 Service unavailable; Client host [1.2.3.4] blocked using zen.spamhaus.org", probeUnknown},
		{"sender address refused", 550, "5.1.8 <verify@localhost>: Sender address rejected", probeUnknown},
		{"ip reputation", 550,
			"5.7.1 Unfortunately, messages from [1.2.3.4] weren't sent. Please contact your provider", probeUnknown},
		{"mailbox full is not a missing mailbox", 552, "5.2.2 Mailbox full", probeUnknown},
		{"bare transaction failure is not a verdict", 554, "Transaction failed", probeUnknown},
		{"command not implemented", 502, "Command not implemented", probeUnknown},
		{"spam refusal without an enhanced status", 550, "Message rejected as spam", probeUnknown},
		{"recipient rejection with a policy tail", 550, "5.7.1 Recipient address rejected: policy restriction", probeUnknown},
		{"sender domain does not exist", 550, "Sender domain does not exist", probeUnknown},

		// Genuine recipient rejections must still be caught.
		{"gmail unknown mailbox", 550,
			"5.1.1 The email account that you tried to reach does not exist", probeRejected},
		{"postfix user unknown", 550,
			"5.1.1 <a@b.com>: Recipient address rejected: User unknown in virtual mailbox table", probeRejected},
		{"office365 answers a missing mailbox under 5.4.1", 550,
			"5.4.1 Recipient address rejected: Access denied", probeRejected},
		{"mailbox disabled", 550, "5.2.1 The mailbox is disabled", probeRejected},
		{"user not local", 551, "User not local; please try forwarding", probeRejected},
		{"bare 550 mailbox unavailable", 550,
			"Requested action not taken: mailbox unavailable", probeRejected},
		{"exim unrouteable", 550, "Unrouteable address", probeRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := classifyPermanentRcpt(tc.code, tc.msg)
			if got != tc.want {
				t.Fatalf("classifyPermanentRcpt(%d, %q) = %v, want %v (reason %q)",
					tc.code, tc.msg, got, tc.want, reason)
			}
			if reason == "" {
				t.Fatal("every verdict must carry a reason for the activity log")
			}
		})
	}
}

func TestIsFQDN(t *testing.T) {
	for _, host := range []string{
		"verify.warmbly.com", "mail.example.co.uk", "a.io", "VERIFY.WARMBLY.COM", "verify.warmbly.com.",
	} {
		if !isFQDN(host) {
			t.Errorf("isFQDN(%q) = false, want true", host)
		}
	}
	// Every one of these gets a session rejected by an RFC-compliant server,
	// which is how a good address ends up marked invalid.
	for _, host := range []string{
		"", "localhost", "warmbly", "box.local", "host.localdomain", "app.internal",
		"server.lan", "1.2.3.4", "::1", "has space.com", "-bad.com", "bad-.com",
		"under_score.com", "trailing..dot.com", "x.c",
	} {
		if isFQDN(host) {
			t.Errorf("isFQDN(%q) = true, want false", host)
		}
	}
}
