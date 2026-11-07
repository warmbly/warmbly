package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

type UpdateUniboxEntry struct {
	UID     *uint32  `json:"uid"`
	Flags   []string `json:"flags"`
	ModSeq  *uint64  `json:"mod_seq"`
	Mailbox *uint32  `json:"mailbox"`
	// FolderPath is the source folder's name, the folder's identity.
	FolderPath *string `json:"folder_path"`
	Folder     *string `json:"folder"`
	// ProviderFolder is the provider's own placement. It moves on every real
	// provider move; Folder only follows when the message was not filed here.
	ProviderFolder *string `json:"provider_folder"`
	// ProviderID is the provider's message id (gmail_id), which a Graph move changes.
	ProviderID *string `json:"provider_id"`
	// Seen is the provider's read state. Set whenever a sync event carries a
	// change to it, so mail read in the customer's own client stops showing
	// as unread here.
	Seen *bool `json:"seen"`
}

type UniboxRepository interface {
	CreateEntry(ctx context.Context, userID uuid.UUID, e *models.EmailMessageStoreData) error
	UpdateEntry(ctx context.Context, userID, emailID, id uuid.UUID, e *UpdateUniboxEntry) error
	GetIncoming(ctx context.Context, userID uuid.UUID, limit int, cursor string) (*models.MailSearchResult, error)
	GetByID(ctx context.Context, userID, id uuid.UUID) (*models.EmailMessageStoreData, error)
	GetForSync(ctx context.Context, userID, emailID, id uuid.UUID) (*models.EmailMessageStoreData, error)
	// GetByIDForOrg is the org-scoped read for the unibox detail view: any
	// member with unibox access can open a message in the org-wide list. It
	// returns the row plus the mailbox OWNER's user_id, which the S3 body key
	// is built from (emails/<ownerID>/<id>), so the body still resolves under
	// the owner even when a different teammate opens it.
	GetByIDForOrg(ctx context.Context, orgID, id uuid.UUID) (*models.EmailMessageStoreData, uuid.UUID, error)
	GetByThread(ctx context.Context, orgID, emailID uuid.UUID, threadID string, limit int, cursor string) (*models.MailSearchResult, error)
	// GetByThreadWithin is GetByThread held to the allowed mailboxes before paging; nil allows every mailbox.
	GetByThreadWithin(ctx context.Context, orgID, emailID uuid.UUID, threadID string, limit int, cursor string, allowed []uuid.UUID) (*models.MailSearchResult, error)
	// GetBySender is the organization's mail from sender, newest first, across every mailbox.
	GetBySender(ctx context.Context, orgID uuid.UUID, sender string, limit int, cursor string) (*models.MailSearchResult, error)
	Search(ctx context.Context, orgID uuid.UUID, params *models.MailSearchParams) (*models.MailSearchResult, error)
	// SearchScopeOwned reports whether each given campaign and contact belongs to the organization; a nil id passes.
	SearchScopeOwned(ctx context.Context, orgID uuid.UUID, campaignID, contactID *uuid.UUID) (campaign, contact bool, err error)
	GetUnseenCount(ctx context.Context, orgID uuid.UUID, emailAccountID *uuid.UUID) (int64, error)
	MarkSeen(ctx context.Context, userID, id uuid.UUID, seen bool) error
	// MarkSeenBulk flips the read state of the given messages and returns the
	// ids that actually changed, which is what gets relayed to the provider.
	MarkSeenBulk(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID, seen bool) ([]uuid.UUID, error)
	// MarkSeenByThreads is MarkSeenBulk addressed by conversation, for callers
	// that hold a list row rather than the ids inside it. Unread reaches only
	// each conversation's newest received message.
	MarkSeenByThreads(ctx context.Context, orgID uuid.UUID, threadIDs []string, seen bool) ([]uuid.UUID, error)
	// MarkSeenByFolder flips the read state of every message in one canonical
	// folder for the whole workspace (the sidebar's "mark all as read").
	MarkSeenByFolder(ctx context.Context, orgID uuid.UUID, folder string, seen bool) ([]uuid.UUID, error)
	// MoveToFolderBulk re-files the given messages into one canonical folder,
	// org-scoped like MarkSeenBulk, and returns every row it matched.
	MoveToFolderBulk(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID, folder string) ([]models.FiledMessage, error)
	// MoveThreadsToFolder files whole conversations. Filing part of one leaves
	// it in the view it was filed out of, which reads as the action having
	// done nothing.
	MoveThreadsToFolder(ctx context.Context, orgID uuid.UUID, threadIDs []string, folder string) ([]models.FiledMessage, error)
	// FolderRelayTargets resolves filed messages for the folder relay: only
	// mailboxes with a worker and relay_folder_moves on, and never a warmup
	// receipt, whose removal the warmup ladder would read as tampering.
	FolderRelayTargets(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) ([]models.FolderRelayTarget, error)
	// SeenRelayTargets names the given messages the way their provider does,
	// with the worker holding each mailbox. Rows whose mailbox has no worker
	// are left out: there is nothing to relay through.
	SeenRelayTargets(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) ([]models.SeenRelayTarget, error)
	Delete(ctx context.Context, userID, id uuid.UUID) error
	// DeleteByFolderPaths drops every stored message one mailbox synced from
	// the named source folders, for folders the owner has excluded from sync.
	// Exact names only; the worker names each subfolder it retires itself.
	DeleteByFolderPaths(ctx context.Context, emailID uuid.UUID, folderPaths []string) (int64, error)
	ListWarmupReviewCandidates(ctx context.Context, afterID uuid.UUID, limit int) ([]models.JobEventNewEmail, error)
	// ListUnprocessedCampaignReplies pages inbound messages that reply
	// processing never claimed and that look like campaign replies: they
	// answer a campaign send, or come from one of the workspace's contacts.
	ListUnprocessedCampaignReplies(ctx context.Context, since time.Time, afterID uuid.UUID, limit int) (CampaignReplyRepairPage, error)
	// ListHistoricalReplies pages inbound mail reply processing handled after
	// its sender became a contact, though it arrived before, outside any
	// campaign thread.
	ListHistoricalReplies(ctx context.Context, since time.Time, afterID uuid.UUID) (HistoricalReplyPage, error)
	// ListInboundFrom returns a workspace's inbound mail from one address,
	// newest first: the evidence behind an opt-out that address triggered.
	ListInboundFrom(ctx context.Context, orgID uuid.UUID, address string, limit int) ([]InboundMessage, error)
	// FirstWrittenTo returns when the workspace's mailboxes first sent the
	// address a message, or nil when they never have.
	FirstWrittenTo(ctx context.Context, orgID uuid.UUID, address string) (*time.Time, error)
	DeferWarmupVerification(ctx context.Context, e *models.JobEventNewEmail) error
	ClaimPendingWarmupVerification(ctx context.Context, limit int) ([]models.JobEventNewEmail, error)
	ProcessPendingWarmupVerification(ctx context.Context, id uuid.UUID, process func(*models.JobEventNewEmail) error) error
	UpdatePendingEmail(ctx context.Context, userID, id uuid.UUID, update func(*models.EmailMessageStoreData)) (bool, error)

	// Snooze: per (organization, user, thread). UpsertSnooze adopts the new
	// snoozed_until even if one already exists; DeleteSnooze removes
	// the row outright (instant un-snooze). ListSnoozes returns the
	// active set for the user.
	// The bulk forms back the conversation list's selection bar, so filing a
	// screenful of mail is one round trip rather than one per row.
	UpsertSnoozes(ctx context.Context, orgID, userID uuid.UUID, threadIDs []string, until time.Time) ([]models.UniboxSnooze, error)
	DeleteSnoozes(ctx context.Context, orgID, userID uuid.UUID, threadIDs []string) error
	ListSnoozes(ctx context.Context, orgID, userID uuid.UUID) ([]models.UniboxSnooze, error)

	// Overview powers the scope rail + top metric strip. Single call
	// so the client doesn't fan out N+M queries for each mailbox/tag.
	Overview(ctx context.Context, orgID uuid.UUID) (*models.UniboxOverview, error)
	OverviewForMailboxes(ctx context.Context, orgID uuid.UUID, accountIDs []uuid.UUID) (*models.UniboxOverview, error)
	// MessageMailboxes lists the mailboxes holding the named messages and every
	// message in the named conversations, within the organization.
	MessageMailboxes(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID, threadIDs []string) ([]uuid.UUID, error)
	CategoriesForMailboxes(ctx context.Context, orgID uuid.UUID, accountIDs []uuid.UUID) ([]models.Group, error)
	// UnseenCountForMailboxes is GetUnseenCount over a set of mailboxes.
	UnseenCountForMailboxes(ctx context.Context, orgID uuid.UUID, accountIDs []uuid.UUID) (int64, error)

	// Conversation labels, workspace-scoped like the inbox they hang off.
	// SetThreadLabels replaces the full label set on a thread (idempotent PUT
	// semantics, only the workspace's own categories are attached);
	// ListThreadLabels returns the current set for one thread.
	SetThreadLabels(ctx context.Context, orgID, userID uuid.UUID, threadID string, categoryIDs []uuid.UUID) ([]models.MiniCategory, error)
	ListThreadLabels(ctx context.Context, orgID uuid.UUID, threadID string) ([]models.MiniCategory, error)
	ListThreadLabelsWithin(ctx context.Context, orgID uuid.UUID, threadID string, accountIDs []uuid.UUID) ([]models.MiniCategory, error)
	// AddThreadLabels attaches labels to a thread WITHOUT removing existing ones
	// (additive; for automation/step "label email" actions). LatestThreadIDForContact
	// finds the workspace's most recent conversation with an address, so a campaign
	// step that knows the contact but not the thread can still label it.
	AddThreadLabels(ctx context.Context, orgID uuid.UUID, threadID string, categoryIDs []uuid.UUID) error
	LatestThreadIDForContact(ctx context.Context, orgID uuid.UUID, email string) (string, error)
	// LatestMessageIDInThread returns the newest RFC Message-ID in a thread, so
	// a reply that arrives with only a provider thread id can still carry the
	// In-Reply-To header the recipient's mail client threads on.
	LatestMessageIDInThread(ctx context.Context, orgID uuid.UUID, threadID string) (string, error)
	// FileSentForward files the sending mailbox's stored copy of a Unibox
	// forward into the conversation it was forwarded from, for a copy synced
	// before the send's Message-ID was known. Returns the rows it moved.
	FileSentForward(ctx context.Context, taskID uuid.UUID) ([]FiledSentForward, error)

	// ListMissingBodyText pages through messages stored before bodies were
	// indexed for search, oldest id first from afterID. SetBodyText writes the
	// rendered text back. Together they are the backfill: without it, search
	// over message text would only ever work for mail synced from now on.
	ListMissingBodyText(ctx context.Context, afterID uuid.UUID, limit int) ([]UniboxBodyTarget, error)
	SetBodyText(ctx context.Context, id uuid.UUID, bodyText string) error
	// BodyBackfillProgress and SaveBodyBackfillProgress keep the walk's place across restarts.
	BodyBackfillProgress(ctx context.Context) (UniboxBodyBackfill, error)
	SaveBodyBackfillProgress(ctx context.Context, p UniboxBodyBackfill) error

	// GroundingByThread and GroundingByAddress return message text for AI
	// prompts. They are separate from the preview queries because they select
	// the body column, which is far too big to carry on a list response.
	GroundingByThread(ctx context.Context, orgID uuid.UUID, threadID string, limit int) ([]models.MessageGrounding, error)
	GroundingByAddress(ctx context.Context, orgID uuid.UUID, address string, limit int) ([]models.MessageGrounding, error)
}

// UniboxBodyTarget is one message awaiting a search-text backfill. UserID is
// the mailbox owner and EmailID the mailbox account; the body's object-storage
// key is built from both.
type UniboxBodyTarget struct {
	ID      uuid.UUID
	UserID  uuid.UUID
	EmailID uuid.UUID
}

type uniboxRepository struct {
	db *db.DB
}

func NewUniboxRepository(db *db.DB) UniboxRepository {
	return &uniboxRepository{db: db}
}

var mailFieldsFull = []string{
	"id", "email_id", "mailbox", "folder_path", "thread_id", "message_id",
	"gmail_id", "parent_id", "uid", "mod_seq",
	"flags", "bcc", "cc", "from_addr", "in_reply_to", "reply_to",
	"to_addr", "subject", "size", "internal_date", "sent_date",
	"snippet", "seen", "updated_at", "created_at", "folder", "provider_folder",
}

var mailFieldsPreview = []string{
	"id", "email_id", "thread_id", "from_addr", "to_addr",
	"subject", "snippet", "internal_date", "seen",
}

// Folders a view never shows unless it names one. Junk is junk; archive is
// where a conversation goes to leave the views the workspace works out of, so
// filing one has to take it out of all of them and not only the Inbox folder.
// "All mail" and reference reads opt archive back in.
const (
	foldersOutsideWorkingViews = `('spam', 'trash', 'archive')`
	foldersOutsideAllMail      = `('spam', 'trash')`
	// Our own copies. Nothing marks one unread: nobody has a sent message to read.
	foldersOutbound = `('sent', 'drafts')`
)

// automatedThreadSQL is the predicate "no person wrote in this conversation":
// one of its messages was judged automated and every message that is not ours
// or junk was too. A new message is not automated until judged, so a reply
// brings the conversation back to the inbox the moment it lands.
func automatedThreadSQL(threadExpr, rowAutomatedExpr, orgArg string) string {
	return fmt.Sprintf(`(CASE WHEN %[1]s = '' THEN %[2]s ELSE (
			EXISTS (
				SELECT 1 FROM unibox_emails am
				WHERE am.thread_id = %[1]s AND am.automated
				  AND am.email_id IN (SELECT id FROM email_accounts WHERE organization_id = %[3]s)
			)
			AND NOT EXISTS (
				SELECT 1 FROM unibox_emails pm
				WHERE pm.thread_id = %[1]s AND NOT pm.automated
				  AND pm.folder NOT IN ('sent', 'drafts', 'spam', 'trash')
				  AND pm.email_id IN (SELECT id FROM email_accounts WHERE organization_id = %[3]s)
			)
		) END)`, threadExpr, rowAutomatedExpr, orgArg)
}

// bareAddrSQL extracts the exact address out of a raw header entry: the last
// bracketed address ("Name <a@b.com>", never one quoted inside the name), a
// bare address before a comment ("a@b.com (note)"), the parenthesised form
// older IMAP rows carry ("Name (a@b.com)"), else the trimmed value. Lowercased,
// and never a substring contains, so a@b.com matches neither xa@b.com nor
// a@b.com.evil.
func bareAddrSQL(expr string) string {
	return fmt.Sprintf(
		`lower(coalesce(substring(%[1]s from '<([^<>]*)>[^<>]*$'), substring(%[1]s from '^[[:space:]]*([^[:space:]()<>"]+@[^[:space:]()<>"]+)[[:space:]]*\('), substring(%[1]s from '\(([^()]*)\)\s*$'), btrim(%[1]s)))`,
		expr,
	)
}

// ownAddressSQL is the predicate "this header entry names a mailbox of the
// organization in $1". The addresses are compared exactly; matching them by
// containment is what made Awaiting reply nearly empty, since a mailbox sends
// as "Name <addr>" and almost never as a bare address.
func ownAddressSQL(arrayExpr, orgArg string) string {
	return fmt.Sprintf(`EXISTS (
				SELECT 1 FROM unnest(%s) AS own(addr)
				WHERE %s IN (SELECT lower(ea.email) FROM email_accounts ea WHERE ea.organization_id = %s)
			)`, arrayExpr, bareAddrSQL("own.addr"), orgArg)
}

// CreateEntry stores a synced message. A mailbox's copy of a forward it sent
// from Unibox is filed into the conversation it was forwarded from, and e
// carries the thread it was stored under.
func (r *uniboxRepository) CreateEntry(ctx context.Context, userID uuid.UUID, e *models.EmailMessageStoreData) error {
	query := `
		WITH fwd AS (` + sentForwardThreadSQL + `)
		INSERT INTO unibox_emails (
			id, user_id, email_id, mailbox, folder_path, thread_id, message_id,
			gmail_id, parent_id, uid, mod_seq,
			flags, bcc, cc, from_addr, in_reply_to, reply_to,
			to_addr, subject, size, internal_date, sent_date,
			snippet, seen, created_at, updated_at, body_text, folder,
			provider_folder, automated, filed_forward
		) VALUES (
			$1, $2, $3, $4, $5, COALESCE((SELECT thread_id FROM fwd), $6), $7,
			$8, $9, $10, $11,
			$12, $13, $14, $15, $16, $17,
			$18, $19, $20, $21, $22,
			$23, $24, $25, $26, $27, $28,
			$28,
			-- A row re-created by a sync keeps the verdict its message already has.
			EXISTS (
				SELECT 1 FROM inbox_tag_results r
				WHERE r.organization_id = (SELECT organization_id FROM email_accounts WHERE id = $3)
				  AND r.message_id = $7 AND $7 <> ''
				  AND r.automated
			),
			EXISTS (SELECT 1 FROM fwd)
		)
		ON CONFLICT (id) DO NOTHING
		RETURNING thread_id
	`

	// The array columns are NOT NULL. A nil Go slice binds as SQL NULL, so a
	// message with no In-Reply-To (any thread root) would fail the insert.
	var threadID string
	err := r.db.QueryRow(ctx, query,
		e.ID, userID, e.EmailID, e.Mailbox, e.FolderPath, e.ThreadID, e.MessageID,
		e.GmailID, e.ParentID, e.UID, e.ModSeq,
		textArray(e.Flags), textArray(e.BCC), textArray(e.CC), textArray(e.FromAddr),
		textArray(e.InReplyTo), textArray(e.ReplyTo), textArray(e.ToAddr),
		e.Subject, e.Size, e.InternalDate, e.SentDate,
		e.Snippet, e.Seen, e.CreatedAt, e.UpdatedAt, e.BodyText,
		// $28 is both columns: a message starts out where the provider put it,
		// and only diverges once someone files it in Warmbly.
		models.NormalizeFolder(e.Folder, e.Flags),
	).Scan(&threadID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Already stored: the conflict leaves the row as it was.
		return nil
	}
	if err != nil {
		return err
	}
	e.ThreadID = threadID
	return nil
}

// FiledSentForward is a stored copy FileSentForward moved, with its mailbox owner.
type FiledSentForward struct {
	UserID  uuid.UUID
	Message models.EmailMessageStoreData
}

func (r *uniboxRepository) FileSentForward(ctx context.Context, taskID uuid.UUID) ([]FiledSentForward, error) {
	// Only the Sent copy can have raced the result, and (email_id, folder) is indexed.
	rows, err := r.db.Query(ctx, `
		UPDATE unibox_emails ue
		SET thread_id = et.forward_thread_id,
		    filed_forward = true,
		    updated_at = NOW()
		FROM tasks t
		JOIN email_tasks et ON et.task_id = t.id
		WHERE t.id = $1
		  AND t.task_type = 'email'
		  AND t.message_id <> ''
		  AND COALESCE(et.forward_thread_id, '') <> ''
		  AND ue.email_id = t.email_account_id
		  AND ue.folder = $2
		  AND ue.message_id IN (t.message_id, btrim(t.message_id, '<> '), '<' || btrim(t.message_id, '<> ') || '>')
		  AND NOT (ue.filed_forward AND ue.thread_id = et.forward_thread_id)
		RETURNING ue.id, ue.user_id, ue.email_id, ue.thread_id, ue.subject, ue.from_addr, ue.snippet, ue.folder, ue.flags
	`, taskID, models.FolderSent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FiledSentForward
	for rows.Next() {
		var f FiledSentForward
		m := &f.Message
		if err := rows.Scan(&m.ID, &f.UserID, &m.EmailID, &m.ThreadID, &m.Subject, &m.FromAddr, &m.Snippet, &m.Folder, &m.Flags); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// sentForwardThreadSQL finds the conversation a mailbox's message ($3, $7) was
// forwarded from, when it is the mailbox's own copy of a Unibox forward.
const sentForwardThreadSQL = `
			SELECT et.forward_thread_id AS thread_id
			FROM tasks t
			JOIN email_tasks et ON et.task_id = t.id
			WHERE $7 <> ''
			  AND t.email_account_id = $3
			  AND t.task_type = 'email'
			  AND t.message_id IN ($7, btrim($7, '<> '), '<' || btrim($7, '<> ') || '>')
			  AND COALESCE(et.forward_thread_id, '') <> ''
			ORDER BY t.created_at DESC
			LIMIT 1`

// textArray coalesces a nil slice to an empty one so it binds as '{}' rather
// than NULL.
func textArray(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func (r *uniboxRepository) UpdateEntry(ctx context.Context, userID, emailID, id uuid.UUID, e *UpdateUniboxEntry) error {
	setClauses := []string{"updated_at = NOW()"}
	args := []any{userID, id}
	argPos := 3

	if e.Flags != nil {
		setClauses = append(setClauses, fmt.Sprintf("flags = $%d", argPos))
		args = append(args, e.Flags)
		argPos++
	}
	if e.Mailbox != nil {
		setClauses = append(setClauses, fmt.Sprintf("mailbox = $%d", argPos))
		args = append(args, *e.Mailbox)
		argPos++
	}
	if e.ModSeq != nil {
		setClauses = append(setClauses, fmt.Sprintf("mod_seq = $%d", argPos))
		args = append(args, *e.ModSeq)
		argPos++
	}
	if e.UID != nil {
		setClauses = append(setClauses, fmt.Sprintf("uid = $%d", argPos))
		args = append(args, *e.UID)
		argPos++
	}
	if e.FolderPath != nil {
		setClauses = append(setClauses, fmt.Sprintf("folder_path = $%d", argPos))
		args = append(args, *e.FolderPath)
		argPos++
	}
	if e.Folder != nil {
		setClauses = append(setClauses, fmt.Sprintf("folder = $%d", argPos))
		args = append(args, *e.Folder)
		argPos++
	}
	if e.ProviderFolder != nil {
		setClauses = append(setClauses, fmt.Sprintf("provider_folder = $%d", argPos))
		args = append(args, *e.ProviderFolder)
		argPos++
	}
	if e.ProviderID != nil {
		setClauses = append(setClauses, fmt.Sprintf("gmail_id = $%d", argPos))
		args = append(args, *e.ProviderID)
		argPos++
	}
	if e.Seen != nil {
		setClauses = append(setClauses, fmt.Sprintf("seen = $%d", argPos))
		args = append(args, *e.Seen)
		argPos++
	}

	if argPos == 3 {
		return nil // nothing to update
	}

	query := fmt.Sprintf(`
		UPDATE unibox_emails
		SET %s
		WHERE user_id = $1 AND id = $2
	`, strings.Join(setClauses, ", "))

	_, err := r.db.Exec(ctx, query, args...)
	return err
}

func (r *uniboxRepository) GetIncoming(ctx context.Context, userID uuid.UUID, limit int, cursor string) (*models.MailSearchResult, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM unibox_emails
		WHERE user_id = $1
	`, strings.Join(mailFieldsPreview, ", "))

	args := []any{userID}
	argPos := 2

	if cursor != "" {
		cursorID, err := uuid.Parse(cursor)
		if err == nil {
			query += fmt.Sprintf(`
				AND (internal_date, id) < (
					SELECT internal_date, id FROM unibox_emails WHERE id = $%d AND user_id = $1
				)`, argPos)
			args = append(args, cursorID)
			argPos++
		}
	}

	query += fmt.Sprintf(` ORDER BY internal_date DESC, id DESC LIMIT $%d`, argPos)
	args = append(args, limit+1)

	return r.queryPreviewList(ctx, query, args, limit)
}

// ErrEmailNotFound is returned when a message id names no row the caller can
// see. A consumer event for a message the unibox never stored is routine, not a
// failure, so it has to be distinguishable from a real read error.
var ErrEmailNotFound = errors.New("email not found")

func (r *uniboxRepository) GetByID(ctx context.Context, userID, id uuid.UUID) (*models.EmailMessageStoreData, error) {
	e, err := r.getMessage(ctx, userID, uuid.Nil, id)
	if err != nil {
		return nil, err
	}
	if !e.Seen {
		_ = r.MarkSeen(ctx, userID, id, true)
		e.Seen = true
	}
	return e, nil
}

// GetForSync reads provider state without treating background work as a user view.
func (r *uniboxRepository) GetForSync(ctx context.Context, userID, emailID, id uuid.UUID) (*models.EmailMessageStoreData, error) {
	if emailID == uuid.Nil {
		return nil, ErrEmailNotFound
	}
	return r.getMessage(ctx, userID, emailID, id)
}

func (r *uniboxRepository) getMessage(ctx context.Context, userID, emailID, id uuid.UUID) (*models.EmailMessageStoreData, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM unibox_emails
		WHERE user_id = $1 AND id = $2
		  AND ($3::uuid = '00000000-0000-0000-0000-000000000000' OR email_id = $3)
	`, strings.Join(mailFieldsFull, ", "))

	var e models.EmailMessageStoreData
	err := r.db.QueryRow(ctx, query, userID, id, emailID).Scan(
		&e.ID, &e.EmailID, &e.Mailbox, &e.FolderPath, &e.ThreadID, &e.MessageID,
		&e.GmailID, &e.ParentID, &e.UID, &e.ModSeq,
		&e.Flags, &e.BCC, &e.CC, &e.FromAddr, &e.InReplyTo, &e.ReplyTo,
		&e.ToAddr, &e.Subject, &e.Size, &e.InternalDate, &e.SentDate,
		&e.Snippet, &e.Seen, &e.UpdatedAt, &e.CreatedAt, &e.Folder, &e.ProviderFolder,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrEmailNotFound
		}
		return nil, err
	}

	return &e, nil
}

// GetByIDForOrg reads a single message scoped to the org's mailboxes (not the
// caller's user_id), mirroring GetByThread, so a non-owner teammate who sees a
// message in the org-scoped list can open it. It also returns the row's owner
// user_id: the body's object-storage key is built from the owner (and the
// row's email_id), so the caller must fetch the body under the owner, not
// under itself.
//
// It does not mark the message read: that is a write with a provider relay
// behind it, and it belongs to the service.
func (r *uniboxRepository) GetByIDForOrg(ctx context.Context, orgID, id uuid.UUID) (*models.EmailMessageStoreData, uuid.UUID, error) {
	query := fmt.Sprintf(`
		SELECT user_id, %s
		FROM unibox_emails
		WHERE id = $2 AND email_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)
	`, strings.Join(mailFieldsFull, ", "))

	var ownerID uuid.UUID
	var e models.EmailMessageStoreData
	err := r.db.QueryRow(ctx, query, orgID, id).Scan(
		&ownerID,
		&e.ID, &e.EmailID, &e.Mailbox, &e.FolderPath, &e.ThreadID, &e.MessageID,
		&e.GmailID, &e.ParentID, &e.UID, &e.ModSeq,
		&e.Flags, &e.BCC, &e.CC, &e.FromAddr, &e.InReplyTo, &e.ReplyTo,
		&e.ToAddr, &e.Subject, &e.Size, &e.InternalDate, &e.SentDate,
		&e.Snippet, &e.Seen, &e.UpdatedAt, &e.CreatedAt, &e.Folder, &e.ProviderFolder,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, uuid.Nil, ErrEmailNotFound
		}
		return nil, uuid.Nil, err
	}

	// Reading it is what marks it read, and that now has to reach the mailbox
	// too, so the service owns the transition (it holds the relay). The row
	// is returned exactly as stored.
	return &e, ownerID, nil
}

// GetByThread returns the messages in a thread. emailID is optional —
// pass uuid.Nil to span every mailbox in the organization (the typical
// unified-inbox case where the caller only knows the thread). Scoped by org,
// not user_id, so any member with unibox access sees the whole conversation,
// matching the org-scoped inbox list.
func (r *uniboxRepository) GetByThread(ctx context.Context, orgID, emailID uuid.UUID, threadID string, limit int, cursor string) (*models.MailSearchResult, error) {
	return r.GetByThreadWithin(ctx, orgID, emailID, threadID, limit, cursor, nil)
}

func (r *uniboxRepository) GetByThreadWithin(ctx context.Context, orgID, emailID uuid.UUID, threadID string, limit int, cursor string, allowed []uuid.UUID) (*models.MailSearchResult, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM unibox_emails
		WHERE email_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)
		  AND thread_id = $2
	`, strings.Join(mailFieldsPreview, ", "))

	args := []any{orgID, threadID}
	argPos := 3

	if emailID != uuid.Nil {
		query += fmt.Sprintf(` AND email_id = $%d`, argPos)
		args = append(args, emailID)
		argPos++
	}
	if allowed != nil {
		query += fmt.Sprintf(` AND email_id = ANY($%d::uuid[])`, argPos)
		args = append(args, allowed)
		argPos++
	}

	if cursor != "" {
		cursorID, err := uuid.Parse(cursor)
		if err == nil {
			query += fmt.Sprintf(`
				AND (internal_date, id) > (
					SELECT internal_date, id FROM unibox_emails
					WHERE id = $%d AND email_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)
				)`, argPos)
			args = append(args, cursorID)
			argPos++
		}
	}

	// Thread display reads oldest-first so each reply renders below the
	// message it replied to.
	query += fmt.Sprintf(` ORDER BY internal_date ASC, id ASC LIMIT $%d`, argPos)
	args = append(args, limit+1)

	res, err := r.queryPreviewList(ctx, query, args, limit)
	if err != nil {
		return nil, err
	}
	if err := r.annotateAnsweredMailboxes(ctx, orgID, res.Data); err != nil {
		return nil, err
	}
	return res, nil
}

// annotateAnsweredMailboxes marks each message that replies to a send from
// another workspace mailbox with that mailbox, read from the task whose
// Message-ID its In-Reply-To names.
func (r *uniboxRepository) annotateAnsweredMailboxes(ctx context.Context, orgID uuid.UUID, emails []models.EmailMessageStoreDataPreview) error {
	if len(emails) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(emails))
	for i, e := range emails {
		ids[i] = e.ID
	}
	const q = `
		SELECT ue.id, answered.email_account_id
		FROM unibox_emails ue
		CROSS JOIN LATERAL (
			SELECT t.email_account_id
			FROM tasks t
			JOIN email_accounts sender ON sender.id = t.email_account_id AND sender.organization_id = $1
			WHERE t.task_type <> 'warmup'
			  AND t.email_account_id <> ue.email_id
			  AND t.message_id = ANY(ARRAY(
			        SELECT v
			        FROM unnest(ue.in_reply_to) AS parent(raw),
			             LATERAL (VALUES (btrim(parent.raw, '<> ')), ('<' || btrim(parent.raw, '<> ') || '>')) AS form(v)
			        WHERE btrim(parent.raw, '<> ') <> ''
			  ))
			ORDER BY t.created_at DESC
			LIMIT 1
		) answered
		WHERE ue.id = ANY($2)
		  AND ue.email_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)
	`
	rows, err := r.db.Query(ctx, q, orgID, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	answered := make(map[uuid.UUID]uuid.UUID, len(emails))
	for rows.Next() {
		var id, mailbox uuid.UUID
		if err := rows.Scan(&id, &mailbox); err != nil {
			return err
		}
		answered[id] = mailbox
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range emails {
		if mailbox, ok := answered[emails[i].ID]; ok {
			emails[i].AnswersMailboxID = &mailbox
		}
	}
	return nil
}

func (r *uniboxRepository) GetBySender(ctx context.Context, orgID uuid.UUID, sender string, limit int, cursor string) (*models.MailSearchResult, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM unibox_emails
		WHERE email_id = ANY(ARRAY(SELECT id FROM email_accounts WHERE organization_id = $1))
		  AND ($2 = ANY(from_addr) OR EXISTS (
			SELECT 1 FROM unnest(from_addr) a(addr) WHERE %s = lower(btrim($2))
		))
	`, strings.Join(mailFieldsPreview, ", "), bareAddrSQL("a.addr"))

	args := []any{orgID, sender}
	argPos := 3

	if cursor != "" {
		cursorID, err := uuid.Parse(cursor)
		if err == nil {
			query += fmt.Sprintf(`
				AND (internal_date, id) < (
					SELECT internal_date, id FROM unibox_emails
					WHERE id = $%d AND email_id = ANY(ARRAY(SELECT id FROM email_accounts WHERE organization_id = $1))
				)`, argPos)
			args = append(args, cursorID)
			argPos++
		}
	}

	query += fmt.Sprintf(` ORDER BY internal_date DESC, id DESC LIMIT $%d`, argPos)
	args = append(args, limit+1)

	return r.queryPreviewList(ctx, query, args, limit)
}

// Search is the main inbox list. It collapses every message sharing a
// thread_id into a single row — the newest message represents the
// conversation (Gmail-style stacking) — and annotates it with the
// thread's message count, an aggregate unread flag, and its assigned
// conversation labels.
//
// Filter altitude:
//   - row-level filters (snooze scope, unseen, date window, subject,
//     sender, mailbox) run inside the matching subquery, so the
//     representative + counts reflect the matched messages; with no
//     content filter (the default inbox) that's the whole thread.
//   - thread/representative-level filters (awaiting reply, category)
//     and keyset pagination run on the collapsed row.
func (r *uniboxRepository) Search(ctx context.Context, orgID uuid.UUID, params *models.MailSearchParams) (*models.MailSearchResult, error) {
	query, args := uniboxSearchQuery(orgID, params)
	return r.queryThreadList(ctx, query, args, params.PageSize)
}

func (r *uniboxRepository) SearchScopeOwned(ctx context.Context, orgID uuid.UUID, campaignID, contactID *uuid.UUID) (bool, bool, error) {
	var campaign, contact bool
	err := r.db.QueryRow(ctx, `
		SELECT $2::uuid IS NULL OR EXISTS (SELECT 1 FROM campaigns WHERE id = $2 AND organization_id = $1),
		       $3::uuid IS NULL OR EXISTS (SELECT 1 FROM contacts WHERE id = $3 AND organization_id = $1)`,
		orgID, campaignID, contactID).Scan(&campaign, &contact)
	return campaign, contact, err
}

func uniboxSearchQuery(orgID uuid.UUID, params *models.MailSearchParams) (string, []any) {
	previewCols := make([]string, len(mailFieldsPreview))
	for i, c := range mailFieldsPreview {
		previewCols[i] = "ue." + c
	}

	// $1 = orgID, which scopes both the mail (through the workspace's
	// mailboxes) and the conversation labels. Dynamic filters start at $2.
	args := []any{orgID}
	argPos := 2

	inner := fmt.Sprintf(`
		SELECT %s, ue.automated
		FROM unibox_emails ue
		WHERE ue.email_id = ANY(ARRAY(SELECT id FROM email_accounts WHERE organization_id = $1))`, strings.Join(previewCols, ", "))

	// Folder scoping. nil = every working folder; IncludeArchived widens that
	// to everything except junk, which is what "All mail" means.
	switch {
	case params.Folder != nil:
		inner += fmt.Sprintf(` AND ue.folder = $%d`, argPos)
		args = append(args, *params.Folder)
		argPos++
	case params.IncludeArchived != nil && *params.IncludeArchived:
		inner += ` AND ue.folder NOT IN ` + foldersOutsideAllMail
	default:
		inner += ` AND ue.folder NOT IN ` + foldersOutsideWorkingViews
	}

	// Snooze handling. nil = exclude snoozed (the inbox default), so
	// threads with an active snooze never appear unless asked for.
	switch {
	case params.Snoozed == nil:
		inner += `
			AND NOT EXISTS (
				SELECT 1 FROM unibox_snoozes s
				WHERE s.organization_id = $1 AND s.user_id = ue.user_id
				  AND s.thread_id = ue.thread_id
				  AND s.snoozed_until > NOW()
			)`
	case params.Snoozed != nil && *params.Snoozed:
		inner += `
			AND EXISTS (
				SELECT 1 FROM unibox_snoozes s
				WHERE s.organization_id = $1 AND s.user_id = ue.user_id
				  AND s.thread_id = ue.thread_id
				  AND s.snoozed_until > NOW()
			)`
	}

	if params.Unseen != nil && *params.Unseen {
		inner += fmt.Sprintf(` AND ue.seen = $%d`, argPos)
		args = append(args, false)
		argPos++
	}

	if params.Since != nil {
		inner += fmt.Sprintf(` AND ue.internal_date >= $%d`, argPos)
		args = append(args, *params.Since)
		argPos++
	}

	if params.Before != nil {
		inner += fmt.Sprintf(` AND ue.internal_date < $%d`, argPos)
		args = append(args, *params.Before)
		argPos++
	}

	if params.Subject != nil && *params.Subject != "" {
		// One box, everything a person searches an inbox for: the words in the
		// subject or the body, and the people the message was with.
		//
		// Three matchers, because no single one covers it:
		//
		//   full text  — websearch_to_tsquery over search_tsv (subject +
		//                preview) and the body. It reads "quoted phrases",
		//                OR and -excluded the way every search box does, and
		//                unlike to_tsquery it cannot be made to raise on
		//                punctuation, so a user typing `re: (urgent)` gets
		//                results rather than a 500.
		//   prefix     — whole words only is wrong while someone is still
		//                typing: "dyno" found 2 of the 100+ messages that say
		//                dynoweb. The sanitized prefix query fixes that.
		//   people     — the addresses. Searching a name or an address was
		//                finding nothing at all unless it also appeared in the
		//                body, which is not what anyone expects from an inbox.
		//
		// The body expression is written exactly as idx_unibox_emails_body_search
		// declares it, or the index is not used.
		q := *params.Subject
		webPos := argPos
		args = append(args, q)
		argPos++

		tsQuery := fmt.Sprintf(`websearch_to_tsquery('english', $%d)`, webPos)

		if prefix := prefixTSQuery(q); prefix != "" {
			tsQuery += fmt.Sprintf(` || to_tsquery('english', $%d)`, argPos)
			args = append(args, prefix)
			argPos++
		}
		match := fmt.Sprintf(`ue.search_tsv @@ (%s)
			OR to_tsvector('english'::regconfig, ue.body_text) @@ (%s)`, tsQuery, tsQuery)

		// Without a literal word trigram, GIN would recheck every message body.
		if participantSearchable(q) {
			people := fmt.Sprintf(`public.unibox_participants(ue.from_addr, ue.to_addr, ue.cc) ILIKE '%%' || $%d || '%%' ESCAPE '\'`, argPos)
			// A multiline query must not match across two joined header entries.
			if strings.ContainsRune(q, '\n') {
				people += fmt.Sprintf(` AND EXISTS (
					SELECT 1 FROM unnest(ue.from_addr || ue.to_addr || ue.cc) AS s(addr)
					WHERE s.addr ILIKE '%%' || $%d || '%%' ESCAPE '\'
				)`, argPos)
			}
			match += ` OR (` + people + `)`
			args = append(args, escapeLikePattern(q))
			argPos++
		}

		inner += ` AND (` + match + `)`
	}

	if params.Sender != nil && *params.Sender != "" {
		// Substring match on the raw header entries ("Name <addr>"),
		// per the documented contract; exact ANY() was unusable.
		inner += fmt.Sprintf(` AND EXISTS (
				SELECT 1 FROM unnest(ue.from_addr) AS f(addr)
				WHERE f.addr ILIKE '%%' || $%d || '%%'
			)`, argPos)
		args = append(args, *params.Sender)
		argPos++
	}

	if params.Address != nil && *params.Address != "" {
		// Either direction: the contact as sender OR as recipient, so the
		// result is the full back-and-forth with that address.
		inner += fmt.Sprintf(` AND EXISTS (
				SELECT 1 FROM unnest(ue.from_addr || ue.to_addr) AS f(addr)
				WHERE f.addr ILIKE '%%' || $%d || '%%'
			)`, argPos)
		args = append(args, *params.Address)
		argPos++
	}

	// Direction resolves against the org's own mailbox addresses, read out of
	// the raw header entry rather than compared to it whole.
	switch {
	case params.Direction != nil && *params.Direction == "sent":
		inner += ` AND ` + ownAddressSQL("ue.from_addr", "$1")
	case params.Direction != nil && *params.Direction == "received":
		inner += ` AND NOT ` + ownAddressSQL("ue.from_addr", "$1")
	}

	scopeCTEs := ""
	campaignPos := 0
	if params.CampaignID != nil || params.ContactID != nil {
		scopeCTEs, campaignPos, args, argPos = conversationScopeCTEs(params, args, argPos)
	}

	agentMailboxScope, campaignMailboxScope := "", ""
	if len(params.EmailAccountIDs) > 0 {
		inner += fmt.Sprintf(` AND ue.email_id = ANY($%d)`, argPos)
		agentMailboxScope = fmt.Sprintf(` AND d.email_account_id = ANY($%d)`, argPos)
		campaignMailboxScope = fmt.Sprintf(` AND cm.email_id = ANY($%d)`, argPos)
		args = append(args, params.EmailAccountIDs)
		argPos++
	}
	if len(params.TagIDs) > 0 {
		tagged := fmt.Sprintf(`(SELECT et.email_id FROM email_tags et JOIN tags tg ON tg.id = et.tag_id
			WHERE tg.organization_id = $1 AND et.tag_id = ANY($%d))`, argPos)
		inner += ` AND ue.email_id IN ` + tagged
		agentMailboxScope += ` AND d.email_account_id IN ` + tagged
		campaignMailboxScope += ` AND cm.email_id IN ` + tagged
		args = append(args, params.TagIDs)
		argPos++
	}

	outerCols := make([]string, len(mailFieldsPreview))
	for i, c := range mailFieldsPreview {
		outerCols[i] = "b." + c
	}
	// Probe only a candidate's thread, instead of windowing every matching body.
	query := fmt.Sprintf(`WITH %smatched AS NOT MATERIALIZED (%s), page AS MATERIALIZED (
		SELECT b.* FROM matched b
		WHERE (b.thread_id = '' OR NOT EXISTS (
			SELECT 1 FROM matched newer
			WHERE newer.thread_id = b.thread_id
			  AND (newer.internal_date, newer.id) > (b.internal_date, b.id)
		))`, scopeCTEs, inner)

	// The scope holds whole conversations, so only the representative row needs
	// it, and as arrays it drives an index scan instead of probing every message.
	if scopeCTEs != "" {
		query += `
			AND (b.thread_id = ANY(ARRAY(SELECT DISTINCT thread_id FROM scope_hits WHERE thread_id <> ''))
			  OR b.id = ANY(ARRAY(SELECT id FROM scope_hits WHERE thread_id = '')))`
	}

	// Awaiting reply: the representative (latest) message is from one of
	// the user's own mailboxes — i.e. they're waiting on the recipient.
	if params.AwaitingReply != nil && *params.AwaitingReply {
		query += `
			AND ` + ownAddressSQL("b.from_addr", "$1")
	}

	// Agent drafts: threads with a pending inbox-agent draft awaiting review.
	if params.AgentDraft != nil && *params.AgentDraft {
		query += `
			AND EXISTS (
				SELECT 1 FROM ai_thread_drafts d
				WHERE d.organization_id = $1
				  AND d.thread_id = b.thread_id
				  AND d.status = 'pending'
				  AND d.email_account_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)` + agentMailboxScope + `
			)`
	}

	if len(params.CategoryIDs) > 0 {
		query += fmt.Sprintf(`
			AND EXISTS (
				SELECT 1 FROM unibox_thread_labels utl
				WHERE utl.organization_id = $1
				  AND utl.thread_id = b.thread_id
				  AND utl.category_id = ANY($%d)
			)`, argPos)
		args = append(args, params.CategoryIDs)
		argPos++
	}

	if params.Automated != nil {
		if *params.Automated {
			query += `
			AND ` + automatedThreadSQL("b.thread_id", "b.automated", "$1")
		} else {
			query += `
			AND NOT ` + automatedThreadSQL("b.thread_id", "b.automated", "$1")
		}
	}

	if params.Uncategorized != nil && *params.Uncategorized {
		query += `
			AND NOT EXISTS (
				SELECT 1 FROM unibox_thread_labels utl
				WHERE utl.organization_id = $1
				  AND utl.thread_id = b.thread_id
			)`
	}

	if params.Cursor != "" {
		cursorID, err := uuid.Parse(params.Cursor)
		if err == nil {
			query += fmt.Sprintf(`
				AND (b.internal_date, b.id) < (
					SELECT internal_date, id FROM unibox_emails
					WHERE id = $%d AND email_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)
				)`, argPos)
			args = append(args, cursorID)
			argPos++
		}
	}

	query += fmt.Sprintf(` ORDER BY b.internal_date DESC, b.id DESC LIMIT $%d`, argPos)
	args = append(args, params.PageSize+1)

	// The filtered campaign wins when a conversation answers more than one.
	campaignOrder := ""
	if campaignPos > 0 {
		campaignOrder = fmt.Sprintf(`ct.campaign_id = $%d DESC, `, campaignPos)
	}

	query += fmt.Sprintf(`
		)
		SELECT %s, stats.message_count, stats.has_unread,
			COALESCE((
				SELECT json_agg(json_build_object('id', c.id, 'title', c.title, 'color', c.color) ORDER BY c.position ASC, c.title ASC)
				FROM unibox_thread_labels utl
				JOIN categories c ON c.id = utl.category_id AND c.organization_id = utl.organization_id
				WHERE utl.organization_id = $1 AND utl.thread_id = b.thread_id
			), '[]'::json) AS labels,
			camp.campaign_id
		FROM page b
		CROSS JOIN LATERAL (
			SELECT COUNT(*) AS message_count, bool_or(NOT m.seen) AS has_unread
			FROM matched m
			WHERE (b.thread_id <> '' AND m.thread_id = b.thread_id)
			   OR (b.thread_id = '' AND m.id = b.id)
		) stats
		LEFT JOIN LATERAL (
			SELECT ct.campaign_id
			FROM unibox_emails cm
			CROSS JOIN LATERAL (SELECT %s AS keys) k
			JOIN tasks t ON t.task_type = 'campaign'
			 AND ((t.message_id <> '' AND t.message_id = ANY(k.keys))
			   OR (cm.thread_id <> '' AND t.thread_id <> '' AND t.email_account_id = cm.email_id AND t.thread_id = cm.thread_id))
			JOIN campaign_tasks ct ON ct.task_id = t.id
			JOIN campaigns cc ON cc.id = ct.campaign_id AND cc.organization_id = $1
			WHERE cm.email_id = ANY(ARRAY(SELECT id FROM email_accounts WHERE organization_id = $1))%s
			  AND ((b.thread_id <> '' AND cm.thread_id = b.thread_id) OR cm.id = b.id)
			ORDER BY %scm.internal_date DESC, t.created_at DESC
			LIMIT 1
		) camp ON true
		ORDER BY b.internal_date DESC, b.id DESC`, strings.Join(outerCols, ", "), messageKeysSQL("cm"), campaignMailboxScope, campaignOrder)
	return query, args
}

// messageKeysSQL is every RFC Message-ID a stored message names (its own, its
// In-Reply-To and its thread root), bare and bracketed, as tasks.message_id may hold either.
func messageKeysSQL(alias string) string {
	return fmt.Sprintf(`ARRAY(
				SELECT f.v
				FROM unnest(%[1]s.in_reply_to || ARRAY[%[1]s.message_id, %[1]s.thread_id]) AS x(ref)
				CROSS JOIN LATERAL (VALUES (BTRIM(x.ref, '<> ')), ('<' || BTRIM(x.ref, '<> ') || '>')) AS f(v)
				WHERE BTRIM(x.ref, '<> ') <> ''
			)`, alias)
}

// conversationScopeCTEs materializes the campaign sends a campaign_id or
// contact_id filter selects and the stored messages tied to them. Each branch
// is an index lookup or a hash join, so the cost follows the scope's size.
func conversationScopeCTEs(params *models.MailSearchParams, args []any, argPos int) (string, int, []any, int) {
	sendFilter := ""
	campaignPos := 0
	if params.CampaignID != nil {
		campaignPos = argPos
		sendFilter += fmt.Sprintf(` AND ct.campaign_id = $%d`, argPos)
		args = append(args, *params.CampaignID)
		argPos++
	}
	contactCTE, contactHits := "", ""
	if params.ContactID != nil {
		sendFilter += fmt.Sprintf(` AND ct.contact_id = $%d`, argPos)
		if params.CampaignID == nil {
			// Mail exchanged with the contact's address: the trigram index finds
			// candidates, the parsed address decides.
			contactCTE = fmt.Sprintf(`scope_contact AS MATERIALIZED (
			SELECT lower(btrim(email)) AS addr,
			       '%%' || replace(replace(replace(lower(btrim(email)), '\', '\\'), '%%', '\%%'), '_', '\_') || '%%' AS pattern
			FROM contacts WHERE id = $%d AND organization_id = $1 AND btrim(email) <> ''
		), `, argPos)
			contactHits = fmt.Sprintf(`
			UNION
			SELECT m.id, m.thread_id FROM unibox_emails m, scope_contact sc
			WHERE m.email_id = ANY(ARRAY(SELECT id FROM email_accounts WHERE organization_id = $1))
			  AND public.unibox_participants(m.from_addr, m.to_addr, m.cc) ILIKE sc.pattern ESCAPE '\'
			  AND EXISTS (SELECT 1 FROM unnest(m.from_addr || m.to_addr) AS p(addr) WHERE %s = sc.addr)`, bareAddrSQL("p.addr"))
		}
		args = append(args, *params.ContactID)
		argPos++
	}

	ctes := `scope_sends AS MATERIALIZED (
			SELECT t.email_account_id, BTRIM(t.message_id, '<> ') AS message_id, t.thread_id, t.created_at
			FROM campaign_tasks ct
			JOIN tasks t ON t.id = ct.task_id
			WHERE ct.campaign_id IS NOT NULL AND ct.contact_id IS NOT NULL
			  AND ct.campaign_id = ANY(ARRAY(SELECT id FROM campaigns WHERE organization_id = $1))
			  AND t.task_type = 'campaign'
			  AND t.email_account_id = ANY(ARRAY(SELECT id FROM email_accounts WHERE organization_id = $1))` + sendFilter + `
		), scope_ids AS MATERIALIZED (
			SELECT DISTINCT message_id AS id FROM scope_sends WHERE message_id <> ''
		), scope_recent AS NOT MATERIALIZED (
			SELECT m.id, m.thread_id, m.message_id, m.in_reply_to FROM unibox_emails m
			WHERE m.email_id = ANY(ARRAY(SELECT id FROM email_accounts WHERE organization_id = $1))
			  AND m.internal_date >= (SELECT min(created_at) - INTERVAL '1 day' FROM scope_sends)
		), ` + contactCTE + `scope_hits AS MATERIALIZED (
			-- A conversation rooted on the send, as IMAP threads it.
			SELECT m.id, m.thread_id FROM unibox_emails m
			WHERE m.thread_id = ANY(ARRAY(SELECT id FROM scope_ids UNION ALL SELECT '<' || id || '>' FROM scope_ids))
			  AND m.email_id = ANY(ARRAY(SELECT id FROM email_accounts WHERE organization_id = $1))
			UNION
			-- The provider's conversation handle the send reported (Gmail).
			SELECT m.id, m.thread_id FROM scope_sends s
			JOIN unibox_emails m ON m.thread_id = s.thread_id AND m.email_id = s.email_account_id
			WHERE s.thread_id <> ''
			UNION
			-- The send's own copy, or a reply naming it, in mail since the first send.
			SELECT m.id, m.thread_id FROM scope_recent m JOIN scope_ids s ON s.id = BTRIM(m.message_id, '<> ')
			UNION
			SELECT m.id, m.thread_id FROM scope_recent m
			CROSS JOIN LATERAL unnest(m.in_reply_to) AS r(ref)
			JOIN scope_ids s ON s.id = BTRIM(r.ref, '<> ')` + contactHits + `
		), `
	return ctes, campaignPos, args, argPos
}

func (r *uniboxRepository) GetUnseenCount(ctx context.Context, orgID uuid.UUID, emailAccountID *uuid.UUID) (int64, error) {
	// Unread threads exactly as the Inbox view (where the badge links) lists
	// them: inbox folder only, snoozed and automated left out.
	query := `SELECT COUNT(DISTINCT COALESCE(NULLIF(ue.thread_id, ''), ue.id::text))
		FROM unibox_emails ue
		WHERE ue.email_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)
		  AND ue.seen = FALSE
		  AND ue.folder = '` + models.FolderInbox + `'
		  AND NOT EXISTS (
			SELECT 1 FROM unibox_snoozes s
			WHERE s.organization_id = $1 AND s.user_id = ue.user_id
			  AND s.thread_id = ue.thread_id
			  AND s.snoozed_until > NOW()
		  )
		  AND NOT ` + automatedThreadSQL("ue.thread_id", "ue.automated", "$1")
	args := []any{orgID}
	if emailAccountID != nil {
		query += ` AND ue.email_id = $2`
		args = append(args, *emailAccountID)
	}

	var count int64
	err := r.db.QueryRow(ctx, query, args...).Scan(&count)
	return count, err
}

func (r *uniboxRepository) UnseenCountForMailboxes(ctx context.Context, orgID uuid.UUID, accountIDs []uuid.UUID) (int64, error) {
	query := `SELECT COUNT(DISTINCT COALESCE(NULLIF(ue.thread_id, ''), ue.id::text))
		FROM unibox_emails ue
		WHERE ue.email_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)
		  AND ue.email_id = ANY($2::uuid[])
		  AND ue.seen = FALSE
		  AND ue.folder = '` + models.FolderInbox + `'
		  AND NOT EXISTS (
			SELECT 1 FROM unibox_snoozes s
			WHERE s.organization_id = $1 AND s.user_id = ue.user_id
			  AND s.thread_id = ue.thread_id
			  AND s.snoozed_until > NOW()
		  )
		  AND NOT ` + automatedThreadSQL("ue.thread_id", "ue.automated", "$1")
	var count int64
	err := r.db.QueryRow(ctx, query, orgID, accountIDs).Scan(&count)
	return count, err
}

func (r *uniboxRepository) MessageMailboxes(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID, threadIDs []string) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `
		SELECT DISTINCT email_id FROM unibox_emails
		WHERE email_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)
		  AND (id = ANY($2::uuid[]) OR COALESCE(NULLIF(thread_id, ''), id::text) = ANY($3::text[]))
	`, orgID, ids, threadIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *uniboxRepository) MarkSeen(ctx context.Context, userID, id uuid.UUID, seen bool) error {
	_, err := r.db.Exec(ctx,
		`UPDATE unibox_emails SET seen = $1, updated_at = NOW() WHERE user_id = $2 AND id = $3`,
		seen, userID, id,
	)
	return err
}

func (r *uniboxRepository) MarkSeenBulk(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID, seen bool) ([]uuid.UUID, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	// Org-scoped so any member with unibox access can clear the shared inbox's
	// unread state, not only the mailbox owner. The unread count is org-wide, so
	// a user_id filter would leave the badge stuck for non-owner members. ANY($3)
	// also covers the single-id case.
	//
	// `seen <> $1` and the RETURNING are what keep the provider relay honest:
	// re-reading a thread that is already read should cost nothing at Gmail.
	rows, err := r.db.Query(ctx,
		`UPDATE unibox_emails SET seen = $1, updated_at = NOW()
		 WHERE id = ANY($3) AND seen <> $1
		   AND ($1 OR folder NOT IN `+foldersOutbound+`)
		   AND email_id IN (SELECT id FROM email_accounts WHERE organization_id = $2)
		 RETURNING id`,
		seen, orgID, ids,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	changed := make([]uuid.UUID, 0, len(ids))
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		changed = append(changed, id)
	}
	return changed, rows.Err()
}

// MarkSeenByThreads is MarkSeenBulk addressed by conversation. The key is the
// same one the list collapses on, so an id that never got a thread still
// resolves to its own single message. Read covers the whole conversation;
// unread selects its newest received message, preferring one outside spam or
// trash, or its newest sent copy if none was received. Drafts are excluded.
func (r *uniboxRepository) MarkSeenByThreads(ctx context.Context, orgID uuid.UUID, threadIDs []string, seen bool) ([]uuid.UUID, error) {
	if len(threadIDs) == 0 {
		return nil, nil
	}
	query := `UPDATE unibox_emails SET seen = $1, updated_at = NOW()
		 WHERE COALESCE(NULLIF(thread_id, ''), id::text) = ANY($3) AND seen <> $1
		   AND email_id IN (SELECT id FROM email_accounts WHERE organization_id = $2)
		 RETURNING id`
	if !seen {
		query = `UPDATE unibox_emails SET seen = $1, updated_at = NOW()
		 WHERE seen <> $1 AND id IN (
			SELECT DISTINCT ON (COALESCE(NULLIF(thread_id, ''), id::text)) id
			FROM unibox_emails
			WHERE COALESCE(NULLIF(thread_id, ''), id::text) = ANY($3)
			  AND folder <> 'drafts'
			  AND email_id IN (SELECT id FROM email_accounts WHERE organization_id = $2)
			ORDER BY COALESCE(NULLIF(thread_id, ''), id::text),
			         folder = 'sent', folder IN ('spam', 'trash'), internal_date DESC, id DESC
		 )
		 RETURNING id`
	}
	rows, err := r.db.Query(ctx, query, seen, orgID, threadIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	changed := make([]uuid.UUID, 0, len(threadIDs))
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		changed = append(changed, id)
	}
	return changed, rows.Err()
}

// MarkSeenByFolder flips the read state of every message in one folder,
// org-scoped like MarkSeenBulk (the sidebar's "mark all as read").
func (r *uniboxRepository) MarkSeenByFolder(ctx context.Context, orgID uuid.UUID, folder string, seen bool) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx,
		`UPDATE unibox_emails SET seen = $1, updated_at = NOW()
		 WHERE folder = $3 AND seen <> $1
		   AND ($1 OR folder NOT IN `+foldersOutbound+`)
		   AND email_id IN (SELECT id FROM email_accounts WHERE organization_id = $2)
		 RETURNING id`,
		seen, orgID, folder,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var changed []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		changed = append(changed, id)
	}
	return changed, rows.Err()
}

// SeenRelayTargets resolves messages to what their provider calls them, plus
// the worker holding the mailbox.
//
// The three providers need different halves of this row (Gmail and Graph a
// message id, IMAP a folder and a UID), so all of it travels and the worker
// takes what its client uses. The read state comes from the row rather than
// from the request, so what is relayed is what Warmbly currently holds.
func (r *uniboxRepository) SeenRelayTargets(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) ([]models.SeenRelayTarget, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.db.Query(ctx,
		`SELECT ue.email_id, ea.worker_id, ue.seen, ue.gmail_id, ue.uid, ue.folder_path, ue.message_id
		 FROM unibox_emails ue
		 JOIN email_accounts ea ON ea.id = ue.email_id
		 WHERE ue.id = ANY($2) AND ea.organization_id = $1 AND ea.worker_id IS NOT NULL
		 ORDER BY ue.email_id`,
		orgID, ids,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.SeenRelayTarget
	for rows.Next() {
		var t models.SeenRelayTarget
		if err := rows.Scan(&t.EmailID, &t.WorkerID, &t.Seen, &t.Ref.ProviderID, &t.Ref.UID, &t.Ref.Folder, &t.Ref.RFCMessageID); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *uniboxRepository) MoveToFolderBulk(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID, folder string) ([]models.FiledMessage, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return r.fileRows(ctx,
		`UPDATE unibox_emails u SET folder = $1, updated_at = NOW()
		 FROM unibox_emails prev
		 WHERE prev.id = u.id AND u.id = ANY($3)
		   AND u.email_id IN (SELECT id FROM email_accounts WHERE organization_id = $2)
		 RETURNING u.id, prev.folder <> $1`,
		folder, orgID, ids,
	)
}

// MoveThreadsToFolder files every message in the named conversations. Filing
// by thread rather than by id is what makes a list row able to archive what it
// shows: the row knows the conversation, not the messages inside it.
func (r *uniboxRepository) MoveThreadsToFolder(ctx context.Context, orgID uuid.UUID, threadIDs []string, folder string) ([]models.FiledMessage, error) {
	if len(threadIDs) == 0 {
		return nil, nil
	}
	return r.fileRows(ctx,
		`UPDATE unibox_emails u SET folder = $1, updated_at = NOW()
		 FROM unibox_emails prev
		 WHERE prev.id = u.id AND COALESCE(NULLIF(u.thread_id, ''), u.id::text) = ANY($3)
		   AND u.email_id IN (SELECT id FROM email_accounts WHERE organization_id = $2)
		 RETURNING u.id, prev.folder <> $1`,
		folder, orgID, threadIDs,
	)
}

// fileRows runs a filing UPDATE. Rows already in the folder come back too,
// unmoved: the provider may still disagree with them. prev is the row as the
// statement found it, which is how the folder it left is read.
func (r *uniboxRepository) fileRows(ctx context.Context, query string, args ...any) ([]models.FiledMessage, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var filed []models.FiledMessage
	for rows.Next() {
		var f models.FiledMessage
		if err := rows.Scan(&f.ID, &f.Moved); err != nil {
			return nil, err
		}
		filed = append(filed, f)
	}
	return filed, rows.Err()
}

// FolderRelayTargets reads back where each filed row sits now, and where its
// provider last had it, so the relay moves only what the provider disagrees on.
func (r *uniboxRepository) FolderRelayTargets(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) ([]models.FolderRelayTarget, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.db.Query(ctx,
		`SELECT ue.email_id, ea.worker_id, ea.provider::text, ue.folder, ue.provider_folder,
		        ue.id, ue.gmail_id, ue.uid, ue.folder_path, ue.message_id, ue.thread_id
		 FROM unibox_emails ue
		 JOIN email_accounts ea ON ea.id = ue.email_id
		 WHERE ue.id = ANY($2) AND ea.organization_id = $1
		   AND ea.worker_id IS NOT NULL AND ea.relay_folder_moves
		   AND NOT EXISTS (
		       SELECT 1 FROM warmup_received wr
		       WHERE wr.email_account_id = ue.email_id AND wr.internal_id = ue.id)
		 ORDER BY ue.email_id`,
		orgID, ids,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.FolderRelayTarget
	for rows.Next() {
		var t models.FolderRelayTarget
		if err := rows.Scan(&t.EmailID, &t.WorkerID, &t.Provider, &t.Folder, &t.Ref.ProviderFolder,
			&t.Ref.ID, &t.Ref.ProviderID, &t.Ref.UID, &t.Ref.FolderPath, &t.Ref.RFCMessageID, &t.Ref.ThreadID); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *uniboxRepository) Delete(ctx context.Context, userID, id uuid.UUID) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM unibox_pending_emails WHERE user_id=$1 AND id=$2`, userID, id); err != nil {
		return err
	}
	// Take a fresh snapshot after waiting for any pending-to-visible transition.
	if _, err := tx.Exec(ctx, `DELETE FROM unibox_emails WHERE user_id=$1 AND id=$2`, userID, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// DeleteByFolderPaths removes the mirror rows for whole source folders. The
// mail stays where it is at the provider; only the platform's copy goes.
// The message map entries go with the rows: the sync reads a mapped
// Message-ID as already stored, so an entry left behind would keep the
// message from ever being imported again if it moved back into a synced
// folder. An arrival still parked on warmup verification is dropped too, or
// it would surface into a folder nobody follows.
func (r *uniboxRepository) DeleteByFolderPaths(ctx context.Context, emailID uuid.UUID, folderPaths []string) (int64, error) {
	if len(folderPaths) == 0 {
		return 0, nil
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		DELETE FROM email_message_map m
		USING unibox_emails u
		WHERE u.email_id = $1 AND u.folder_path = ANY($2)
		  AND m.email_id = u.email_id AND m.message_id = u.message_id`, emailID, folderPaths); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM email_message_map m
		USING unibox_pending_emails p
		WHERE p.email_account_id = $1 AND p.payload->'message'->>'folder_path' = ANY($2)
		  AND m.email_id = p.email_account_id AND m.message_id = p.payload->'message'->>'message_id'`, emailID, folderPaths); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM unibox_pending_emails
		WHERE email_account_id = $1 AND payload->'message'->>'folder_path' = ANY($2)`, emailID, folderPaths); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM unibox_emails WHERE email_id = $1 AND folder_path = ANY($2)`, emailID, folderPaths)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// queryPreviewList executes a query returning preview rows with limit+1 pagination.
func (r *uniboxRepository) queryPreviewList(ctx context.Context, query string, args []any, limit int) (*models.MailSearchResult, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	emails := make([]models.EmailMessageStoreDataPreview, 0, limit)
	for rows.Next() {
		var e models.EmailMessageStoreDataPreview
		if err := rows.Scan(
			&e.ID, &e.EmailID, &e.ThreadID, &e.FromAddr, &e.ToAddr,
			&e.Subject, &e.Snippet, &e.InternalDate, &e.Seen,
		); err != nil {
			return nil, err
		}
		// Message-level paths (thread detail / by-sender) don't collapse,
		// so there are no per-thread aggregates; keep labels [] not null.
		e.Labels = []models.MiniCategory{}
		emails = append(emails, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var hasMore bool
	var nextCursor *string
	if limit > 0 && len(emails) > limit {
		hasMore = true
		// The last row returned: the next page reads strictly past it, so
		// pointing at the probe row instead would skip that row entirely.
		cursor := emails[limit-1].ID.String()
		nextCursor = &cursor
		emails = emails[:limit]
	}

	return &models.MailSearchResult{
		Data: emails,
		Pagination: models.CPagination{
			HasMore:    hasMore,
			NextCursor: nextCursor,
		},
	}, nil
}

// queryThreadList executes a thread-collapsed query. Each row is the
// representative (newest) message of a thread plus the per-thread
// message_count, has_unread flag, and aggregated labels json. Mirrors
// queryPreviewList's limit+1 keyset pagination.
func (r *uniboxRepository) queryThreadList(ctx context.Context, query string, args []any, limit int) (*models.MailSearchResult, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	emails := make([]models.EmailMessageStoreDataPreview, 0, limit)
	for rows.Next() {
		var e models.EmailMessageStoreDataPreview
		var labelsJSON []byte
		if err := rows.Scan(
			&e.ID, &e.EmailID, &e.ThreadID, &e.FromAddr, &e.ToAddr,
			&e.Subject, &e.Snippet, &e.InternalDate, &e.Seen,
			&e.MessageCount, &e.HasUnread, &labelsJSON, &e.CampaignID,
		); err != nil {
			return nil, err
		}
		// Always non-nil so the API marshals labels to [] not null.
		e.Labels = []models.MiniCategory{}
		if len(labelsJSON) > 0 {
			if err := json.Unmarshal(labelsJSON, &e.Labels); err != nil {
				return nil, err
			}
		}
		emails = append(emails, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var hasMore bool
	var nextCursor *string
	if limit > 0 && len(emails) > limit {
		hasMore = true
		// The last row returned: the next page reads strictly past it, so
		// pointing at the probe row instead would skip that row entirely.
		cursor := emails[limit-1].ID.String()
		nextCursor = &cursor
		emails = emails[:limit]
	}

	return &models.MailSearchResult{
		Data: emails,
		Pagination: models.CPagination{
			HasMore:    hasMore,
			NextCursor: nextCursor,
		},
	}, nil
}

// ── Conversation labels ─────────────────────────────────────────────────

// SetThreadLabels replaces the full label set on a thread. Only the
// workspace's own categories are attached (a SELECT-guarded insert), so a
// bogus or another workspace's category_id is silently dropped rather than
// trusted. Idempotent: re-sending the same set is a no-op.
//
// The label belongs to the workspace, like the inbox it hangs off: a teammate
// must see how a conversation was filed. userID records who filed it.
func (r *uniboxRepository) SetThreadLabels(ctx context.Context, orgID, userID uuid.UUID, threadID string, categoryIDs []uuid.UUID) ([]models.MiniCategory, error) {
	if threadID == "" {
		return nil, errors.New("threadID required")
	}
	if categoryIDs == nil {
		categoryIDs = []uuid.UUID{}
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Drop labels no longer in the desired set. With an empty set this
	// clears every label (category_id = ANY('{}') is false → NOT false).
	if _, err := tx.Exec(ctx, `
		DELETE FROM unibox_thread_labels
		WHERE organization_id = $1 AND thread_id = $2 AND NOT (category_id = ANY($3))
	`, orgID, threadID, categoryIDs); err != nil {
		return nil, err
	}

	// Add the rest, but only categories that actually belong to the
	// workspace. ON CONFLICT keeps the upsert idempotent.
	if len(categoryIDs) > 0 {
		var applier any
		if userID != uuid.Nil {
			applier = userID
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO unibox_thread_labels (organization_id, user_id, thread_id, category_id)
			SELECT $1, $2, $3, c.id
			FROM categories c
			WHERE c.organization_id = $1 AND c.id = ANY($4)
			ON CONFLICT (organization_id, thread_id, category_id) DO NOTHING
		`, orgID, applier, threadID, categoryIDs); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return r.ListThreadLabels(ctx, orgID, threadID)
}

// ListThreadLabels returns the conversation's current labels, ordered to
// match the category palette ordering used everywhere else.
func (r *uniboxRepository) ListThreadLabels(ctx context.Context, orgID uuid.UUID, threadID string) ([]models.MiniCategory, error) {
	rows, err := r.db.Query(ctx, `
		SELECT c.id, c.title, c.color
		FROM unibox_thread_labels utl
		JOIN categories c ON c.id = utl.category_id AND c.organization_id = utl.organization_id
		WHERE utl.organization_id = $1 AND utl.thread_id = $2
		ORDER BY c.position ASC, c.title ASC
	`, orgID, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.MiniCategory, 0)
	for rows.Next() {
		var mc models.MiniCategory
		if err := rows.Scan(&mc.ID, &mc.Title, &mc.Color); err != nil {
			return nil, err
		}
		out = append(out, mc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// AddThreadLabels additively attaches labels to a thread (never removes any),
// so an automation/step action can tag a conversation without clobbering labels
// a teammate set by hand. Only the workspace's own categories are attached
// (SELECT-guarded), mirroring SetThreadLabels; a bogus id is silently dropped.
// An automation has no human behind it, so the applier is left NULL.
func (r *uniboxRepository) AddThreadLabels(ctx context.Context, orgID uuid.UUID, threadID string, categoryIDs []uuid.UUID) error {
	if threadID == "" || len(categoryIDs) == 0 {
		return nil
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO unibox_thread_labels (organization_id, thread_id, category_id)
		SELECT $1, $2, c.id
		FROM categories c
		WHERE c.organization_id = $1 AND c.id = ANY($3)
		ON CONFLICT (organization_id, thread_id, category_id) DO NOTHING
	`, orgID, threadID, categoryIDs)
	return err
}

// LatestThreadIDForContact returns the thread id of the most recent conversation
// where the address SENT a message into the workspace's unibox (an inbound reply), or
// "" when there is none. Matching on from_addr (not to_addr) is deliberate: the
// "label email" action only makes sense once the contact has replied, so a
// contact that never responded resolves to "" and the action is a clean no-op.
// Addresses are raw header forms ("Name <a@b.com>" or a bare address); the EXACT
// address is extracted (bareAddrSQL)
// and compared case-insensitively — never a substring contains, so a@b.com does
// not match xa@b.com or a@b.com.evil.
func (r *uniboxRepository) LatestThreadIDForContact(ctx context.Context, orgID uuid.UUID, email string) (string, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return "", nil
	}
	rows, err := r.db.Query(ctx, fmt.Sprintf(`
		SELECT thread_id
		FROM unibox_emails
		WHERE email_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)
		  AND thread_id <> ''
		  AND EXISTS (
			SELECT 1 FROM unnest(from_addr) a
			WHERE %s = lower($2)
		  )
		ORDER BY internal_date DESC
		LIMIT 1
	`, bareAddrSQL("a")), orgID, email)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if rows.Next() {
		var threadID string
		if err := rows.Scan(&threadID); err != nil {
			return "", err
		}
		return threadID, nil
	}
	return "", rows.Err()
}

// LatestMessageIDInThread returns the newest non-empty RFC Message-ID in the
// thread. Scoped through email_accounts so one organization cannot read another
// organization's conversation, matching every other org-scoped unibox read.
func (r *uniboxRepository) LatestMessageIDInThread(ctx context.Context, orgID uuid.UUID, threadID string) (string, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return "", nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT message_id
		FROM unibox_emails
		WHERE thread_id = $2
		  AND message_id <> ''
		  -- A filed forward never reached the people in the conversation.
		  AND NOT filed_forward
		  AND email_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)
		ORDER BY internal_date DESC
		LIMIT 1
	`, orgID, threadID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if rows.Next() {
		var messageID string
		if err := rows.Scan(&messageID); err != nil {
			return "", err
		}
		return messageID, nil
	}
	return "", rows.Err()
}

// ── Snoozes ────────────────────────────────────────────────────────────

func (r *uniboxRepository) UpsertSnoozes(ctx context.Context, orgID, userID uuid.UUID, threadIDs []string, until time.Time) ([]models.UniboxSnooze, error) {
	if len(threadIDs) == 0 {
		return nil, errors.New("threadIDs required")
	}
	rows, err := r.db.Query(ctx, `
		INSERT INTO unibox_snoozes (organization_id, user_id, thread_id, snoozed_until, created_at, updated_at)
		SELECT $4, $1, t, $3, NOW(), NOW() FROM unnest($2::text[]) AS t
		ON CONFLICT (organization_id, user_id, thread_id) DO UPDATE SET
			snoozed_until = EXCLUDED.snoozed_until,
			updated_at    = NOW()
		RETURNING id, user_id, thread_id, snoozed_until, created_at, updated_at
	`, userID, threadIDs, until, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.UniboxSnooze, 0, len(threadIDs))
	for rows.Next() {
		var s models.UniboxSnooze
		if err := rows.Scan(&s.ID, &s.UserID, &s.ThreadID, &s.SnoozedUntil, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *uniboxRepository) DeleteSnoozes(ctx context.Context, orgID, userID uuid.UUID, threadIDs []string) error {
	if len(threadIDs) == 0 {
		return nil
	}
	_, err := r.db.Exec(ctx,
		`DELETE FROM unibox_snoozes WHERE organization_id = $3 AND user_id = $1 AND thread_id = ANY($2)`,
		userID, threadIDs, orgID,
	)
	return err
}

func (r *uniboxRepository) ListSnoozes(ctx context.Context, orgID, userID uuid.UUID) ([]models.UniboxSnooze, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, user_id, thread_id, snoozed_until, created_at, updated_at
		FROM unibox_snoozes
		WHERE organization_id = $2 AND user_id = $1 AND snoozed_until > NOW()
		ORDER BY snoozed_until ASC
	`, userID, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.UniboxSnooze, 0)
	for rows.Next() {
		var s models.UniboxSnooze
		if err := rows.Scan(&s.ID, &s.UserID, &s.ThreadID, &s.SnoozedUntil, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ── Overview ───────────────────────────────────────────────────────────
//
// One statement computes everything the scope rail and metric strip need.
// The organization's messages are read once into a materialized set carrying
// the facts every counter shares (snoozed, automated conversation, pending
// agent draft), and each counter is a hash aggregate over that set.

func (r *uniboxRepository) Overview(ctx context.Context, orgID uuid.UUID) (*models.UniboxOverview, error) {
	return r.overview(ctx, orgID, nil)
}

// OverviewForMailboxes is Overview over only the named mailboxes, for a
// credential limited to them.
func (r *uniboxRepository) OverviewForMailboxes(ctx context.Context, orgID uuid.UUID, accountIDs []uuid.UUID) (*models.UniboxOverview, error) {
	if len(accountIDs) == 0 {
		accountIDs = models.NoneMatch()
	}
	return r.overview(ctx, orgID, accountIDs)
}

// overviewAutomatedOrgWide judges a conversation over every mailbox of the
// organization, as automatedThreadSQL does, when the scanned set is narrower.
const overviewAutomatedOrgWide = `
		auto AS (
			SELECT am.thread_id,
				bool_or(am.automated)
				AND NOT COALESCE(bool_or(NOT am.automated AND am.folder NOT IN ('sent', 'drafts', 'spam', 'trash')), false) AS auto
			FROM unibox_emails am
			WHERE am.email_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)
			  AND am.thread_id IN (SELECT thread_id FROM raw WHERE thread_id <> '')
			GROUP BY am.thread_id
		),`

// overviewAutomatedScanned is the same judgement when the scanned set is the whole organization.
const overviewAutomatedScanned = `
		auto AS (
			SELECT thread_id,
				bool_or(automated)
				AND NOT COALESCE(bool_or(NOT automated AND folder NOT IN ('sent', 'drafts', 'spam', 'trash')), false) AS auto
			FROM raw
			WHERE thread_id <> ''
			GROUP BY thread_id
		),`

func overviewSQL(limited bool) string {
	auto := overviewAutomatedScanned
	if limited {
		auto = overviewAutomatedOrgWide
	}
	// Counts are per THREAD, matching the thread-collapsed list; unthreaded
	// mail is its own conversation. Archived mail counts for All mail and
	// nowhere else, and snoozed conversations stay out of every count but
	// Snoozed, so a badge never counts rows its view then hides.
	return `
		WITH mb AS (
			SELECT id FROM email_accounts
			WHERE organization_id = $1 AND ($4::uuid[] IS NULL OR id = ANY($4::uuid[]))
		),
		raw AS MATERIALIZED (
			SELECT e.id, e.thread_id, e.user_id, e.email_id, e.folder, e.seen, e.automated, e.internal_date, e.from_addr
			FROM unibox_emails e
			WHERE e.email_id IN (SELECT id FROM mb)
		),` + auto + `
		snz AS (
			SELECT DISTINCT user_id, thread_id FROM unibox_snoozes
			WHERE organization_id = $1 AND snoozed_until > NOW()
		),
		agent AS (
			SELECT DISTINCT thread_id FROM ai_thread_drafts
			WHERE organization_id = $1 AND status = 'pending' AND email_account_id IN (SELECT id FROM mb)
		),
		m AS MATERIALIZED (
			SELECT
				COALESCE(NULLIF(raw.thread_id, ''), raw.id::text) AS tkey,
				raw.thread_id, raw.email_id, raw.folder, raw.seen, raw.automated, raw.internal_date, raw.from_addr,
				snz.thread_id IS NOT NULL AS is_snoozed,
				agent.thread_id IS NOT NULL AS has_agent_draft,
				CASE WHEN raw.thread_id = '' THEN raw.automated ELSE COALESCE(auto.auto, false) END AS thread_automated
			FROM raw
			LEFT JOIN snz ON snz.user_id = raw.user_id AND snz.thread_id = raw.thread_id
			LEFT JOIN agent ON agent.thread_id = raw.thread_id
			LEFT JOIN auto ON auto.thread_id = raw.thread_id
		),
		ue AS (
			SELECT tkey, from_addr, internal_date, seen, automated, is_snoozed, has_agent_draft,
				folder = 'drafts' AS is_draft,
				folder IN ` + foldersOutbound + ` AS is_ours,
				folder = 'archive' AS is_archived
			FROM m
			WHERE folder NOT IN ` + foldersOutsideAllMail + `
		),
		threads AS (
			SELECT
				tkey,
				bool_or(is_snoozed)                              AS is_snoozed,
				bool_or(NOT is_archived)                         AS working,
				bool_or((is_draft OR (NOT is_archived AND has_agent_draft)) AND NOT is_snoozed) AS has_draft,
				bool_or(NOT seen) FILTER (WHERE NOT is_archived) AS has_unread,
				max(internal_date) FILTER (WHERE NOT is_archived) AS last_date,
				-- The same rule as automatedThreadSQL, over rows already in hand.
				bool_or(automated) AND NOT COALESCE(bool_or(NOT automated AND NOT is_ours), false) AS is_automated
			FROM ue
			GROUP BY tkey
		),
		-- Byte order is enough to group by; a linguistic sort of every key is what this would cost otherwise.
		latest_per_thread AS (
			SELECT DISTINCT ON (tkey COLLATE "C") tkey, from_addr
			FROM ue
			WHERE NOT is_snoozed AND NOT is_archived
			ORDER BY tkey COLLATE "C", internal_date DESC
		),
		-- A header naming one of our addresses contains its domain, so this cheap test runs before the exact one.
		own_domains AS (
			SELECT DISTINCT '%@' || lower(split_part(email, '@', 2)) || '%' AS pattern
			FROM email_accounts WHERE organization_id = $1
		),
		folder_threads AS (
			SELECT folder, tkey, bool_or(NOT seen) AS unread
			FROM m
			WHERE NOT is_snoozed AND NOT (folder = 'inbox' AND thread_automated)
			GROUP BY folder, tkey
		),
		-- One row per (mailbox, conversation) in the working views; the mailbox and tag counters share it.
		box_threads AS (
			SELECT email_id, tkey, bool_or(NOT seen) AS unread
			FROM m
			WHERE folder NOT IN ` + foldersOutsideWorkingViews + ` AND NOT thread_automated AND NOT is_snoozed
			GROUP BY email_id, tkey
		),
		box_counts AS (
			SELECT email_id, COUNT(*) FILTER (WHERE unread) AS unread, COUNT(*) AS total
			FROM box_threads
			GROUP BY email_id
		),
		tag_counts AS (
			SELECT tag_id, COUNT(*) FILTER (WHERE unread) AS unread, COUNT(*) AS total
			FROM (
				SELECT et.tag_id, bt.tkey, bool_or(bt.unread) AS unread
				FROM box_threads bt
				JOIN email_tags et ON et.email_id = bt.email_id
				GROUP BY et.tag_id, bt.tkey
			) tt
			GROUP BY tag_id
		),
		label_threads AS (
			SELECT thread_id, bool_or(NOT seen) AS has_unread
			FROM m
			WHERE folder NOT IN ` + foldersOutsideWorkingViews + ` AND NOT is_snoozed
			GROUP BY thread_id
		)
		SELECT
			COUNT(*) FILTER (WHERE NOT t.is_snoozed),
			COUNT(*) FILTER (WHERE NOT t.is_snoozed AND t.working AND NOT t.is_automated AND t.has_unread),
			COUNT(*) FILTER (WHERE NOT t.is_snoozed AND t.working AND NOT t.is_automated AND t.last_date >= $2),
			COUNT(*) FILTER (WHERE NOT t.is_snoozed AND t.working AND NOT t.is_automated AND t.last_date >= $3),
			COUNT(*) FILTER (WHERE t.is_snoozed AND t.working),
			COUNT(*) FILTER (WHERE NOT t.is_snoozed AND t.working AND t.is_automated),
			COUNT(*) FILTER (WHERE NOT t.is_snoozed AND t.working AND t.is_automated AND t.has_unread),
			(SELECT COUNT(*) FROM latest_per_thread l
				WHERE CASE WHEN lower(array_to_string(l.from_addr, ' ')) LIKE ANY (SELECT pattern FROM own_domains)
					THEN ` + ownAddressSQL("l.from_addr", "$1") + ` ELSE false END),
			(SELECT COUNT(*) FROM ai_thread_drafts d
				WHERE d.organization_id = $1 AND d.status = 'pending' AND d.email_account_id IN (SELECT id FROM mb)),
			COUNT(*) FILTER (WHERE t.has_draft),
			(SELECT COALESCE(json_agg(json_build_object('folder', f.folder, 'unread', f.unread, 'total', f.total)), '[]')
				FROM (SELECT folder, COUNT(*) FILTER (WHERE unread) AS unread, COUNT(*) AS total
					FROM folder_threads GROUP BY folder) f),
			(SELECT COALESCE(json_agg(json_build_object('id', ea.id, 'email', ea.email, 'name', ea.name,
					'unread', COALESCE(bc.unread, 0), 'total', COALESCE(bc.total, 0)) ORDER BY ea.email), '[]')
				FROM email_accounts ea
				LEFT JOIN box_counts bc ON bc.email_id = ea.id
				WHERE ea.organization_id = $1 AND ea.id IN (SELECT id FROM mb)),
			(SELECT COALESCE(json_agg(json_build_object('id', tg.id, 'title', tg.title, 'color', tg.color,
					'unread', COALESCE(tc.unread, 0), 'total', COALESCE(tc.total, 0)) ORDER BY tg.position, tg.title), '[]')
				FROM tags tg
				LEFT JOIN tag_counts tc ON tc.tag_id = tg.id
				-- A mailbox-limited view lists only the tags on its own mailboxes.
				WHERE tg.organization_id = $1
				  AND ($4::uuid[] IS NULL OR EXISTS (
					SELECT 1 FROM email_tags et2 WHERE et2.tag_id = tg.id AND et2.email_id IN (SELECT id FROM mb)
				  ))),
			(SELECT COALESCE(json_agg(json_build_object('id', cc.id, 'title', cc.title, 'color', cc.color,
					'unread', cc.unread, 'total', cc.total) ORDER BY cc.position, cc.title), '[]')
				FROM (
					SELECT c.id, c.title, c.color, c.position,
						COUNT(*) FILTER (WHERE lt.thread_id IS NOT NULL AND lt.has_unread) AS unread,
						COUNT(*) FILTER (WHERE lt.thread_id IS NOT NULL)                   AS total
					FROM categories c
					LEFT JOIN unibox_thread_labels utl ON utl.category_id = c.id AND utl.organization_id = c.organization_id
					LEFT JOIN label_threads lt ON lt.thread_id = utl.thread_id
					WHERE c.organization_id = $1
					  AND ($4::uuid[] IS NULL OR EXISTS (
						SELECT 1 FROM unibox_thread_labels visible_label
						WHERE visible_label.organization_id = c.organization_id AND visible_label.category_id = c.id
						  AND visible_label.thread_id IN (SELECT thread_id FROM raw)
					  ))
					GROUP BY c.id, c.title, c.color, c.position
				) cc)
		FROM threads t
	`
}

func (r *uniboxRepository) overview(ctx context.Context, orgID uuid.UUID, accountIDs []uuid.UUID) (*models.UniboxOverview, error) {
	now := time.Now().UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	weekStart := todayStart.AddDate(0, 0, -6)

	overview := &models.UniboxOverview{
		GeneratedAt:      now,
		WindowTodayStart: todayStart,
		WindowWeekStart:  weekStart,
	}
	var only any
	if accountIDs != nil {
		only = accountIDs
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// The plan is cheap hash aggregates; JIT compiling it costs more than running it.
	if _, err := tx.Exec(ctx, `SET LOCAL jit = off`); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `SET LOCAL work_mem = '64MB'`); err != nil {
		return nil, err
	}
	var folders, mailboxes, tags, categories []byte
	err = tx.QueryRow(ctx, overviewSQL(accountIDs != nil), orgID, todayStart, weekStart, only).Scan(
		&overview.Total,
		&overview.Unread,
		&overview.Today,
		&overview.Week,
		&overview.Snoozed,
		&overview.Automated,
		&overview.AutomatedUnread,
		&overview.AwaitingReply,
		&overview.AwaitingAgentDraft,
		&overview.Drafts,
		&folders, &mailboxes, &tags, &categories,
	)
	if err != nil {
		return nil, err
	}

	var folderRows []models.UniboxFolderOverview
	overview.Mailboxes = make([]models.UniboxMailboxOverview, 0)
	overview.Tags = make([]models.UniboxTagOverview, 0)
	overview.Categories = make([]models.UniboxCategoryOverview, 0)
	for _, part := range []struct {
		raw []byte
		dst any
	}{{folders, &folderRows}, {mailboxes, &overview.Mailboxes}, {tags, &overview.Tags}, {categories, &overview.Categories}} {
		if err := json.Unmarshal(part.raw, part.dst); err != nil {
			return nil, fmt.Errorf("unibox overview: decode: %w", err)
		}
	}

	// Zero-filled over the canonical folders so the sidebar renders a stable list.
	folderCounts := make(map[string]models.UniboxFolderOverview, len(folderRows))
	for _, f := range folderRows {
		folderCounts[f.Folder] = f
	}
	overview.Folders = make([]models.UniboxFolderOverview, 0, len(models.MailFolders))
	for _, name := range models.MailFolders {
		f := folderCounts[name]
		f.Folder = name
		overview.Folders = append(overview.Folders, f)
	}
	return overview, nil
}

// ListMissingBodyText returns messages whose searchable text was never
// recorded. Keyset-ordered by id so a sweep can walk the table once without
// re-reading rows it has already handled, including the ones whose body turns
// out to be empty and stays that way.
func (r *uniboxRepository) ListMissingBodyText(ctx context.Context, afterID uuid.UUID, limit int) ([]UniboxBodyTarget, error) {
	query := `
		SELECT id, user_id, email_id
		FROM unibox_emails
		WHERE body_text = '' AND id > $1
		ORDER BY id
		LIMIT $2
	`
	rows, err := r.db.Query(ctx, query, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]UniboxBodyTarget, 0, limit)
	for rows.Next() {
		var t UniboxBodyTarget
		if err := rows.Scan(&t.ID, &t.UserID, &t.EmailID); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// uniboxBodyBackfillKey is the admin_settings row holding the backfill's place.
const uniboxBodyBackfillKey = "unibox.body_text_backfill"

// UniboxBodyBackfill is how far the body text backfill has walked, and whether it finished.
type UniboxBodyBackfill struct {
	After uuid.UUID `json:"after"`
	Done  bool      `json:"done"`
}

// BodyBackfillProgress returns the stored place, or the start when none is stored.
func (r *uniboxRepository) BodyBackfillProgress(ctx context.Context) (UniboxBodyBackfill, error) {
	var p UniboxBodyBackfill
	var raw []byte
	err := r.db.QueryRow(ctx, `SELECT value FROM admin_settings WHERE key = $1`, uniboxBodyBackfillKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	return p, json.Unmarshal(raw, &p)
}

// SaveBodyBackfillProgress stores the place reached.
func (r *uniboxRepository) SaveBodyBackfillProgress(ctx context.Context, p UniboxBodyBackfill) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `
		INSERT INTO admin_settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, uniboxBodyBackfillKey, raw)
	return err
}

// SetBodyText records the searchable rendering of a message body.
func (r *uniboxRepository) SetBodyText(ctx context.Context, id uuid.UUID, bodyText string) error {
	_, err := r.db.Exec(ctx, `UPDATE unibox_emails SET body_text = $2 WHERE id = $1`, id, bodyText)
	return err
}

// groundingColumns is the projection both grounding queries share.
const groundingColumns = `id, from_addr, to_addr, subject, body_text, snippet, internal_date`

// GroundingByThread returns a conversation oldest-first for prompt grounding.
func (r *uniboxRepository) GroundingByThread(ctx context.Context, orgID uuid.UUID, threadID string, limit int) ([]models.MessageGrounding, error) {
	query := `
		SELECT ` + groundingColumns + `
		FROM unibox_emails
		WHERE email_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)
		  AND thread_id = $2
		ORDER BY internal_date ASC, id ASC
		LIMIT $3
	`
	return r.scanGrounding(ctx, query, orgID, threadID, limit)
}

// GroundingByAddress returns the most recent correspondence with one address in
// either direction, newest-first, for grounding a brand-new email.
func (r *uniboxRepository) GroundingByAddress(ctx context.Context, orgID uuid.UUID, address string, limit int) ([]models.MessageGrounding, error) {
	query := `
		SELECT ` + groundingColumns + `
		FROM unibox_emails
		WHERE email_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)
		  AND (EXISTS (SELECT 1 FROM unnest(from_addr) AS f(addr) WHERE f.addr ILIKE '%' || $2 || '%')
		    OR EXISTS (SELECT 1 FROM unnest(to_addr) AS t(addr) WHERE t.addr ILIKE '%' || $2 || '%'))
		ORDER BY internal_date DESC, id DESC
		LIMIT $3
	`
	return r.scanGrounding(ctx, query, orgID, address, limit)
}

func (r *uniboxRepository) scanGrounding(ctx context.Context, query string, orgID uuid.UUID, match string, limit int) ([]models.MessageGrounding, error) {
	rows, err := r.db.Query(ctx, query, orgID, match, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.MessageGrounding, 0, limit)
	for rows.Next() {
		var m models.MessageGrounding
		if err := rows.Scan(&m.ID, &m.FromAddr, &m.ToAddr, &m.Subject, &m.BodyText, &m.Snippet, &m.SentAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// prefixTSQuery turns what the user has typed into a prefix tsquery, so a
// half-typed word still matches: "dynow" finds "dynoweb".
//
// Built here rather than in SQL because to_tsquery is a parser, not a matcher:
// an `&`, a `:` or an unbalanced bracket in the input makes it raise, and a
// search box is exactly where those arrive. Everything that is not a letter, a
// digit or a space is dropped, which leaves nothing that can change the shape
// of the query. Returns "" when there is nothing left to search on, and the
// caller then relies on the other matchers.
//
// Terms are ANDed, matching what websearch_to_tsquery does with a bare phrase,
// so adding a word narrows rather than widens.
func prefixTSQuery(input string) string {
	const maxTerms = 8
	if hasWebSearchSyntax(input) {
		return ""
	}

	cleaned := strings.Map(func(r rune) rune {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			return unicode.ToLower(r)
		default:
			return ' '
		}
	}, input)

	terms := strings.Fields(cleaned)
	if len(terms) == 0 {
		return ""
	}
	if len(terms) > maxTerms {
		return ""
	}
	for i, t := range terms {
		terms[i] = t + ":*"
	}
	return strings.Join(terms, " & ")
}

func hasWebSearchSyntax(input string) bool {
	if strings.Contains(input, `"`) {
		return true
	}
	for _, term := range strings.Fields(input) {
		if strings.HasPrefix(term, "-") || strings.EqualFold(term, "OR") {
			return true
		}
	}
	return false
}

func participantSearchable(input string) bool {
	consecutive := 0
	for _, r := range input {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			consecutive++
			if consecutive >= 3 {
				return true
			}
		} else {
			consecutive = 0
		}
	}
	return false
}

func escapeLikePattern(input string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(input)
}
