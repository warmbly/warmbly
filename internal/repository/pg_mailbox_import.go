package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// MailboxImportRowInsert is one row of a new import as stored.
type MailboxImportRowInsert struct {
	Line     int
	Email    string
	Domain   string
	MailHost string
	Status   string
	// Payload is already sealed with the organization's DEK.
	Payload string
	Fields  map[string]string
	Code    string
	Cause   string
	Message string
}

// ImportWorkRow is a row handed to the runner, with what it needs from its import.
type ImportWorkRow struct {
	ImportID   uuid.UUID
	OrgID      uuid.UUID
	CreatedBy  *uuid.UUID
	OnExisting string
	Settings   []byte
	Line       int
	Email      string
	MailHost   string
	Payload    string
	Attempts   int
	// Code is the row's recorded code; set only by ParkedRows.
	Code string
	// AccountID is the mailbox an earlier claim of the row created; set by Claim.
	AccountID *uuid.UUID
	// ImportCreatedAt is when the import started; set by Claim.
	ImportCreatedAt time.Time
}

// ParkedVendorCause is the cause of a row waiting on its vendor to authorize Warmbly; its import is not finished.
const ParkedVendorCause = "vendor_authorizing"

// FinishedImport is an import the last row of which just settled.
type FinishedImport struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	CreatedBy *uuid.UUID
}

// PendingSignin is a row waiting on a sign-in whose mailbox now exists.
type PendingSignin struct {
	OrgID     uuid.UUID
	Email     string
	AccountID uuid.UUID
}

// MailboxImportRepository stores mailbox imports and hands their rows out to
// the runner. Every read a caller can reach is scoped by organization.
type MailboxImportRepository interface {
	Create(ctx context.Context, imp *models.MailboxImport, settings, columns []byte, rows []MailboxImportRowInsert) error
	Get(ctx context.Context, orgID, id uuid.UUID) (*models.MailboxImport, error)
	List(ctx context.Context, orgID uuid.UUID, before *time.Time, beforeID *uuid.UUID, limit int) ([]models.MailboxImport, error)
	ListRows(ctx context.Context, orgID, id uuid.UUID, statuses []string, cause string, afterLine, limit int) ([]models.MailboxImportRow, error)
	// GetRow is one row with its sealed payload.
	GetRow(ctx context.Context, orgID, id uuid.UUID, line int) (*models.MailboxImportRow, string, error)
	// Retryable lists failed rows that still hold credentials, optionally one cause or named lines.
	Retryable(ctx context.Context, orgID, id uuid.UUID, cause string, lines []int) ([]ImportWorkRow, error)
	// Requeue puts one row back in the queue; an empty payload keeps the stored one.
	Requeue(ctx context.Context, id uuid.UUID, line int, payload string) error
	// Reopen marks a finished import running again after a retry.
	Reopen(ctx context.Context, orgID, id uuid.UUID) error
	Cancel(ctx context.Context, orgID, id uuid.UUID) error
	Claim(ctx context.Context, limit int, lease time.Duration) ([]ImportWorkRow, error)
	// FinishRow records a row's outcome. With attempt > 0 it only applies to that claim of the row, so a
	// replica whose lease lapsed cannot overwrite the outcome another replica recorded.
	FinishRow(ctx context.Context, id uuid.UUID, line, attempt int, status, code, cause, message string, accountID *uuid.UUID, keepPayload bool) error
	// SetRowAccount records the mailbox a claimed row just created, before its settings are applied.
	SetRowAccount(ctx context.Context, id uuid.UUID, line, attempt int, accountID uuid.UUID) error
	// Release hands a claimed row that never started back to the queue without counting the claim.
	Release(ctx context.Context, id uuid.UUID, line, attempt int) error
	// Touch renews a claimed row's lease when work on it starts; false when another replica has it now.
	Touch(ctx context.Context, id uuid.UUID, line, attempt int, lease time.Duration) (bool, error)
	// SettleOrphans closes rows left running in imports that are no longer running.
	SettleOrphans(ctx context.Context) error
	// CompleteFinished closes running imports with nothing left to do.
	CompleteFinished(ctx context.Context, credentialDays int) ([]FinishedImport, error)
	// PurgeExpired drops sealed credentials past their window and old imports.
	PurgeExpired(ctx context.Context, retentionDays int) error
	// ResolveSignin marks the workspace's rows waiting on this address connected
	// and returns them with the payload they held, so their settings can apply.
	ResolveSignin(ctx context.Context, orgID uuid.UUID, email string, accountID uuid.UUID) ([]ImportWorkRow, error)
	PendingSignins(ctx context.Context, limit int) ([]PendingSignin, error)
	// ParkedRows lists rows waiting on sign-in under one cause that still hold credentials.
	ParkedRows(ctx context.Context, cause string, limit int) ([]ImportWorkRow, error)
	// SetRowMailHost records the host a vendor row resolved to.
	SetRowMailHost(ctx context.Context, id uuid.UUID, line int, mailHost string) error
	// SetParkedMessage rewrites a parked row's message; false when it already said that.
	SetParkedMessage(ctx context.Context, id uuid.UUID, line int, cause, message string) (bool, error)
	// Dismiss hides an import from List.
	Dismiss(ctx context.Context, orgID, id uuid.UUID) error
	// TouchParked moves rows still waiting to the back of ParkedRows, so no import holds the others back.
	TouchParked(ctx context.Context, cause string, rows []ImportWorkRow) error
	// CoveredSigninRows lists rows waiting on a Google or Microsoft sign-in, in imports not cancelled,
	// that an active grant of their workspace now covers. Code carries the row's cause.
	CoveredSigninRows(ctx context.Context, limit int) ([]ImportWorkRow, error)
	// ResumeParked queues a parked row again and reopens its import when it had completed.
	ResumeParked(ctx context.Context, id uuid.UUID, line int, cause string) error
	GetMapping(ctx context.Context, orgID uuid.UUID, signature string) (models.MailboxImportMapping, bool, error)
	SaveMapping(ctx context.Context, orgID uuid.UUID, signature string, mapping models.MailboxImportMapping) error
}

type mailboxImportRepository struct {
	DB *db.DB
}

func NewMailboxImportRepository(database *db.DB) MailboxImportRepository {
	return &mailboxImportRepository{DB: database}
}

func (r *mailboxImportRepository) Create(ctx context.Context, imp *models.MailboxImport, settings, columns []byte, rows []MailboxImportRowInsert) error {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return err
	}
	defer tx.Rollback(ctx)

	query := `
		INSERT INTO mailbox_imports (id, organization_id, created_by, source, filename, vendor, status, on_existing, settings, columns, total, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now(), now())
		RETURNING created_at, updated_at`
	if err := tx.QueryRow(ctx, query, imp.ID, imp.OrganizationID, imp.CreatedBy, imp.Source, imp.Filename, imp.Vendor,
		imp.Status, imp.OnExisting, settings, columns, imp.Total).Scan(&imp.CreatedAt, &imp.UpdatedAt); err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return err
	}

	src := make([][]any, 0, len(rows))
	for _, row := range rows {
		fields, err := json.Marshal(row.Fields)
		if err != nil {
			return err
		}
		src = append(src, []any{imp.ID, row.Line, row.Email, row.Domain, row.MailHost, row.Status, row.Payload, fields, row.Code, row.Cause, row.Message})
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"mailbox_import_rows"},
		[]string{"import_id", "line", "email", "domain", "mail_host", "status", "payload", "fields", "code", "cause", "message"},
		pgx.CopyFromRows(src)); err != nil {
		db.CaptureError(err, "copy mailbox_import_rows", nil, "copyfrom")
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return err
	}
	return nil
}

const importColumns = `id, organization_id, created_by, status, source, filename, vendor, on_existing,
	created_at, updated_at, finished_at, credentials_expire_at, total`

func scanImport(row pgx.Row, imp *models.MailboxImport) error {
	return row.Scan(&imp.ID, &imp.OrganizationID, &imp.CreatedBy, &imp.Status, &imp.Source, &imp.Filename, &imp.Vendor,
		&imp.OnExisting, &imp.CreatedAt, &imp.UpdatedAt, &imp.FinishedAt, &imp.CredentialsExpireAt, &imp.Total)
}

func (r *mailboxImportRepository) Get(ctx context.Context, orgID, id uuid.UUID) (*models.MailboxImport, error) {
	query := `SELECT ` + importColumns + ` FROM mailbox_imports WHERE organization_id = $1 AND id = $2`
	var imp models.MailboxImport
	if err := scanImport(r.DB.QueryRow(ctx, query, orgID, id), &imp); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		db.CaptureError(err, query, nil, "queryrow")
		return nil, err
	}
	if err := r.fillCounts(ctx, &imp); err != nil {
		return nil, err
	}
	return &imp, nil
}

// fillCounts adds the per-status counts and the causes of the rows that did not connect.
func (r *mailboxImportRepository) fillCounts(ctx context.Context, imp *models.MailboxImport) error {
	query := `SELECT status, count(*) FROM mailbox_import_rows WHERE import_id = $1 GROUP BY status`
	rows, err := r.DB.Query(ctx, query, imp.ID)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return err
	}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			rows.Close()
			db.CaptureError(err, query, nil, "scan")
			return err
		}
		switch status {
		case models.ImportRowQueued:
			imp.Counts.Queued = n
		case models.ImportRowRunning:
			imp.Counts.Running = n
		case models.ImportRowConnected:
			imp.Counts.Connected = n
		case models.ImportRowUpdated:
			imp.Counts.Updated = n
		case models.ImportRowSkipped:
			imp.Counts.Skipped = n
		case models.ImportRowFailed:
			imp.Counts.Failed = n
		case models.ImportRowNeedsSignin:
			imp.Counts.NeedsSignin = n
		case models.ImportRowCancelled:
			imp.Counts.Cancelled = n
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	query = `
		SELECT CASE code WHEN 'google_signin' THEN 'google' ELSE 'microsoft' END, count(*)
		FROM mailbox_import_rows
		WHERE import_id = $1 AND status = 'needs_signin' AND cause = '` + ParkedVendorCause + `'
		GROUP BY 1`
	arows, err := r.DB.Query(ctx, query, imp.ID)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return err
	}
	imp.Authorizing = map[string]int{}
	for arows.Next() {
		var provider string
		var n int
		if err := arows.Scan(&provider, &n); err != nil {
			arows.Close()
			db.CaptureError(err, query, nil, "scan")
			return err
		}
		imp.Authorizing[provider] = n
	}
	arows.Close()
	if err := arows.Err(); err != nil {
		return err
	}

	query = `
		SELECT cause, count(*), bool_or(payload <> '' AND status = 'failed')
		FROM mailbox_import_rows
		WHERE import_id = $1 AND status IN ('failed', 'needs_signin') AND cause <> ''
		GROUP BY cause
		ORDER BY count(*) DESC, cause`
	rows, err = r.DB.Query(ctx, query, imp.ID)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return err
	}
	defer rows.Close()
	imp.Causes = make([]models.MailboxImportCause, 0)
	for rows.Next() {
		var c models.MailboxImportCause
		if err := rows.Scan(&c.Cause, &c.Count, &c.Retryable); err != nil {
			db.CaptureError(err, query, nil, "scan")
			return err
		}
		imp.Causes = append(imp.Causes, c)
	}
	return rows.Err()
}

func (r *mailboxImportRepository) List(ctx context.Context, orgID uuid.UUID, before *time.Time, beforeID *uuid.UUID, limit int) ([]models.MailboxImport, error) {
	query := `SELECT ` + importColumns + ` FROM mailbox_imports
		WHERE organization_id = $1 AND dismissed_at IS NULL AND ($2::timestamptz IS NULL OR (created_at, id) < ($2, $3))
		ORDER BY created_at DESC, id DESC
		LIMIT $4`
	rows, err := r.DB.Query(ctx, query, orgID, before, beforeID, limit)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	out := make([]models.MailboxImport, 0)
	for rows.Next() {
		var imp models.MailboxImport
		if err := scanImport(rows, &imp); err != nil {
			rows.Close()
			db.CaptureError(err, query, nil, "scan")
			return nil, err
		}
		out = append(out, imp)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if err := r.fillCounts(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

const importRowColumns = `r.line, r.email, r.mail_host, r.status, r.code, r.cause, r.message, r.email_account_id,
	r.payload <> '', r.fields, r.updated_at`

func scanImportRow(row pgx.Row, out *models.MailboxImportRow, extra ...any) error {
	var fields []byte
	dest := []any{&out.Line, &out.Email, &out.MailHost, &out.Status, &out.Code, &out.Cause, &out.Message,
		&out.EmailAccountID, &out.Retryable, &fields, &out.UpdatedAt}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		return err
	}
	out.Fields = map[string]string{}
	if len(fields) > 0 {
		_ = json.Unmarshal(fields, &out.Fields)
	}
	// Only a failed row is retried; a sign-in row keeps its payload for its settings.
	out.Retryable = out.Retryable && out.Status == models.ImportRowFailed
	return nil
}

func (r *mailboxImportRepository) ListRows(ctx context.Context, orgID, id uuid.UUID, statuses []string, cause string, afterLine, limit int) ([]models.MailboxImportRow, error) {
	var statusParam any
	if len(statuses) > 0 {
		statusParam = statuses
	}
	query := `SELECT ` + importRowColumns + `
		FROM mailbox_import_rows r
		JOIN mailbox_imports i ON i.id = r.import_id
		WHERE i.organization_id = $1 AND r.import_id = $2
		  AND ($3::text[] IS NULL OR r.status = ANY($3))
		  AND ($4 = '' OR r.cause = $4)
		  AND r.line > $5
		ORDER BY r.line
		LIMIT $6`
	rows, err := r.DB.Query(ctx, query, orgID, id, statusParam, cause, afterLine, limit)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	out := make([]models.MailboxImportRow, 0)
	for rows.Next() {
		var row models.MailboxImportRow
		if err := scanImportRow(rows, &row); err != nil {
			db.CaptureError(err, query, nil, "scan")
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *mailboxImportRepository) GetRow(ctx context.Context, orgID, id uuid.UUID, line int) (*models.MailboxImportRow, string, error) {
	query := `SELECT ` + importRowColumns + `, r.payload
		FROM mailbox_import_rows r
		JOIN mailbox_imports i ON i.id = r.import_id
		WHERE i.organization_id = $1 AND r.import_id = $2 AND r.line = $3`
	var row models.MailboxImportRow
	var payload string
	if err := scanImportRow(r.DB.QueryRow(ctx, query, orgID, id, line), &row, &payload); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", nil
		}
		db.CaptureError(err, query, nil, "queryrow")
		return nil, "", err
	}
	return &row, payload, nil
}

func (r *mailboxImportRepository) Retryable(ctx context.Context, orgID, id uuid.UUID, cause string, lines []int) ([]ImportWorkRow, error) {
	var linesParam any
	if len(lines) > 0 {
		linesParam = lines
	}
	query := `
		SELECT r.import_id, i.organization_id, r.line, r.email, r.mail_host, r.payload
		FROM mailbox_import_rows r
		JOIN mailbox_imports i ON i.id = r.import_id
		WHERE i.organization_id = $1 AND r.import_id = $2 AND r.status = 'failed' AND r.payload <> ''
		  AND ($3 = '' OR r.cause = $3)
		  AND ($4::int[] IS NULL OR r.line = ANY($4))
		ORDER BY r.line`
	rows, err := r.DB.Query(ctx, query, orgID, id, cause, linesParam)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	var out []ImportWorkRow
	for rows.Next() {
		var w ImportWorkRow
		if err := rows.Scan(&w.ImportID, &w.OrgID, &w.Line, &w.Email, &w.MailHost, &w.Payload); err != nil {
			db.CaptureError(err, query, nil, "scan")
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (r *mailboxImportRepository) Requeue(ctx context.Context, id uuid.UUID, line int, payload string) error {
	query := `
		UPDATE mailbox_import_rows
		SET status = 'queued', lease_until = NULL, attempts = 0, code = '', cause = '', message = '',
		    payload = CASE WHEN $3 = '' THEN payload ELSE $3 END, updated_at = now()
		WHERE import_id = $1 AND line = $2 AND status = 'failed'`
	if _, err := r.DB.Exec(ctx, query, id, line, payload); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}

func (r *mailboxImportRepository) Reopen(ctx context.Context, orgID, id uuid.UUID) error {
	query := `UPDATE mailbox_imports SET status = 'running', finished_at = NULL, updated_at = now()
		WHERE organization_id = $1 AND id = $2`
	if _, err := r.DB.Exec(ctx, query, orgID, id); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}

func (r *mailboxImportRepository) Cancel(ctx context.Context, orgID, id uuid.UUID) error {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return err
	}
	defer tx.Rollback(ctx)
	query := `
		UPDATE mailbox_imports
		SET status = 'cancelled', finished_at = now(), updated_at = now(),
		    credentials_expire_at = now() + make_interval(days => $3)
		WHERE organization_id = $1 AND id = $2 AND status = 'running'`
	tag, err := tx.Exec(ctx, query, orgID, id, config.MailboxImportCredentialDays)
	if err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	query = `UPDATE mailbox_import_rows SET status = 'cancelled', payload = '', lease_until = NULL, updated_at = now()
		WHERE import_id = $1 AND status = 'queued'`
	if _, err := tx.Exec(ctx, query, id); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	// Rows parked on a vendor authorization fall back to sign-in, since nothing resumes a cancelled import.
	query = `UPDATE mailbox_import_rows SET cause = code, message = 'Waiting for someone to sign in as this mailbox.', updated_at = now()
		WHERE import_id = $1 AND status = 'needs_signin' AND cause = '` + ParkedVendorCause + `'`
	if _, err := tx.Exec(ctx, query, id); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return tx.Commit(ctx)
}

func (r *mailboxImportRepository) Claim(ctx context.Context, limit int, lease time.Duration) ([]ImportWorkRow, error) {
	// SKIP LOCKED lets every backend run the loop at once; the lease hands a
	// row back if the process working it dies.
	query := `
		WITH due AS (
			SELECT r.import_id, r.line
			FROM mailbox_import_rows r
			JOIN mailbox_imports i ON i.id = r.import_id
			WHERE i.status = 'running'
			  AND (r.status = 'queued' OR (r.status = 'running' AND r.lease_until < now()))
			-- Interleaved by line, so one large import does not hold every other workspace's back.
			ORDER BY r.line, i.created_at
			LIMIT $1
			FOR UPDATE OF r SKIP LOCKED
		)
		UPDATE mailbox_import_rows r
		SET status = 'running', lease_until = now() + $2::interval, attempts = r.attempts + 1, updated_at = now()
		FROM due, mailbox_imports i
		WHERE r.import_id = due.import_id AND r.line = due.line AND i.id = r.import_id
		RETURNING r.import_id, i.organization_id, i.created_by, i.on_existing, i.settings, r.line, r.email, r.mail_host, r.payload, r.attempts, r.email_account_id, i.created_at`
	rows, err := r.DB.Query(ctx, query, limit, lease.String())
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	var out []ImportWorkRow
	for rows.Next() {
		var w ImportWorkRow
		if err := rows.Scan(&w.ImportID, &w.OrgID, &w.CreatedBy, &w.OnExisting, &w.Settings, &w.Line, &w.Email, &w.MailHost, &w.Payload, &w.Attempts, &w.AccountID, &w.ImportCreatedAt); err != nil {
			db.CaptureError(err, query, nil, "scan")
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (r *mailboxImportRepository) FinishRow(ctx context.Context, id uuid.UUID, line, attempt int, status, code, cause, message string, accountID *uuid.UUID, keepPayload bool) error {
	query := `
		UPDATE mailbox_import_rows
		SET status = $3, code = $4, cause = $5, message = $6,
		    email_account_id = COALESCE($7, email_account_id),
		    payload = CASE WHEN $8 THEN payload ELSE '' END,
		    lease_until = NULL, updated_at = now()
		WHERE import_id = $1 AND line = $2
		  AND ($9 = 0 OR (status = 'running' AND attempts = $9))`
	if _, err := r.DB.Exec(ctx, query, id, line, status, code, cause, message, accountID, keepPayload, attempt); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	if _, err := r.DB.Exec(ctx, `UPDATE mailbox_imports SET updated_at = now() WHERE id = $1`, id); err != nil {
		db.CaptureError(err, "", nil, "exec")
	}
	return nil
}

func (r *mailboxImportRepository) SetRowAccount(ctx context.Context, id uuid.UUID, line, attempt int, accountID uuid.UUID) error {
	query := `UPDATE mailbox_import_rows SET email_account_id = $4
		WHERE import_id = $1 AND line = $2 AND status = 'running' AND attempts = $3`
	if _, err := r.DB.Exec(ctx, query, id, line, attempt, accountID); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}

func (r *mailboxImportRepository) Release(ctx context.Context, id uuid.UUID, line, attempt int) error {
	query := `UPDATE mailbox_import_rows SET status = 'queued', lease_until = NULL, attempts = attempts - 1
		WHERE import_id = $1 AND line = $2 AND status = 'running' AND attempts = $3`
	if _, err := r.DB.Exec(ctx, query, id, line, attempt); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}

func (r *mailboxImportRepository) Touch(ctx context.Context, id uuid.UUID, line, attempt int, lease time.Duration) (bool, error) {
	query := `UPDATE mailbox_import_rows SET lease_until = now() + $4::interval
		WHERE import_id = $1 AND line = $2 AND status = 'running' AND attempts = $3`
	tag, err := r.DB.Exec(ctx, query, id, line, attempt, lease.String())
	if err != nil {
		db.CaptureError(err, query, nil, "exec")
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *mailboxImportRepository) SettleOrphans(ctx context.Context) error {
	query := `
		UPDATE mailbox_import_rows r
		SET status = 'cancelled', payload = '', lease_until = NULL, updated_at = now()
		FROM mailbox_imports i
		WHERE i.id = r.import_id AND i.status <> 'running'
		  AND r.status IN ('queued', 'running') AND (r.lease_until IS NULL OR r.lease_until < now())`
	if _, err := r.DB.Exec(ctx, query); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}

func (r *mailboxImportRepository) CompleteFinished(ctx context.Context, credentialDays int) ([]FinishedImport, error) {
	query := `
		UPDATE mailbox_imports i
		SET status = 'completed', finished_at = now(), updated_at = now(),
		    credentials_expire_at = now() + make_interval(days => $1)
		WHERE i.status = 'running'
		  AND NOT EXISTS (
			SELECT 1 FROM mailbox_import_rows r
			WHERE r.import_id = i.id
			  AND (r.status IN ('queued', 'running') OR (r.status = 'needs_signin' AND r.cause = '` + ParkedVendorCause + `'))
		  )
		RETURNING i.id, i.organization_id, i.created_by`
	rows, err := r.DB.Query(ctx, query, credentialDays)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	var out []FinishedImport
	for rows.Next() {
		var f FinishedImport
		if err := rows.Scan(&f.ID, &f.OrgID, &f.CreatedBy); err != nil {
			db.CaptureError(err, query, nil, "scan")
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (r *mailboxImportRepository) PurgeExpired(ctx context.Context, retentionDays int) error {
	query := `
		UPDATE mailbox_import_rows r
		SET payload = ''
		FROM mailbox_imports i
		WHERE i.id = r.import_id AND r.payload <> ''
		  AND i.credentials_expire_at IS NOT NULL AND i.credentials_expire_at < now()
		  AND r.status NOT IN ('queued', 'running')`
	if _, err := r.DB.Exec(ctx, query); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	query = `DELETE FROM mailbox_imports WHERE finished_at IS NOT NULL AND finished_at < now() - make_interval(days => $1)`
	if _, err := r.DB.Exec(ctx, query, retentionDays); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}

func (r *mailboxImportRepository) ResolveSignin(ctx context.Context, orgID uuid.UUID, email string, accountID uuid.UUID) ([]ImportWorkRow, error) {
	query := `
		WITH hit AS (
			SELECT r.import_id, r.line, r.payload
			FROM mailbox_import_rows r
			JOIN mailbox_imports i ON i.id = r.import_id
			WHERE i.organization_id = $1 AND r.status = 'needs_signin' AND lower(r.email) = lower($2)
			FOR UPDATE OF r
		)
		UPDATE mailbox_import_rows r
		SET status = 'connected', email_account_id = $3, payload = '', code = '', cause = '', message = '', updated_at = now()
		FROM hit, mailbox_imports i
		WHERE r.import_id = hit.import_id AND r.line = hit.line AND i.id = r.import_id
		RETURNING r.import_id, i.organization_id, i.created_by, i.on_existing, i.settings, r.line, r.email, r.mail_host, hit.payload`
	rows, err := r.DB.Query(ctx, query, orgID, email, accountID)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	var out []ImportWorkRow
	for rows.Next() {
		var w ImportWorkRow
		if err := rows.Scan(&w.ImportID, &w.OrgID, &w.CreatedBy, &w.OnExisting, &w.Settings, &w.Line, &w.Email, &w.MailHost, &w.Payload); err != nil {
			db.CaptureError(err, query, nil, "scan")
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (r *mailboxImportRepository) PendingSignins(ctx context.Context, limit int) ([]PendingSignin, error) {
	query := `
		SELECT DISTINCT i.organization_id, r.email, ea.id
		FROM mailbox_import_rows r
		JOIN mailbox_imports i ON i.id = r.import_id
		JOIN email_accounts ea ON ea.organization_id = i.organization_id AND lower(ea.email) = lower(r.email)
		WHERE r.status = 'needs_signin'
		LIMIT $1`
	rows, err := r.DB.Query(ctx, query, limit)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	var out []PendingSignin
	for rows.Next() {
		var p PendingSignin
		if err := rows.Scan(&p.OrgID, &p.Email, &p.AccountID); err != nil {
			db.CaptureError(err, query, nil, "scan")
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *mailboxImportRepository) ParkedRows(ctx context.Context, cause string, limit int) ([]ImportWorkRow, error) {
	query := `
		SELECT r.import_id, i.organization_id, i.created_by, r.line, r.email, r.code, r.payload
		FROM mailbox_import_rows r
		JOIN mailbox_imports i ON i.id = r.import_id
		WHERE r.status = 'needs_signin' AND r.cause = $1 AND r.payload <> '' AND i.status = 'running'
		ORDER BY r.updated_at
		LIMIT $2`
	rows, err := r.DB.Query(ctx, query, cause, limit)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	var out []ImportWorkRow
	for rows.Next() {
		var w ImportWorkRow
		if err := rows.Scan(&w.ImportID, &w.OrgID, &w.CreatedBy, &w.Line, &w.Email, &w.Code, &w.Payload); err != nil {
			db.CaptureError(err, query, nil, "scan")
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (r *mailboxImportRepository) SetRowMailHost(ctx context.Context, id uuid.UUID, line int, mailHost string) error {
	query := `UPDATE mailbox_import_rows SET mail_host = $3 WHERE import_id = $1 AND line = $2`
	if _, err := r.DB.Exec(ctx, query, id, line, mailHost); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}

func (r *mailboxImportRepository) SetParkedMessage(ctx context.Context, id uuid.UUID, line int, cause, message string) (bool, error) {
	query := `UPDATE mailbox_import_rows SET message = $4, updated_at = now()
		WHERE import_id = $1 AND line = $2 AND status = 'needs_signin' AND cause = $3 AND message <> $4`
	tag, err := r.DB.Exec(ctx, query, id, line, cause, message)
	if err != nil {
		db.CaptureError(err, query, nil, "exec")
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *mailboxImportRepository) Dismiss(ctx context.Context, orgID, id uuid.UUID) error {
	query := `UPDATE mailbox_imports SET dismissed_at = now(), updated_at = now()
		WHERE organization_id = $1 AND id = $2 AND dismissed_at IS NULL`
	if _, err := r.DB.Exec(ctx, query, orgID, id); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}

func (r *mailboxImportRepository) TouchParked(ctx context.Context, cause string, rows []ImportWorkRow) error {
	if len(rows) == 0 {
		return nil
	}
	ids, lines := make([]uuid.UUID, len(rows)), make([]int32, len(rows))
	for i, w := range rows {
		ids[i], lines[i] = w.ImportID, int32(w.Line)
	}
	query := `
		UPDATE mailbox_import_rows r SET updated_at = now()
		FROM unnest($2::uuid[], $3::int[]) AS k(import_id, line)
		WHERE r.import_id = k.import_id AND r.line = k.line AND r.status = 'needs_signin' AND r.cause = $1`
	if _, err := r.DB.Exec(ctx, query, cause, ids, lines); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}

func (r *mailboxImportRepository) CoveredSigninRows(ctx context.Context, limit int) ([]ImportWorkRow, error) {
	// Coverage is decided here, so rows no grant covers can never crowd out the ones it does.
	query := `
		SELECT r.import_id, i.organization_id, r.line, r.email, r.mail_host, r.cause
		FROM mailbox_import_rows r
		JOIN mailbox_imports i ON i.id = r.import_id
		WHERE r.status = 'needs_signin' AND r.cause IN ('microsoft_signin', 'google_signin')
		  AND r.payload <> '' AND i.status <> 'cancelled'
		  AND EXISTS (
			SELECT 1 FROM mailbox_domain_grants g
			WHERE g.organization_id = i.organization_id AND g.status = 'active'
			  AND g.provider = CASE r.cause WHEN 'microsoft_signin' THEN 'microsoft' ELSE 'google' END
			  AND lower(split_part(r.email, '@', 2)) = ANY(g.domains)
		  )
		ORDER BY r.updated_at
		LIMIT $1`
	rows, err := r.DB.Query(ctx, query, limit)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	var out []ImportWorkRow
	for rows.Next() {
		var w ImportWorkRow
		if err := rows.Scan(&w.ImportID, &w.OrgID, &w.Line, &w.Email, &w.MailHost, &w.Code); err != nil {
			db.CaptureError(err, query, nil, "scan")
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (r *mailboxImportRepository) ResumeParked(ctx context.Context, id uuid.UUID, line int, cause string) error {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return err
	}
	defer tx.Rollback(ctx)
	query := `
		UPDATE mailbox_import_rows
		SET status = 'queued', lease_until = NULL, attempts = 0, code = '', cause = '', message = '', updated_at = now()
		WHERE import_id = $1 AND line = $2 AND status = 'needs_signin' AND cause = $3`
	tag, err := tx.Exec(ctx, query, id, line, cause)
	if err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	query = `UPDATE mailbox_imports SET status = 'running', finished_at = NULL, credentials_expire_at = NULL, updated_at = now()
		WHERE id = $1 AND status = 'completed'`
	if _, err := tx.Exec(ctx, query, id); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return tx.Commit(ctx)
}

func (r *mailboxImportRepository) GetMapping(ctx context.Context, orgID uuid.UUID, signature string) (models.MailboxImportMapping, bool, error) {
	query := `SELECT mapping FROM mailbox_import_mappings WHERE organization_id = $1 AND signature = $2`
	var raw []byte
	if err := r.DB.QueryRow(ctx, query, orgID, signature).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		db.CaptureError(err, query, nil, "queryrow")
		return nil, false, err
	}
	var m models.MailboxImportMapping
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false, fmt.Errorf("mailbox import mapping: %w", err)
	}
	return m, true, nil
}

func (r *mailboxImportRepository) SaveMapping(ctx context.Context, orgID uuid.UUID, signature string, mapping models.MailboxImportMapping) error {
	raw, err := json.Marshal(mapping)
	if err != nil {
		return err
	}
	query := `
		INSERT INTO mailbox_import_mappings (organization_id, signature, mapping, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (organization_id, signature) DO UPDATE SET mapping = EXCLUDED.mapping, updated_at = now()`
	if _, err := r.DB.Exec(ctx, query, orgID, signature, raw); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}
