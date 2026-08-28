package arf

import "testing"

// A real Hotmail/SNDS-shaped report: the machine-readable part carries the
// feedback type, and the reported message's own headers follow it.
const abuseReport = `This is an email abuse report for an email message received on Tue, 3 Mar 2026.

--boundary
Content-Type: message/feedback-report

Feedback-Type: abuse
User-Agent: SomeGenerator/1.0
Version: 1
Original-Rcpt-To: <recipient@hotmail.com>
Arrival-Date: Tue, 3 Mar 2026 10:00:00 +0000
Message-ID: <report-envelope@provider.example>

--boundary
Content-Type: message/rfc822

From: sender@ourdomain.com
To: recipient@hotmail.com
Subject: Quick question
Message-ID: <the-original-send@ourdomain.com>
`

func TestDetect(t *testing.T) {
	tests := []struct {
		name                       string
		from, subject, contentType string
		want                       bool
	}{
		{"feedback-report content type", "x@y.com", "hi", "multipart/report; report-type=feedback-report", true},
		{"the inner part's type", "x@y.com", "hi", "message/feedback-report", true},
		{"a provider FBL address", "staff@hotmail.com", "complaint", "text/plain", true},
		{"an abuse mailbox", "abuse@provider.example", "", "text/plain", true},
		{"the report subject", "x@y.com", "Email Feedback Report for IP 1.2.3.4", "text/plain", true},
		{"ordinary mail", "ada@acme.com", "Re: your note", "text/plain", false},
		// A bounce is a different report type and must not be read as a
		// complaint: suppressing on it is right, scoring it as a complaint is not.
		{"a delivery report is not a complaint", "mailer-daemon@acme.com", "Undeliverable", "multipart/report; report-type=delivery-status", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Detect(tt.from, tt.subject, tt.contentType); got != tt.want {
				t.Errorf("Detect() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseAbuseReport(t *testing.T) {
	r := Parse(abuseReport)
	if !r.IsComplaint {
		t.Error("Feedback-Type: abuse did not read as a complaint")
	}
	// The LAST Message-ID is the reported message; the first is the report's
	// own envelope. Taking the first would resolve to nothing.
	if r.OriginalMessageID != "the-original-send@ourdomain.com" {
		t.Errorf("OriginalMessageID = %q, want the reported send", r.OriginalMessageID)
	}
	if r.ComplainedRecipient != "recipient@hotmail.com" {
		t.Errorf("ComplainedRecipient = %q", r.ComplainedRecipient)
	}
	if r.UserAgent != "SomeGenerator/1.0" {
		t.Errorf("UserAgent = %q", r.UserAgent)
	}
}

func TestParseIgnoresNonAbuseFeedbackTypes(t *testing.T) {
	// "not-spam" is a recipient rescuing mail from junk. Recording that as a
	// complaint would invert the signal entirely.
	for _, kind := range []string{"not-spam", "fraud", "opt-out", "virus"} {
		body := "Feedback-Type: " + kind + "\nMessage-ID: <x@y.com>\n"
		if Parse(body).IsComplaint {
			t.Errorf("Feedback-Type: %s was read as a complaint", kind)
		}
	}
}

func TestParseOnUnrelatedBodyIsInert(t *testing.T) {
	r := Parse("Hi Ada,\n\nJust following up on my last note.\n\nThanks")
	if r.IsComplaint || r.OriginalMessageID != "" || r.ComplainedRecipient != "" {
		t.Errorf("ordinary mail parsed as a complaint: %+v", r)
	}
}

// Original-Mail-From is the SENDER of the reported message. Reading it as the
// complainer would have suppressed the customer's own sending address.
func TestParseNeverReadsTheSenderAsTheComplainer(t *testing.T) {
	body := `Feedback-Type: abuse
Original-Mail-From: <sender@ourdomain.com>
Message-ID: <the-original-send@ourdomain.com>
`
	r := Parse(body)
	if !r.IsComplaint {
		t.Fatal("expected a complaint")
	}
	if r.ComplainedRecipient != "" {
		t.Errorf("ComplainedRecipient = %q, want empty; that address is the sender", r.ComplainedRecipient)
	}
}
