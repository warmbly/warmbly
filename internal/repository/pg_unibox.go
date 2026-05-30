package repository

import (
	"context"
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
	GetByThread(ctx context.Context, userID, emailID uuid.UUID, threadID string, limit int, cursor string) (*models.MailSearchResult, error)
	GetBySender(ctx context.Context, userID uuid.UUID, sender string, limit int, cursor string) (*models.MailSearchResult, error)
	Search(ctx context.Context, userID uuid.UUID, params *models.MailSearchParams) (*models.MailSearchResult, error)
	GetUnseenCount(ctx context.Context, userID uuid.UUID, emailAccountID *uuid.UUID) (int64, error)
	MarkSeen(ctx context.Context, userID, id uuid.UUID, seen bool) error
	MarkSeenBulk(ctx context.Context, userID uuid.UUID, ids []uuid.UUID, seen bool) error
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
	Overview(ctx context.Context, userID uuid.UUID) (*models.UniboxOverview, error)
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

// GetByThread returns the messages in a thread. emailID is optional —
// pass uuid.Nil to span every mailbox the user owns (the typical
// unified-inbox case where the caller only knows the thread).
func (r *uniboxRepository) GetByThread(ctx context.Context, userID, emailID uuid.UUID, threadID string, limit int, cursor string) (*models.MailSearchResult, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM unibox_emails
		WHERE user_id = $1 AND thread_id = $2
	`, strings.Join(mailFieldsPreview, ", "))

	args := []any{userID, threadID}
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

func (r *uniboxRepository) Search(ctx context.Context, userID uuid.UUID, params *models.MailSearchParams) (*models.MailSearchResult, error) {
	// Build a list of `ue.<col>` references so we can table-alias the
	// rows when we join in snoozes / awaiting-reply scopes.
	previewCols := make([]string, len(mailFieldsPreview))
	for i, c := range mailFieldsPreview {
		previewCols[i] = "ue." + c
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM unibox_emails ue
		WHERE ue.user_id = $1
	`, strings.Join(previewCols, ", "))

	args := []any{userID}
	argPos := 2

	// Snooze handling. nil = exclude snoozed (the inbox default), so
	// rows whose thread has an active snooze never appear unless the
	// caller asks for them explicitly.
	switch {
	case params.Snoozed == nil:
		query += fmt.Sprintf(`
			AND NOT EXISTS (
				SELECT 1 FROM unibox_snoozes s
				WHERE s.user_id = ue.user_id
				  AND s.thread_id = ue.thread_id
				  AND s.snoozed_until > NOW()
			)`)
	case params.Snoozed != nil && *params.Snoozed:
		query += fmt.Sprintf(`
			AND EXISTS (
				SELECT 1 FROM unibox_snoozes s
				WHERE s.user_id = ue.user_id
				  AND s.thread_id = ue.thread_id
				  AND s.snoozed_until > NOW()
			)`)
	}

	// Awaiting reply: latest message in this thread (for this user)
	// was sent by one of the user's own mailboxes. We join the
	// mailbox emails to scope the "from us" check correctly.
	if params.AwaitingReply != nil && *params.AwaitingReply {
		query += fmt.Sprintf(`
			AND ue.id IN (
				SELECT DISTINCT ON (latest.thread_id) latest.id
				FROM unibox_emails latest
				WHERE latest.user_id = $1
				ORDER BY latest.thread_id, latest.internal_date DESC
			)
			AND EXISTS (
				SELECT 1 FROM email_accounts ea
				WHERE ea.user_id = $1
				  AND ea.email = ANY(ue.from_addr)
			)`)
	}

	if params.Unseen != nil && *params.Unseen {
		query += fmt.Sprintf(` AND ue.seen = $%d`, argPos)
		args = append(args, false)
		argPos++
	}

	if params.Since != nil {
		query += fmt.Sprintf(` AND ue.internal_date >= $%d`, argPos)
		args = append(args, *params.Since)
		argPos++
	}

	if params.Until != nil {
		query += fmt.Sprintf(` AND ue.internal_date <= $%d`, argPos)
		args = append(args, *params.Until)
		argPos++
	}

	if params.Subject != nil && *params.Subject != "" {
		query += fmt.Sprintf(` AND ue.search_tsv @@ plainto_tsquery('english', $%d)`, argPos)
		args = append(args, *params.Subject)
		argPos++
	}

	if params.Sender != nil && *params.Sender != "" {
		query += fmt.Sprintf(` AND $%d = ANY(ue.from_addr)`, argPos)
		args = append(args, *params.Sender)
		argPos++
	}

	if len(params.EmailAccountIDs) > 0 {
		query += fmt.Sprintf(` AND ue.email_id = ANY($%d)`, argPos)
		args = append(args, params.EmailAccountIDs)
		argPos++
	}

	if params.Cursor != "" {
		cursorID, err := uuid.Parse(params.Cursor)
		if err == nil {
			query += fmt.Sprintf(`
				AND (ue.internal_date, ue.id) < (
					SELECT internal_date, id FROM unibox_emails WHERE id = $%d
				)`, argPos)
			args = append(args, cursorID)
			argPos++
		}
	}

	query += fmt.Sprintf(` ORDER BY ue.internal_date DESC, ue.id DESC LIMIT $%d`, argPos)
	args = append(args, params.PageSize+1)

	return r.queryPreviewList(ctx, query, args, params.PageSize)
}

func (r *uniboxRepository) GetUnseenCount(ctx context.Context, userID uuid.UUID, emailAccountID *uuid.UUID) (int64, error) {
	var count int64

	if emailAccountID != nil {
		err := r.db.QueryRow(ctx,
			`SELECT COUNT(*) FROM unibox_emails WHERE user_id = $1 AND email_id = $2 AND seen = FALSE`,
			userID, *emailAccountID,
		).Scan(&count)
		return count, err
	}

	err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM unibox_emails WHERE user_id = $1 AND seen = FALSE`,
		userID,
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

func (r *uniboxRepository) MarkSeenBulk(ctx context.Context, userID uuid.UUID, ids []uuid.UUID, seen bool) error {
	if len(ids) == 0 {
		return nil
	}
	if len(ids) == 1 {
		return r.MarkSeen(ctx, userID, ids[0], seen)
	}

	_, err := r.db.Exec(ctx,
		`UPDATE unibox_emails SET seen = $1, updated_at = NOW() WHERE user_id = $2 AND id = ANY($3)`,
		seen, userID, ids,
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

func (r *uniboxRepository) Overview(ctx context.Context, userID uuid.UUID) (*models.UniboxOverview, error) {
	now := time.Now().UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	weekStart := todayStart.AddDate(0, 0, -6)

	overview := &models.UniboxOverview{
		GeneratedAt:      now,
		WindowTodayStart: todayStart,
		WindowWeekStart:  weekStart,
	}

	// Single aggregate over unibox_emails — cheap.
	err := r.db.QueryRow(ctx, `
		WITH ue AS (
			SELECT
				e.thread_id,
				e.email_id,
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
			WHERE e.user_id = $1
		),
		latest_per_thread AS (
			SELECT DISTINCT ON (thread_id) thread_id, from_addr
			FROM ue
			WHERE NOT is_snoozed
			ORDER BY thread_id, internal_date DESC
		),
		user_mailbox_emails AS (
			SELECT email FROM email_accounts WHERE user_id = $1
		)
		SELECT
			COUNT(*) FILTER (WHERE NOT is_snoozed)                                                               AS total,
			COUNT(*) FILTER (WHERE NOT is_snoozed AND NOT seen)                                                  AS unread,
			COUNT(*) FILTER (WHERE NOT is_snoozed AND internal_date >= $2)                                       AS today,
			COUNT(*) FILTER (WHERE NOT is_snoozed AND internal_date >= $3)                                       AS week,
			COUNT(*) FILTER (WHERE is_snoozed)                                                                   AS snoozed,
			(SELECT COUNT(*) FROM latest_per_thread l
				WHERE EXISTS (SELECT 1 FROM user_mailbox_emails u WHERE u.email = ANY(l.from_addr)))             AS awaiting
		FROM ue
	`, userID, todayStart, weekStart).Scan(
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
	// mailboxes still show up in the rail with a zero count.
	mailboxRows, err := r.db.Query(ctx, `
		SELECT
			ea.id,
			ea.email,
			ea.name,
			COUNT(ue.id) FILTER (WHERE ue.id IS NOT NULL AND NOT ue.seen
				AND NOT EXISTS (
					SELECT 1 FROM unibox_snoozes s
					WHERE s.user_id = ea.user_id AND s.thread_id = ue.thread_id AND s.snoozed_until > NOW()
				)) AS unread,
			COUNT(ue.id) FILTER (WHERE ue.id IS NOT NULL
				AND NOT EXISTS (
					SELECT 1 FROM unibox_snoozes s
					WHERE s.user_id = ea.user_id AND s.thread_id = ue.thread_id AND s.snoozed_until > NOW()
				)) AS total
		FROM email_accounts ea
		LEFT JOIN unibox_emails ue ON ue.email_id = ea.id AND ue.user_id = ea.user_id
		WHERE ea.user_id = $1
		GROUP BY ea.id, ea.email, ea.name
		ORDER BY ea.email ASC
	`, userID)
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
	tagRows, err := r.db.Query(ctx, `
		SELECT
			t.id,
			t.title,
			t.color,
			COUNT(ue.id) FILTER (WHERE ue.id IS NOT NULL AND NOT ue.seen
				AND NOT EXISTS (
					SELECT 1 FROM unibox_snoozes s
					WHERE s.user_id = t.user_id AND s.thread_id = ue.thread_id AND s.snoozed_until > NOW()
				)) AS unread,
			COUNT(ue.id) FILTER (WHERE ue.id IS NOT NULL
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
	`, userID)
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

	return overview, nil
}
