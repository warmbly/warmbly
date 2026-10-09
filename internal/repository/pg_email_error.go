package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
)

// EmailAccountError represents an error associated with an email account
type EmailAccountError struct {
	ID             uuid.UUID  `json:"id"`
	EmailAccountID uuid.UUID  `json:"email_account_id"`
	UserID         uuid.UUID  `json:"user_id"`
	ErrorCode      string     `json:"error_code"`
	Severity       string     `json:"severity"`
	ResolveMethod  string     `json:"resolve_method"`
	Title          string     `json:"title"`
	Message        string     `json:"message"`
	UserMessage    *string    `json:"user_message,omitempty"`
	ActionRequired *string    `json:"action_required,omitempty"`
	TaskID         *uuid.UUID `json:"task_id,omitempty"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	ResolvedBy     *string    `json:"resolved_by,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// CreateEmailAccountError contains data for creating a new error record
type CreateEmailAccountError struct {
	EmailAccountID uuid.UUID
	UserID         uuid.UUID
	ErrorCode      string
	Severity       string
	ResolveMethod  string
	Title          string
	Message        string
	UserMessage    *string
	ActionRequired *string
	TaskID         *uuid.UUID
}

// EmailAccountErrorRepository defines operations for email account errors
type EmailAccountErrorRepository interface {
	// CreateOnce records an error unless the account already has an unresolved
	// one with the same code, in which case it writes nothing and returns a
	// nil record. It is the only write path: migration 000145 makes a second
	// unresolved row for one code impossible, so an unconditional insert would
	// only turn a repeat into a constraint violation.
	CreateOnce(ctx context.Context, err *CreateEmailAccountError) (*EmailAccountError, *errx.Error)
	GetByAccountID(ctx context.Context, accountID uuid.UUID, unresolvedOnly bool) ([]EmailAccountError, *errx.Error)
	// GetByAccountIDs is the batched form, keyed by account id, so a status list
	// reads every mailbox's errors in one query. Each account's errors keep the
	// newest-first order.
	GetByAccountIDs(ctx context.Context, accountIDs []uuid.UUID, unresolvedOnly bool) (map[uuid.UUID][]EmailAccountError, *errx.Error)
	GetByUserID(ctx context.Context, userID uuid.UUID, limit int) ([]EmailAccountError, *errx.Error)
	Resolve(ctx context.Context, errorID uuid.UUID, resolvedBy string) *errx.Error
	ResolveByMethod(ctx context.Context, accountID uuid.UUID, method string) *errx.Error
	// ResolveByCodes resolves the account's unresolved errors carrying any of
	// the given codes, for a flow that just fixed that class of error.
	ResolveByCodes(ctx context.Context, accountID uuid.UUID, codes []string, resolvedBy string) *errx.Error
	// ResolveByCodesBefore is ResolveByCodes bounded to errors raised before
	// a given moment, so evidence of recovery cannot resolve a failure that
	// happened after it.
	ResolveByCodesBefore(ctx context.Context, accountID uuid.UUID, codes []string, before time.Time, resolvedBy string) *errx.Error
	ResolveAllForAccount(ctx context.Context, accountID uuid.UUID, resolvedBy string) *errx.Error
}

type emailAccountErrorRepository struct {
	DB *db.DB
}

// NewEmailAccountErrorRepository creates a new email account error repository
func NewEmailAccountErrorRepository(database *db.DB) EmailAccountErrorRepository {
	return &emailAccountErrorRepository{
		DB: database,
	}
}

// Create stores a new email account error
// CreateOnce refreshes unresolved transport observations without creating or notifying twice.
//
// A mail server that refuses the same command every pass is not a new problem
// every pass. The sync loop retries about once a minute and reports what it
// got, so one IMAP_UNKNOWN that nobody can fix wrote 1440 identical rows a day
// into the mailbox's error list (issue #405).
//
// Both guards are load-bearing. WHERE NOT EXISTS answers the ordinary repeat
// without touching the index, and ON CONFLICT answers the race it cannot see:
// two workers relaying the same failure in the same instant both find no row.
// The unique index behind it is partial on the unresolved rows (migration
// 000145), so resolved history is untouched and a problem that returns after
// it was fixed is recorded again.
func (r *emailAccountErrorRepository) CreateOnce(ctx context.Context, data *CreateEmailAccountError) (*EmailAccountError, *errx.Error) {
	// Unresolved transport timestamps represent the latest failure observation.
	query := `
		WITH observed AS (
			UPDATE email_account_errors SET created_at=NOW()
			WHERE email_account_id=$1 AND user_id=$2 AND error_code=$3 AND resolved_at IS NULL
			  AND error_code IN ('SERVER_UNREACHABLE','CONNECTION_LOST','RESOURCE_NOT_FOUND','IMAP_UNKNOWN')
			RETURNING id
		)
		INSERT INTO email_account_errors (
			email_account_id, user_id, error_code, severity, resolve_method,
			title, message, user_message, action_required, task_id
		)
		-- The casts are load-bearing: a SELECT source, unlike VALUES, gives
		-- the planner no target columns to infer the parameter types from.
		SELECT $1::uuid, $2::uuid, $3::varchar, $4::email_error_severity,
		       $5::email_error_resolve_method, $6::varchar, $7::text, $8::text,
		       $9::text, $10::uuid
		WHERE NOT EXISTS (
			SELECT 1 FROM email_account_errors
			WHERE email_account_id = $1 AND error_code = $3 AND resolved_at IS NULL
		)
		ON CONFLICT DO NOTHING
		RETURNING id, email_account_id, user_id, error_code, severity, resolve_method,
		          title, message, user_message, action_required, task_id,
		          resolved_at, resolved_by, created_at
	`

	params := []any{
		data.EmailAccountID,
		data.UserID,
		data.ErrorCode,
		data.Severity,
		data.ResolveMethod,
		data.Title,
		data.Message,
		data.UserMessage,
		data.ActionRequired,
		data.TaskID,
	}

	var e EmailAccountError
	err := r.DB.QueryRow(ctx, query, params...).Scan(
		&e.ID, &e.EmailAccountID, &e.UserID, &e.ErrorCode, &e.Severity, &e.ResolveMethod,
		&e.Title, &e.Message, &e.UserMessage, &e.ActionRequired, &e.TaskID,
		&e.ResolvedAt, &e.ResolvedBy, &e.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		// The row the caller wanted is already on screen, unresolved.
		return nil, nil
	}
	// The mailbox was deleted between the worker relaying the failure and this
	// write. Nothing is left to attach the error to and nobody can read it, so
	// declining is the same answer as the duplicate above rather than an
	// incident: every one of these events names an id a worker held minutes
	// ago, so a deleted mailbox with a busy sync loop reported one of these a
	// minute.
	if IsForeignKeyViolation(err) {
		return nil, nil
	}
	if err != nil {
		db.CaptureError(err, query, params, "queryrow")
		return nil, errx.InternalError()
	}

	return &e, nil
}

// GetByAccountID retrieves errors for a specific email account
func (r *emailAccountErrorRepository) GetByAccountID(ctx context.Context, accountID uuid.UUID, unresolvedOnly bool) ([]EmailAccountError, *errx.Error) {
	query := `
		SELECT id, email_account_id, user_id, error_code, severity, resolve_method,
		       title, message, user_message, action_required, task_id,
		       resolved_at, resolved_by, created_at
		FROM email_account_errors
		WHERE email_account_id = $1
	`

	if unresolvedOnly {
		query += " AND resolved_at IS NULL"
	}

	query += " ORDER BY created_at DESC"

	rows, err := r.DB.Query(ctx, query, accountID)
	if err != nil {
		db.CaptureError(err, query, []any{accountID}, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	var errors []EmailAccountError
	for rows.Next() {
		var e EmailAccountError
		err := rows.Scan(
			&e.ID, &e.EmailAccountID, &e.UserID, &e.ErrorCode, &e.Severity, &e.ResolveMethod,
			&e.Title, &e.Message, &e.UserMessage, &e.ActionRequired, &e.TaskID,
			&e.ResolvedAt, &e.ResolvedBy, &e.CreatedAt,
		)
		if err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		errors = append(errors, e)
	}

	return errors, nil
}

// GetByAccountIDs retrieves errors for many email accounts in one query.
func (r *emailAccountErrorRepository) GetByAccountIDs(ctx context.Context, accountIDs []uuid.UUID, unresolvedOnly bool) (map[uuid.UUID][]EmailAccountError, *errx.Error) {
	out := make(map[uuid.UUID][]EmailAccountError, len(accountIDs))
	if len(accountIDs) == 0 {
		return out, nil
	}

	query := `
		SELECT id, email_account_id, user_id, error_code, severity, resolve_method,
		       title, message, user_message, action_required, task_id,
		       resolved_at, resolved_by, created_at
		FROM email_account_errors
		WHERE email_account_id = ANY($1::uuid[])
	`

	if unresolvedOnly {
		query += " AND resolved_at IS NULL"
	}

	query += " ORDER BY created_at DESC"

	rows, err := r.DB.Query(ctx, query, accountIDs)
	if err != nil {
		db.CaptureError(err, query, []any{accountIDs}, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	for rows.Next() {
		var e EmailAccountError
		err := rows.Scan(
			&e.ID, &e.EmailAccountID, &e.UserID, &e.ErrorCode, &e.Severity, &e.ResolveMethod,
			&e.Title, &e.Message, &e.UserMessage, &e.ActionRequired, &e.TaskID,
			&e.ResolvedAt, &e.ResolvedBy, &e.CreatedAt,
		)
		if err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		out[e.EmailAccountID] = append(out[e.EmailAccountID], e)
	}
	if err := rows.Err(); err != nil {
		db.CaptureError(err, "", nil, "rows")
		return nil, errx.InternalError()
	}

	return out, nil
}

// GetByUserID retrieves recent errors for a user across all their email accounts
func (r *emailAccountErrorRepository) GetByUserID(ctx context.Context, userID uuid.UUID, limit int) ([]EmailAccountError, *errx.Error) {
	query := `
		SELECT id, email_account_id, user_id, error_code, severity, resolve_method,
		       title, message, user_message, action_required, task_id,
		       resolved_at, resolved_by, created_at
		FROM email_account_errors
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`

	rows, err := r.DB.Query(ctx, query, userID, limit)
	if err != nil {
		db.CaptureError(err, query, []any{userID, limit}, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	var errors []EmailAccountError
	for rows.Next() {
		var e EmailAccountError
		err := rows.Scan(
			&e.ID, &e.EmailAccountID, &e.UserID, &e.ErrorCode, &e.Severity, &e.ResolveMethod,
			&e.Title, &e.Message, &e.UserMessage, &e.ActionRequired, &e.TaskID,
			&e.ResolvedAt, &e.ResolvedBy, &e.CreatedAt,
		)
		if err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		errors = append(errors, e)
	}

	return errors, nil
}

// Resolve marks a specific error as resolved
func (r *emailAccountErrorRepository) Resolve(ctx context.Context, errorID uuid.UUID, resolvedBy string) *errx.Error {
	query := `
		UPDATE email_account_errors
		SET resolved_at = NOW(), resolved_by = $1
		WHERE id = $2 AND resolved_at IS NULL
	`

	cmd, err := r.DB.Exec(ctx, query, resolvedBy, errorID)
	if err != nil {
		db.CaptureError(err, query, []any{resolvedBy, errorID}, "exec")
		return errx.InternalError()
	}
	if cmd.RowsAffected() == 0 {
		return errx.ErrNotFound
	}

	return nil
}

// ResolveByMethod resolves all errors for an account that have the specified resolve method
func (r *emailAccountErrorRepository) ResolveByMethod(ctx context.Context, accountID uuid.UUID, method string) *errx.Error {
	query := `
		UPDATE email_account_errors
		SET resolved_at = NOW(), resolved_by = $1
		WHERE email_account_id = $2
		  AND resolve_method = $3
		  AND resolved_at IS NULL
	`

	resolvedBy := "system:" + method
	_, err := r.DB.Exec(ctx, query, resolvedBy, accountID, method)
	if err != nil {
		db.CaptureError(err, query, []any{resolvedBy, accountID, method}, "exec")
		return errx.InternalError()
	}

	return nil
}

// ResolveByCodes resolves the account's unresolved errors carrying any of the given codes
func (r *emailAccountErrorRepository) ResolveByCodes(ctx context.Context, accountID uuid.UUID, codes []string, resolvedBy string) *errx.Error {
	if len(codes) == 0 {
		return nil
	}

	query := `
		UPDATE email_account_errors
		SET resolved_at = NOW(), resolved_by = $1
		WHERE email_account_id = $2
		  AND error_code = ANY($3::text[])
		  AND resolved_at IS NULL
	`

	_, err := r.DB.Exec(ctx, query, resolvedBy, accountID, codes)
	if err != nil {
		db.CaptureError(err, query, []any{resolvedBy, accountID, codes}, "exec")
		return errx.InternalError()
	}

	return nil
}

// ResolveByCodesBefore resolves the account's unresolved errors carrying any
// of the codes that were raised before the given moment.
//
// The bound matters because the bus can redeliver: JetStream is configured
// with MaxDeliver and no MaxAckPending, so an older event can arrive after a
// newer one. Without it, a stale "the sync succeeded" could clear a failure
// that happened afterwards, and the mailbox would look healthy while it was
// not, until the next distinct outage raised a fresh row.
func (r *emailAccountErrorRepository) ResolveByCodesBefore(ctx context.Context, accountID uuid.UUID, codes []string, before time.Time, resolvedBy string) *errx.Error {
	if len(codes) == 0 {
		return nil
	}

	query := `
		UPDATE email_account_errors
		SET resolved_at = NOW(), resolved_by = $1
		WHERE email_account_id = $2
		  AND error_code = ANY($3::text[])
		  AND created_at < $4
		  AND resolved_at IS NULL
	`

	_, err := r.DB.Exec(ctx, query, resolvedBy, accountID, codes, before)
	if err != nil {
		db.CaptureError(err, query, []any{resolvedBy, accountID, codes, before}, "exec")
		return errx.InternalError()
	}

	return nil
}

// ResolveAllForAccount resolves all unresolved errors for an email account
func (r *emailAccountErrorRepository) ResolveAllForAccount(ctx context.Context, accountID uuid.UUID, resolvedBy string) *errx.Error {
	query := `
		UPDATE email_account_errors
		SET resolved_at = NOW(), resolved_by = $1
		WHERE email_account_id = $2 AND resolved_at IS NULL
	`

	_, err := r.DB.Exec(ctx, query, resolvedBy, accountID)
	if err != nil {
		db.CaptureError(err, query, []any{resolvedBy, accountID}, "exec")
		return errx.InternalError()
	}

	return nil
}

// Helper to map errx.MailErrorResolveMethod to DB enum value
func MapResolveMethod(method errx.MailErrorResolveMethod) string {
	switch method {
	case errx.MailErrorResolveMethodAuth:
		return "OAUTH"
	case errx.MailErrorResolveMethodRetry:
		return "RETRY"
	case errx.MailErrorResolveMethodReload:
		return "RELOAD"
	default:
		return "NONE"
	}
}

// Helper to map errx.MailErrorType to DB enum value
func MapSeverity(errType errx.MailErrorType) string {
	switch errType {
	case errx.MailErrorCritical:
		return "CRITICAL"
	case errx.MailErrorWarning:
		return "WARNING"
	case errx.MailErrorInformational:
		return "INFORMATIONAL"
	default:
		return "WARNING"
	}
}
