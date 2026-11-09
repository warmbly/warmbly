package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

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
// ImportInvalidation clears derived rows keyed by records actually imported.
type ImportInvalidation struct {
	ResetColumns   []string
	Table          string
	Column         string
	Key            string
	LockNamespaces []int64
}

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
	// ForeignKeys lists the table's foreign keys with their column pairs, so a
	// composite key is checked as one reference.
	ForeignKeys(ctx context.Context, table string) ([]ForeignKey, error)

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
	// CountForeignRows counts rows whose primary key is already held by a row
	// outside owner, or whose reference resolves to a row outside its
	// target's owner. Every owner is a WHERE fragment with $1 the organization
	// id. tx may be nil outside an import.
	CountForeignRows(ctx context.Context, tx pgx.Tx, table, owner string, pk []string, refs []TenantReference, orgID uuid.UUID, rows []json.RawMessage) (int64, error)
	// InsertBatch writes rows into table, honouring the conflict strategy. An
	// overwrite only updates an existing row that owner selects for orgID.
	InsertBatch(ctx context.Context, tx pgx.Tx, table string, cols []string, rows []json.RawMessage, conflict models.OrgImportConflict, pk []string, owner string, orgID uuid.UUID, resetOnOverwrite []string, invalidations []ImportInvalidation) (int64, error)
	// DeveloperBlocked reports an operator's block on building apps for this
	// workspace or person.
	DeveloperBlocked(ctx context.Context, tx pgx.Tx, orgID, userID uuid.UUID) (bool, error)
	// MergeOrganization applies the archive's organization row onto an
	// existing workspace, restricted to cols.
	MergeOrganization(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, cols []string, row json.RawMessage) error
	// ResolveUsersByEmail maps lowercased emails to destination user ids.
	ResolveUsersByEmail(ctx context.Context, emails []string) (map[string]uuid.UUID, error)
	// MarkMailboxesNeedingReconnect flags imported mailboxes whose credentials
	// did not travel, so the dashboard prompts for a reconnect instead of the
	// worker failing every send against an empty password.
	MarkMailboxesNeedingReconnect(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) (int64, error)
	// MissingOAuthApps counts the archived OAuth app ids the destination workspace does not hold after their insert.
	MissingOAuthApps(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, ids []uuid.UUID) (int64, error)
	// OAuthAppSecrets reads the stored secrets of the destination's apps among ids, skipping empty ones.
	OAuthAppSecrets(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error)
	// RestoreOAuthAppSecret puts a destination app's secret back after a row without one overwrote it.
	RestoreOAuthAppSecret(ctx context.Context, tx pgx.Tx, orgID, id uuid.UUID, secret string) error
	// ChangedOAuthAppClients counts archived OAuth apps whose id the destination already holds under a different client.
	ChangedOAuthAppClients(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, ids []uuid.UUID, clientIDs []string) (int64, error)
	// MarkIntegrationsNeedingReconnect flags, once each, connections whose credentials or inbound secret did not travel.
	MarkIntegrationsNeedingReconnect(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, credentialsDetail, inboundDetail string) ([]IntegrationReconnect, error)
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
	// IsBytea marks a bytea column, which to_jsonb renders as a \x hex string.
	IsBytea bool
}

// Writable reports whether the column accepts a supplied value.
func (c ColumnInfo) Writable() bool { return !c.IsGenerated && !c.IsIdentity }

// ForeignKey is one declared foreign key; Columns[i] references RefColumns[i].
type ForeignKey struct {
	Columns    []string
	RefTable   string
	RefColumns []string
}

// TenantReference is a foreign key whose target rows belong to an
// organization, with the WHERE fragment ($1 the organization id) selecting them.
type TenantReference struct {
	ForeignKey
	RefOwner string
}

type orgTransferRepository struct {
	DB *db.DB
}

type orgImportTx struct {
	pgx.Tx
	indexedOrigin         bool
	importedSendTasks     []uuid.UUID
	importedHoldTasks     []uuid.UUID
	importedAttemptNonces []uuid.UUID
}

const (
	importOriginTask = iota + 1
	importOriginHold
	importOriginAttempt
)

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
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	indexed, err := setupImportOrigin(ctx, tx)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return &orgImportTx{Tx: tx, indexedOrigin: indexed, importedSendTasks: []uuid.UUID{}, importedHoldTasks: []uuid.UUID{}, importedAttemptNonces: []uuid.UUID{}}, nil
}

func setupImportOrigin(ctx context.Context, tx pgx.Tx) (bool, error) {
	if _, err := tx.Exec(ctx, `SAVEPOINT org_import_origin_setup`); err != nil {
		return false, fmt.Errorf("prepare import provenance: %w", err)
	}
	_, createErr := tx.Exec(ctx, `CREATE TEMP TABLE pg_temp.org_import_send_origin (
		kind smallint NOT NULL, id uuid NOT NULL, PRIMARY KEY (kind, id)) ON COMMIT DROP`)
	indexed := createErr == nil
	if createErr != nil {
		var pgErr *pgconn.PgError
		if !errors.As(createErr, &pgErr) || pgErr.Code != "42501" {
			return false, fmt.Errorf("create import provenance: %w", createErr)
		}
		if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT org_import_origin_setup`); err != nil {
			return false, fmt.Errorf("restore import after missing TEMP privilege: %w", err)
		}
		var allowed bool
		if err := tx.QueryRow(ctx, `SELECT has_database_privilege(current_user,current_database(),'TEMP')`).Scan(&allowed); err != nil {
			return false, fmt.Errorf("check import TEMP privilege after initialization failure: %w", errors.Join(createErr, err))
		}
		if allowed {
			return false, fmt.Errorf("create import provenance: %w", createErr)
		}
	}
	if _, err := tx.Exec(ctx, `RELEASE SAVEPOINT org_import_origin_setup`); err != nil {
		return false, fmt.Errorf("finish import provenance setup: %w", err)
	}
	return indexed, nil
}

// ---------- schema introspection ----------

func (r *orgTransferRepository) TableColumns(ctx context.Context, table string) ([]ColumnInfo, error) {
	rows, err := r.DB.Query(ctx, `
		SELECT a.attname,
		       a.attgenerated <> '' AS is_generated,
		       a.attidentity <> ''  AS is_identity,
		       a.attnotnull         AS not_null,
		       a.atttypid = 'bytea'::regtype AS is_bytea
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
		if err := rows.Scan(&c.Name, &c.IsGenerated, &c.IsIdentity, &c.NotNull, &c.IsBytea); err != nil {
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

func (r *orgTransferRepository) ForeignKeys(ctx context.Context, table string) ([]ForeignKey, error) {
	rows, err := r.DB.Query(ctx, `
		SELECT rc.relname,
		       ARRAY(SELECT a.attname::text FROM unnest(con.conkey) WITH ORDINALITY k(num, ord)
		               JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.num
		              ORDER BY k.ord),
		       ARRAY(SELECT a.attname::text FROM unnest(con.confkey) WITH ORDINALITY k(num, ord)
		               JOIN pg_attribute a ON a.attrelid = con.confrelid AND a.attnum = k.num
		              ORDER BY k.ord)
		  FROM pg_constraint con
		  JOIN pg_class c      ON c.oid = con.conrelid
		  JOIN pg_namespace n  ON n.oid = c.relnamespace
		  JOIN pg_class rc     ON rc.oid = con.confrelid
		 WHERE con.contype = 'f'
		   AND n.nspname = 'public'
		   AND c.relname = $1
		 ORDER BY con.conname
	`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ForeignKey
	for rows.Next() {
		var fk ForeignKey
		if err := rows.Scan(&fk.RefTable, &fk.Columns, &fk.RefColumns); err != nil {
			return nil, err
		}
		out = append(out, fk)
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

	q := `SELECT to_jsonb(` + quoteIdent(table) + `) FROM public.` + quoteIdent(table) + ` WHERE ` + scope
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

func (r *orgTransferRepository) CountForeignRows(
	ctx context.Context,
	tx pgx.Tx,
	table, owner string,
	pk []string,
	refs []TenantReference,
	orgID uuid.UUID,
	rows []json.RawMessage,
) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	// Each owner names its table's columns unqualified, so it sits alone in a
	// subquery's FROM and resolves against that row, never the archive row.
	var checks []string
	if len(pk) > 0 && owner != "" {
		checks = append(checks, `EXISTS (SELECT 1 FROM public.`+quoteIdent(table)+` AS existing
		        WHERE `+pairClause("existing", pk, "archive_row", pk)+` AND (`+owner+`) IS NOT TRUE)`)
	}
	for _, ref := range refs {
		if len(ref.Columns) == 0 || len(ref.Columns) != len(ref.RefColumns) || ref.RefOwner == "" {
			continue
		}
		checks = append(checks, `EXISTS (SELECT 1 FROM public.`+quoteIdent(ref.RefTable)+` AS target
		        WHERE `+pairClause("target", ref.RefColumns, "archive_row", ref.Columns)+` AND (`+ref.RefOwner+`) IS NOT TRUE)`)
	}
	if len(checks) == 0 {
		return 0, nil
	}
	payload, err := json.Marshal(rows)
	if err != nil {
		return 0, err
	}
	q := `SELECT count(*)
	        FROM jsonb_populate_recordset(NULL::public.` + quoteIdent(table) + `, $2::jsonb) AS archive_row
	       WHERE ` + strings.Join(checks, ` OR `)
	var row pgx.Row
	if tx != nil {
		row = tx.QueryRow(ctx, q, orgID, payload)
	} else {
		row = r.DB.QueryRow(ctx, q, orgID, payload)
	}
	var n int64
	if err := row.Scan(&n); err != nil {
		return 0, fmt.Errorf("check ownership of %s: %w", table, err)
	}
	return n, nil
}

// pairClause builds `l.a = r.x AND ...`; a NULL on either side matches nothing.
func pairClause(left string, leftCols []string, right string, rightCols []string) string {
	parts := make([]string, len(leftCols))
	for i := range leftCols {
		parts[i] = left + "." + quoteIdent(leftCols[i]) + " = " + right + "." + quoteIdent(rightCols[i])
	}
	return strings.Join(parts, " AND ")
}

// orgParam is an owner fragment's organization placeholder, never the start of $10.
var orgParam = regexp.MustCompile(`\$1\b`)

// bindOrgParam renumbers an owner fragment's $1 to $n.
func bindOrgParam(fragment string, n int) string {
	return orgParam.ReplaceAllLiteralString(fragment, "$"+strconv.Itoa(n))
}

// matchClause builds `e.a IS NOT DISTINCT FROM c.a AND ...` for a key.
func matchClause(pk []string) string {
	parts := make([]string, len(pk))
	for i, c := range pk {
		parts[i] = "e." + quoteIdent(c) + " IS NOT DISTINCT FROM c." + quoteIdent(c)
	}
	return strings.Join(parts, " AND ")
}

func importOriginContains(kind int, id string, indexed bool) string {
	if indexed {
		// Keep the lookup scalar to avoid de-correlation into a scan of the growing TEMP ledger.
		return `COALESCE((SELECT true FROM pg_temp.org_import_send_origin origin WHERE origin.kind=` + strconv.Itoa(kind) + ` AND origin.id=` + id + ` LIMIT 1),false)`
	}
	switch kind {
	case importOriginTask:
		return id + `=ANY($import_source_send_tasks::uuid[])`
	case importOriginHold:
		return id + `=ANY($import_source_hold_tasks::uuid[])`
	default:
		return id + `=ANY($import_source_attempt_nonces::uuid[])`
	}
}

func localImportSendTask(taskID string, indexed bool) string {
	return `EXISTS(SELECT 1 FROM tasks local_task WHERE local_task.id=` + taskID + ` AND (` + localImportSendTaskRow("local_task", indexed) + `))`
}

func localImportSendTaskRow(alias string, indexed bool) string {
	return strings.ReplaceAll(fmt.Sprintf(`NOT (%s) AND (
		local_task.send_executor_nonce IS NOT NULL
		OR local_task.send_reserved_at IS NOT NULL
		OR local_task.send_executor_started_at IS NOT NULL
		OR local_task.send_executor_result IS NOT NULL
		OR local_task.send_result_state IS NOT NULL
		OR local_task.send_result_evidence IS NOT NULL
		OR local_task.send_result_applied_at IS NOT NULL
		OR local_task.send_released_at IS NOT NULL
		OR (local_task.status='completed' AND local_task.completed_at IS NOT NULL)
		OR EXISTS(SELECT 1 FROM email_accounts local_mailbox WHERE local_mailbox.id=local_task.email_account_id
			AND local_mailbox.send_recovery_hold AND local_mailbox.send_recovery_task_id=local_task.id
			AND NOT (%s))
		OR EXISTS(SELECT 1 FROM outbound_attempts local_attempt WHERE local_attempt.task_id=local_task.id
			AND NOT (%s))
		OR EXISTS(SELECT 1 FROM warmup_tasks local_dispatch WHERE local_dispatch.task_id=local_task.id AND (
			local_dispatch.dispatch_nonce IS NOT NULL OR local_dispatch.dispatch_started_at IS NOT NULL
			OR local_dispatch.dispatch_result IS NOT NULL OR local_dispatch.warmup_charged_date IS NOT NULL
			OR local_dispatch.warmup_refunded_at IS NOT NULL))
		OR EXISTS(SELECT 1 FROM send_recovery_resolutions local_resolution WHERE local_resolution.recovery_task_id=local_task.id AND local_resolution.recorded_locally_at IS NOT NULL))`,
		importOriginContains(importOriginTask, "local_task.id", indexed),
		importOriginContains(importOriginHold, "local_task.id", indexed),
		importOriginContains(importOriginAttempt, "local_attempt.nonce", indexed)), "local_task.", alias+".")
}

func importSendOverwriteGuard(table string, indexed bool) string {
	switch table {
	case "tasks":
		return localImportSendTaskRow("dest", indexed)
	case "email_tasks", "campaign_tasks", "warmup_tasks", "warmup_tokens", "outbound_attempts", "task_failures", "task_dead_letters", "task_execution_keys":
		return localImportSendTask("dest.task_id", indexed)
	case "send_recovery_resolutions":
		return `dest.recorded_locally_at IS NOT NULL`
	default:
		return ""
	}
}

func importSendColumnValue(table, column string, indexed bool) string {
	value := "EXCLUDED." + quoteIdent(column)
	switch table {
	case "email_accounts":
		switch column {
		case "send_recovery_hold", "send_recovery_task_id", "send_recovery_reason", "send_recovery_probes", "send_recovery_probe_at":
			return `CASE WHEN dest.send_recovery_hold THEN dest.` + quoteIdent(column) + ` ELSE ` + value + ` END`
		case "send_cooldown_reason", "send_cooldown_strikes", "send_pause_detail":
			// The pause details follow whichever pause end the row keeps.
			return `CASE WHEN dest.send_cooldown_until>=COALESCE(EXCLUDED.send_cooldown_until,'-infinity'::timestamptz) THEN dest.` + quoteIdent(column) + ` ELSE ` + value + ` END`
		case "send_cooldown_until":
			return `GREATEST(dest.send_cooldown_until,` + value + `)`
		case "send_cooldown_provider":
			return `CASE WHEN dest.send_recovery_hold OR (dest.send_cooldown_until>NOW() AND dest.send_cooldown_until>COALESCE(EXCLUDED.send_cooldown_until,'-infinity'::timestamptz)) THEN dest.send_cooldown_provider ELSE ` + value + ` END`
		}
	case "warmup_statistics":
		if column == "emails_sent" || column == "emails_replied" {
			return `CASE WHEN EXISTS(SELECT 1 FROM warmup_tasks local_charge JOIN tasks local_task ON local_task.id=local_charge.task_id
				WHERE local_task.email_account_id=dest.email_account_id
				AND NOT (` + importOriginContains(importOriginTask, "local_task.id", indexed) + `) AND (
				(local_charge.warmup_charged_date=dest.date AND local_charge.warmup_reply_charged IS NOT NULL)
				OR (local_charge.warmup_charged_date IS NULL AND local_task.completed_at IS NOT NULL
				AND (local_task.completed_at AT TIME ZONE 'UTC')::date=dest.date
				AND (local_task.status='completed' OR local_task.send_result_state IS NOT NULL)))) THEN dest.` + quoteIdent(column) + ` ELSE ` + value + ` END`
		}
	}
	return value
}

var ErrImportSendRecoveryConflict = errors.New("archive and destination have different restrictive send-recovery holds, so nothing was imported")

func (r *orgTransferRepository) InsertBatch(
	ctx context.Context,
	tx pgx.Tx,
	table string,
	cols []string,
	rows []json.RawMessage,
	conflict models.OrgImportConflict,
	pk []string,
	owner string,
	orgID uuid.UUID,
	resetOnOverwrite []string,
	invalidations []ImportInvalidation,
) (int64, error) {
	if len(rows) == 0 || len(cols) == 0 {
		return 0, nil
	}
	payload, err := json.Marshal(rows)
	if err != nil {
		return 0, err
	}

	args := []any{payload}
	importTx, _ := tx.(*orgImportTx)
	indexed := importTx != nil && importTx.indexedOrigin
	quoted := quoteIdents(cols)
	q := `INSERT INTO public.` + quoteIdent(table) + ` AS dest (` + quoted + `)
	      SELECT ` + quoted + `
	        FROM jsonb_populate_recordset(NULL::public.` + quoteIdent(table) + `, $1::jsonb) AS incoming`
	if guard := importSendOverwriteGuard(table, indexed); guard != "" && table != "tasks" && table != "send_recovery_resolutions" {
		// New child keys cannot inject archive payload or execution evidence into a retained local task.
		q += ` WHERE NOT (` + strings.ReplaceAll(guard, "dest.", "incoming.") + `)`
	}

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
			sets = append(sets, quoteIdent(c)+" = "+importSendColumnValue(table, c, indexed))
		}
		for _, c := range resetOnOverwrite {
			sets = append(sets, quoteIdent(c)+" = DEFAULT")
		}
		if len(sets) == 0 {
			// Every column is part of the key, so there is nothing to update.
			q += ` ON CONFLICT DO NOTHING`
		} else {
			q += ` ON CONFLICT (` + quoteIdents(pk) + `) DO UPDATE SET ` + strings.Join(sets, ", ")
			var conditions []string
			if owner != "" {
				// The owner sits in its own FROM, since here an unqualified column is ambiguous with EXCLUDED.
				conditions = append(conditions, `EXISTS (SELECT 1 FROM public.`+quoteIdent(table)+` AS owned
				        WHERE `+pairClause("owned", pk, "dest", pk)+` AND (`+bindOrgParam(owner, 2)+`))`)
				args = append(args, orgID)
			}
			// Imported history cannot rewind an existing destination-local send or its application marker.
			if guard := importSendOverwriteGuard(table, indexed); guard != "" {
				conditions = append(conditions, `NOT (`+guard+`)`)
			}
			if len(conditions) > 0 {
				q += ` WHERE ` + strings.Join(conditions, ` AND `)
			}
		}
	default:
		// Untargeted, so it covers every unique constraint on the table, not
		// just the primary key. An archive row that collides on a secondary
		// unique index is skipped rather than aborting the whole import.
		q += ` ON CONFLICT DO NOTHING`
	}
	if strings.Contains(q, "$import_source_") {
		if importTx == nil {
			return 0, errors.New("import send tracking requires an import transaction")
		}
		for _, binding := range []struct {
			name string
			ids  []uuid.UUID
		}{
			{"$import_source_send_tasks", importTx.importedSendTasks},
			{"$import_source_hold_tasks", importTx.importedHoldTasks},
			{"$import_source_attempt_nonces", importTx.importedAttemptNonces},
		} {
			if strings.Contains(q, binding.name) {
				args = append(args, binding.ids)
				q = strings.ReplaceAll(q, binding.name, fmt.Sprintf("$%d", len(args)))
			}
		}
	}

	if len(invalidations) > 0 {
		for _, relation := range invalidations {
			for _, namespace := range relation.LockNamespaces {
				lockQuery := `SELECT pg_advisory_xact_lock(hashtextextended(keys.key::text,$2))
				FROM (SELECT DISTINCT ` + quoteIdent(relation.Key) + ` AS key
				FROM jsonb_populate_recordset(NULL::public.` + quoteIdent(table) + `,$1::jsonb)) keys
				ORDER BY keys.key::text`
				if _, err := tx.Exec(ctx, lockQuery, payload, namespace); err != nil {
					return 0, fmt.Errorf("lock import checkpoints for %s: %w", table, err)
				}
			}
		}
		if table == "email_accounts" {
			if importTx != nil && indexed {
				_, err := tx.Exec(ctx, `INSERT INTO pg_temp.org_import_send_origin (kind, id)
					SELECT DISTINCT $2::smallint, incoming.send_recovery_task_id
					FROM jsonb_populate_recordset(NULL::public.email_accounts,$1::jsonb) incoming
					WHERE incoming.send_recovery_hold AND incoming.send_recovery_task_id IS NOT NULL
					AND NOT EXISTS(SELECT 1 FROM tasks local_task JOIN email_accounts local_mailbox ON local_mailbox.id=local_task.email_account_id
						WHERE local_task.id=incoming.send_recovery_task_id AND local_mailbox.send_recovery_hold
						AND local_mailbox.send_recovery_task_id=local_task.id)
					ON CONFLICT DO NOTHING`, payload, importOriginHold)
				if err != nil {
					return 0, fmt.Errorf("classify imported holds: %w", err)
				}
			} else if importTx != nil {
				holdRows, err := tx.Query(ctx, `SELECT DISTINCT incoming.send_recovery_task_id
					FROM jsonb_populate_recordset(NULL::public.email_accounts,$1::jsonb) incoming
					WHERE incoming.send_recovery_hold AND incoming.send_recovery_task_id IS NOT NULL
					AND NOT EXISTS(SELECT 1 FROM tasks local_task JOIN email_accounts local_mailbox ON local_mailbox.id=local_task.email_account_id
						WHERE local_task.id=incoming.send_recovery_task_id AND local_mailbox.send_recovery_hold
						AND local_mailbox.send_recovery_task_id=local_task.id)`, payload)
				if err != nil {
					return 0, fmt.Errorf("classify imported holds: %w", err)
				}
				ids, err := pgx.CollectRows(holdRows, pgx.RowTo[uuid.UUID])
				if err != nil {
					return 0, err
				}
				importTx.importedHoldTasks = append(importTx.importedHoldTasks, ids...)
			}
		}
		if conflict == models.OrgImportConflictOverwrite && table == "email_accounts" {
			var incompatible bool
			err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM jsonb_populate_recordset(NULL::public.email_accounts,$1::jsonb) incoming
				JOIN email_accounts current ON current.id=incoming.id WHERE current.organization_id=$2
				AND current.send_recovery_hold AND incoming.send_recovery_hold
				AND (current.send_recovery_task_id IS DISTINCT FROM incoming.send_recovery_task_id OR current.send_recovery_reason IS DISTINCT FROM incoming.send_recovery_reason))`, payload, orgID).Scan(&incompatible)
			if err != nil {
				return 0, err
			}
			if incompatible {
				return 0, ErrImportSendRecoveryConflict
			}
		}
		keys := make([]string, 0, len(invalidations))
		seen := map[string]bool{}
		for _, relation := range invalidations {
			if !seen[relation.Key] {
				keys = append(keys, relation.Key)
				seen[relation.Key] = true
			}
		}
		q = `WITH imported AS (` + q + ` RETURNING ` + quoteIdents(keys) + `)`
		for i, relation := range invalidations {
			if len(relation.ResetColumns) > 0 {
				sets := make([]string, 0, len(relation.ResetColumns))
				for _, c := range relation.ResetColumns {
					sets = append(sets, quoteIdent(c)+" = DEFAULT")
				}
				q += fmt.Sprintf(", invalidated_%d AS (UPDATE public.%s SET %s WHERE %s IN (SELECT %s FROM imported))", i, quoteIdent(relation.Table), strings.Join(sets, ", "), quoteIdent(relation.Column), quoteIdent(relation.Key))
			} else {
				q += fmt.Sprintf(", invalidated_%d AS (DELETE FROM public.%s WHERE %s IN (SELECT %s FROM imported))", i, quoteIdent(relation.Table), quoteIdent(relation.Column), quoteIdent(relation.Key))
			}
		}
		q += ` SELECT COUNT(*) FROM imported`
		var written int64
		if err := tx.QueryRow(ctx, q, args...).Scan(&written); err != nil {
			return 0, fmt.Errorf("insert into %s and invalidate checkpoints: %w", table, err)
		}
		return written, nil
	}
	if table == "tasks" || table == "outbound_attempts" {
		if importTx == nil {
			return 0, errors.New("import send tracking requires an import transaction")
		}
		key := "id"
		kind := importOriginTask
		if table == "outbound_attempts" {
			key = "nonce"
			kind = importOriginAttempt
		}
		if indexed {
			written := `WITH written AS (` + q + ` RETURNING ` + quoteIdent(key) + `), recorded AS (
				INSERT INTO pg_temp.org_import_send_origin (kind, id)
				SELECT ` + strconv.Itoa(kind) + `, ` + quoteIdent(key) + ` FROM written
				ON CONFLICT DO NOTHING RETURNING 1)
				SELECT (SELECT COUNT(*) FROM written), (SELECT COUNT(*) FROM recorded)`
			var count, recorded int64
			if err := tx.QueryRow(ctx, written, args...).Scan(&count, &recorded); err != nil {
				return 0, fmt.Errorf("insert into %s and record provenance: %w", table, err)
			}
			return count, nil
		}
		writtenRows, err := tx.Query(ctx, q+" RETURNING "+quoteIdent(key), args...)
		if err != nil {
			return 0, fmt.Errorf("insert into %s: %w", table, err)
		}
		ids, err := pgx.CollectRows(writtenRows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return 0, err
		}
		// Only actual source writes lose preservation provenance; concurrent local completions remain visible.
		if table == "tasks" {
			importTx.importedSendTasks = append(importTx.importedSendTasks, ids...)
		} else {
			importTx.importedAttemptNonces = append(importTx.importedAttemptNonces, ids...)
		}
		return int64(len(ids)), nil
	}
	tag, err := tx.Exec(ctx, q, args...)
	if err != nil {
		return 0, fmt.Errorf("insert into %s: %w", table, err)
	}
	return tag.RowsAffected(), nil
}

func (r *orgTransferRepository) DeveloperBlocked(ctx context.Context, tx pgx.Tx, orgID, userID uuid.UUID) (bool, error) {
	var blocked bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM oauth_developer_blocks
		   WHERE organization_id = $1 OR (user_id = $2 AND $2 <> '00000000-0000-0000-0000-000000000000'::uuid)
		)`, orgID, userID).Scan(&blocked)
	return blocked, err
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
		         LEFT JOIN organization_oauth_apps a ON a.id = o.oauth_app_id
		          WHERE o.email_account_id = ea.id
		            AND COALESCE(o.refresh_token, '') <> ''
		            AND (o.oauth_app_id IS NULL OR COALESCE(a.client_secret, '') <> '')
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

// IntegrationReconnect names a connection an import left needing reconnection.
type IntegrationReconnect struct {
	Provider string
	Label    string
	// CredentialsMissing reports an API key, OAuth token or config the archive blanked.
	CredentialsMissing bool
	// InboundClosed reports that the connection had an inbound URL and it was closed.
	InboundClosed bool
}

// MarkIntegrationsNeedingReconnect moves every connection holding a value the archive blanked ("" where the app writes NULL) to reauth_required.
// A blank config or inbound secret closes the inbound URL in its clear, sealed and digest columns alike.
func (r *orgTransferRepository) MarkIntegrationsNeedingReconnect(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, credentialsDetail, inboundDetail string) ([]IntegrationReconnect, error) {
	rows, err := tx.Query(ctx, `
		WITH stale AS (
			SELECT id,
			       COALESCE(config_encrypted = '\x'::bytea OR access_token_encrypted = '' OR refresh_token_encrypted = '', false) AS credentials_missing,
			       COALESCE(config_encrypted = '\x'::bytea OR inbound_secret_encrypted = '', false) AS close_inbound,
			       (inbound_secret IS NOT NULL OR inbound_secret_encrypted IS NOT NULL OR inbound_secret_digest IS NOT NULL) AS had_inbound
			  FROM integration_connections
			 WHERE organization_id = $1
			   AND (config_encrypted = '\x'::bytea OR access_token_encrypted = '' OR refresh_token_encrypted = '' OR inbound_secret_encrypted = '')
			   FOR UPDATE
		)
		UPDATE integration_connections ic
		   SET status = CASE WHEN ic.status = 'disconnected' THEN ic.status ELSE 'reauth_required' END,
		       health = 'down',
		       health_detail = CASE WHEN s.credentials_missing THEN $2 ELSE $3 END,
		       health_checked_at = NOW(),
		       inbound_secret = CASE WHEN s.close_inbound THEN NULL ELSE ic.inbound_secret END,
		       inbound_secret_encrypted = CASE WHEN s.close_inbound THEN NULL ELSE ic.inbound_secret_encrypted END,
		       inbound_secret_digest = CASE WHEN s.close_inbound THEN NULL ELSE ic.inbound_secret_digest END,
		       config_encrypted = NULLIF(ic.config_encrypted, '\x'::bytea),
		       access_token_encrypted = NULLIF(ic.access_token_encrypted, ''),
		       refresh_token_encrypted = NULLIF(ic.refresh_token_encrypted, ''),
		       updated_at = NOW()
		  FROM stale s
		 WHERE ic.id = s.id AND ic.organization_id = $1
		RETURNING ic.provider, ic.label, s.credentials_missing, s.close_inbound AND s.had_inbound
	`, orgID, credentialsDetail, inboundDetail)
	if err != nil {
		return nil, fmt.Errorf("flag integrations needing reconnect: %w", err)
	}
	defer rows.Close()
	var out []IntegrationReconnect
	for rows.Next() {
		var c IntegrationReconnect
		if err := rows.Scan(&c.Provider, &c.Label, &c.CredentialsMissing, &c.InboundClosed); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
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

func (r *orgTransferRepository) MissingOAuthApps(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, ids []uuid.UUID) (int64, error) {
	var n int64
	err := tx.QueryRow(ctx, `SELECT count(*) FROM unnest($2::uuid[]) AS u(id)
		WHERE NOT EXISTS (SELECT 1 FROM organization_oauth_apps a WHERE a.id = u.id AND a.organization_id = $1)`, orgID, ids).Scan(&n)
	return n, err
}

func (r *orgTransferRepository) ChangedOAuthAppClients(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, ids []uuid.UUID, clientIDs []string) (int64, error) {
	var n int64
	err := tx.QueryRow(ctx, `SELECT count(*) FROM unnest($2::uuid[], $3::text[]) AS u(id, client_id)
		JOIN organization_oauth_apps a ON a.id = u.id
		WHERE a.organization_id <> $1 OR a.client_id <> u.client_id`, orgID, ids, clientIDs).Scan(&n)
	return n, err
}

func (r *orgTransferRepository) OAuthAppSecrets(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if len(ids) == 0 {
		return out, nil
	}
	// Every matching row is locked, empty secret or not, so a save racing the import waits for it.
	rows, err := tx.Query(ctx, `SELECT id, client_secret FROM organization_oauth_apps
		WHERE organization_id = $1 AND id = ANY($2::uuid[]) FOR UPDATE`, orgID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var secret string
		if err := rows.Scan(&id, &secret); err != nil {
			return nil, err
		}
		if secret != "" {
			out[id] = secret
		}
	}
	return out, rows.Err()
}

func (r *orgTransferRepository) RestoreOAuthAppSecret(ctx context.Context, tx pgx.Tx, orgID, id uuid.UUID, secret string) error {
	_, err := tx.Exec(ctx, `UPDATE organization_oauth_apps SET client_secret = $3
		WHERE organization_id = $1 AND id = $2 AND client_secret = ''`, orgID, id, secret)
	return err
}
