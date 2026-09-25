package replyclassify

import (
	"strings"
	"testing"
)

// groupRejection is a Gmail failure notice for a Google Group that refuses the
// post: plain text, no RFC 3464 status part, and the original message's
// headers (List-Unsubscribe included) returned underneath.
const groupRejection = `Hello sender@example.com,

We're writing to let you know that the group you tried to contact (info) may not exist, or you may not have permission to post messages to the group. A few more details on why you weren't able to post:

 * You might have spelled or formatted the group name incorrectly.
 * The owner of the group may have removed this group.

Thanks,

example.org admins



----- Original message -----

From: "Sender" <sender@example.com>
To: info@example.org
Subject: Inquiry
List-Unsubscribe: <https://t.example.com/unsubscribe/abc123>
Message-ID: <a8fcccd2@example.com>
List-Unsubscribe-Post: List-Unsubscribe=One-Click
`

// flatten is what bodies stored before line breaks were kept look like.
func flatten(s string) string { return strings.Join(strings.Fields(s), " ") }

// A failure notice carries Auto-Submitted: auto-replied like a vacation
// responder does. Read in that order it held the lead as out of office; it is
// a machine report and must say so.
func TestBounceIsNeverOutOfOffice(t *testing.T) {
	got := Classify(Input{
		Headers: map[string][]string{
			"From":                {"Mail Delivery Subsystem <mailer-daemon@googlemail.com>"},
			"Auto-Submitted":      {"auto-replied"},
			"X-Failed-Recipients": {"info@example.org"},
		},
		Subject:  "Delivery Status Notification (Failure)",
		BodyText: groupRejection,
	})
	if got.Class != ClassAutoReply {
		t.Fatalf("class = %q, want %q", got.Class, ClassAutoReply)
	}

	for _, subject := range []string{"Undeliverable: Inquiry", "Mail delivery failed: returning message to sender", "Undelivered Mail Returned to Sender"} {
		if got := Classify(Input{Subject: subject, Headers: map[string][]string{"Auto-Submitted": {"auto-replied"}}}); got.Class != ClassAutoReply {
			t.Errorf("%q: class = %q, want %q", subject, got.Class, ClassAutoReply)
		}
	}
}

// A real vacation responder keeps its class: the bounce check must not swallow
// every Auto-Submitted message.
func TestVacationResponderStaysOutOfOffice(t *testing.T) {
	got := Classify(Input{
		Headers:  map[string][]string{"From": {"Jane <jane@example.org>"}, "Auto-Submitted": {"auto-replied"}, "Return-Path": {"<>"}},
		Subject:  "Re: Inquiry",
		BodyText: "I am away until Monday.",
	})
	if got.Class != ClassOutOfOffice {
		t.Fatalf("class = %q, want %q", got.Class, ClassOutOfOffice)
	}
}

// The quoted history is not the reply, whether the body kept its line breaks
// or was stored on one line, and whether the attribution wrapped.
func TestOptOutIgnoresQuotedHistoryInEveryShape(t *testing.T) {
	quotedFooter := "Sounds good, talk Monday.\n\nOn Tue, Sep 22, 2026 at 10:00 AM Sender <sender@example.com> wrote:\n> Quick question...\n> Reply STOP or unsubscribe here: https://t.example.com/unsubscribe/abc\n"
	wrapped := "Sounds good, talk Monday.\n\nOn Tue, Sep 22, 2026 at 10:00 AM Sender <\nsender@example.com> wrote:\n> Please remove me? Unsubscribe: https://t.example.com/u/1\n"
	outlook := "Thanks, will read it.\n\n________________________________\nFrom: Sender <sender@example.com>\nSent: Tuesday\nSubject: Inquiry\n\nunsubscribe"
	cases := map[string]string{
		"quoted footer":           quotedFooter,
		"quoted footer, one line": flatten(quotedFooter),
		"wrapped attribution":     wrapped,
		"outlook, one line":       flatten(outlook),
		"failure notice":          groupRejection,
		"failure notice, 1 line":  flatten(groupRejection),
	}
	for name, body := range cases {
		if IsOptOut("Re: Inquiry", body) {
			t.Errorf("%s: read as an opt-out", name)
		}
	}

	// What the person wrote above the quote still counts.
	if !IsOptOut("Re: Inquiry", flatten("Please remove me from your list.\n\nOn Tue, Sep 22, 2026 Sender <s@example.com> wrote:\n> hi")) {
		t.Error("an opt-out above a one-line quote was missed")
	}
}

func TestIsBulkMail(t *testing.T) {
	if !IsBulkMail(map[string][]string{"List-Unsubscribe": {"<https://news.example.com/u>"}}) {
		t.Error("List-Unsubscribe not read as list mail")
	}
	if !IsBulkMail(map[string][]string{"list-id": {"<updates.example.com>"}}) {
		t.Error("List-Id not read as list mail")
	}
	if IsBulkMail(map[string][]string{"From": {"jane@example.org"}}) {
		t.Error("a person's reply read as list mail")
	}
}

func TestFlagHeaders(t *testing.T) {
	h := FlagHeaders([]string{"\\Seen", "Auto-Submitted:auto-replied", "List-Unsubscribe:<https://example.org/u?a=b>", "Junk"})
	if got := h["Auto-Submitted"]; len(got) != 1 || got[0] != "auto-replied" {
		t.Errorf("Auto-Submitted = %v", got)
	}
	if got := h["List-Unsubscribe"]; len(got) != 1 || got[0] != "<https://example.org/u?a=b>" {
		t.Errorf("a value with a colon was cut: %v", got)
	}
	if len(h) != 2 {
		t.Errorf("system and bare flags leaked into headers: %v", h)
	}
}

// An attribution carries a date, a time or an address; prose that happens to
// say "on" and "wrote:" is the reply and is read whole.
func TestStripQuotedKeepsProseThatMentionsWriting(t *testing.T) {
	body := "I'm not on the right team for this, our CTO wrote: please take me off your list."
	if got := StripQuoted(body); got != body {
		t.Fatalf("StripQuoted cut prose to %q", got)
	}
	if !IsOptOut("Re: Hi", body) {
		t.Error("an opt-out after prose mentioning writing was missed")
	}
}

// German Outlook quotes the original under a "Von: / Gesendet:" block or an
// "Ursprüngliche Nachricht" separator; both carry our own footer. Read once a
// workspace says its mail is German.
func TestStripQuotedGermanOutlook(t *testing.T) {
	cases := []string{
		"Danke, passt.\n\nVon: Max Muster <max@example.de>\nGesendet: Montag, 3. März 2025 10:12\nAn: Anna\nBetreff: Zusammenarbeit\n\nAntworten Sie mit STOP, um sich abzumelden. Reply stop to unsubscribe.",
		"Danke, passt.\n\n-----Ursprüngliche Nachricht-----\nVon: Max\nReply stop to unsubscribe.",
		"Danke, passt. Von: Max Muster <max@example.de> Datum: 3. März 2025 An: Anna Betreff: Zusammenarbeit Reply stop to unsubscribe.",
		"Danke, passt.\n\n---------- Weitergeleitete Nachricht ---------\nVon: Max",
	}
	for _, body := range cases {
		if got := StripQuoted(body, "de"); got != "Danke, passt." {
			t.Errorf("StripQuoted(%q) = %q", body, got)
		}
		if IsOptOut("AW: Zusammenarbeit", StripQuoted(body, "de")) {
			t.Errorf("the quoted footer opted out: %q", body)
		}
		if StripQuoted(body) == "Danke, passt." {
			t.Errorf("read without German: %q", body)
		}
	}
	prose := "Das Angebot kam von: unserem Partner, gesendet hat er es nie."
	if got := StripQuoted(prose, "de"); got != prose {
		t.Errorf("prose cut to %q", got)
	}
}
