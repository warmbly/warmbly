package goog

import (
	"encoding/base64"
	"strings"
	"testing"

	"google.golang.org/api/gmail/v1"
)

func b64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

// A feedback report's machine-readable parts have to reach the body, or the ARF
// parser downstream sees the human notice and nothing else. Testing the parser
// directly could never catch that: this is the seam where it broke.
func TestExtractBodyKeepsFeedbackReportParts(t *testing.T) {
	parts := []*gmail.MessagePart{{
		MimeType: "multipart/report",
		Parts: []*gmail.MessagePart{
			{MimeType: "text/plain", Body: &gmail.MessagePartBody{Data: b64("This is an email abuse report.")}},
			{MimeType: "message/feedback-report", Body: &gmail.MessagePartBody{
				Data: b64("Feedback-Type: abuse\nUser-Agent: X/1\nOriginal-Rcpt-To: <who@hotmail.com>\n"),
			}},
			{MimeType: "message/rfc822", Body: &gmail.MessagePartBody{
				Data: b64("From: us@ourdomain.com\nMessage-ID: <the-send@ourdomain.com>\n"),
			}},
		},
	}}

	plain, _ := extractBody(parts)
	for _, want := range []string{"Feedback-Type: abuse", "the-send@ourdomain.com", "Original-Rcpt-To"} {
		if !strings.Contains(plain, want) {
			t.Errorf("body is missing %q; the complaint parser will see nothing:\n%s", want, plain)
		}
	}
}

// The same seam decides whether a bounce's Status and returned headers are
// visible, which is what makes a permanent bounce distinguishable.
func TestExtractBodyKeepsDeliveryStatusParts(t *testing.T) {
	parts := []*gmail.MessagePart{{
		MimeType: "multipart/report",
		Parts: []*gmail.MessagePart{
			{MimeType: "text/plain", Body: &gmail.MessagePartBody{Data: b64("Delivery has failed.")}},
			{MimeType: "message/delivery-status", Body: &gmail.MessagePartBody{
				Data: b64("Final-Recipient: rfc822; gone@acme.com\nAction: failed\nStatus: 5.1.1\n"),
			}},
			{MimeType: "message/rfc822-headers", Body: &gmail.MessagePartBody{
				Data: b64("Message-ID: <the-send@ourdomain.com>\n"),
			}},
		},
	}}

	plain, _ := extractBody(parts)
	for _, want := range []string{"Status: 5.1.1", "gone@acme.com", "the-send@ourdomain.com"} {
		if !strings.Contains(plain, want) {
			t.Errorf("body is missing %q; the bounce parser cannot judge permanence:\n%s", want, plain)
		}
	}
}

// Ordinary mail must be unaffected: an alternative body still yields exactly
// its two parts, with no report text spliced in.
func TestExtractBodyOrdinaryMailIsUnchanged(t *testing.T) {
	parts := []*gmail.MessagePart{{
		MimeType: "multipart/alternative",
		Parts: []*gmail.MessagePart{
			{MimeType: "text/plain", Body: &gmail.MessagePartBody{Data: b64("Hi Ada")}},
			{MimeType: "text/html", Body: &gmail.MessagePartBody{Data: b64("<p>Hi Ada</p>")}},
		},
	}}
	plain, html := extractBody(parts)
	if plain != "Hi Ada" {
		t.Errorf("plain = %q, want exactly the text part", plain)
	}
	if html != "<p>Hi Ada</p>" {
		t.Errorf("html = %q", html)
	}
}
