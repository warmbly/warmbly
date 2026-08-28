package models

import "github.com/google/uuid"

// JobEventInboundComplaint is emitted by the worker when a synced inbound
// message is an abuse feedback report (RFC 5965) for one of our sends. The
// worker parses the report it has in hand (no SQL); the consumer resolves the
// original send and records the complaint. Only abuse-type reports are emitted,
// so a "not-spam" report can never be recorded as a complaint.
type JobEventInboundComplaint struct {
	UserID  uuid.UUID `json:"user_id"`
	EmailID uuid.UUID `json:"email_id"`
	// OriginalMessageID is the RFC Message-ID of the reported outbound message,
	// used to resolve the campaign/contact/task.
	OriginalMessageID string `json:"original_message_id"`
	// ComplainedRecipient is the address that reported it; several providers
	// redact this, so it is often empty.
	ComplainedRecipient string `json:"complained_recipient"`
	// Provider is the reporting agent, for the event record.
	Provider string `json:"provider"`
}
