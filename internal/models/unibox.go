package models

import (
	"time"

	"github.com/google/uuid"
)

type EmailMessage struct { // used for sending to the user
	ID      uuid.UUID `json:"id"`       // Gmail
	GmailID string    `json:"gmail_id"` // Gmail
	UID     uint32    `json:"uid"`      // IMAP

	ParentID string `json:"parent_id"`
	ThreadID string `json:"thread_id"`

	Flags []string `json:"flags"` // or labelids

	BCC       []string  `json:"bcc"`
	CC        []string  `json:"cc"`
	Date      time.Time `json:"date"`
	From      []string  `json:"from"`
	InReplyTo []string  `json:"in_reply_to"`
	MessageID string    `json:"message_id"`
	ReplyTo   []string  `json:"ReplyTo"`
	To        []string  `json:"to"`
	Subject   string    `json:"subject"`

	Size int64 `json:"size"`

	// Internal Date
	InternalDate time.Time `json:"internal_date"`

	// ModSeq (CONDSTORE)
	ModSeq uint64 `json:"mod_seq"`

	// Body
	BodyPlain string `json:"body_plain"`
	BodyHTML  string `json:"body_html"`
}

type EmailMessageData struct { // used when for kafka when an email arrives
	ID uuid.UUID `json:"id"`

	// Gmail Only
	GmailID string `json:"gmail_id"` // msg.Id (Unique)
	Snippet string `json:"snippet"`

	// Threading
	ParentID string `json:"parent_id"` // Last ID in In-Reply-To
	ThreadID string `json:"thread_id"` // Root Message ID

	// Imap UID (Not Unique, it can change if the email moves)
	UID uint32 `json:"uid"`

	// Flags
	Flags []string `json:"flags"`

	// Envelope
	BCC       []string  `json:"bcc"`
	CC        []string  `json:"cc"`
	Date      time.Time `json:"date"`
	From      []string  `json:"from"`
	InReplyTo []string  `json:"in_reply_to"`
	MessageID string    `json:"message_id"` // Unique
	ReplyTo   []string  `json:"reply_to"`
	Sender    []string  `json:"sender"`
	Subject   string    `json:"subject"`
	To        []string  `json:"to"`

	// RFC822 Size
	Size int64 `json:"size"`

	// Internal Date
	InternalDate time.Time `json:"internal_date"`

	// ModSeq (CONDSTORE)
	ModSeq uint64 `json:"mod_seq"`

	// Body
	BodyPlain string `json:"body_plain"`
	BodyHTML  string `json:"body_html"`
}

type EmailMessageStoreData struct {
	ID           uuid.UUID `json:"id"`
	EmailID      uuid.UUID `json:"email_id"`
	Mailbox      uint32    `json:"mailbox"`
	ThreadID     string    `json:"thread_id"`
	MessageID    string    `json:"message_id"`
	GmailID      string    `json:"gmail_id"`
	ParentID     string    `json:"parent_id"`
	UID          uint32    `json:"uid"`
	ModSeq       uint64    `json:"mod_seq"`
	Flags        []string  `json:"flags"`
	BCC          []string  `json:"bcc"`
	CC           []string  `json:"cc"`
	FromAddr     []string  `json:"from_addr"`
	InReplyTo    []string  `json:"in_reply_to"`
	ReplyTo      []string  `json:"reply_to"`
	ToAddr       []string  `json:"to_addr"`
	Subject      string    `json:"subject"`
	Size         int64     `json:"size"`
	InternalDate time.Time `json:"internal_date"`
	SentDate     time.Time `json:"sent_date"`
	Snippet      string    `json:"snippet"`
	Seen         bool      `json:"seen"`
	UpdatedAt    time.Time `json:"updated_at"`
	CreatedAt    time.Time `json:"created_at"`
}

type EmailMessageStoreDataPreview struct {
	ID           uuid.UUID `json:"id"`
	EmailID      uuid.UUID `json:"email_id"`
	ThreadID     string    `json:"thread_id"`
	Subject      string    `json:"subject"`
	Snippet      string    `json:"snippet"`
	InternalDate time.Time `json:"internal_date"`
	Seen         bool      `json:"seen"`
}

type EmailParent struct { // used to get information from the parent email
	ID        uuid.UUID `json:"id" avro:"id"`
	MessageID string    `json:"message_id" avro:"message_id"`
	ThreadID  string    `json:"thread_id" avro:"thread_id"`
}

type MailThreadResult struct {
	Data       []EmailMessageStoreData `json:"data"`
	Pagination CPagination             `json:"pagination"`
}

type MailSearchResult struct {
	Data       []EmailMessageStoreDataPreview `json:"data"`
	Pagination CPagination                    `json:"pagination"`
}

type MailSearchParams struct {
	Sender   *string
	Unseen   *bool
	Subject  *string
	Since    *time.Time
	Until    *time.Time
	PageSize int
	Cursor   string
}

type MarkSeen struct {
	EmailIDs []uuid.UUID `json:"email_ids"`
	Seen     bool        `json:"seen"`
}
