package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// ContactImportJob is a claimed import handed to the runner.
type ContactImportJob struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	CreatedBy *uuid.UUID
	Filename  string
	Options   []byte
	Attempts  int
}

// ContactImportFileRow is one stored row of an uploaded file.
type ContactImportFileRow struct {
	Line   int
	Cells  []string
	Email  string
	Reason string
}

// ContactImportRepository stores contact imports and their rows. Every read a
// caller can reach is scoped by organization; the runner's calls are by id.
type ContactImportRepository interface {
	// Create stores a draft with every parsed row of the file, line i+1 for rows[i].
	Create(ctx context.Context, imp *models.ContactImport, rows [][]string) error
	// Get is nil when the import is not in the organization.
	Get(ctx context.Context, orgID, id uuid.UUID) (*models.ContactImport, error)
	List(ctx context.Context, orgID uuid.UUID, before *time.Time, beforeID *uuid.UUID, limit int) ([]models.ContactImport, error)
	// CountActive is how many drafts and runs the organization holds.
	CountActive(ctx context.Context, orgID uuid.UUID) (int, error)
	// Rows returns the stored rows in line order, only unsettled ones when pendingOnly.
	Rows(ctx context.Context, id uuid.UUID, pendingOnly bool) ([]ContactImportFileRow, error)
	// SaveDraft stores a draft's in-progress options. False when it is not a draft.
	SaveDraft(ctx context.Context, orgID, id uuid.UUID, options []byte) (bool, error)
	// Queue moves a draft to queued with its final header choice and options.
	// False when the import is not a draft any more.
	Queue(ctx context.Context, orgID, id uuid.UUID, hasHeader bool, columns []string, options []byte) (bool, error)
	// Cancel stops a draft, queued or running import. False when it had already finished.
	Cancel(ctx context.Context, orgID, id uuid.UUID) (bool, error)
	// Claim takes the oldest queued import, or a running one whose lease lapsed.
	Claim(ctx context.Context, lease time.Duration) (*ContactImportJob, error)
	// Touch renews a claim's lease; false when it was cancelled or claimed again.
	Touch(ctx context.Context, id uuid.UUID, attempt int, lease time.Duration) (bool, error)
	// Settle records the outcome of each row.
	Settle(ctx context.Context, id uuid.UUID, outcomes []models.ContactImportRowOutcome) error
	// TouchedContacts are the contacts earlier runs of the import created, updated or linked.
	TouchedContacts(ctx context.Context, id uuid.UUID) ([]uuid.UUID, error)
	// Finish closes a claim. A cancelled import stays cancelled but keeps what the run learned.
	// It returns the status the import ended at, or "" when the claim was no longer held.
	Finish(ctx context.Context, id uuid.UUID, attempt int, status models.ContactImportStatus, quality *models.ContactImportQuality, segmentsPinned *bool, notes []string, errMsg string) (models.ContactImportStatus, error)
	// Failures are the first failed rows with their cells.
	Failures(ctx context.Context, orgID, id uuid.UUID, limit int) ([]models.ContactImportRowError, error)
	// PurgeExpired drops stale drafts and old finished imports.
	PurgeExpired(ctx context.Context, draftHours, retentionDays int) error
	GetMapping(ctx context.Context, orgID uuid.UUID, signature string) ([]models.ContactImportColumnMapping, bool, error)
	SaveMapping(ctx context.Context, orgID uuid.UUID, signature string, mapping []models.ContactImportColumnMapping) error
}

type contactImportRepository struct {
	DB *db.DB
}

func NewContactImportRepository(database *db.DB) ContactImportRepository {
	return &contactImportRepository{DB: database}
}

func (r *contactImportRepository) Create(ctx context.Context, imp *models.ContactImport, rows [][]string) error {
	columns, err := json.Marshal(imp.Columns)
	if err != nil {
		return err
	}
	var preview []byte
	if imp.Preview != nil {
		if preview, err = json.Marshal(imp.Preview); err != nil {
			return err
		}
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return err
	}
	defer tx.Rollback(ctx)

	query := `
		INSERT INTO contact_imports (id, organization_id, created_by, filename, format, status, has_header, columns, total, preview)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING created_at, updated_at`
	if err := tx.QueryRow(ctx, query, imp.ID, imp.OrganizationID, imp.CreatedBy, imp.Filename, imp.Format,
		imp.Status, imp.HasHeader, columns, imp.Total, preview).Scan(&imp.CreatedAt, &imp.UpdatedAt); err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return err
	}

	src := make([][]any, 0, len(rows))
	for i, row := range rows {
		if row == nil {
			row = []string{}
		}
		cells, err := json.Marshal(row)
		if err != nil {
			return err
		}
		src = append(src, []any{imp.ID, i + 1, cells})
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"contact_import_rows"},
		[]string{"import_id", "line", "cells"}, pgx.CopyFromRows(src)); err != nil {
		db.CaptureError(err, "copy contact_import_rows", nil, "copyfrom")
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return err
	}
	return nil
}

// contactImportSelect reads an import with its row counts. A draft's rows are
// all pending, so they are not counted; only a draft carries its preview.
const contactImportSelect = `
	SELECT i.id, i.organization_id, i.created_by, i.filename, i.format, i.status, i.has_header,
	       i.columns, i.options, CASE WHEN i.status = 'draft' THEN i.preview END,
	       i.total, i.quality, i.segments_pinned, i.notes, i.error,
	       i.created_at, i.updated_at, i.started_at, i.finished_at,
	       COALESCE(c.imported, 0), COALESCE(c.updated, 0), COALESCE(c.skipped, 0), COALESCE(c.failed, 0)
	FROM contact_imports i
	LEFT JOIN LATERAL (
		SELECT count(*) FILTER (WHERE r.status = 'imported') AS imported,
		       count(*) FILTER (WHERE r.status = 'updated') AS updated,
		       count(*) FILTER (WHERE r.status = 'skipped') AS skipped,
		       count(*) FILTER (WHERE r.status = 'failed') AS failed
		FROM contact_import_rows r
		WHERE r.import_id = i.id AND i.status <> 'draft'
	) c ON true`

func scanContactImport(row pgx.Row) (*models.ContactImport, error) {
	var imp models.ContactImport
	var columns, options, preview, quality, notes []byte
	if err := row.Scan(&imp.ID, &imp.OrganizationID, &imp.CreatedBy, &imp.Filename, &imp.Format, &imp.Status, &imp.HasHeader,
		&columns, &options, &preview, &imp.Total, &quality, &imp.SegmentsPinned, &notes, &imp.Error,
		&imp.CreatedAt, &imp.UpdatedAt, &imp.StartedAt, &imp.FinishedAt,
		&imp.Imported, &imp.Updated, &imp.Skipped, &imp.Failed); err != nil {
		return nil, err
	}
	imp.Columns, imp.Notes = []string{}, []string{}
	if err := json.Unmarshal(columns, &imp.Columns); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(notes, &imp.Notes); err != nil {
		return nil, err
	}
	if len(options) > 0 && string(options) != "{}" {
		var opts models.ContactImportCommit
		if err := json.Unmarshal(options, &opts); err != nil {
			return nil, err
		}
		imp.Options = &opts
	}
	if len(preview) > 0 {
		var p models.ContactImportPreview
		if err := json.Unmarshal(preview, &p); err != nil {
			return nil, err
		}
		imp.Preview = &p
	}
	if len(quality) > 0 {
		var q models.ContactImportQuality
		if err := json.Unmarshal(quality, &q); err != nil {
			return nil, err
		}
		imp.Quality = &q
	}
	imp.Processed = imp.Imported + imp.Updated + imp.Skipped + imp.Failed
	return &imp, nil
}

func (r *contactImportRepository) Get(ctx context.Context, orgID, id uuid.UUID) (*models.ContactImport, error) {
	query := contactImportSelect + ` WHERE i.organization_id = $1 AND i.id = $2`
	imp, err := scanContactImport(r.DB.QueryRow(ctx, query, orgID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return nil, err
	}
	return imp, nil
}

func (r *contactImportRepository) List(ctx context.Context, orgID uuid.UUID, before *time.Time, beforeID *uuid.UUID, limit int) ([]models.ContactImport, error) {
	query := contactImportSelect + `
		WHERE i.organization_id = $1
		  AND ($2::timestamptz IS NULL OR (i.created_at, i.id) < ($2, $3))
		ORDER BY i.created_at DESC, i.id DESC
		LIMIT $4`
	rows, err := r.DB.Query(ctx, query, orgID, before, beforeID, limit)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	out := []models.ContactImport{}
	for rows.Next() {
		imp, err := scanContactImport(rows)
		if err != nil {
			db.CaptureError(err, query, nil, "scan")
			return nil, err
		}
		out = append(out, *imp)
	}
	return out, rows.Err()
}

func (r *contactImportRepository) CountActive(ctx context.Context, orgID uuid.UUID) (int, error) {
	query := `SELECT count(*) FROM contact_imports WHERE organization_id = $1 AND status IN ('draft', 'queued', 'running')`
	var n int
	if err := r.DB.QueryRow(ctx, query, orgID).Scan(&n); err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return 0, err
	}
	return n, nil
}

func (r *contactImportRepository) Rows(ctx context.Context, id uuid.UUID, pendingOnly bool) ([]ContactImportFileRow, error) {
	query := `SELECT line, cells, email, reason FROM contact_import_rows
		WHERE import_id = $1 AND (NOT $2 OR status = 'pending') ORDER BY line`
	rows, err := r.DB.Query(ctx, query, id, pendingOnly)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	var out []ContactImportFileRow
	for rows.Next() {
		var row ContactImportFileRow
		var cells []byte
		if err := rows.Scan(&row.Line, &cells, &row.Email, &row.Reason); err != nil {
			db.CaptureError(err, query, nil, "scan")
			return nil, err
		}
		if err := json.Unmarshal(cells, &row.Cells); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *contactImportRepository) SaveDraft(ctx context.Context, orgID, id uuid.UUID, options []byte) (bool, error) {
	query := `UPDATE contact_imports SET options = $3, updated_at = now()
		WHERE organization_id = $1 AND id = $2 AND status = 'draft'`
	tag, err := r.DB.Exec(ctx, query, orgID, id, options)
	if err != nil {
		db.CaptureError(err, query, nil, "exec")
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *contactImportRepository) Queue(ctx context.Context, orgID, id uuid.UUID, hasHeader bool, columns []string, options []byte) (bool, error) {
	cols, err := json.Marshal(columns)
	if err != nil {
		return false, err
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return false, err
	}
	defer tx.Rollback(ctx)

	var status string
	query := `SELECT status FROM contact_imports WHERE organization_id = $1 AND id = $2 FOR UPDATE`
	if err := tx.QueryRow(ctx, query, orgID, id).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		db.CaptureError(err, query, nil, "queryrow")
		return false, err
	}
	if status != string(models.ContactImportDraft) {
		return false, nil
	}
	// The header row is not data; what is left is exactly what runs.
	if hasHeader {
		if _, err := tx.Exec(ctx, `DELETE FROM contact_import_rows WHERE import_id = $1 AND line = 1`, id); err != nil {
			db.CaptureError(err, "", nil, "exec")
			return false, err
		}
	}
	query = `
		UPDATE contact_imports
		SET status = 'queued', has_header = $2, columns = $3, options = $4, preview = NULL, updated_at = now(),
		    total = (SELECT count(*) FROM contact_import_rows WHERE import_id = $1)
		WHERE id = $1`
	if _, err := tx.Exec(ctx, query, id, hasHeader, cols, options); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return false, err
	}
	return true, nil
}

func (r *contactImportRepository) Cancel(ctx context.Context, orgID, id uuid.UUID) (bool, error) {
	query := `
		UPDATE contact_imports
		SET status = 'cancelled', finished_at = now(), lease_until = NULL, updated_at = now()
		WHERE organization_id = $1 AND id = $2 AND status IN ('draft', 'queued', 'running')`
	tag, err := r.DB.Exec(ctx, query, orgID, id)
	if err != nil {
		db.CaptureError(err, query, nil, "exec")
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *contactImportRepository) Claim(ctx context.Context, lease time.Duration) (*ContactImportJob, error) {
	// SKIP LOCKED lets every backend run the loop at once; the lease hands an
	// import back if the process running it dies.
	query := `
		UPDATE contact_imports i
		SET status = 'running', attempts = i.attempts + 1, lease_until = now() + $1::interval,
		    started_at = COALESCE(i.started_at, now()), updated_at = now()
		WHERE i.id = (
			SELECT id FROM contact_imports
			WHERE status = 'queued' OR (status = 'running' AND lease_until < now())
			ORDER BY created_at
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING i.id, i.organization_id, i.created_by, i.filename, i.options, i.attempts`
	var job ContactImportJob
	err := r.DB.QueryRow(ctx, query, lease.String()).Scan(&job.ID, &job.OrgID, &job.CreatedBy, &job.Filename, &job.Options, &job.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return nil, err
	}
	return &job, nil
}

func (r *contactImportRepository) Touch(ctx context.Context, id uuid.UUID, attempt int, lease time.Duration) (bool, error) {
	query := `UPDATE contact_imports SET lease_until = now() + $3::interval, updated_at = now()
		WHERE id = $1 AND attempts = $2 AND status = 'running'`
	tag, err := r.DB.Exec(ctx, query, id, attempt, lease.String())
	if err != nil {
		db.CaptureError(err, query, nil, "exec")
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *contactImportRepository) Settle(ctx context.Context, id uuid.UUID, outcomes []models.ContactImportRowOutcome) error {
	if len(outcomes) == 0 {
		return nil
	}
	lines := make([]int32, len(outcomes))
	statuses := make([]string, len(outcomes))
	emails := make([]string, len(outcomes))
	reasons := make([]string, len(outcomes))
	contacts := make([]string, len(outcomes))
	for i, o := range outcomes {
		lines[i], statuses[i], emails[i], reasons[i] = int32(o.Line), o.Status, o.Email, o.Reason
		if o.ContactID != nil {
			contacts[i] = o.ContactID.String()
		}
	}
	query := `
		UPDATE contact_import_rows r
		SET status = v.status, email = v.email, reason = v.reason, contact_id = NULLIF(v.contact_id, '')::uuid
		FROM unnest($2::int[], $3::text[], $4::text[], $5::text[], $6::text[]) AS v(line, status, email, reason, contact_id)
		WHERE r.import_id = $1 AND r.line = v.line`
	if _, err := r.DB.Exec(ctx, query, id, lines, statuses, emails, reasons, contacts); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}

func (r *contactImportRepository) TouchedContacts(ctx context.Context, id uuid.UUID) ([]uuid.UUID, error) {
	query := `SELECT contact_id FROM contact_import_rows
		WHERE import_id = $1 AND contact_id IS NOT NULL AND status IN ('imported', 'updated', 'skipped')`
	rows, err := r.DB.Query(ctx, query, id)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var cid uuid.UUID
		if err := rows.Scan(&cid); err != nil {
			db.CaptureError(err, query, nil, "scan")
			return nil, err
		}
		out = append(out, cid)
	}
	return out, rows.Err()
}

func (r *contactImportRepository) Finish(ctx context.Context, id uuid.UUID, attempt int, status models.ContactImportStatus, quality *models.ContactImportQuality, segmentsPinned *bool, notes []string, errMsg string) (models.ContactImportStatus, error) {
	var q []byte
	if quality != nil {
		var err error
		if q, err = json.Marshal(quality); err != nil {
			return "", err
		}
	}
	if notes == nil {
		notes = []string{}
	}
	n, err := json.Marshal(notes)
	if err != nil {
		return "", err
	}
	query := `
		UPDATE contact_imports
		SET status = CASE WHEN status = 'cancelled' THEN status ELSE $3 END,
		    quality = COALESCE($4::jsonb, quality), segments_pinned = $5, notes = $6, error = $7,
		    finished_at = COALESCE(finished_at, now()), lease_until = NULL, updated_at = now()
		WHERE id = $1 AND attempts = $2 AND status IN ('running', 'cancelled')
		RETURNING status`
	var ended string
	if err := r.DB.QueryRow(ctx, query, id, attempt, string(status), q, segmentsPinned, n, errMsg).Scan(&ended); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		db.CaptureError(err, query, nil, "queryrow")
		return "", err
	}
	return models.ContactImportStatus(ended), nil
}

func (r *contactImportRepository) Failures(ctx context.Context, orgID, id uuid.UUID, limit int) ([]models.ContactImportRowError, error) {
	query := `
		SELECT r.line, r.email, r.reason, r.cells
		FROM contact_import_rows r
		JOIN contact_imports i ON i.id = r.import_id
		WHERE i.organization_id = $1 AND r.import_id = $2 AND r.status = 'failed'
		ORDER BY r.line
		LIMIT $3`
	rows, err := r.DB.Query(ctx, query, orgID, id, limit)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	out := []models.ContactImportRowError{}
	for rows.Next() {
		var e models.ContactImportRowError
		var cells []byte
		if err := rows.Scan(&e.Line, &e.Email, &e.Reason, &cells); err != nil {
			db.CaptureError(err, query, nil, "scan")
			return nil, err
		}
		if err := json.Unmarshal(cells, &e.Values); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *contactImportRepository) PurgeExpired(ctx context.Context, draftHours, retentionDays int) error {
	query := `
		DELETE FROM contact_imports
		WHERE (status = 'draft' AND created_at < now() - make_interval(hours => $1))
		   OR (status = 'cancelled' AND started_at IS NULL AND finished_at < now() - make_interval(hours => $1))
		   OR (finished_at IS NOT NULL AND finished_at < now() - make_interval(days => $2))`
	if _, err := r.DB.Exec(ctx, query, draftHours, retentionDays); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}

func (r *contactImportRepository) GetMapping(ctx context.Context, orgID uuid.UUID, signature string) ([]models.ContactImportColumnMapping, bool, error) {
	query := `SELECT mapping FROM contact_import_mappings WHERE organization_id = $1 AND signature = $2`
	var raw []byte
	if err := r.DB.QueryRow(ctx, query, orgID, signature).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		db.CaptureError(err, query, nil, "queryrow")
		return nil, false, err
	}
	var m []models.ContactImportColumnMapping
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false, err
	}
	return m, true, nil
}

func (r *contactImportRepository) SaveMapping(ctx context.Context, orgID uuid.UUID, signature string, mapping []models.ContactImportColumnMapping) error {
	raw, err := json.Marshal(mapping)
	if err != nil {
		return err
	}
	query := `
		INSERT INTO contact_import_mappings (organization_id, signature, mapping, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (organization_id, signature) DO UPDATE SET mapping = EXCLUDED.mapping, updated_at = now()`
	if _, err := r.DB.Exec(ctx, query, orgID, signature, raw); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}
