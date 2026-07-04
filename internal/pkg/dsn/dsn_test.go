package dsn

import "testing"

const permanentDSN = `This is the mail system at host mail.example.com.

I'm sorry to have to inform you that your message could not
be delivered to one or more recipients.

Final-Recipient: rfc822; nobody@invalid.example.com
Action: failed
Status: 5.1.1
Diagnostic-Code: smtp; 550 5.1.1 user unknown

--- Original message headers ---
From: sender@yourdomain.com
Message-ID: <camp-abc-123@yourdomain.com>
Subject: Quick question
`

const transientDSN = `Delivery is delayed.

Final-Recipient: rfc822; busy@example.com
Action: delayed
Status: 4.4.1
Message-ID: <camp-xyz-999@yourdomain.com>
`

func TestParsePermanent(t *testing.T) {
	r := Parse(permanentDSN)
	if !r.IsBounce || !r.Permanent {
		t.Fatalf("expected permanent bounce, got %+v", r)
	}
	if r.FailedRecipient != "nobody@invalid.example.com" {
		t.Errorf("failed recipient = %q", r.FailedRecipient)
	}
	if r.OriginalMessageID != "camp-abc-123@yourdomain.com" {
		t.Errorf("original message id = %q", r.OriginalMessageID)
	}
}

func TestParseTransientNotPermanent(t *testing.T) {
	r := Parse(transientDSN)
	if !r.IsBounce {
		t.Fatal("expected a bounce")
	}
	if r.Permanent {
		t.Error("4.x.x/delayed must not be permanent (would over-suppress)")
	}
}

func TestParseOrdinaryBody(t *testing.T) {
	r := Parse("Hi, thanks for reaching out, let's chat next week.")
	if r.IsBounce || r.Permanent {
		t.Errorf("ordinary mail misread as bounce: %+v", r)
	}
}

func TestDetect(t *testing.T) {
	if !Detect("Mail Delivery Subsystem <MAILER-DAEMON@google.com>", "", "") {
		t.Error("mailer-daemon not detected")
	}
	if !Detect("x@y.com", "Undeliverable: Quick question", "") {
		t.Error("bounce subject not detected")
	}
	if !Detect("x@y.com", "", "multipart/report; report-type=delivery-status") {
		t.Error("multipart/report not detected")
	}
	if Detect("alice@example.com", "Re: your email", "text/plain") {
		t.Error("ordinary reply misdetected as bounce")
	}
}
