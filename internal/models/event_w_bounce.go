package models

import "github.com/google/uuid"

// JobEventInboundBounce is emitted by the worker when a synced inbound message
// is a permanent (hard) delivery-status notification for one of our sends. The
// worker parses the DSN it already has in hand (no SQL); the consumer resolves
// the original send and records the bounce. Only permanent failures are emitted,
// so acting on this never over-suppresses a transient failure.
type JobEventInboundBounce struct {
	UserID  uuid.UUID `json:"user_id"`
	EmailID uuid.UUID `json:"email_id"`
	// OriginalMessageID is the RFC Message-ID of the bounced outbound message,
	// used to resolve the campaign/contact/task.
	OriginalMessageID string `json:"original_message_id"`
	// FailedRecipient is the address that bounced, when the DSN exposed it.
	FailedRecipient string `json:"failed_recipient"`
	// Reason is a short human string (the bounce subject) for the event record.
	Reason string `json:"reason"`
}
