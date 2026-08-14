package whatsapp

import (
	"time"

	"github.com/google/uuid"
)

// ChannelEvent is the normalized provider event the rest of Warmbly consumes.
// Provider-specific payloads never leave the adapter after normalization.
type ChannelEvent struct {
	Channel           string    `json:"channel"` // WHATSAPP
	Provider          string    `json:"provider"`
	EventType         string    `json:"event_type"`
	ExternalMessageID string    `json:"external_message_id,omitempty"`
	ExternalEventID   string    `json:"external_event_id,omitempty"`
	ExternalThreadID  string    `json:"external_thread_id,omitempty"`
	Instance          string    `json:"instance,omitempty"`
	FromE164          string    `json:"from,omitempty"`
	ToE164            string    `json:"to,omitempty"`
	OccurredAt        time.Time `json:"occurred_at"`
	Content           Content   `json:"content"`
	ConnectionState   string    `json:"connection_state,omitempty"`
	ErrorCode         string    `json:"error_code,omitempty"`
	ErrorMessage      string    `json:"error_message,omitempty"`
	// OrganizationID is filled after instance→org mapping, not by the provider.
	OrganizationID uuid.UUID `json:"organization_id,omitempty"`
}

// Content is the message body projection we persist.
type Content struct {
	Type string `json:"type"` // text | other
	Text string `json:"text,omitempty"`
}

// IdempotencyKey builds a stable key for webhook dedupe.
func (e ChannelEvent) IdempotencyKey() string {
	if e.ExternalEventID != "" {
		return e.Provider + ":" + e.ExternalEventID
	}
	if e.ExternalMessageID != "" {
		return e.Provider + ":" + e.EventType + ":" + e.ExternalMessageID
	}
	return e.Provider + ":" + e.EventType + ":" + e.FromE164 + ":" + e.OccurredAt.UTC().Format(time.RFC3339Nano)
}
