package models

import "github.com/google/uuid"

type JobEventNewEmail struct {
	UserID  uuid.UUID              `json:"user_id" avro:"user_id"`
	Message *EmailMessageStoreData `json:"message" avro:"message"`
}

type JobEventRemoveEmail struct {
	UserID  uuid.UUID `json:"user_id" avro:"user_id"`
	EmailID uuid.UUID `json:"email_id" avro:"email_id"`
	ID      uuid.UUID `json:"id" avro:"id"`
}

type JobEventFlags struct {
	UserID  uuid.UUID `json:"user_id" avro:"user_id"`
	EmailID uuid.UUID `json:"email_id" avro:"email_id"`
	ID      uuid.UUID `json:"id" avro:"id"`
	Flags   []string  `json:"flags" avro:"flags"`
}

type JobEventEmailUpdate struct {
	UserID  uuid.UUID `json:"user_id" avro:"user_id"`
	EmailID uuid.UUID `json:"email_id" avro:"email_id"`
	ID      uuid.UUID `json:"id" avro:"id"`
	UID     uint32    `json:"uid" avro:"uid"`
	// int64 for the reason on JobEventHistoryIDUpdate.HistoryID. RFC 7162
	// caps a MODSEQ at 2^63-1, so the narrower type cannot lose one.
	ModSeq uint64 `json:"mod_seq" avro:"mod_seq"`
	// Mailbox is the folder's UIDVALIDITY, the generation UID belongs to.
	Mailbox uint32 `json:"mailbox" avro:"mailbox"`
	// FolderPath is the folder's name, its identity. Empty on events from
	// workers predating the field (the consumer then keeps the stored value).
	FolderPath string `json:"folder_path,omitempty" avro:"folder_path"`
	// Folder is the canonical folder the message now sits in; empty on events
	// from workers predating folder tracking (the consumer then keeps the
	// stored value).
	Folder string   `json:"folder,omitempty" avro:"folder"`
	Flags  []string `json:"flags" avro:"flags"`
}
