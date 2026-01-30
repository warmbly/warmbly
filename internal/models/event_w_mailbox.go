package models

import "github.com/google/uuid"

type JobEventMailboxUpdate struct {
	UserID  uuid.UUID `json:"user_id"`
	EmailID uuid.UUID `json:"email_id"`
	Data    *Mailbox  `json:"data"`
}

type JobEventMailboxDelete struct {
	UserID      uuid.UUID `json:"user_id"`
	EmailID     uuid.UUID `json:"email_id"`
	UIDValidity uint32    `json:"uid_validity"`
}
