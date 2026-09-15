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
	// int64 for the reason on JobEventHistoryIDUpdate.HistoryID. RFC 7162
	// caps a MODSEQ at 2^63-1, so the narrower type cannot lose one.
	ModSeq int64 `json:"mod_seq"`
	// Mailbox is the folder's UIDVALIDITY, the generation UID belongs to.
	Mailbox uint32 `json:"mailbox"`
	// FolderPath is the folder's name, its identity. Empty on events from
	// workers predating the field (the consumer then keeps the stored value).
	FolderPath string `json:"folder_path,omitempty"`
	// Folder is the canonical folder the message now sits in; empty on events
	// from workers predating folder tracking (the consumer then keeps the
	// stored value).
	Folder string   `json:"folder,omitempty"`
	Flags  []string `json:"flags"`
}
