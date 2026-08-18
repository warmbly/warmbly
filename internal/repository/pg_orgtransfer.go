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

// OrgTransferRepository moves whole organizations in and out of the database.
//
// Unlike every other repository here it is deliberately schema-generic: it
// reads and writes tables by name rather than through typed structs. That is
// the only way an archive stays correct as the schema grows, and it is why
// rows travel as jsonb — Postgres converts every column type in both
// directions, so arrays, jsonb, tsvector, inet, and enums need no Go-side
// mapping that could drift.
//
// Identifier safety: table names come from the compiled registry, and column
// names are always intersected against the destination catalog before they
// reach a query. Nothing from an uploaded archive is ever interpolated.
type OrgTransferRepository interface {
	// ---- schema introspection ----

	// TableColumns returns the destination's columns for a table, or nil when
	// the table does not exist here at all.
	TableColumns(ctx context.Context, table string) ([]ColumnInfo, error)
	// PrimaryKeyColumns returns the table's primary key, empty when it has none.
	PrimaryKeyColumns(ctx context.Context, table string) ([]string, error)
	// ForeignKeyRefs maps each column with a declared foreign key to the table
	// it points at. The importer uses it to find references whose target is not
	// part of this import, and asking the catalog means a constraint added
	// later is covered without a code change.
	ForeignKeyRefs(ctx context.Context, table string) (map[string]string, error)

	// ---- export ----

	// StreamScoped calls fn once per row with that row as a JSON object. The
	// slice is only valid for the duration of the call.
	StreamScoped(ctx context.Context, table, scope string, orgID uuid.UUID, fn func(row []byte) error) error
	OrganizationRow(ctx context.Context, orgID uuid.UUID) ([]byte, error)
	ArchiveMembers(ctx context.Context, orgID uuid.UUID) ([]models.OrgArchiveUser, error)

	// ---- import ----

	// CountExisting reports how many of the supplied primary-key values are
	// already present, for the preflight report.
	CountExisting(ctx context.Context, table string, pk []string, rows []json.RawMessage) (int64, error)
	// InsertBatch writes rows into table, honouring the conflict strategy.
	InsertBatch(ctx context.Context, tx pgx.Tx, table string, cols []string, rows []json.RawMessage, conflict models.OrgImportConflict, pk []string) (int64, error)
	// MergeOrganization applies the archive's organization row onto an
	// existing workspace, restricted to cols.
	MergeOrganization(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, cols []string, row json.RawMessage) error
	// ResolveUsersByEmail maps lowercased emails to destination user ids.
	ResolveUsersByEmail(ctx context.Context, emails []string) (map[string]uuid.UUID, error)
	// MarkMailboxesNeedingReconnect flags imported mailboxes whose credentials
	// did not travel, so the dashboard prompts for a reconnect instead of the
	// worker failing every send against an empty password.
	MarkMailboxesNeedingReconnect(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) (int64, error)
	// Begin starts the import transaction.
	Begin(ctx context.Context) (pgx.Tx, error)

	// ---- export jobs ----

	CreateExportJob(ctx context.Context, j *models.OrgExportJob) error
	GetExportJob(ctx context.Context, orgID, id uuid.UUID) (*models.OrgExportJob, error)
	ListExportJobs(ctx context.Context, orgID uuid.UUID, limit int) ([]models.OrgExportJob, error)
	UpdateExportProgress(ctx context.Context, id uuid.UUID, percent int, stage string) error
	CompleteExportJob(ctx context.Context, id uuid.UUID, key string, size int64, sha string, counts map[string]int64, expiresAt time.Time) error
	FailExportJob(ctx context.Context, id uuid.UUID, reason string) error
	DeleteExportJob(ctx context.Context, orgID, id uuid.UUID) (string, error)
	ListExpiredExports(ctx context.Context, now time.Time, limit int) ([]models.OrgExportJob, error)
	MarkExportExpired(ctx context.Context, id uuid.UUID) error
	HasActiveExport(ctx context.Context, orgID uuid.UUID) (bool, error)

	// ---- import jobs ----

	CreateImportJob(ctx context.Context, j *models.OrgImportJob) error
	GetImportJob(ctx context.Context, orgID, id uuid.UUID) (*models.OrgImportJob, error)
	ListImportJobs(ctx context.Context, orgID uuid.UUID, limit int) ([]models.OrgImportJob, error)
	UpdateImportProgress(ctx context.Context, id uuid.UUID, percent int, stage string) error
	CompleteImportJob(ctx context.Context, id uuid.UUID, counts map[string]int64, warnings []string) error
	FailImportJob(ctx context.Context, id uuid.UUID, reason string) error
	HasActiveImport(ctx context.Context, orgID uuid.UUID) (bool, error)

	// FailStaleTransfers closes out jobs whose process died mid-run. Transfers
	// execute in the accepting process (so a passphrase is never persisted),
	// which means a restart leaves a job running forever without this.
	FailStaleTransfers(ctx context.Context, before time.Time) error
}

// ColumnInfo is one destination column. Generated and identity columns cannot
// be written, so they are filtered out of every INSERT; NotNull decides whether
// a reference the import cannot satisfy can be blanked or has to be redirected.
type ColumnInfo struct {
	Name        string
	IsGenerated bool
	IsIdentity  bool
	NotNull     bool
}

// Writable reports whether the column accepts a supplied value.
func (c ColumnInfo) Writable() bool { return !c.IsGenerated && !c.IsIdentity }

type orgTransferRepository struct {
	DB *db.DB
}

func NewOrgTransferRepository(database *db.DB) OrgTransferRepository {
	return &orgTransferRepository{DB: database}
}

// quoteIdent renders one identifier safely. Every identifier reaching this
// point has already been matched against the catalog; quoting is the second
// line of defence and the reason a column named "order" works at all.
func quoteIdent(name string) string {
	return pgx.Identifier{name}.Sanitize()
}

func quoteIdents(names []string) string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = quoteIdent(n)
	}
	return strings.Join(out, ", ")
}

func (r *orgTransferRepository) Begin(ctx context.Context) (pgx.Tx, error) {
	return r.DB.Begin(ctx)
}

// ---------- schema introspection ----------

func (r *orgTransferRepository) TableColumns(ctx context.Context, table string) ([]ColumnInfo, error) {
	rows, err := r.DB.Query(ctx, `
		SELECT a.attname,
		       a.attgenerated <> '' AS is_generated,
		       a.attidentity <> ''  AS is_identity,
		       a.attnotnull         AS not_null
		  FROM pg_attribute a
		  JOIN pg_class c     ON c.oid = a.attrelid
		  JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public'
		   AND c.relname = $1
		   AND c.relkind = 'r'
		   AND a.attnum > 0
		   AND NOT a.attisdropped
		 ORDER BY a.attnum
	`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ColumnInfo
	for rows.Next() {
		var c ColumnInfo
		if err := rows.Scan(&c.Name, &c.IsGenerated, &c.IsIdentity, &c.NotNull); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *orgTransferRepository) PrimaryKeyColumns(ctx context.Context, table string) ([]string, error) {
	rows, err := r.DB.Query(ctx, `
		SELECT a.attname
		  FROM pg_index i
		  JOIN pg_class c     ON c.oid = i.indrelid
		  JOIN pg_namespace n ON n.oid = c.relnamespace
		  JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = ANY (i.indkey)
		 WHERE n.nspname = 'public'
		   AND c.relname = $1
		   AND i.indisprimary
		 ORDER BY array_position(i.indkey, a.attnum)
	`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func (r *orgTransferRepository) ForeignKeyRefs(ctx context.Context, table string) (map[string]string, error) {
	rows, err := r.DB.Query(ctx, `
		SELECT a.attname, rc.relname
		  FROM pg_constraint con
		  JOIN pg_class c      ON c.oid = con.conrelid
		  JOIN pg_namespace n  ON n.oid = c.relnamespace
		  JOIN pg_class rc     ON rc.oid = con.confrelid
		  JOIN pg_attribute a  ON a.attrelid = c.oid AND a.attnum = ANY (con.conkey)
		 WHERE con.contype = 'f'
		   AND n.nspname = 'public'
		   AND c.relname = $1
	`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var column, target string
		if err := rows.Scan(&column, &target); err != nil {
			return nil, err
		}
		out[column] = target
	}
	return out, rows.Err()
}

// ---------- export ----------

// StreamScoped reads a table with to_jsonb so every column type is rendered by
// Postgres itself. It runs inside a transaction with the statement timeout
// lifted: the pool sets a 60s default, and a full inbox export legitimately
// runs longer than that.
func (r *orgTransferRepository) StreamScoped(ctx context.Context, table, scope string, orgID uuid.UUID, fn func(row []byte) error) error {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout = 0`); err != nil {
		return fmt.Errorf("lift statement timeout: %w", err)
	}

	q := `SELECT to_jsonb(t) FROM public.` + quoteIdent(table) + ` t WHERE ` + scope
	rows, err := tx.Query(ctx, q, orgID)
	if err != nil {
		return fmt.Errorf("read %s: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return fmt.Errorf("scan %s: %w", table, err)
		}
		if err := fn(raw); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read %s: %w", table, err)
	}
	return nil
}

func (r *orgTransferRepository) OrganizationRow(ctx context.Context, orgID uuid.UUID) ([]byte, error) {
	var raw []byte
	err := r.DB.QueryRow(ctx, `SELECT to_jsonb(o) FROM organizations o WHERE o.id = $1`, orgID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return raw, err
}

func (r *orgTransferRepository) ArchiveMembers(ctx context.Context, orgID uuid.UUID) ([]models.OrgArchiveUser, error) {
	rows, err := r.DB.Query(ctx, `
		SELECT u.id, u.email, u.first_name, u.last_name, m.role,
		       (o.owner_user_id = u.id) AS is_owner
		  FROM organization_members m
		  JOIN users u         ON u.id = m.user_id
		  JOIN organizations o ON o.id = m.organization_id
		 WHERE m.organization_id = $1
		 ORDER BY is_owner DESC, u.email
	`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.OrgArchiveUser
	for rows.Next() {
		var u models.OrgArchiveUser
		if err := rows.Scan(&u.ID, &u.Email, &u.FirstName, &u.LastName, &u.Role, &u.IsOwner); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ---------- import ----------

func (r *orgTransferRepository) CountExisting(ctx context.Context, table string, pk []string, rows []json.RawMessage) (int64, error) {
	if len(pk) == 0 || len(rows) == 0 {
		return 0, nil
	}
	payload, err := json.Marshal(rows)
	if err != nil {
		return 0, err
	}
	// Compare the candidate keys against the live table by projecting both
	// sides through the table's own row type, so a uuid in the archive matches
	// a uuid column rather than being compared as text.
	q := `
		SELECT count(*)
		  FROM jsonb_populate_recordset(NULL::public.` + quoteIdent(table) + `, $1::jsonb) AS c
		 WHERE EXISTS (
		       SELECT 1 FROM public.` + quoteIdent(table) + ` AS e
		        WHERE ` + matchClause(pk) + `
		 )`
	var n int64
	if err := r.DB.QueryRow(ctx, q, payload).Scan(&n); err != nil {
		return 0, fmt.Errorf("count existing %s: %w", table, err)
	}
	return n, nil
}

// matchClause builds `e.a IS NOT DISTINCT FROM c.a AND ...` for a key.
func matchClause(pk []string) string {
	parts := make([]string, len(pk))
	for i, c := range pk {
		parts[i] = "e." + quoteIdent(c) + " IS NOT DISTINCT FROM c." + quoteIdent(c)
	}
	return strings.Join(parts, " AND ")
}

func (r *orgTransferRepository) InsertBatch(
	ctx context.Context,
	tx pgx.Tx,
	table string,
	cols []string,
	rows []json.RawMessage,
	conflict models.OrgImportConflict,
	pk []string,
) (int64, error) {
	if len(rows) == 0 || len(cols) == 0 {
		return 0, nil
	}
	payload, err := json.Marshal(rows)
	if err != nil {
		return 0, err
	}

	quoted := quoteIdents(cols)
	q := `INSERT INTO public.` + quoteIdent(table) + ` (` + quoted + `)
	      SELECT ` + quoted + `
	        FROM jsonb_populate_recordset(NULL::public.` + quoteIdent(table) + `, $1::jsonb)`

	switch {
	case conflict == models.OrgImportConflictOverwrite && len(pk) > 0:
		sets := make([]string, 0, len(cols))
		key := make(map[string]bool, len(pk))
		for _, c := range pk {
			key[c] = true
		}
		for _, c := range cols {
			if key[c] {
				continue
			}
			sets = append(sets, quoteIdent(c)+" = EXCLUDED."+quoteIdent(c))
		}
		if len(sets) == 0 {
			// Every column is part of the key, so there is nothing to update.
			q += ` ON CONFLICT DO NOTHING`
		} else {
			q += ` ON CONFLICT (` + quoteIdents(pk) + `) DO UPDATE SET ` + strings.Join(sets, ", ")
		}
	default:
		// Untargeted, so it covers every unique constraint on the table, not
		// just the primary key. An archive row that collides on a secondary
		// unique index is skipped rather than aborting the whole import.
		q += ` ON CONFLICT DO NOTHING`
	}

	tag, err := tx.Exec(ctx, q, payload)
	if err != nil {
		return 0, fmt.Errorf("insert into %s: %w", table, err)
	}
	return tag.RowsAffected(), nil
}

func (r *orgTransferRepository) MergeOrganization(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, cols []string, row json.RawMessage) error {
	if len(cols) == 0 {
		return nil
	}
	sets := make([]string, len(cols))
	for i, c := range cols {
		sets[i] = quoteIdent(c) + " = s." + quoteIdent(c)
	}
	q := `UPDATE organizations o
	         SET ` + strings.Join(sets, ", ") + `
	        FROM jsonb_populate_record(NULL::organizations, $1::jsonb) AS s
	       WHERE o.id = $2`
	if _, err := tx.Exec(ctx, q, row, orgID); err != nil {
		return fmt.Errorf("merge organization: %w", err)
	}
	return nil
}

// MarkMailboxesNeedingReconnect finds mailboxes in the org that have no usable
// credentials — no OAuth row, and no SMTP/IMAP row with a password — and moves
// them to 'revoked', which is the state the dashboard already renders as
// "reconnect this mailbox".
func (r *orgTransferRepository) MarkMailboxesNeedingReconnect(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) (int64, error) {
	tag, err := tx.Exec(ctx, `
		UPDATE email_accounts ea
		   SET status = 'revoked', updated_at = NOW()
		 WHERE ea.organization_id = $1
		   AND ea.status <> 'revoked'
		   AND NOT EXISTS (
		         SELECT 1 FROM email_accounts_oauth o
		          WHERE o.email_account_id = ea.id
		            AND COALESCE(o.refresh_token, '') <> ''
		   )
		   AND NOT EXISTS (
		         SELECT 1 FROM email_accounts_smtp_imap s
		          WHERE s.email_account_id = ea.id
		            AND COALESCE(s.smtp_password, '') <> ''
		   )
	`, orgID)
	if err != nil {
		return 0, fmt.Errorf("flag mailboxes needing reconnect: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *orgTransferRepository) ResolveUsersByEmail(ctx context.Context, emails []string) (map[string]uuid.UUID, error) {
	out := make(map[string]uuid.UUID, len(emails))
	if len(emails) == 0 {
		return out, nil
	}
	rows, err := r.DB.Query(ctx, `SELECT id, lower(email) FROM users WHERE lower(email) = ANY($1)`, emails)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id uuid.UUID
		var email string
		if err := rows.Scan(&id, &email); err != nil {
			return nil, err
		}
		out[email] = id
	}
	return out, rows.Err()
}

// ---------- export jobs ----------

const exportJobCols = `id, organization_id, requested_by, status, groups, include_secrets,
	format_version, progress_percent, progress_stage, archive_key, archive_bytes,
	archive_sha256, row_counts, error_message, started_at, completed_at, expires_at,
	created_at, updated_at`

func scanExportJob(row pgx.Row) (*models.OrgExportJob, error) {
	var j models.OrgExportJob
	var groups []string
	var counts []byte
	if err := row.Scan(
		&j.ID, &j.OrganizationID, &j.RequestedBy, &j.Status, &groups, &j.IncludeSecrets,
		&j.FormatVersion, &j.ProgressPercent, &j.ProgressStage, &j.ArchiveKey, &j.ArchiveBytes,
		&j.ArchiveSHA256, &counts, &j.ErrorMessage, &j.StartedAt, &j.CompletedAt, &j.ExpiresAt,
		&j.CreatedAt, &j.UpdatedAt,
	); err != nil {
		return nil, err
	}
	j.Groups = toGroups(groups)
	j.RowCounts = decodeCounts(counts)
	return &j, nil
}

func toGroups(in []string) []models.OrgDataGroup {
	out := make([]models.OrgDataGroup, len(in))
	for i, g := range in {
		out[i] = models.OrgDataGroup(g)
	}
	return out
}

func fromGroups(in []models.OrgDataGroup) []string {
	out := make([]string, len(in))
	for i, g := range in {
		out[i] = string(g)
	}
	return out
}

func decodeCounts(raw []byte) map[string]int64 {
	out := map[string]int64{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func (r *orgTransferRepository) CreateExportJob(ctx context.Context, j *models.OrgExportJob) error {
	return r.DB.QueryRow(ctx, `
		INSERT INTO org_export_jobs (organization_id, requested_by, groups, include_secrets, format_version)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, status, created_at, updated_at
	`, j.OrganizationID, j.RequestedBy, fromGroups(j.Groups), j.IncludeSecrets, models.OrgTransferFormatVersion,
	).Scan(&j.ID, &j.Status, &j.CreatedAt, &j.UpdatedAt)
}

func (r *orgTransferRepository) GetExportJob(ctx context.Context, orgID, id uuid.UUID) (*models.OrgExportJob, error) {
	j, err := scanExportJob(r.DB.QueryRow(ctx,
		`SELECT `+exportJobCols+` FROM org_export_jobs WHERE id = $1 AND organization_id = $2`, id, orgID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return j, err
}

func (r *orgTransferRepository) ListExportJobs(ctx context.Context, orgID uuid.UUID, limit int) ([]models.OrgExportJob, error) {
	rows, err := r.DB.Query(ctx,
		`SELECT `+exportJobCols+` FROM org_export_jobs WHERE organization_id = $1 ORDER BY created_at DESC LIMIT $2`,
		orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.OrgExportJob{}
	for rows.Next() {
		j, err := scanExportJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

func (r *orgTransferRepository) UpdateExportProgress(ctx context.Context, id uuid.UUID, percent int, stage string) error {
	_, err := r.DB.Exec(ctx, `
		UPDATE org_export_jobs
		   SET progress_percent = $2, progress_stage = $3, updated_at = NOW()
		 WHERE id = $1
	`, id, clampPercent(percent), stage)
	return err
}

func (r *orgTransferRepository) CompleteExportJob(ctx context.Context, id uuid.UUID, key string, size int64, sha string, counts map[string]int64, expiresAt time.Time) error {
	raw, err := json.Marshal(counts)
	if err != nil {
		return err
	}
	_, err = r.DB.Exec(ctx, `
		UPDATE org_export_jobs
		   SET status = 'completed', progress_percent = 100, progress_stage = 'done',
		       archive_key = $2, archive_bytes = $3, archive_sha256 = $4,
		       row_counts = $5, expires_at = $6, completed_at = NOW(), updated_at = NOW()
		 WHERE id = $1
	`, id, key, size, sha, raw, expiresAt)
	return err
}

func (r *orgTransferRepository) FailExportJob(ctx context.Context, id uuid.UUID, reason string) error {
	_, err := r.DB.Exec(ctx, `
		UPDATE org_export_jobs
		   SET status = 'failed', error_message = $2, completed_at = NOW(), updated_at = NOW()
		 WHERE id = $1
	`, id, truncateReason(reason))
	return err
}

// DeleteExportJob removes the row and returns the archive key so the caller can
// drop the object too.
func (r *orgTransferRepository) DeleteExportJob(ctx context.Context, orgID, id uuid.UUID) (string, error) {
	var key *string
	err := r.DB.QueryRow(ctx, `
		DELETE FROM org_export_jobs WHERE id = $1 AND organization_id = $2 RETURNING archive_key
	`, id, orgID).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if key == nil {
		return "", nil
	}
	return *key, nil
}

func (r *orgTransferRepository) ListExpiredExports(ctx context.Context, now time.Time, limit int) ([]models.OrgExportJob, error) {
	rows, err := r.DB.Query(ctx, `
		SELECT `+exportJobCols+`
		  FROM org_export_jobs
		 WHERE status = 'completed' AND expires_at IS NOT NULL AND expires_at < $1
		 ORDER BY expires_at
		 LIMIT $2
	`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.OrgExportJob{}
	for rows.Next() {
		j, err := scanExportJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

func (r *orgTransferRepository) MarkExportExpired(ctx context.Context, id uuid.UUID) error {
	_, err := r.DB.Exec(ctx, `
		UPDATE org_export_jobs
		   SET status = 'expired', archive_key = NULL, progress_stage = 'expired', updated_at = NOW()
		 WHERE id = $1
	`, id)
	return err
}

func (r *orgTransferRepository) HasActiveExport(ctx context.Context, orgID uuid.UUID) (bool, error) {
	var exists bool
	err := r.DB.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM org_export_jobs WHERE organization_id = $1 AND status IN ('queued', 'running')
		)`, orgID).Scan(&exists)
	return exists, err
}

// ---------- import jobs ----------

const importJobCols = `id, organization_id, requested_by, status, archive_key, archive_bytes,
	archive_sha256, source_manifest, groups, conflict_strategy, progress_percent, progress_stage,
	row_counts, warnings, error_message, started_at, completed_at, created_at, updated_at`

func scanImportJob(row pgx.Row) (*models.OrgImportJob, error) {
	var j models.OrgImportJob
	var groups []string
	var manifest, counts, warnings []byte
	if err := row.Scan(
		&j.ID, &j.OrganizationID, &j.RequestedBy, &j.Status, &j.ArchiveKey, &j.ArchiveBytes,
		&j.ArchiveSHA256, &manifest, &groups, &j.ConflictStrategy, &j.ProgressPercent, &j.ProgressStage,
		&counts, &warnings, &j.ErrorMessage, &j.StartedAt, &j.CompletedAt, &j.CreatedAt, &j.UpdatedAt,
	); err != nil {
		return nil, err
	}
	j.Groups = toGroups(groups)
	j.RowCounts = decodeCounts(counts)
	j.Warnings = []string{}
	if len(warnings) > 0 {
		_ = json.Unmarshal(warnings, &j.Warnings)
	}
	if len(manifest) > 2 {
		var info models.OrgArchiveInfo
		if json.Unmarshal(manifest, &info) == nil {
			j.SourceManifest = &info
		}
	}
	return &j, nil
}

func (r *orgTransferRepository) CreateImportJob(ctx context.Context, j *models.OrgImportJob) error {
	manifest, err := json.Marshal(j.SourceManifest)
	if err != nil {
		return err
	}
	return r.DB.QueryRow(ctx, `
		INSERT INTO org_import_jobs
		  (organization_id, requested_by, archive_key, archive_bytes, archive_sha256,
		   source_manifest, groups, conflict_strategy)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, status, created_at, updated_at
	`, j.OrganizationID, j.RequestedBy, j.ArchiveKey, j.ArchiveBytes, j.ArchiveSHA256,
		manifest, fromGroups(j.Groups), j.ConflictStrategy,
	).Scan(&j.ID, &j.Status, &j.CreatedAt, &j.UpdatedAt)
}

func (r *orgTransferRepository) GetImportJob(ctx context.Context, orgID, id uuid.UUID) (*models.OrgImportJob, error) {
	j, err := scanImportJob(r.DB.QueryRow(ctx,
		`SELECT `+importJobCols+` FROM org_import_jobs WHERE id = $1 AND organization_id = $2`, id, orgID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return j, err
}

func (r *orgTransferRepository) ListImportJobs(ctx context.Context, orgID uuid.UUID, limit int) ([]models.OrgImportJob, error) {
	rows, err := r.DB.Query(ctx,
		`SELECT `+importJobCols+` FROM org_import_jobs WHERE organization_id = $1 ORDER BY created_at DESC LIMIT $2`,
		orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.OrgImportJob{}
	for rows.Next() {
		j, err := scanImportJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

func (r *orgTransferRepository) UpdateImportProgress(ctx context.Context, id uuid.UUID, percent int, stage string) error {
	_, err := r.DB.Exec(ctx, `
		UPDATE org_import_jobs
		   SET progress_percent = $2, progress_stage = $3, updated_at = NOW()
		 WHERE id = $1
	`, id, clampPercent(percent), stage)
	return err
}

func (r *orgTransferRepository) CompleteImportJob(ctx context.Context, id uuid.UUID, counts map[string]int64, warnings []string) error {
	rawCounts, err := json.Marshal(counts)
	if err != nil {
		return err
	}
	if warnings == nil {
		warnings = []string{}
	}
	rawWarnings, err := json.Marshal(warnings)
	if err != nil {
		return err
	}
	_, err = r.DB.Exec(ctx, `
		UPDATE org_import_jobs
		   SET status = 'completed', progress_percent = 100, progress_stage = 'done',
		       row_counts = $2, warnings = $3, completed_at = NOW(), updated_at = NOW()
		 WHERE id = $1
	`, id, rawCounts, rawWarnings)
	return err
}

func (r *orgTransferRepository) FailImportJob(ctx context.Context, id uuid.UUID, reason string) error {
	_, err := r.DB.Exec(ctx, `
		UPDATE org_import_jobs
		   SET status = 'failed', error_message = $2, completed_at = NOW(), updated_at = NOW()
		 WHERE id = $1
	`, id, truncateReason(reason))
	return err
}

func (r *orgTransferRepository) HasActiveImport(ctx context.Context, orgID uuid.UUID) (bool, error) {
	var exists bool
	err := r.DB.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM org_import_jobs WHERE organization_id = $1 AND status IN ('queued', 'running')
		)`, orgID).Scan(&exists)
	return exists, err
}

func (r *orgTransferRepository) FailStaleTransfers(ctx context.Context, before time.Time) error {
	const reason = "The instance restarted while this was running. Start it again."
	if _, err := r.DB.Exec(ctx, `
		UPDATE org_export_jobs
		   SET status = 'failed', error_message = $2, completed_at = NOW(), updated_at = NOW()
		 WHERE status IN ('queued', 'running') AND updated_at < $1
	`, before, reason); err != nil {
		return err
	}
	_, err := r.DB.Exec(ctx, `
		UPDATE org_import_jobs
		   SET status = 'failed', error_message = $2, completed_at = NOW(), updated_at = NOW()
		 WHERE status IN ('queued', 'running') AND updated_at < $1
	`, before, reason)
	return err
}

func clampPercent(p int) int {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

// truncateReason keeps a runaway driver error from filling the column.
func truncateReason(s string) string {
	const max = 4000
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
