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

	// Body. BodyHTML is sanitized for display: scripts, event handlers, and
	// unsafe URL schemes are stripped before it leaves the API.
	BodyPlain string `json:"body_plain"`
	BodyHTML  string `json:"body_html"`
	// BodyTruncated marks a message whose stored body could not be read, so
	// BodyPlain holds only the preview snippet. Clients show a notice instead
	// of presenting a partial message as the whole thing.
	BodyTruncated bool `json:"body_truncated"`
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

	// Canonical folder the provider reported the message in (Folder* consts);
	// empty when the source path predates folder tracking.
	Folder string `json:"folder,omitempty"`

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
	ID      uuid.UUID `json:"id"`
	EmailID uuid.UUID `json:"email_id"`
	Mailbox uint32    `json:"mailbox"`
	// Folder is the canonical folder (see the Folder* constants) the message
	// was in at sync time. Empty on events from workers predating the field;
	// the consumer normalizes before storing.
	Folder       string    `json:"folder,omitempty"`
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
	// BodyText is a bounded plain-text rendering of the message, carried on the
	// new-email event so the consumer can make the message findable by what it
	// says. The full body goes to object storage, never here.
	BodyText  string    `json:"body_text,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
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

// MessageGrounding is one message rendered for an AI prompt: the stored body
// text when it exists, with the preview snippet as the fallback for mail synced
// before bodies were indexed. Deliberately separate from the preview shape so a
// 16 KB body can never leak into a list response by accident.
type MessageGrounding struct {
	ID       uuid.UUID `json:"id"`
	FromAddr []string  `json:"from_addr"`
	ToAddr   []string  `json:"to_addr"`
	Subject  string    `json:"subject"`
	BodyText string    `json:"body_text"`
	Snippet  string    `json:"snippet"`
	SentAt   time.Time `json:"sent_at"`
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

// Canonical mail folders. Every unibox message carries exactly one, derived
// from provider placement at sync time (IMAP special-use attributes, Gmail
// labels, Graph well-known folders) and enforced by a CHECK on the column.
const (
	FolderInbox   = "inbox"
	FolderSent    = "sent"
	FolderDrafts  = "drafts"
	FolderArchive = "archive"
	FolderSpam    = "spam"
	FolderTrash   = "trash"
)

// MailFolders lists every canonical folder, in sidebar order.
var MailFolders = []string{FolderInbox, FolderSent, FolderDrafts, FolderArchive, FolderSpam, FolderTrash}

// ValidFolder reports whether f is one of the canonical folder values.
func ValidFolder(f string) bool {
	switch f {
	case FolderInbox, FolderSent, FolderDrafts, FolderArchive, FolderSpam, FolderTrash:
		return true
	}
	return false
}

// NormalizeFolder resolves the folder to persist for a message: the worker's
// value when it sent a valid one, otherwise a flag-derived fallback so events
// from workers predating the folder field still file spam and drafts sanely.
func NormalizeFolder(folder string, flags []string) string {
	if ValidFolder(folder) {
		return folder
	}
	// Deletion outranks the rest: a message can carry \Deleted alongside a
	// spam or draft flag, and the provider-driven paths treat trash as the
	// stronger placement. Scanning for it first keeps a flag-ordering
	// accident from filing deleted mail as spam.
	for _, f := range flags {
		if f == "\\Deleted" {
			return FolderTrash
		}
	}
	for _, f := range flags {
		switch f {
		case "SPAM", "\\Junk", "\\Spam", "Junk":
			return FolderSpam
		case "\\Draft":
			return FolderDrafts
		}
	}
	return FolderInbox
}

type MailSearchResult struct {
	Data       []EmailMessageStoreDataPreview `json:"data"`
	Pagination CPagination                    `json:"pagination"`
}

type MailSearchParams struct {
	Sender *string
	// Address matches either side of the exchange (from_addr OR to_addr),
	// giving "every conversation with this person" regardless of direction.
	// Substring match on the raw header entries, like Sender.
	Address *string
	// Direction narrows to messages the org sent ("sent") or received
	// ("received"), resolved against the org's own mailbox addresses.
	// nil = both directions.
	Direction *string
	Unseen    *bool
	Subject   *string
	Since     *time.Time
	Until     *time.Time
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
	// AgentDraft, when true, narrows to threads that have a pending
	// inbox-agent draft awaiting human review (M10). nil = no filter.
	AgentDraft *bool
	// CategoryIDs restricts results to threads carrying at least one of
	// these conversation labels. Empty = no category filter.
	CategoryIDs []uuid.UUID
	// Uncategorized, when true, narrows to threads carrying no
	// conversation labels at all. nil = no filter.
	Uncategorized *bool
	// Folder narrows to one canonical folder (inbox/sent/drafts/archive/
	// spam/trash). nil = every folder except spam and trash, so junk never
	// bleeds into the combined view.
	Folder   *string
	PageSize int
	Cursor   string
}

type MarkSeen struct {
	EmailIDs []uuid.UUID `json:"email_ids"`
	// Folder, when set, marks every unread message in that folder for the
	// whole workspace instead of the explicit id list.
	Folder string `json:"folder,omitempty"`
	Seen   bool   `json:"seen"`
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

// UniboxFolderOverview gives the rail per-folder thread counts. Always
// emitted for all six canonical folders, zero-filled, so the sidebar
// renders a stable list.
type UniboxFolderOverview struct {
	Folder string `json:"folder"`
	Unread int64  `json:"unread"`
	Total  int64  `json:"total"`
}

// UniboxOverview powers the scope rail + top metric strip in one
// request. Computed at /unibox/overview.
type UniboxOverview struct {
	Total         int64 `json:"total"`
	Unread        int64 `json:"unread"`
	Today         int64 `json:"today"`
	Week          int64 `json:"week"`
	Snoozed       int64 `json:"snoozed"`
	AwaitingReply int64 `json:"awaiting_reply"`
	// AwaitingAgentDraft is the count of threads with a pending inbox-agent draft
	// waiting for human review (M10).
	AwaitingAgentDraft int64 `json:"awaiting_agent_draft"`
	ScheduledPending   int64 `json:"scheduled_pending"`
	// ScheduledPendingMax is the hard cap on pending scheduled email
	// tasks per user. The dashboard shows current/max so the user
	// sees how close they are to the limit before hitting it.
	ScheduledPendingMax int64                    `json:"scheduled_pending_max"`
	Folders             []UniboxFolderOverview   `json:"folders"`
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
