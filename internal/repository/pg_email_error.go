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
	Create(ctx context.Context, err *CreateEmailAccountError) (*EmailAccountError, *errx.Error)
	GetByAccountID(ctx context.Context, accountID uuid.UUID, unresolvedOnly bool) ([]EmailAccountError, *errx.Error)
	GetByUserID(ctx context.Context, userID uuid.UUID, limit int) ([]EmailAccountError, *errx.Error)
	Resolve(ctx context.Context, errorID uuid.UUID, resolvedBy string) *errx.Error
	ResolveByMethod(ctx context.Context, accountID uuid.UUID, method string) *errx.Error
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
func (r *emailAccountErrorRepository) Create(ctx context.Context, data *CreateEmailAccountError) (*EmailAccountError, *errx.Error) {
	query := `
		INSERT INTO email_account_errors (
			email_account_id, user_id, error_code, severity, resolve_method,
			title, message, user_message, action_required, task_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
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

// GetUnresolvedByCode checks if there's already an unresolved error with the same code
func (r *emailAccountErrorRepository) GetUnresolvedByCode(ctx context.Context, accountID uuid.UUID, errorCode string) (*EmailAccountError, *errx.Error) {
	query := `
		SELECT id, email_account_id, user_id, error_code, severity, resolve_method,
		       title, message, user_message, action_required, task_id,
		       resolved_at, resolved_by, created_at
		FROM email_account_errors
		WHERE email_account_id = $1
		  AND error_code = $2
		  AND resolved_at IS NULL
		ORDER BY created_at DESC
		LIMIT 1
	`

	var e EmailAccountError
	err := r.DB.QueryRow(ctx, query, accountID, errorCode).Scan(
		&e.ID, &e.EmailAccountID, &e.UserID, &e.ErrorCode, &e.Severity, &e.ResolveMethod,
		&e.Title, &e.Message, &e.UserMessage, &e.ActionRequired, &e.TaskID,
		&e.ResolvedAt, &e.ResolvedBy, &e.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // No existing error found
		}
		db.CaptureError(err, query, []any{accountID, errorCode}, "queryrow")
		return nil, errx.InternalError()
	}

	return &e, nil
}
