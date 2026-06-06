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
	FromAddr     []string  `json:"from_addr"`
	ToAddr       []string  `json:"to_addr"`
	Subject      string    `json:"subject"`
	Snippet      string    `json:"snippet"`
	InternalDate time.Time `json:"internal_date"`
	Seen         bool      `json:"seen"`

	// Thread-stacking fields. The inbox list collapses to one row per
	// thread (the newest message), so these summarise the whole
	// conversation behind the row:
	//   MessageCount — number of messages in the thread (within the
	//                  current filter scope); 1 for a singleton.
	//   HasUnread    — true if any message in the thread is unseen, so
	//                  the row can bold the whole conversation Gmail-style
	//                  rather than only when the latest message is unread.
	// Both are zero-valued on the message-level paths (GetByThread /
	// GetBySender / GetIncoming) that don't collapse.
	MessageCount int64 `json:"message_count"`
	HasUnread    bool  `json:"has_unread"`

	// Labels are the conversation's assigned categories (denormalised
	// id/title/color so the row renders chips without a second lookup).
	// Always non-nil so it marshals to [] not null.
	Labels []MiniCategory `json:"labels"`
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
	Sender  *string
	Unseen  *bool
	Subject *string
	Since   *time.Time
	Until   *time.Time
	// EmailAccountIDs restricts results to messages received by one of
	// these mailboxes. Empty = no account filter. The frontend tag
	// filter resolves client-side to the matching account IDs and
	// passes them here.
	EmailAccountIDs []uuid.UUID
	// Snoozed scopes the result set:
	//   nil   → exclude snoozed threads (default inbox behaviour)
	//   true  → only snoozed threads
	//   false → ignore the snooze filter entirely (raw view)
	Snoozed *bool
	// AwaitingReply, when true, narrows to threads where the latest
	// message in the thread was sent by the user (i.e. the recipient
	// hasn't replied yet). nil = no filter.
	AwaitingReply *bool
	// CategoryIDs restricts results to threads carrying at least one of
	// these conversation labels. Empty = no category filter.
	CategoryIDs []uuid.UUID
	PageSize    int
	Cursor      string
}

type MarkSeen struct {
	EmailIDs []uuid.UUID `json:"email_ids"`
	Seen     bool        `json:"seen"`
}

// UniboxSnooze hides a thread from the user's inbox until SnoozedUntil
// passes. UNIQUE per (user, thread); a second snooze on the same
// thread updates SnoozedUntil in place.
type UniboxSnooze struct {
	ID           uuid.UUID `json:"id"`
	UserID       uuid.UUID `json:"user_id"`
	ThreadID     string    `json:"thread_id"`
	SnoozedUntil time.Time `json:"snoozed_until"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// UniboxMailboxOverview captures per-mailbox counters for the scope
// rail. Total counts every message in the mailbox; unread is the
// classic dot-badge number.
type UniboxMailboxOverview struct {
	ID     uuid.UUID `json:"id"`
	Email  string    `json:"email"`
	Name   string    `json:"name"`
	Unread int64     `json:"unread"`
	Total  int64     `json:"total"`
}

// UniboxTagOverview gives the rail per-tag counts; resolved by joining
// emails through the mailboxes that carry the tag.
type UniboxTagOverview struct {
	ID     uuid.UUID `json:"id"`
	Title  string    `json:"title"`
	Color  string    `json:"color"`
	Unread int64     `json:"unread"`
	Total  int64     `json:"total"`
}

// UniboxCategoryOverview gives the rail per-conversation-label counts.
// Resolved by joining threads through unibox_thread_labels. Unread/Total
// are counted as THREADS (matching the thread-stacked list), not
// messages.
type UniboxCategoryOverview struct {
	ID     uuid.UUID `json:"id"`
	Title  string    `json:"title"`
	Color  string    `json:"color"`
	Unread int64     `json:"unread"`
	Total  int64     `json:"total"`
}

// UniboxOverview powers the scope rail + top metric strip in one
// request. Computed at /unibox/overview.
type UniboxOverview struct {
	Total            int64 `json:"total"`
	Unread           int64 `json:"unread"`
	Today            int64 `json:"today"`
	Week             int64 `json:"week"`
	Snoozed          int64 `json:"snoozed"`
	AwaitingReply    int64 `json:"awaiting_reply"`
	ScheduledPending int64 `json:"scheduled_pending"`
	// ScheduledPendingMax is the hard cap on pending scheduled email
	// tasks per user. The dashboard shows current/max so the user
	// sees how close they are to the limit before hitting it.
	ScheduledPendingMax int64                    `json:"scheduled_pending_max"`
	Mailboxes           []UniboxMailboxOverview  `json:"mailboxes"`
	Tags                []UniboxTagOverview      `json:"tags"`
	Categories          []UniboxCategoryOverview `json:"categories"`
	GeneratedAt         time.Time                `json:"generated_at"`
	WindowTodayStart    time.Time                `json:"window_today_start"`
	WindowWeekStart     time.Time                `json:"window_week_start"`
}

// UniboxThreadLabels is the assign/replace payload for a conversation's
// labels. CategoryIDs is the full desired set — the handler diffs it
// against what's stored, so a PUT-style replace is idempotent.
type UniboxThreadLabels struct {
	ThreadID    string      `json:"thread_id" binding:"required"`
	CategoryIDs []uuid.UUID `json:"category_ids"`
}

// UniboxScheduledItem describes one queued outbound message the user
// can review or cancel before it fires. The shape mirrors what the
// scheduled-list view needs to render: who it's going to, when, the
// thread it'll thread into, and which mailbox is sending.
type UniboxScheduledItem struct {
	TaskID      uuid.UUID `json:"task_id"`
	ScheduledAt time.Time `json:"scheduled_at"`
	CreatedAt   time.Time `json:"created_at"`

	// Sending mailbox.
	AccountID    uuid.UUID `json:"account_id"`
	AccountEmail string    `json:"account_email"`
	AccountName  string    `json:"account_name"`

	// Recipients + message contents (preview only).
	To      []string `json:"to"`
	CC      []string `json:"cc,omitempty"`
	BCC     []string `json:"bcc,omitempty"`
	Subject string   `json:"subject"`
	Snippet string   `json:"snippet"`

	// Thread the reply will land in (when the user queued from unibox).
	ThreadID *string `json:"thread_id,omitempty"`
}
