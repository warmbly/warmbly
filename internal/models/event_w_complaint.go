package models

import "github.com/google/uuid"

type JobEventInboundComplaint struct {
	UserID            uuid.UUID `json:"user_id"`
	EmailID           uuid.UUID `json:"email_id"`
	OriginalMessageID string    `json:"original_message_id"`
	Recipient         string    `json:"recipient"`
	FeedbackType      string    `json:"feedback_type"`
}
