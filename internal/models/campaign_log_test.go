package models

import (
	"strings"
	"testing"
)

func storedFailure(code string, willRetry bool, reason string) CampaignLog {
	return CampaignLog{
		EventType: "email_failed",
		Message:   "Could not send to ann@example.com (attempt 1 of 5), will retry: " + reason,
		Metadata: map[string]interface{}{
			"level": "error", "code": code, "error": reason, "attempts": float64(1), "max_attempts": float64(5), "will_retry": willRetry,
		},
	}
}

func TestDeferTransientSendRetry(t *testing.T) {
	for _, code := range []string{"SERVER_UNREACHABLE", "CONNECTION_LOST", "SENDING_TOO_FAST"} {
		l := storedFailure(code, true, "The connection to the mail server could not be established.")
		l.DeferTransientSendRetry()
		if l.EventType != "email_deferred" || l.Metadata["level"] != "info" ||
			!strings.HasPrefix(l.Message, "Sending to ann@example.com was delayed") || !strings.Contains(l.Message, "attempt 1 of 5") {
			t.Fatalf("%s: %+v", code, l)
		}
	}
	for name, l := range map[string]CampaignLog{
		"last attempt":       storedFailure("SERVER_UNREACHABLE", false, "x"),
		"permanent refusal":  storedFailure("RECIPIENT_REJECTED", true, "x"),
		"mailbox not loaded": storedFailure("SERVER_UNREACHABLE", true, MailboxNotLoadedPrefix+" x"),
	} {
		l.DeferTransientSendRetry()
		if l.EventType != "email_failed" || l.Metadata["level"] != "error" {
			t.Fatalf("%s rewritten: %+v", name, l)
		}
	}
}
