package models

import "github.com/google/uuid"

type JobEventMailboxUpdate struct {
	UserID  uuid.UUID `json:"user_id" avro:"user_id"`
	EmailID uuid.UUID `json:"email_id" avro:"email_id"`
	Data    *Mailbox  `json:"data" avro:"data"`
}

// JobEventMailboxDelete retires a folder that is no longer in the listing.
type JobEventMailboxDelete struct {
	UserID  uuid.UUID `json:"user_id" avro:"user_id"`
	EmailID uuid.UUID `json:"email_id" avro:"email_id"`
	// Mailbox is the folder's name, which is what identifies it. Empty only
	// on events from workers that predate the name being the identity; the
	// consumer then falls back to UIDValidity.
	Mailbox string `json:"mailbox,omitempty" avro:"mailbox"`
	// UIDValidity is that legacy fallback and nothing else.
	UIDValidity uint32 `json:"uid_validity" avro:"uid_validity"`
}

// JobEventMailboxRename is a folder that kept its UIDVALIDITY under a new
// name, which is what an IMAP RENAME looks like from the listing. The stored
// folder row and the mail filed under the old name both move.
type JobEventMailboxRename struct {
	UserID  uuid.UUID `json:"user_id" avro:"user_id"`
	EmailID uuid.UUID `json:"email_id" avro:"email_id"`
	From    string    `json:"from" avro:"from"`
	To      string    `json:"to" avro:"to"`
}
