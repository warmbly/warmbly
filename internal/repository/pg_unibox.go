package repository

import (
	"context"
	"fmt"
	"strings"

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
	"id", "email_id", "thread_id", "subject", "snippet", "internal_date", "seen",
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

func (r *uniboxRepository) GetByThread(ctx context.Context, userID, emailID uuid.UUID, threadID string, limit int, cursor string) (*models.MailSearchResult, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM unibox_emails
		WHERE user_id = $1 AND email_id = $2 AND thread_id = $3
	`, strings.Join(mailFieldsPreview, ", "))

	args := []any{userID, emailID, threadID}
	argPos := 4

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
	query := fmt.Sprintf(`
		SELECT %s
		FROM unibox_emails
		WHERE user_id = $1
	`, strings.Join(mailFieldsPreview, ", "))

	args := []any{userID}
	argPos := 2

	if params.Unseen != nil && *params.Unseen {
		query += fmt.Sprintf(` AND seen = $%d`, argPos)
		args = append(args, false)
		argPos++
	}

	if params.Since != nil {
		query += fmt.Sprintf(` AND internal_date >= $%d`, argPos)
		args = append(args, *params.Since)
		argPos++
	}

	if params.Until != nil {
		query += fmt.Sprintf(` AND internal_date <= $%d`, argPos)
		args = append(args, *params.Until)
		argPos++
	}

	if params.Subject != nil && *params.Subject != "" {
		query += fmt.Sprintf(` AND search_tsv @@ plainto_tsquery('english', $%d)`, argPos)
		args = append(args, *params.Subject)
		argPos++
	}

	if params.Sender != nil && *params.Sender != "" {
		query += fmt.Sprintf(` AND $%d = ANY(from_addr)`, argPos)
		args = append(args, *params.Sender)
		argPos++
	}

	if len(params.EmailAccountIDs) > 0 {
		query += fmt.Sprintf(` AND email_id = ANY($%d)`, argPos)
		args = append(args, params.EmailAccountIDs)
		argPos++
	}

	if params.Cursor != "" {
		cursorID, err := uuid.Parse(params.Cursor)
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
		if err := rows.Scan(&e.ID, &e.EmailID, &e.ThreadID, &e.Subject, &e.Snippet, &e.InternalDate, &e.Seen); err != nil {
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
