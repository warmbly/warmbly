package goog

import (
	"encoding/base64"
	"slices"
	"strings"
	"testing"

	"google.golang.org/api/gmail/v1"
)

func gh(n, v string) *gmail.MessagePartHeader { return &gmail.MessagePartHeader{Name: n, Value: v} }

func hasPrefix(flags []string, prefix string) bool {
	for _, f := range flags {
		if strings.HasPrefix(f, prefix) {
			return true
		}
	}
	return false
}

func TestGmailMapping_ReadStateInversionAndLabels(t *testing.T) {
	m := &gmail.Message{
		Id: "gid1", ThreadId: "t1",
		LabelIds: []string{"UNREAD", "STARRED", "SPAM", "CATEGORY_PROMOTIONS", "IMPORTANT"},
		Payload:  &gmail.MessagePart{Headers: []*gmail.MessagePartHeader{gh("Subject", "hi")}},
	}
	d := GmailMessageToEmailData(m)
	if slices.Contains(d.Flags, "\\Seen") {
		t.Error("UNREAD present must NOT map to \\Seen")
	}
	for _, want := range []string{"\\Flagged", "\\Important", "SPAM", "CATEGORY_PROMOTIONS"} {
		if !slices.Contains(d.Flags, want) {
			t.Errorf("missing flag %q in %v", want, d.Flags)
		}
	}

	// No UNREAD label => the message is read.
	seen := GmailMessageToEmailData(&gmail.Message{Id: "x", Payload: &gmail.MessagePart{}})
	if !slices.Contains(seen.Flags, "\\Seen") {
		t.Error("absence of UNREAD must map to \\Seen")
	}
}

func TestGmailMapping_SinglePartBodyUnpaddedBase64URL(t *testing.T) {
	raw := base64.RawURLEncoding.EncodeToString([]byte("Hello, world")) // unpadded, as Gmail sends
	m := &gmail.Message{
		Id:      "x",
		Payload: &gmail.MessagePart{MimeType: "text/plain", Body: &gmail.MessagePartBody{Data: raw}},
	}
	d := GmailMessageToEmailData(m)
	if d.BodyPlain != "Hello, world" {
		t.Errorf("single-part body not extracted: %q", d.BodyPlain)
	}
}

func TestGmailMapping_WarmupAndClassificationHeaders(t *testing.T) {
	m := &gmail.Message{
		Id: "x",
		Payload: &gmail.MessagePart{Headers: []*gmail.MessagePartHeader{
			gh("X-Mailtrace-Verify", "tok123"),
			gh("auto-submitted", "auto-replied"), // lowercase -> case-insensitive match
		}},
	}
	d := GmailMessageToEmailData(m)
	if !slices.Contains(d.Flags, "X-Mailtrace-Verify:tok123") {
		t.Errorf("warmup token pseudo-flag missing: %v", d.Flags)
	}
	if !hasPrefix(d.Flags, "Auto-Submitted:") {
		t.Errorf("classification header not surfaced case-insensitively: %v", d.Flags)
	}
}

func TestGmailMapping_NilPayloadSafe(t *testing.T) {
	// History stubs / deleted messages can arrive without a payload.
	d := GmailMessageToEmailData(&gmail.Message{Id: "x"})
	if d.GmailID != "x" {
		t.Errorf("nil payload should still map id, got %q", d.GmailID)
	}
}
