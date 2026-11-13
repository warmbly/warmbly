package models

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type CampaignLog struct {
	ID         uuid.UUID              `json:"id"`
	CampaignID uuid.UUID              `json:"campaign_id"`
	EventType  string                 `json:"event_type"`
	Message    string                 `json:"message"`
	Metadata   map[string]interface{} `json:"metadata"`
	CreatedAt  time.Time              `json:"created_at"`
}

type CampaignLogsResult struct {
	Data       []CampaignLog `json:"data"`
	Pagination CPagination   `json:"pagination"`
}

// TransientSendRetryMessage is the campaign log line for a send a temporary provider failure delayed; ok is false for any other failure.
func TransientSendRetryMessage(recipient, code string, attempt, maxAttempts int) (msg string, ok bool) {
	switch code {
	case "SERVER_UNREACHABLE", "CONNECTION_LOST":
		return fmt.Sprintf("Sending to %s was delayed because the mail provider did not respond (attempt %d of %d). Warmbly retries it automatically.", recipient, attempt, maxAttempts), true
	case "SENDING_TOO_FAST":
		return fmt.Sprintf("Sending to %s was delayed because the mail provider asked Warmbly to slow down (attempt %d of %d). Warmbly retries it automatically.", recipient, attempt, maxAttempts), true
	}
	return "", false
}

// DeferTransientSendRetry shows a stored retryable send failure from a temporary provider failure as a delay rather than an error.
func (l *CampaignLog) DeferTransientSendRetry() {
	reason, _ := l.Metadata["error"].(string)
	// A mailbox still loading has its own evidence-gated rewrite.
	if l.EventType != "email_failed" || l.Metadata["will_retry"] != true || strings.HasPrefix(reason, MailboxNotLoadedPrefix) {
		return
	}
	code, _ := l.Metadata["code"].(string)
	attempt, _ := l.Metadata["attempts"].(float64)
	maxAttempts, _ := l.Metadata["max_attempts"].(float64)
	who, _, found := strings.Cut(strings.TrimPrefix(l.Message, "Could not send to "), " (attempt ")
	if !found || !strings.HasPrefix(l.Message, "Could not send to ") {
		who = "the recipient"
	}
	msg, ok := TransientSendRetryMessage(who, code, int(attempt), int(maxAttempts))
	if !ok {
		return
	}
	l.EventType, l.Message = "email_deferred", msg
	l.Metadata["level"] = "info"
}
