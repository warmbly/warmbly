package msgraph

import (
	"slices"
	"strings"
	"testing"
)

func TestBuildMIME_HeadersThreadingAndBodies(t *testing.T) {
	hdrs := []hdr{
		{"From", "a@b.com"},
		{"To", "c@d.com"},
		{"Subject", "hi"},
		{"Message-ID", "<mid@x>"},
		{"In-Reply-To", "<parent@x>"},
		{"References", "<parent@x>"},
		{"X-Mailtrace-Verify", "tok"},
	}
	raw, err := buildMIME(hdrs, "plain body", "<p>html</p>", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{
		"Message-ID: <mid@x>",
		"In-Reply-To: <parent@x>",
		"References: <parent@x>",
		"X-Mailtrace-Verify: tok",
		"multipart/alternative",
		"text/html",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("MIME missing %q", want)
		}
	}
}

func TestBuildMIME_PlainOnly(t *testing.T) {
	raw, err := buildMIME([]hdr{{"From", "a@b.com"}, {"Subject", "s"}}, "just text", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "multipart") {
		t.Error("plain-only message should not be multipart")
	}
}

func TestBuildMIME_Attachment(t *testing.T) {
	raw, err := buildMIME(
		[]hdr{{"From", "a@b.com"}},
		"body", "",
		[]Attachment{{Filename: "f.txt", MimeType: "text/plain", Data: []byte("data")}},
	)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "multipart/mixed") {
		t.Error("attachment message must be multipart/mixed")
	}
	if !strings.Contains(s, `filename="f.txt"`) {
		t.Error("attachment filename missing")
	}
}

func TestToEmailData_MappingAndFlags(t *testing.T) {
	m := &graphMessage{
		ID:                "gid",
		InternetMessageID: "<mid@x>",
		ConversationID:    "conv",
		Subject:           "hello",
		IsRead:            true,
		From:              &graphRecipient{EmailAddress: graphEmailAddress{Name: "Alice", Address: "a@b.com"}},
		ToRecipients:      []graphRecipient{{EmailAddress: graphEmailAddress{Address: "c@d.com"}}},
		Body:              &graphItemBody{ContentType: "text", Content: "hello body"},
		InternetMessageHeaders: []graphHeader{
			{Name: "X-Mailtrace-Verify", Value: "tok"},
			{Name: "Auto-Submitted", Value: "auto-replied"},
		},
	}
	d := m.toEmailData()
	if d.GmailID != "gid" || d.MessageID != "<mid@x>" || d.ThreadID != "conv" {
		t.Errorf("ids wrong: %+v", d)
	}
	if !slices.Contains(d.Flags, "\\Seen") {
		t.Error("isRead should map to \\Seen")
	}
	if !slices.Contains(d.Flags, "X-Mailtrace-Verify:tok") {
		t.Errorf("warmup pseudo-flag missing: %v", d.Flags)
	}
	if d.BodyPlain != "hello body" {
		t.Errorf("text body wrong: %q", d.BodyPlain)
	}
	if len(d.From) != 1 || d.From[0] != "Alice <a@b.com>" {
		t.Errorf("from formatting: %v", d.From)
	}
}
