package models

import "github.com/google/uuid"

type JobEventNewEmail struct {
	UserID  uuid.UUID              `json:"user_id"`
	Message *EmailMessageStoreData `json:"message"`
}

type JobEventRemoveEmail struct {
	UserID  uuid.UUID `json:"user_id"`
	EmailID uuid.UUID `json:"email_id"`
	ID      uuid.UUID `json:"id"`
}

type JobEventFlags struct {
	UserID  uuid.UUID `json:"user_id"`
	EmailID uuid.UUID `json:"email_id"`
	ID      uuid.UUID `json:"id"`
	Flags   []string  `json:"flags"`
}

type JobEventEmailUpdate struct {
	UserID  uuid.UUID `json:"user_id"`
	EmailID uuid.UUID `json:"email_id"`
	ID      uuid.UUID `json:"id"`
	UID     uint32    `json:"uid"`
	ModSeq  uint64    `json:"mod_seq"`
	Mailbox uint32    `json:"mailbox"`
	Flags   []string  `json:"flags"`
}
