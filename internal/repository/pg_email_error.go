package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// EmailAccountError represents an error associated with an email account
type EmailAccountError struct {
	ID                uuid.UUID                             `json:"id"`
	EmailAccountID    uuid.UUID                             `json:"email_account_id"`
	UserID            uuid.UUID                             `json:"user_id"`
	ErrorCode         string                                `json:"error_code"`
	Severity          string                                `json:"severity"`
	ResolveMethod     string                                `json:"resolve_method"`
	Title             string                                `json:"title"`
	Message           string                                `json:"message"`
	UserMessage       *string                               `json:"user_message,omitempty"`
	ActionRequired    *string                               `json:"action_required,omitempty"`
	TaskID            *uuid.UUID                            `json:"task_id,omitempty"`
	ResolvedAt        *time.Time                            `json:"resolved_at,omitempty"`
	ResolvedBy        *string                               `json:"resolved_by,omitempty"`
	CreatedAt         time.Time                             `json:"created_at"`
	Operation         models.MailTransportOperation         `json:"operation"`
	ObservationSource models.MailTransportObservationSource `json:"observation_source"`
	ReceivedAt        *time.Time                            `json:"received_at,omitempty"`
}

// CreateEmailAccountError contains data for creating a new error record
type CreateEmailAccountError struct {
	TransportGeneration uuid.UUID
	EmailAccountID      uuid.UUID
	UserID              uuid.UUID
	ErrorCode           string
	Severity            string
	ResolveMethod       string
	Title               string
	Message             string
	UserMessage         *string
	ActionRequired      *string
	TaskID              *uuid.UUID
	ObservedAt          *time.Time
	Operation           models.MailTransportOperation
}

// EmailAccountErrorRepository defines operations for email account errors
type EmailAccountErrorRepository interface {
	// CreateOnce deduplicates unresolved reports by mailbox, code and operation.
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
	RecordTransportSuccess(ctx context.Context, accountID uuid.UUID, operation models.MailTransportOperation, at time.Time, taskID *uuid.UUID, generation uuid.UUID) *errx.Error
	ResolveAllForAccount(ctx context.Context, accountID uuid.UUID, resolvedBy string) *errx.Error
	// SwitchOffUnreachable switches off up to limit mailboxes whose server has refused every connection for
	// silentFor, records why on each, and returns them.
	SwitchOffUnreachable(ctx context.Context, silentFor time.Duration, limit int) ([]UnreachableMailbox, *errx.Error)
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

// CreateOnce serializes observations with operation recovery and deduplicates warnings.
func (r *emailAccountErrorRepository) CreateOnce(ctx context.Context, data *CreateEmailAccountError) (*EmailAccountError, *errx.Error) {
	tx, err := beginResultTx(ctx, r.DB)
	if err != nil {
		return nil, errx.InternalError()
	}
	defer tx.Rollback(ctx)
	if err := lockTransportMailbox(ctx, tx, data.EmailAccountID); err != nil {
		return nil, errx.InternalError()
	}
	operation := data.Operation.Conservative()
	currentGeneration := false
	if data.TransportGeneration != uuid.Nil {
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM email_accounts WHERE id=$1 AND transport_generation=$2)`, data.EmailAccountID, data.TransportGeneration).Scan(&currentGeneration); err != nil {
			return nil, errx.InternalError()
		}
	}
	if operation.Known() && !currentGeneration {
		operation = models.MailTransportUnknown
	}
	if operation == models.MailTransportSync && data.TaskID != nil || operation == models.MailTransportSend && (data.TaskID == nil || *data.TaskID == uuid.Nil) {
		operation = models.MailTransportUnknown
	}
	recordID := uuid.New()
	query := `
		WITH observation AS (
		    SELECT CASE WHEN $11::timestamptz > to_timestamp(0) AND $11 <= NOW()
		        THEN $11 ELSE NOW() END AS at,
		        CASE WHEN $11::timestamptz > to_timestamp(0) AND $11 <= NOW()
		        THEN 'worker' ELSE 'receipt' END AS source
		), candidate AS (
		    SELECT observation.*, recovery.recovered_at
		    FROM observation LEFT JOIN LATERAL (
		        SELECT MAX(recovered_at) AS recovered_at
		        FROM email_account_transport_recovery
		        WHERE email_account_id=$1 AND (operation=$13 OR $13='unknown')
		        HAVING COUNT(*)=CASE WHEN $13='unknown' THEN 2 ELSE 1 END
		            AND observation.at < MIN(recovered_at) - INTERVAL '1 second'
		    ) recovery ON $3 IN ('SERVER_UNREACHABLE','CONNECTION_LOST','RESOURCE_NOT_FOUND','IMAP_UNKNOWN','SENDING_TOO_FAST','QUOTA_EXCEEDED')
		)
		INSERT INTO email_account_errors AS existing (
			id, email_account_id, user_id, error_code, severity, resolve_method,
			title, message, user_message, action_required, task_id, created_at,
			operation,observation_source,received_at,resolved_at,resolved_by
		)
		SELECT $12,$1,$2,$3,$4,$5,$6,
		    CASE WHEN $3='SERVER_UNREACHABLE' AND $17::boolean
		        AND (NOT $14::boolean OR source<>'worker') THEN $15 ELSE $7 END,
		    CASE WHEN $3='SERVER_UNREACHABLE' AND $17::boolean AND $8=$7
		        AND (NOT $14::boolean OR source<>'worker') THEN $15 ELSE $8 END,
		    $9,$10,at,$13,source,NOW(),recovered_at,
		    CASE WHEN recovered_at IS NOT NULL THEN $13 || ' recovered' END
		FROM candidate
		WHERE EXISTS(SELECT 1 FROM email_accounts WHERE id=$1 AND user_id=$2)
		    AND NOT (recovered_at IS NOT NULL AND EXISTS(
		        SELECT 1 FROM email_account_errors historical
		        WHERE historical.email_account_id=$1 AND historical.user_id=$2
		            AND historical.error_code=$3 AND historical.operation=$13
		            AND historical.created_at=candidate.at
		            AND historical.task_id IS NOT DISTINCT FROM $10::uuid
		            AND historical.resolved_by=$13 || ' recovered'))
		ON CONFLICT (email_account_id,error_code,operation)
		WHERE resolved_at IS NULL AND observation_source IN ('worker','receipt') DO UPDATE
		SET created_at=CASE WHEN existing.user_id=EXCLUDED.user_id
		    AND existing.error_code IN ('SERVER_UNREACHABLE','CONNECTION_LOST','RESOURCE_NOT_FOUND','IMAP_UNKNOWN','SENDING_TOO_FAST','QUOTA_EXCEEDED')
		    THEN GREATEST(existing.created_at,EXCLUDED.created_at) ELSE existing.created_at END,
		    task_id=CASE WHEN existing.user_id=EXCLUDED.user_id
		    AND existing.error_code IN ('SERVER_UNREACHABLE','CONNECTION_LOST','RESOURCE_NOT_FOUND','IMAP_UNKNOWN','SENDING_TOO_FAST','QUOTA_EXCEEDED')
		    THEN COALESCE(existing.task_id,EXCLUDED.task_id) ELSE existing.task_id END,
		    observation_source=CASE WHEN existing.user_id=EXCLUDED.user_id
		    AND EXCLUDED.created_at >= existing.created_at
		    AND existing.error_code IN ('SERVER_UNREACHABLE','CONNECTION_LOST','RESOURCE_NOT_FOUND','IMAP_UNKNOWN','SENDING_TOO_FAST','QUOTA_EXCEEDED')
		    THEN EXCLUDED.observation_source ELSE existing.observation_source END,
		    received_at=CASE WHEN existing.user_id=EXCLUDED.user_id
		    THEN GREATEST(existing.received_at,EXCLUDED.received_at) ELSE existing.received_at END,
		    message=CASE WHEN existing.user_id=EXCLUDED.user_id
		        AND $14::boolean AND EXCLUDED.observation_source='worker'
		        AND EXCLUDED.created_at>existing.created_at
		        AND existing.error_code='SERVER_UNREACHABLE'
		        AND (existing.message LIKE $16 OR $17::boolean)
		    THEN CASE WHEN $17::boolean THEN EXCLUDED.message ELSE $15 END
		    WHEN existing.user_id=EXCLUDED.user_id
		        AND EXCLUDED.created_at>existing.created_at
		        AND existing.error_code='SERVER_UNREACHABLE'
		        AND existing.message LIKE $16
		        AND (NOT $14::boolean OR EXCLUDED.observation_source<>'worker')
		    THEN $15
		    ELSE existing.message END,
		    user_message=CASE WHEN existing.user_message=existing.message THEN
		        CASE WHEN existing.user_id=EXCLUDED.user_id
		            AND $14::boolean AND EXCLUDED.observation_source='worker'
		            AND EXCLUDED.created_at>existing.created_at
		            AND existing.error_code='SERVER_UNREACHABLE'
		            AND (existing.message LIKE $16 OR $17::boolean)
		        THEN CASE WHEN $17::boolean THEN EXCLUDED.message ELSE $15 END
		        WHEN existing.user_id=EXCLUDED.user_id
		            AND EXCLUDED.created_at>existing.created_at
		            AND existing.error_code='SERVER_UNREACHABLE'
		            AND existing.message LIKE $16
		            AND (NOT $14::boolean OR EXCLUDED.observation_source<>'worker')
		        THEN $15
		        ELSE existing.user_message END
		    ELSE existing.user_message END
		RETURNING id, email_account_id, user_id, error_code, severity, resolve_method,
		          title, message, user_message, action_required, task_id,
		          resolved_at, resolved_by, created_at,operation,observation_source,received_at
	`

	resolveMethod := data.ResolveMethod
	if resolveMethod == string(errx.MailErrorResolveMethodNone) {
		resolveMethod = MapResolveMethod(errx.MailErrorResolveMethodNone)
	}
	params := []any{
		data.EmailAccountID,
		data.UserID,
		data.ErrorCode,
		data.Severity,
		resolveMethod,
		data.Title,
		data.Message,
		data.UserMessage,
		data.ActionRequired,
		data.TaskID,
		data.ObservedAt,
		recordID,
		operation,
		currentGeneration,
		errx.ErrMailServerUnreachable.Message,
		"The connection to the mail server could not be established (IMAP %",
		errx.IsKnownIMAPConnectionMessage(data.Message),
	}

	var e EmailAccountError
	err = tx.QueryRow(ctx, query, params...).Scan(
		&e.ID, &e.EmailAccountID, &e.UserID, &e.ErrorCode, &e.Severity, &e.ResolveMethod,
		&e.Title, &e.Message, &e.UserMessage, &e.ActionRequired, &e.TaskID,
		&e.ResolvedAt, &e.ResolvedBy, &e.CreatedAt, &e.Operation, &e.ObservationSource, &e.ReceivedAt,
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
	if err := tx.Commit(ctx); err != nil {
		return nil, errx.InternalError()
	}
	if e.ID != recordID || e.ResolvedAt != nil {
		return nil, nil
	}

	return &e, nil
}

// GetByAccountID retrieves errors for a specific email account
func (r *emailAccountErrorRepository) GetByAccountID(ctx context.Context, accountID uuid.UUID, unresolvedOnly bool) ([]EmailAccountError, *errx.Error) {
	query := `
		SELECT id, email_account_id, user_id, error_code, severity, resolve_method,
		       title, message, user_message, action_required, task_id,
		       resolved_at, resolved_by, created_at, operation, observation_source, received_at
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
			&e.ResolvedAt, &e.ResolvedBy, &e.CreatedAt, &e.Operation, &e.ObservationSource, &e.ReceivedAt,
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
		       resolved_at, resolved_by, created_at, operation, observation_source, received_at
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
			&e.ResolvedAt, &e.ResolvedBy, &e.CreatedAt, &e.Operation, &e.ObservationSource, &e.ReceivedAt,
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
		       resolved_at, resolved_by, created_at, operation, observation_source, received_at
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
			&e.ResolvedAt, &e.ResolvedBy, &e.CreatedAt, &e.Operation, &e.ObservationSource, &e.ReceivedAt,
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
