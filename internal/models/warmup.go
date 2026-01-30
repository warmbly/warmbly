package models

import (
	"time"

	"github.com/google/uuid"
)

type WarmupToken struct {
	Token              uuid.UUID  `json:"token"`
	TaskID             uuid.UUID  `json:"task_id"`
	SenderAccountID    uuid.UUID  `json:"sender_account_id"`
	RecipientAccountID uuid.UUID  `json:"recipient_account_id"`
	CreatedAt          time.Time  `json:"created_at"`
	ConsumedAt         *time.Time `json:"consumed_at,omitempty"`
	ExpiresAt          time.Time  `json:"expires_at"`
}

// WarmupEmailAction represents actions to perform on a detected warmup email
type WarmupEmailAction struct {
	UserID  uuid.UUID `json:"user_id"`
	EmailID uuid.UUID `json:"email_id"`
	GmailID string    `json:"gmail_id"`
	UID     uint32    `json:"uid"`
	Actions []string  `json:"actions"` // "move_to_warmbly", "mark_read", "remove_from_spam", "mark_important"
}
