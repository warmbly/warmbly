package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

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
}

type UniboxRepository interface {
	CreateEntry(ctx context.Context, userID uuid.UUID, e *models.EmailMessageStoreData) error
	UpdateEntry(ctx context.Context, userID, emailID, id uuid.UUID, e *UpdateUniboxEntry) error
	GetIncoming(ctx context.Context, userID uuid.UUID, limit int, cursor string) (*models.MailSearchResult, error)
	GetByID(ctx context.Context, userID, id uuid.UUID) (*models.EmailMessageStoreData, error)
	// GetByIDForOrg is the org-scoped read for the unibox detail view: any
	// member with unibox access can open a message in the org-wide list. It
	// returns the row plus the mailbox OWNER's user_id, which the S3 body key
	// is built from (emails/<ownerID>/<id>), so the body still resolves under
	// the owner even when a different teammate opens it.
	GetByIDForOrg(ctx context.Context, orgID, id uuid.UUID) (*models.EmailMessageStoreData, uuid.UUID, error)
	GetByThread(ctx context.Context, orgID, emailID uuid.UUID, threadID string, limit int, cursor string) (*models.MailSearchResult, error)
	GetBySender(ctx context.Context, userID uuid.UUID, sender string, limit int, cursor string) (*models.MailSearchResult, error)
	Search(ctx context.Context, orgID, userID uuid.UUID, params *models.MailSearchParams) (*models.MailSearchResult, error)
	GetUnseenCount(ctx context.Context, orgID uuid.UUID, emailAccountID *uuid.UUID) (int64, error)
	MarkSeen(ctx context.Context, userID, id uuid.UUID, seen bool) error
	MarkSeenBulk(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID, seen bool) error
	Delete(ctx context.Context, userID, id uuid.UUID) error

	// Snooze: per (user, thread). UpsertSnooze adopts the new
	// snoozed_until even if one already exists; DeleteSnooze removes
	// the row outright (instant un-snooze). ListSnoozes returns the
	// active set for the user.
	UpsertSnooze(ctx context.Context, userID uuid.UUID, threadID string, until time.Time) (*models.UniboxSnooze, error)
	DeleteSnooze(ctx context.Context, userID uuid.UUID, threadID string) error
	ListSnoozes(ctx context.Context, userID uuid.UUID) ([]models.UniboxSnooze, error)

	// Overview powers the scope rail + top metric strip. Single call
	// so the client doesn't fan out N+M queries for each mailbox/tag.
	Overview(ctx context.Context, orgID uuid.UUID) (*models.UniboxOverview, error)

	// Conversation labels. SetThreadLabels replaces the full label set
	// on a thread (idempotent PUT semantics, only the user's own
	// categories are attached). ListThreadLabels returns the current
	// set for one thread.
	SetThreadLabels(ctx context.Context, userID uuid.UUID, threadID string, categoryIDs []uuid.UUID) ([]models.MiniCategory, error)
	ListThreadLabels(ctx context.Context, userID uuid.UUID, threadID string) ([]models.MiniCategory, error)
	// AddThreadLabels attaches labels to a thread WITHOUT removing existing ones
	// (additive; for automation/step "label email" actions). LatestThreadIDForContact
	// finds the user's most recent conversation with an address, so a campaign
	// step that knows the contact but not the thread can still label it.
	AddThreadLabels(ctx context.Context, userID uuid.UUID, threadID string, categoryIDs []uuid.UUID) error
	LatestThreadIDForContact(ctx context.Context, userID uuid.UUID, email string) (string, error)
}

type uniboxRepository struct {
	db *db.DB
}

func NewUniboxRepository(db *db.DB) UniboxRepository {
	return &uniboxRepository{db: db}
}

var mailFieldsFull = []string{
	"id", "email_id", "mailbox", "thread_id", "message_id",
	"gmail_id", "parent_id", "uid", "mod_seq",
	"flags", "bcc", "cc", "from_addr", "in_reply_to", "reply_to",
	"to_addr", "subject", "size", "internal_date", "sent_date",
	"snippet", "seen", "updated_at", "created_at",
}

var mailFieldsPreview = []string{
	"id", "email_id", "thread_id", "from_addr", "to_addr",
	"subject", "snippet", "internal_date", "seen",
}

func (r *uniboxRepository) CreateEntry(ctx context.Context, userID uuid.UUID, e *models.EmailMessageStoreData) error {
	query := `
		INSERT INTO unibox_emails (
			id, user_id, email_id, mailbox, thread_id, message_id,
			gmail_id, parent_id, uid, mod_seq,
			flags, bcc, cc, from_addr, in_reply_to, reply_to,
			to_addr, subject, size, internal_date, sent_date,
			snippet, seen, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16,
			$17, $18, $19, $20, $21,
			$22, $23, $24, $25
		)
		ON CONFLICT (id) DO NOTHING
	`

	_, err := r.db.Exec(ctx, query,
		e.ID, userID, e.EmailID, e.Mailbox, e.ThreadID, e.MessageID,
		e.GmailID, e.ParentID, e.UID, e.ModSeq,
		e.Flags, e.BCC, e.CC, e.FromAddr, e.InReplyTo, e.ReplyTo,
		e.ToAddr, e.Subject, e.Size, e.InternalDate, e.SentDate,
		e.Snippet, e.Seen, e.CreatedAt, e.UpdatedAt,
	)
	return err
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
					SELECT internal_date, id FROM unibox_emails WHERE id = $%d
				)`, argPos)
			args = append(args, cursorID)
			argPos++
		}
	}

	query += fmt.Sprintf(` ORDER BY internal_date DESC, id DESC LIMIT $%d`, argPos)
	args = append(args, limit+1)

	return r.queryPreviewList(ctx, query, args, limit)
}

func (r *uniboxRepository) GetByID(ctx context.Context, userID, id uuid.UUID) (*models.EmailMessageStoreData, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM unibox_emails
		WHERE user_id = $1 AND id = $2
	`, strings.Join(mailFieldsFull, ", "))

	var e models.EmailMessageStoreData
	err := r.db.QueryRow(ctx, query, userID, id).Scan(
		&e.ID, &e.EmailID, &e.Mailbox, &e.ThreadID, &e.MessageID,
		&e.GmailID, &e.ParentID, &e.UID, &e.ModSeq,
		&e.Flags, &e.BCC, &e.CC, &e.FromAddr, &e.InReplyTo, &e.ReplyTo,
		&e.ToAddr, &e.Subject, &e.Size, &e.InternalDate, &e.SentDate,
		&e.Snippet, &e.Seen, &e.UpdatedAt, &e.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("email not found")
		}
		return nil, err
	}

	// Auto-mark as seen
	if !e.Seen {
		_ = r.MarkSeen(ctx, userID, id, true)
		e.Seen = true
	}

	return &e, nil
}

// GetByIDForOrg reads a single message scoped to the org's mailboxes (not the
// caller's user_id), mirroring GetByThread, so a non-owner teammate who sees a
// message in the org-scoped list can open it. It also returns the row's owner
// user_id: the S3 body key is built from the owner (emails/<ownerID>/<id>), so
// the caller must fetch the body under the owner, not under itself.
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
		&e.ID, &e.EmailID, &e.Mailbox, &e.ThreadID, &e.MessageID,
		&e.GmailID, &e.ParentID, &e.UID, &e.ModSeq,
		&e.Flags, &e.BCC, &e.CC, &e.FromAddr, &e.InReplyTo, &e.ReplyTo,
		&e.ToAddr, &e.Subject, &e.Size, &e.InternalDate, &e.SentDate,
		&e.Snippet, &e.Seen, &e.UpdatedAt, &e.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, uuid.Nil, fmt.Errorf("email not found")
		}
		return nil, uuid.Nil, err
	}

	// Auto-mark as seen, org-scoped so any member clears the shared unread state.
	if !e.Seen {
		_ = r.MarkSeenBulk(ctx, orgID, []uuid.UUID{id}, true)
		e.Seen = true
	}

	return &e, ownerID, nil
}

// GetByThread returns the messages in a thread. emailID is optional —
// pass uuid.Nil to span every mailbox in the organization (the typical
// unified-inbox case where the caller only knows the thread). Scoped by org,
// not user_id, so any member with unibox access sees the whole conversation,
// matching the org-scoped inbox list.
func (r *uniboxRepository) GetByThread(ctx context.Context, orgID, emailID uuid.UUID, threadID string, limit int, cursor string) (*models.MailSearchResult, error) {
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

	if cursor != "" {
		cursorID, err := uuid.Parse(cursor)
		if err == nil {
			query += fmt.Sprintf(`
				AND (internal_date, id) < (
					SELECT internal_date, id FROM unibox_emails WHERE id = $%d
				)`, argPos)
			args = append(args, cursorID)
			argPos++
		}
	}

	// Thread display reads oldest-first so each reply renders below the
	// message it replied to.
	query += fmt.Sprintf(` ORDER BY internal_date ASC, id ASC LIMIT $%d`, argPos)
	args = append(args, limit+1)

	return r.queryPreviewList(ctx, query, args, limit)
}

func (r *uniboxRepository) GetBySender(ctx context.Context, userID uuid.UUID, sender string, limit int, cursor string) (*models.MailSearchResult, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM unibox_emails
		WHERE user_id = $1 AND $2 = ANY(from_addr)
	`, strings.Join(mailFieldsPreview, ", "))

	args := []any{userID, sender}
	argPos := 3

	if cursor != "" {
		cursorID, err := uuid.Parse(cursor)
		if err == nil {
			query += fmt.Sprintf(`
				AND (internal_date, id) < (
					SELECT internal_date, id FROM unibox_emails WHERE id = $%d
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
//     sender, mailbox) run inside the windowed subquery, so the
//     representative + counts reflect the matched messages; with no
//     content filter (the default inbox) that's the whole thread.
//   - thread/representative-level filters (awaiting reply, category)
//     and keyset pagination run on the collapsed row.
func (r *uniboxRepository) Search(ctx context.Context, orgID, userID uuid.UUID, params *models.MailSearchParams) (*models.MailSearchResult, error) {
	previewCols := make([]string, len(mailFieldsPreview))
	for i, c := range mailFieldsPreview {
		previewCols[i] = "ue." + c
	}

	// $1 = orgID (scope mail to the workspace's mailboxes); $2 = userID
	// (per-user thread labels stay personal). Dynamic filters start at $3.
	args := []any{orgID, userID}
	argPos := 3

	// ── Inner windowed subquery: row-level filters + per-thread aggs ──
	// Partition by the thread, but treat an empty thread_id (the column
	// default for mail with no usable threading headers) as its own
	// singleton keyed by row id — otherwise every unthreaded message
	// would collapse into one bogus "conversation".
	inner := fmt.Sprintf(`
		SELECT %s,
			ROW_NUMBER() OVER (PARTITION BY COALESCE(NULLIF(ue.thread_id, ''), ue.id::text) ORDER BY ue.internal_date DESC, ue.id DESC) AS rn,
			COUNT(*)              OVER (PARTITION BY COALESCE(NULLIF(ue.thread_id, ''), ue.id::text)) AS message_count,
			bool_or(NOT ue.seen)  OVER (PARTITION BY COALESCE(NULLIF(ue.thread_id, ''), ue.id::text)) AS has_unread
		FROM unibox_emails ue
		WHERE ue.email_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)`, strings.Join(previewCols, ", "))

	// Snooze handling. nil = exclude snoozed (the inbox default), so
	// threads with an active snooze never appear unless asked for.
	switch {
	case params.Snoozed == nil:
		inner += `
			AND NOT EXISTS (
				SELECT 1 FROM unibox_snoozes s
				WHERE s.user_id = ue.user_id
				  AND s.thread_id = ue.thread_id
				  AND s.snoozed_until > NOW()
			)`
	case params.Snoozed != nil && *params.Snoozed:
		inner += `
			AND EXISTS (
				SELECT 1 FROM unibox_snoozes s
				WHERE s.user_id = ue.user_id
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

	if params.Until != nil {
		inner += fmt.Sprintf(` AND ue.internal_date <= $%d`, argPos)
		args = append(args, *params.Until)
		argPos++
	}

	if params.Subject != nil && *params.Subject != "" {
		inner += fmt.Sprintf(` AND ue.search_tsv @@ plainto_tsquery('english', $%d)`, argPos)
		args = append(args, *params.Subject)
		argPos++
	}

	if params.Sender != nil && *params.Sender != "" {
		inner += fmt.Sprintf(` AND $%d = ANY(ue.from_addr)`, argPos)
		args = append(args, *params.Sender)
		argPos++
	}

	if len(params.EmailAccountIDs) > 0 {
		inner += fmt.Sprintf(` AND ue.email_id = ANY($%d)`, argPos)
		args = append(args, params.EmailAccountIDs)
		argPos++
	}

	// ── Outer: pick the representative row + thread-level filters ─────
	// b.<col> exposes the preview columns; labels are aggregated per
	// thread so the row renders its chips without a second round trip.
	outerCols := make([]string, len(mailFieldsPreview))
	for i, c := range mailFieldsPreview {
		outerCols[i] = "b." + c
	}
	query := fmt.Sprintf(`
		SELECT %s, b.message_count, b.has_unread,
			COALESCE(
				(
					SELECT json_agg(json_build_object('id', c.id, 'title', c.title, 'color', c.color) ORDER BY c.position ASC, c.title ASC)
					FROM unibox_thread_labels utl
					JOIN categories c ON c.id = utl.category_id
					WHERE utl.user_id = $2 AND utl.thread_id = b.thread_id
				), '[]'::json
			) AS labels
		FROM (%s) b
		WHERE b.rn = 1`, strings.Join(outerCols, ", "), inner)

	// Awaiting reply: the representative (latest) message is from one of
	// the user's own mailboxes — i.e. they're waiting on the recipient.
	if params.AwaitingReply != nil && *params.AwaitingReply {
		query += `
			AND EXISTS (
				SELECT 1 FROM email_accounts ea
				WHERE ea.organization_id = $1
				  AND ea.email = ANY(b.from_addr)
			)`
	}

	if len(params.CategoryIDs) > 0 {
		query += fmt.Sprintf(`
			AND EXISTS (
				SELECT 1 FROM unibox_thread_labels utl
				WHERE utl.user_id = $2
				  AND utl.thread_id = b.thread_id
				  AND utl.category_id = ANY($%d)
			)`, argPos)
		args = append(args, params.CategoryIDs)
		argPos++
	}

	if params.Cursor != "" {
		cursorID, err := uuid.Parse(params.Cursor)
		if err == nil {
			query += fmt.Sprintf(`
				AND (b.internal_date, b.id) < (
					SELECT internal_date, id FROM unibox_emails WHERE id = $%d
				)`, argPos)
			args = append(args, cursorID)
			argPos++
		}
	}

	query += fmt.Sprintf(` ORDER BY b.internal_date DESC, b.id DESC LIMIT $%d`, argPos)
	args = append(args, params.PageSize+1)

	return r.queryThreadList(ctx, query, args, params.PageSize)
}

func (r *uniboxRepository) GetUnseenCount(ctx context.Context, orgID uuid.UUID, emailAccountID *uuid.UUID) (int64, error) {
	var count int64

	// Count unread THREADS (distinct, empty-thread-safe), not messages,
	// so the badge agrees with the collapsed list + Overview.Unread.
	if emailAccountID != nil {
		err := r.db.QueryRow(ctx,
			`SELECT COUNT(DISTINCT COALESCE(NULLIF(thread_id, ''), id::text))
			 FROM unibox_emails
			 WHERE email_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)
			   AND email_id = $2 AND seen = FALSE`,
			orgID, *emailAccountID,
		).Scan(&count)
		return count, err
	}

	err := r.db.QueryRow(ctx,
		`SELECT COUNT(DISTINCT COALESCE(NULLIF(thread_id, ''), id::text))
		 FROM unibox_emails
		 WHERE email_id IN (SELECT id FROM email_accounts WHERE organization_id = $1) AND seen = FALSE`,
		orgID,
	).Scan(&count)
	return count, err
}

func (r *uniboxRepository) MarkSeen(ctx context.Context, userID, id uuid.UUID, seen bool) error {
	_, err := r.db.Exec(ctx,
		`UPDATE unibox_emails SET seen = $1, updated_at = NOW() WHERE user_id = $2 AND id = $3`,
		seen, userID, id,
	)
	return err
}

func (r *uniboxRepository) MarkSeenBulk(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID, seen bool) error {
	if len(ids) == 0 {
		return nil
	}
	// Org-scoped so any member with unibox access can clear the shared inbox's
	// unread state, not only the mailbox owner. The unread count is org-wide, so
	// a user_id filter would leave the badge stuck for non-owner members. ANY($3)
	// also covers the single-id case.
	_, err := r.db.Exec(ctx,
		`UPDATE unibox_emails SET seen = $1, updated_at = NOW()
		 WHERE id = ANY($3) AND email_id IN (SELECT id FROM email_accounts WHERE organization_id = $2)`,
		seen, orgID, ids,
	)
	return err
}

func (r *uniboxRepository) Delete(ctx context.Context, userID, id uuid.UUID) error {
	_, err := r.db.Exec(ctx,
		`DELETE FROM unibox_emails WHERE user_id = $1 AND id = $2`,
		userID, id,
	)
	return err
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

	var hasMore bool
	var nextCursor *string
	if len(emails) > limit {
		hasMore = true
		cursor := emails[limit].ID.String()
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
			&e.MessageCount, &e.HasUnread, &labelsJSON,
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

	var hasMore bool
	var nextCursor *string
	if len(emails) > limit {
		hasMore = true
		cursor := emails[limit].ID.String()
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
// user's own categories are attached (a SELECT-guarded insert), so a
// bogus or someone else's category_id is silently dropped rather than
// trusted. Idempotent: re-sending the same set is a no-op.
func (r *uniboxRepository) SetThreadLabels(ctx context.Context, userID uuid.UUID, threadID string, categoryIDs []uuid.UUID) ([]models.MiniCategory, error) {
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
		WHERE user_id = $1 AND thread_id = $2 AND NOT (category_id = ANY($3))
	`, userID, threadID, categoryIDs); err != nil {
		return nil, err
	}

	// Add the rest, but only categories that actually belong to the
	// user. ON CONFLICT keeps the upsert idempotent.
	if len(categoryIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO unibox_thread_labels (user_id, thread_id, category_id)
			SELECT $1, $2, c.id
			FROM categories c
			WHERE c.user_id = $1 AND c.id = ANY($3)
			ON CONFLICT (user_id, thread_id, category_id) DO NOTHING
		`, userID, threadID, categoryIDs); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return r.ListThreadLabels(ctx, userID, threadID)
}

// ListThreadLabels returns the conversation's current labels, ordered to
// match the category palette ordering used everywhere else.
func (r *uniboxRepository) ListThreadLabels(ctx context.Context, userID uuid.UUID, threadID string) ([]models.MiniCategory, error) {
	rows, err := r.db.Query(ctx, `
		SELECT c.id, c.title, c.color
		FROM unibox_thread_labels utl
		JOIN categories c ON c.id = utl.category_id
		WHERE utl.user_id = $1 AND utl.thread_id = $2
		ORDER BY c.position ASC, c.title ASC
	`, userID, threadID)
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
	return out, nil
}

// AddThreadLabels additively attaches labels to a thread (never removes any),
// so an automation/step action can tag a conversation without clobbering labels
// a teammate set by hand. Only the user's own categories are attached
// (SELECT-guarded), mirroring SetThreadLabels; a bogus id is silently dropped.
func (r *uniboxRepository) AddThreadLabels(ctx context.Context, userID uuid.UUID, threadID string, categoryIDs []uuid.UUID) error {
	if threadID == "" || len(categoryIDs) == 0 {
		return nil
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO unibox_thread_labels (user_id, thread_id, category_id)
		SELECT $1, $2, c.id
		FROM categories c
		WHERE c.user_id = $1 AND c.id = ANY($3)
		ON CONFLICT (user_id, thread_id, category_id) DO NOTHING
	`, userID, threadID, categoryIDs)
	return err
}

// LatestThreadIDForContact returns the thread id of the most recent conversation
// where the address SENT a message into the user's unibox (an inbound reply), or
// "" when there is none. Matching on from_addr (not to_addr) is deliberate: the
// "label email" action only makes sense once the contact has replied, so a
// contact that never responded resolves to "" and the action is a clean no-op.
// Addresses are raw header forms ("Name <a@b.com>" or a bare address); the EXACT
// address is extracted (the text inside angle brackets, else the trimmed value)
// and compared case-insensitively — never a substring contains, so a@b.com does
// not match xa@b.com or a@b.com.evil.
func (r *uniboxRepository) LatestThreadIDForContact(ctx context.Context, userID uuid.UUID, email string) (string, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return "", nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT thread_id
		FROM unibox_emails
		WHERE user_id = $1 AND thread_id <> ''
		  AND EXISTS (
			SELECT 1 FROM unnest(from_addr) a
			WHERE lower(coalesce(substring(a from '<([^>]*)>'), btrim(a))) = lower($2)
		  )
		ORDER BY internal_date DESC
		LIMIT 1
	`, userID, email)
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

// ── Snoozes ────────────────────────────────────────────────────────────

func (r *uniboxRepository) UpsertSnooze(ctx context.Context, userID uuid.UUID, threadID string, until time.Time) (*models.UniboxSnooze, error) {
	if threadID == "" {
		return nil, errors.New("threadID required")
	}
	row := r.db.QueryRow(ctx, `
		INSERT INTO unibox_snoozes (user_id, thread_id, snoozed_until, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
		ON CONFLICT (user_id, thread_id) DO UPDATE SET
			snoozed_until = EXCLUDED.snoozed_until,
			updated_at    = NOW()
		RETURNING id, user_id, thread_id, snoozed_until, created_at, updated_at
	`, userID, threadID, until)

	var s models.UniboxSnooze
	if err := row.Scan(&s.ID, &s.UserID, &s.ThreadID, &s.SnoozedUntil, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *uniboxRepository) DeleteSnooze(ctx context.Context, userID uuid.UUID, threadID string) error {
	_, err := r.db.Exec(ctx,
		`DELETE FROM unibox_snoozes WHERE user_id = $1 AND thread_id = $2`,
		userID, threadID,
	)
	return err
}

func (r *uniboxRepository) ListSnoozes(ctx context.Context, userID uuid.UUID) ([]models.UniboxSnooze, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, user_id, thread_id, snoozed_until, created_at, updated_at
		FROM unibox_snoozes
		WHERE user_id = $1 AND snoozed_until > NOW()
		ORDER BY snoozed_until ASC
	`, userID)
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
	return out, nil
}

// ── Overview ───────────────────────────────────────────────────────────
//
// One round trip computes everything the scope rail + top metric strip
// needs. We use CTEs so each metric is a single sequential scan rather
// than N+M queries from the client.

func (r *uniboxRepository) Overview(ctx context.Context, orgID uuid.UUID) (*models.UniboxOverview, error) {
	now := time.Now().UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	weekStart := todayStart.AddDate(0, 0, -6)

	overview := &models.UniboxOverview{
		GeneratedAt:      now,
		WindowTodayStart: todayStart,
		WindowWeekStart:  weekStart,
	}

	// Single aggregate over unibox_emails — cheap. Counts are per
	// THREAD (matching the thread-collapsed list), not per message, so
	// the rail numbers line up with the rows the user sees. Empty
	// thread_ids fall back to row id so unthreaded mail counts as its
	// own conversation.
	err := r.db.QueryRow(ctx, `
		WITH ue AS (
			SELECT
				COALESCE(NULLIF(e.thread_id, ''), e.id::text) AS tkey,
				e.from_addr,
				e.internal_date,
				e.seen,
				EXISTS (
					SELECT 1 FROM unibox_snoozes s
					WHERE s.user_id = e.user_id
					  AND s.thread_id = e.thread_id
					  AND s.snoozed_until > NOW()
				) AS is_snoozed
			FROM unibox_emails e
			WHERE e.email_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)
		),
		threads AS (
			SELECT
				tkey,
				bool_or(is_snoozed) AS is_snoozed,
				bool_or(NOT seen)   AS has_unread,
				max(internal_date)  AS last_date
			FROM ue
			GROUP BY tkey
		),
		latest_per_thread AS (
			SELECT DISTINCT ON (tkey) tkey, from_addr
			FROM ue
			WHERE NOT is_snoozed
			ORDER BY tkey, internal_date DESC
		),
		user_mailbox_emails AS (
			SELECT email FROM email_accounts WHERE organization_id = $1
		)
		SELECT
			COUNT(*) FILTER (WHERE NOT t.is_snoozed)                            AS total,
			COUNT(*) FILTER (WHERE NOT t.is_snoozed AND t.has_unread)           AS unread,
			COUNT(*) FILTER (WHERE NOT t.is_snoozed AND t.last_date >= $2)      AS today,
			COUNT(*) FILTER (WHERE NOT t.is_snoozed AND t.last_date >= $3)      AS week,
			COUNT(*) FILTER (WHERE t.is_snoozed)                                AS snoozed,
			(SELECT COUNT(*) FROM latest_per_thread l
				WHERE EXISTS (SELECT 1 FROM user_mailbox_emails u WHERE u.email = ANY(l.from_addr)))             AS awaiting
		FROM threads t
	`, orgID, todayStart, weekStart).Scan(
		&overview.Total,
		&overview.Unread,
		&overview.Today,
		&overview.Week,
		&overview.Snoozed,
		&overview.AwaitingReply,
	)
	if err != nil {
		return nil, err
	}

	// Per-mailbox counters. LEFT JOIN against unibox_emails so empty
	// mailboxes still show up in the rail with a zero count. Counts are
	// per THREAD (distinct thread key, empty-thread-safe) to match the
	// collapsed list, not per message.
	mailboxRows, err := r.db.Query(ctx, `
		SELECT
			ea.id,
			ea.email,
			ea.name,
			COUNT(DISTINCT COALESCE(NULLIF(ue.thread_id, ''), ue.id::text)) FILTER (WHERE ue.id IS NOT NULL AND NOT ue.seen
				AND NOT EXISTS (
					SELECT 1 FROM unibox_snoozes s
					WHERE s.user_id = ea.user_id AND s.thread_id = ue.thread_id AND s.snoozed_until > NOW()
				)) AS unread,
			COUNT(DISTINCT COALESCE(NULLIF(ue.thread_id, ''), ue.id::text)) FILTER (WHERE ue.id IS NOT NULL
				AND NOT EXISTS (
					SELECT 1 FROM unibox_snoozes s
					WHERE s.user_id = ea.user_id AND s.thread_id = ue.thread_id AND s.snoozed_until > NOW()
				)) AS total
		FROM email_accounts ea
		LEFT JOIN unibox_emails ue ON ue.email_id = ea.id AND ue.user_id = ea.user_id
		WHERE ea.organization_id = $1
		GROUP BY ea.id, ea.email, ea.name
		ORDER BY ea.email ASC
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer mailboxRows.Close()

	overview.Mailboxes = make([]models.UniboxMailboxOverview, 0)
	for mailboxRows.Next() {
		var m models.UniboxMailboxOverview
		if err := mailboxRows.Scan(&m.ID, &m.Email, &m.Name, &m.Unread, &m.Total); err != nil {
			return nil, err
		}
		overview.Mailboxes = append(overview.Mailboxes, m)
	}

	// Per-tag counters. Mailbox tags live in `tags` + `email_tags`.
	// Per THREAD (distinct thread key) so threads aren't over-counted by
	// message multiplicity or the email_tags fan-out.
	tagRows, err := r.db.Query(ctx, `
		SELECT
			t.id,
			t.title,
			t.color,
			COUNT(DISTINCT COALESCE(NULLIF(ue.thread_id, ''), ue.id::text)) FILTER (WHERE ue.id IS NOT NULL AND NOT ue.seen
				AND NOT EXISTS (
					SELECT 1 FROM unibox_snoozes s
					WHERE s.user_id = t.user_id AND s.thread_id = ue.thread_id AND s.snoozed_until > NOW()
				)) AS unread,
			COUNT(DISTINCT COALESCE(NULLIF(ue.thread_id, ''), ue.id::text)) FILTER (WHERE ue.id IS NOT NULL
				AND NOT EXISTS (
					SELECT 1 FROM unibox_snoozes s
					WHERE s.user_id = t.user_id AND s.thread_id = ue.thread_id AND s.snoozed_until > NOW()
				)) AS total
		FROM tags t
		LEFT JOIN email_tags et ON et.tag_id = t.id
		LEFT JOIN email_accounts ea ON ea.id = et.email_id AND ea.user_id = t.user_id
		LEFT JOIN unibox_emails ue ON ue.email_id = ea.id AND ue.user_id = ea.user_id
		WHERE t.user_id = $1
		GROUP BY t.id, t.title, t.color, t.position
		ORDER BY t.position ASC, t.title ASC
	`, orgID)
	if err != nil {
		// Tags are optional; never let an empty tag join take the
		// whole overview down.
		overview.Tags = []models.UniboxTagOverview{}
		return overview, nil
	}
	defer tagRows.Close()

	overview.Tags = make([]models.UniboxTagOverview, 0)
	for tagRows.Next() {
		var t models.UniboxTagOverview
		if err := tagRows.Scan(&t.ID, &t.Title, &t.Color, &t.Unread, &t.Total); err != nil {
			return nil, err
		}
		overview.Tags = append(overview.Tags, t)
	}

	// Per-conversation-label counters. total / unread count THREADS (one
	// row per (thread,category) in unibox_thread_labels), restricted to
	// threads that still have a non-snoozed message. unread = the thread
	// has any unseen message. Like tags, labels are optional — never let
	// the join fail the whole overview.
	overview.Categories = make([]models.UniboxCategoryOverview, 0)
	catRows, err := r.db.Query(ctx, `
		WITH thread_state AS (
			SELECT e.user_id, e.thread_id, bool_or(NOT e.seen) AS has_unread
			FROM unibox_emails e
			WHERE e.user_id = $1
			  AND NOT EXISTS (
				SELECT 1 FROM unibox_snoozes s
				WHERE s.user_id = e.user_id AND s.thread_id = e.thread_id AND s.snoozed_until > NOW()
			  )
			GROUP BY e.user_id, e.thread_id
		)
		SELECT
			c.id, c.title, c.color,
			COUNT(*) FILTER (WHERE ts.thread_id IS NOT NULL AND ts.has_unread) AS unread,
			COUNT(*) FILTER (WHERE ts.thread_id IS NOT NULL)                   AS total
		FROM categories c
		LEFT JOIN unibox_thread_labels utl ON utl.category_id = c.id AND utl.user_id = c.user_id
		LEFT JOIN thread_state ts ON ts.user_id = utl.user_id AND ts.thread_id = utl.thread_id
		WHERE c.user_id = $1
		GROUP BY c.id, c.title, c.color, c.position
		ORDER BY c.position ASC, c.title ASC
	`, orgID)
	if err != nil {
		return overview, nil
	}
	defer catRows.Close()

	for catRows.Next() {
		var cat models.UniboxCategoryOverview
		if err := catRows.Scan(&cat.ID, &cat.Title, &cat.Color, &cat.Unread, &cat.Total); err != nil {
			return overview, nil
		}
		overview.Categories = append(overview.Categories, cat)
	}

	return overview, nil
}
