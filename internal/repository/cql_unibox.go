package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gocql/gocql"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/cdb"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils"
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
	db *cdb.Client
}

func NewUniboxRepository(db *cdb.Client) UniboxRepository {
	return &uniboxRepository{db: db}
}

var MailFieldsFull = []string{
	"id", "email_id", "mailbox", "thread_id", "message_id",
	"gmail_id", "parent_id", "uid", "mod_seq",
	"flags", "bcc", "cc", "from_addr", "in_reply_to", "reply_to",
	"to_addr", "subject", "size", "internal_date", "sent_date",
	"snippet", "seen", "updated_at", "created_at",
}

var MailFields = []string{
	"id", "email_id", "thread_id", "subject", "snippet", "internal_date", "seen",
}

// ----------------------------------------------------------------------
// Insert or Update Email (batched for main table + email_addrs)
// ----------------------------------------------------------------------
func (r *uniboxRepository) CreateEntry(ctx context.Context, userId uuid.UUID, e *models.EmailMessageStoreData) error {
	batch := r.db.Session.NewBatch(gocql.LoggedBatch).WithContext(ctx)

	columns := []string{
		"id", "user_id", "email_id", "mailbox", "message_id",
		"gmail_id", "parent_id", "uid", "mod_seq",
		"flags", "bcc", "cc", "from_addr", "in_reply_to", "reply_to",
		"to_addr", "subject", "size", "internal_date", "sent_date",
		"snippet", "seen", "updated_at", "created_at",
	}

	cql := fmt.Sprintf(`
		INSERT INTO emails (
			%s
		) VALUES (
			%s
		)`, strings.Join(columns, ","), strings.Join(utils.MakeArray("?", len(columns)), ","),
	)

	params := []any{
		e.ID, userId, e.EmailID, e.Mailbox, e.ThreadID, e.MessageID,
		e.GmailID, e.ParentID, e.UID, e.ModSeq,
		e.Flags, e.BCC, e.CC, e.FromAddr, e.InReplyTo, e.ReplyTo,
		e.ToAddr, e.Subject, e.Size, e.InternalDate, e.SentDate,
		e.Snippet, e.Seen, e.UpdatedAt, e.CreatedAt,
	}

	if e.ThreadID != "" {
		columns = append(columns, "thread_id")
		params = append(params, e.ThreadID)
	}

	// Insert into main emails table
	batch.Query(
		cql,
		params...,
	)

	// Insert into email_addrs for each sender (for fast sender lookup)
	for _, sender := range e.FromAddr {
		if sender == "" {
			continue
		}

		cql := `
			INSERT INTO email_addrs (email_id, type, sender, message_id, created_at)
			VALUES (?, 'from', ?, ?, ?)
		`

		params := []any{
			e.EmailID,
			sender,
			e.MessageID,
			e.CreatedAt,
		}

		batch.Query(
			cql,
			params...,
		)
	}

	return r.db.Session.ExecuteBatch(batch)
}

func (r *uniboxRepository) UpdateEntry(ctx context.Context, userID, emailID, id uuid.UUID, e *UpdateUniboxEntry) error {
	setClauses := []string{}
	args := []any{userID, emailID, id}
	argPos := 4

	if e.Flags != nil {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "flags", argPos))
		args = append(args, e.Flags)
		argPos++
	}
	if e.Mailbox != nil {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "mailbox", argPos))
		args = append(args, *e.Mailbox)
		argPos++
	}
	if e.ModSeq != nil {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "mod_seq", argPos))
		args = append(args, *e.ModSeq)
		argPos++
	}
	if e.UID != nil {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", "uid", argPos))
		args = append(args, *e.UID)
		argPos++
	}

	cql := fmt.Sprintf(`
		UPDATE emails
		SET %s
		WHERE user_id = $1 AND email_id = $2 AND id = $3
	`, strings.Join(setClauses, ","))

	return r.db.Query(
		cql,
		args...,
	).WithContext(ctx).Exec()
}

func (r *uniboxRepository) GetIncoming(ctx context.Context, userID uuid.UUID, limit int, cursor string) (*models.MailSearchResult, error) {
	var pageState []byte
	if cursor != "" {
		pageState = cdb.DecodePageState(cursor)
	}

	cql := fmt.Sprintf(`
		SELECT %s
		FROM emails
		WHERE user_id = ?
	`, strings.Join(MailFields, ","))

	params := []any{
		userID,
	}

	iter := r.db.Query(
		cql,
		params...,
	).PageSize(limit).PageState(pageState).WithContext(ctx).Iter()

	var emails []models.EmailMessageStoreDataPreview
	var e models.EmailMessageStoreDataPreview
	for iter.Scan(&e) {
		emails = append(emails, e)
	}

	nextState := iter.PageState()
	hasMore := len(nextState) > 0
	var nextCursor *string
	if hasMore {
		encoded := cdb.EncodePageState(nextState)
		nextCursor = &encoded
	}

	if err := iter.Close(); err != nil {
		return nil, err
	}

	return &models.MailSearchResult{
		Data: emails,
		Pagination: models.CPagination{
			HasMore:    hasMore,
			NextCursor: nextCursor,
		},
	}, nil
}

// ----------------------------------------------------------------------
// Get Email By ID (auto-mark as seen)
// ----------------------------------------------------------------------
func (r *uniboxRepository) GetByID(ctx context.Context, userID, id uuid.UUID) (*models.EmailMessageStoreData, error) {
	cql := fmt.Sprintf(`
		SELECT %s
		FROM emails
		WHERE user_id = ? AND id = ?
	`, strings.Join(MailFieldsFull, ","))

	var e models.EmailMessageStoreData
	err := r.db.Query(cql, userID, id).WithContext(ctx).Scan(&e)
	if err != nil {
		if errors.Is(err, gocql.ErrNotFound) {
			return nil, errors.New("email not found")
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

// ----------------------------------------------------------------------
// Get Emails by Thread (email_id + thread_id)
// ----------------------------------------------------------------------
func (r *uniboxRepository) GetByThread(ctx context.Context, userID, emailID uuid.UUID, threadID string, limit int, cursor string) (*models.MailSearchResult, error) {
	var pageState []byte
	if cursor != "" {
		pageState = cdb.DecodePageState(cursor)
	}

	iter := r.db.Query(`
		SELECT %s
		FROM emails_by_thread
		WHERE user_id = ? AND email_id = ? AND thread_id = ?`,
		userID, emailID, threadID,
	).PageSize(limit).PageState(pageState).WithContext(ctx).Iter()

	var emails []models.EmailMessageStoreDataPreview
	var e models.EmailMessageStoreDataPreview
	for iter.Scan(&e) {
		emails = append(emails, e)
	}

	nextState := iter.PageState()
	hasMore := len(nextState) > 0
	var nextCursor *string
	if hasMore {
		encoded := cdb.EncodePageState(nextState)
		nextCursor = &encoded
	}

	if err := iter.Close(); err != nil {
		return nil, err
	}

	return &models.MailSearchResult{
		Data: emails,
		Pagination: models.CPagination{
			HasMore:    hasMore,
			NextCursor: nextCursor,
		},
	}, nil
}

// ----------------------------------------------------------------------
// Get Emails by Sender (using email_addrs table + driver pagination)
// ----------------------------------------------------------------------
func (r *uniboxRepository) GetBySender(ctx context.Context, userID uuid.UUID, sender string, limit int, cursor string) (*models.MailSearchResult, error) {
	var pageState []byte
	if cursor != "" {
		pageState = cdb.DecodePageState(cursor)
	}

	iter := r.db.Query(`
		SELECT email_message_id
		FROM email_addrs
		WHERE user_id = ? AND addr_type = 'from' AND addr = ?`,
		userID, sender,
	).PageSize(limit).PageState(pageState).WithContext(ctx).Iter()

	var messageIDs []string
	var msgID string
	for iter.Scan(&msgID) {
		messageIDs = append(messageIDs, msgID)
	}

	nextState := iter.PageState()
	hasMore := len(nextState) > 0
	var nextCursor *string
	if hasMore {
		encoded := cdb.EncodePageState(nextState)
		nextCursor = &encoded
	}

	if err := iter.Close(); err != nil {
		return nil, err
	}

	// Fetch full email details for these IDs
	fullEmails, err := r.getEmailsByMessageIDs(ctx, userID, messageIDs)
	if err != nil {
		return nil, err
	}

	return &models.MailSearchResult{
		Data: fullEmails,
		Pagination: models.CPagination{
			NextCursor: nextCursor,
			HasMore:    hasMore,
		},
	}, nil
}

func (r *uniboxRepository) getEmailsByMessageIDs(ctx context.Context, userId uuid.UUID, messageIds []string) ([]models.EmailMessageStoreDataPreview, error) {
	if len(messageIds) == 0 {
		return make([]models.EmailMessageStoreDataPreview, 0), nil
	}

	cql := fmt.Sprintf(`
        SELECT %s
        FROM emails
        WHERE user_id = ? AND message_id IN ?
    `, strings.Join(MailFields, ","))

	params := []any{
		userId,
		messageIds,
	}

	iter := r.db.Query(
		cql,
		params...,
	).WithContext(ctx).Iter()

	var results []models.EmailMessageStoreDataPreview
	var e models.EmailMessageStoreDataPreview

	for iter.Scan(&e) {
		results = append(results, e)
	}

	if err := iter.Close(); err != nil {
		return nil, err
	}

	return results, nil
}

// ----------------------------------------------------------------------
// Mark as Seen/Unseen
// ----------------------------------------------------------------------
func (r *uniboxRepository) MarkSeen(ctx context.Context, userId, messageId uuid.UUID, seen bool) error {
	cql := `
		UPDATE emails
		SET seen = ?
		WHERE user_id = ? AND id = ?
	`

	params := []any{
		seen,
		userId,
		messageId,
	}

	return r.db.Query(
		cql,
		params...,
	).WithContext(ctx).Exec()
}

func (r *uniboxRepository) MarkSeenBulk(
	ctx context.Context,
	userId uuid.UUID,
	messageIDs []uuid.UUID,
	seen bool,
) error {
	if len(messageIDs) == 0 {
		return nil
	} else if len(messageIDs) == 1 {
		return r.MarkSeen(ctx, userId, messageIDs[0], seen)
	}

	cql := `
        UPDATE emails
        SET seen = ?
        WHERE user_id = ? AND id IN ?
    `

	params := []any{
		seen,
		userId,
		messageIDs,
	}

	return r.db.Query(
		cql,
		params...,
	).WithContext(ctx).Exec()
}

// ----------------------------------------------------------------------
// Delete Email
// ----------------------------------------------------------------------
func (r *uniboxRepository) Delete(ctx context.Context, userId, id uuid.UUID) error {
	cql := `
		DELETE FROM emails
		WHERE user_id = ? AND message_id = ?
	`

	params := []any{
		userId,
		id,
	}

	return r.db.Query(
		cql,
		params...,
	).WithContext(ctx).Exec()
}

// ----------------------------------------------------------------------
// Search Emails with filters
// ----------------------------------------------------------------------
func (r *uniboxRepository) Search(ctx context.Context, userID uuid.UUID, params *models.MailSearchParams) (*models.MailSearchResult, error) {
	var pageState []byte
	if params.Cursor != "" {
		pageState = cdb.DecodePageState(params.Cursor)
	}

	limit := params.PageSize
	if limit <= 0 {
		limit = 50
	}

	// Build query with ALLOW FILTERING for additional conditions
	// Note: In production with large datasets, consider using materialized views
	// or secondary indexes for better performance
	cql := fmt.Sprintf(`
		SELECT %s
		FROM emails
		WHERE user_id = ?
	`, strings.Join(MailFields, ","))

	queryParams := []any{userID}

	// Cassandra doesn't support arbitrary WHERE clauses like SQL
	// We need to fetch and filter in-memory for complex queries
	// For better performance, use materialized views for common query patterns

	if params.Unseen != nil && *params.Unseen {
		cql += " AND seen = ?"
		queryParams = append(queryParams, false)
	}

	cql += " ALLOW FILTERING"

	iter := r.db.Query(cql, queryParams...).
		PageSize(limit).
		PageState(pageState).
		WithContext(ctx).
		Iter()

	var emails []models.EmailMessageStoreDataPreview
	var e models.EmailMessageStoreDataPreview

	for iter.Scan(&e.ID, &e.EmailID, &e.ThreadID, &e.Subject, &e.Snippet, &e.InternalDate, &e.Seen) {
		// Apply client-side filters for fields that can't be filtered in CQL
		if params.Since != nil && e.InternalDate.Before(*params.Since) {
			continue
		}
		if params.Until != nil && e.InternalDate.After(*params.Until) {
			continue
		}
		if params.Subject != nil && *params.Subject != "" {
			if !strings.Contains(strings.ToLower(e.Subject), strings.ToLower(*params.Subject)) {
				continue
			}
		}

		emails = append(emails, e)
	}

	nextState := iter.PageState()
	hasMore := len(nextState) > 0
	var nextCursor *string
	if hasMore {
		encoded := cdb.EncodePageState(nextState)
		nextCursor = &encoded
	}

	if err := iter.Close(); err != nil {
		return nil, err
	}

	return &models.MailSearchResult{
		Data: emails,
		Pagination: models.CPagination{
			HasMore:    hasMore,
			NextCursor: nextCursor,
		},
	}, nil
}

// ----------------------------------------------------------------------
// Get Unseen Count
// ----------------------------------------------------------------------
func (r *uniboxRepository) GetUnseenCount(ctx context.Context, userID uuid.UUID, emailAccountID *uuid.UUID) (int64, error) {
	cql := `
		SELECT COUNT(*)
		FROM emails
		WHERE user_id = ? AND seen = false
		ALLOW FILTERING
	`

	params := []any{userID}

	if emailAccountID != nil {
		cql = `
			SELECT COUNT(*)
			FROM emails
			WHERE user_id = ? AND email_id = ? AND seen = false
			ALLOW FILTERING
		`
		params = append(params, *emailAccountID)
	}

	var count int64
	if err := r.db.Query(cql, params...).WithContext(ctx).Scan(&count); err != nil {
		return 0, err
	}

	return count, nil
}
