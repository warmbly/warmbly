package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

// ToolMigrationScope is whose migrations a call reaches: exactly one is set.
type ToolMigrationScope struct {
	OrgID    *uuid.UUID
	AgencyID *uuid.UUID
}

// predicate is the scope's WHERE fragment over tool_migrations aliased m, with $1 as the owner.
func (s ToolMigrationScope) predicate() (string, uuid.UUID, bool) {
	switch {
	case s.OrgID != nil && s.AgencyID == nil:
		return "m.organization_id = $1", *s.OrgID, true
	case s.AgencyID != nil && s.OrgID == nil:
		return "m.agency_id = $1", *s.AgencyID, true
	}
	return "", uuid.Nil, false
}

// ErrToolMigrationScope is a call with neither or both owners set.
var ErrToolMigrationScope = errors.New("tool migration scope must name exactly one owner")

// ToolMigrationScanJob is a claimed migration whose source still has to be read.
type ToolMigrationScanJob struct {
	ID       uuid.UUID
	Token    uuid.UUID
	Provider string
	Sealed   string
	Attempts int
}

// ToolMigrationTargetInsert is one target written when a migration starts.
type ToolMigrationTargetInsert struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	WorkspaceID    string
	KeyIndex       int
	WorkspaceName  string
}

// ToolMigrationTargetJob is a claimed target with what the runner needs of its migration.
type ToolMigrationTargetJob struct {
	ID              uuid.UUID
	MigrationID     uuid.UUID
	OrgID           uuid.UUID
	WorkspaceID     string
	KeyIndex        int
	WorkspaceName   string
	Phase           string
	Cursor          []byte
	Counts          []byte
	Followup        []byte
	MailboxImportID *uuid.UUID
	CategoryID      *uuid.UUID
	Attempts        int
	// Token names this holder; every write it makes is refused once another holds the target.
	Token uuid.UUID

	Provider   string
	Label      string
	Sealed     *string
	Options    []byte
	ViaSession bool
	CreatedBy  *uuid.UUID
	AgencyID   *uuid.UUID
}

// ToolMigrationProgress is a target's checkpoint.
type ToolMigrationProgress struct {
	Phase           string
	Cursor          []byte
	Counts          []byte
	Followup        []byte
	MailboxImportID *uuid.UUID
	CategoryID      *uuid.UUID
}

// ToolMigrationItemState is what reserving an item found.
type ToolMigrationItemState int

const (
	// ItemReserved is a new reservation this holder now owns.
	ItemReserved ToolMigrationItemState = iota
	// ItemDone is an item already recorded; its id is returned.
	ItemDone
	// ItemPending is a reservation an earlier holder made and never completed: its write may have happened.
	ItemPending
	// ItemLost means the target is no longer this holder's.
	ItemLost
)

// ToolMigrationRepository stores migrations from other outreach tools. Every
// read a caller reaches is scoped to its organization or agency; the runner's
// calls are by id under a lease.
type ToolMigrationRepository interface {
	Create(ctx context.Context, m *models.ToolMigration, sealed string, viaSession bool) error
	// Get is nil when the migration is outside the scope.
	Get(ctx context.Context, scope ToolMigrationScope, id uuid.UUID) (*models.ToolMigration, error)
	List(ctx context.Context, scope ToolMigrationScope, before *time.Time, beforeID *uuid.UUID, limit int) ([]models.ToolMigration, error)
	// CountActive counts the scope's migrations still scanning, waiting or running.
	CountActive(ctx context.Context, scope ToolMigrationScope) (int, error)
	ClaimScan(ctx context.Context, lease time.Duration) (*ToolMigrationScanJob, error)
	// RenewScan extends a held scan's lease; false when it is no longer this holder's.
	RenewScan(ctx context.Context, id, token uuid.UUID, lease time.Duration) (bool, error)
	// FinishScan ends a scan as ready or failed; false when the claim was lost.
	FinishScan(ctx context.Context, id, token uuid.UUID, inventory []byte, status models.ToolMigrationStatus, errMsg string) (bool, error)
	// ClaimStart holds a ready migration while it is started, returning the client workspaces an
	// earlier attempt created; false when it is not ready or another start holds it.
	ClaimStart(ctx context.Context, scope ToolMigrationScope, id uuid.UUID, hold time.Duration) (bool, map[string]uuid.UUID, error)
	// RecordClient remembers a client workspace a start created for one source workspace.
	RecordClient(ctx context.Context, id uuid.UUID, key string, orgID uuid.UUID) error
	// ReleaseStart lets a start that failed part way be tried again.
	ReleaseStart(ctx context.Context, id uuid.UUID) error
	// Start moves a claimed ready migration to running with its targets; false when it was not ready.
	Start(ctx context.Context, scope ToolMigrationScope, id uuid.UUID, options []byte, targets []ToolMigrationTargetInsert) (bool, error)
	// Cancel stops a migration and its unfinished targets and wipes its keys; false when it had ended.
	Cancel(ctx context.Context, scope ToolMigrationScope, id uuid.UUID) (bool, error)
	// Delete removes a migration, and with it its keys, unless it is running; false when nothing was removed.
	Delete(ctx context.Context, scope ToolMigrationScope, id uuid.UUID) (bool, error)
	ClaimTarget(ctx context.Context, lease time.Duration) (*ToolMigrationTargetJob, error)
	// RenewTarget extends a held target's lease; false when it was canceled, removed or claimed again.
	RenewTarget(ctx context.Context, id, token uuid.UUID, lease time.Duration) (bool, error)
	// SaveTarget checkpoints a held target and renews its lease; false when it is no longer this holder's.
	SaveTarget(ctx context.Context, id, token uuid.UUID, lease time.Duration, p ToolMigrationProgress) (bool, error)
	// FinishTarget ends a held target; false when the claim was lost.
	FinishTarget(ctx context.Context, id, token uuid.UUID, status models.ToolMigrationTargetStatus, p ToolMigrationProgress, errMsg string) (bool, error)
	// Finalize ends a running migration whose targets have all ended and wipes its keys.
	// It returns the status it ended at, or "" when it is still running or had ended.
	Finalize(ctx context.Context, migrationID uuid.UUID) (models.ToolMigrationStatus, error)
	// Item is a completed item; a pending reservation reads as absent.
	Item(ctx context.Context, targetID uuid.UUID, kind, sourceID string) (uuid.UUID, bool, error)
	// ReserveItem claims an item before the write it records, while the holder still holds the target.
	ReserveItem(ctx context.Context, targetID, token, orgID uuid.UUID, kind, sourceID string) (ToolMigrationItemState, uuid.UUID, error)
	// CompleteItem records what a reserved item became; false when the target is no longer this holder's.
	CompleteItem(ctx context.Context, targetID, token uuid.UUID, kind, sourceID string, warmblyID uuid.UUID) (bool, error)
	// PutItems records completed items; false when the target is no longer this holder's.
	PutItems(ctx context.Context, targetID, token, orgID uuid.UUID, kind string, items map[string]uuid.UUID) (bool, error)
	// ItemsIn reports which of sourceIDs the target has already recorded under kind.
	ItemsIn(ctx context.Context, targetID uuid.UUID, kind string, sourceIDs []string) (map[string]bool, error)
	CountItems(ctx context.Context, targetID uuid.UUID, kind string) (int, error)
	// SetFollowup replaces a target's follow-up list, within its migration.
	SetFollowup(ctx context.Context, migrationID, targetID uuid.UUID, followup []byte) error
	// MailboxIDsByEmail maps lowercased addresses to the organization's mailboxes.
	MailboxIDsByEmail(ctx context.Context, orgID uuid.UUID, emails []string) (map[string]uuid.UUID, error)
	// Upkeep wipes keys nothing will use again and ends work that can no longer finish.
	// It returns the migrations it ended, so their workspaces can be told.
	Upkeep(ctx context.Context, idle time.Duration, maxAttempts int) ([]uuid.UUID, error)
}

type toolMigrationRepository struct {
	DB *db.DB
}

func NewToolMigrationRepository(database *db.DB) ToolMigrationRepository {
	return &toolMigrationRepository{DB: database}
}

func (r *toolMigrationRepository) Create(ctx context.Context, m *models.ToolMigration, sealed string, viaSession bool) error {
	accounts, err := json.Marshal(nonNilSlice(m.Accounts))
	if err != nil {
		return err
	}
	query := `
		INSERT INTO tool_migrations (id, organization_id, agency_id, created_by_user_id, provider, label, status,
			credentials_sealed, accounts, via_session)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING created_at, updated_at`
	if err := r.DB.QueryRow(ctx, query, m.ID, m.OrganizationID, m.AgencyID, m.CreatedByUserID, string(m.Provider), m.Label,
		string(m.Status), sealed, accounts, viaSession).Scan(&m.CreatedAt, &m.UpdatedAt); err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return err
	}
	m.CredentialsHeld = sealed != ""
	return nil
}

const toolMigrationSelect = `
	SELECT m.id, m.organization_id, m.agency_id, m.created_by_user_id, m.provider, m.label, m.status,
	       m.credentials_sealed IS NOT NULL, m.accounts, m.inventory, m.options, m.error,
	       m.started_at, m.finished_at, m.created_at, m.updated_at
	FROM tool_migrations m`

func scanToolMigration(row pgx.Row) (*models.ToolMigration, error) {
	var m models.ToolMigration
	var provider, status string
	var accounts, inventory, options []byte
	if err := row.Scan(&m.ID, &m.OrganizationID, &m.AgencyID, &m.CreatedByUserID, &provider, &m.Label, &status,
		&m.CredentialsHeld, &accounts, &inventory, &options, &m.Error,
		&m.StartedAt, &m.FinishedAt, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, err
	}
	m.Provider, m.Status = source.ProviderID(provider), models.ToolMigrationStatus(status)
	m.Accounts, m.Inventory, m.Targets = []source.Account{}, []models.ToolMigrationWorkspace{}, []models.ToolMigrationTarget{}
	if len(accounts) > 0 {
		_ = json.Unmarshal(accounts, &m.Accounts)
	}
	if len(inventory) > 0 {
		_ = json.Unmarshal(inventory, &m.Inventory)
	}
	if len(options) > 0 {
		var o models.ToolMigrationOptions
		if json.Unmarshal(options, &o) == nil {
			m.Options = &o
		}
	}
	return &m, nil
}

func (r *toolMigrationRepository) Get(ctx context.Context, scope ToolMigrationScope, id uuid.UUID) (*models.ToolMigration, error) {
	pred, owner, ok := scope.predicate()
	if !ok {
		return nil, ErrToolMigrationScope
	}
	query := toolMigrationSelect + ` WHERE ` + pred + ` AND m.id = $2`
	m, err := scanToolMigration(r.DB.QueryRow(ctx, query, owner, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return nil, err
	}
	list := []models.ToolMigration{*m}
	if err := r.attachTargets(ctx, list); err != nil {
		return nil, err
	}
	return &list[0], nil
}

func (r *toolMigrationRepository) List(ctx context.Context, scope ToolMigrationScope, before *time.Time, beforeID *uuid.UUID, limit int) ([]models.ToolMigration, error) {
	pred, owner, ok := scope.predicate()
	if !ok {
		return nil, ErrToolMigrationScope
	}
	query := toolMigrationSelect + ` WHERE ` + pred + `
		AND ($2::timestamptz IS NULL OR (m.created_at, m.id) < ($2, $3::uuid))
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT $4`
	rows, err := r.DB.Query(ctx, query, owner, before, beforeID, limit)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	out := []models.ToolMigration{}
	for rows.Next() {
		m, err := scanToolMigration(rows)
		if err != nil {
			db.CaptureError(err, query, nil, "scan")
			return nil, err
		}
		out = append(out, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.attachTargets(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// attachTargets reads every target of the migrations in one query.
func (r *toolMigrationRepository) attachTargets(ctx context.Context, list []models.ToolMigration) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(list))
	at := make(map[uuid.UUID]int, len(list))
	for i := range list {
		ids[i], at[list[i].ID] = list[i].ID, i
	}
	query := `
		SELECT t.id, t.migration_id, t.organization_id, COALESCE(o.name, ''), t.workspace_id, t.key_index, t.workspace_name,
		       t.status, t.phase, t.counts, t.followup, t.error, t.mailbox_import_id, t.category_id,
		       t.started_at, t.finished_at, t.updated_at
		FROM tool_migration_targets t
		LEFT JOIN organizations o ON o.id = t.organization_id
		WHERE t.migration_id = ANY($1::uuid[])
		ORDER BY t.created_at, t.id`
	rows, err := r.DB.Query(ctx, query, ids)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var t models.ToolMigrationTarget
		var status, phase string
		var counts, followup []byte
		if err := rows.Scan(&t.ID, &t.MigrationID, &t.OrganizationID, &t.OrgName, &t.Workspace.ID, &t.Workspace.Key, &t.Workspace.Name,
			&status, &phase, &counts, &followup, &t.Error, &t.MailboxImportID, &t.CategoryID,
			&t.StartedAt, &t.FinishedAt, &t.UpdatedAt); err != nil {
			db.CaptureError(err, query, nil, "scan")
			return err
		}
		t.Status, t.Phase = models.ToolMigrationTargetStatus(status), source.Kind(phase)
		_ = json.Unmarshal(counts, &t.Counts)
		_ = json.Unmarshal(followup, &t.Followup)
		i := at[t.MigrationID]
		list[i].Targets = append(list[i].Targets, t)
	}
	return rows.Err()
}

func (r *toolMigrationRepository) CountActive(ctx context.Context, scope ToolMigrationScope) (int, error) {
	pred, owner, ok := scope.predicate()
	if !ok {
		return 0, ErrToolMigrationScope
	}
	query := `SELECT count(*) FROM tool_migrations m WHERE ` + pred + ` AND m.status IN ('scanning', 'ready', 'running')`
	var n int
	if err := r.DB.QueryRow(ctx, query, owner).Scan(&n); err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return 0, err
	}
	return n, nil
}

func (r *toolMigrationRepository) ClaimScan(ctx context.Context, lease time.Duration) (*ToolMigrationScanJob, error) {
	query := `
		UPDATE tool_migrations m
		SET attempts = m.attempts + 1, lease_token = $2, lease_until = now() + $1::interval, updated_at = now()
		WHERE m.id = (
			SELECT id FROM tool_migrations
			WHERE status = 'scanning' AND credentials_sealed IS NOT NULL
			  AND (lease_until IS NULL OR lease_until < now())
			ORDER BY created_at
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING m.id, m.lease_token, m.provider, m.credentials_sealed, m.attempts`
	var job ToolMigrationScanJob
	err := r.DB.QueryRow(ctx, query, lease.String(), uuid.New()).Scan(&job.ID, &job.Token, &job.Provider, &job.Sealed, &job.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return nil, err
	}
	return &job, nil
}

func (r *toolMigrationRepository) RenewScan(ctx context.Context, id, token uuid.UUID, lease time.Duration) (bool, error) {
	return r.exec(ctx, `UPDATE tool_migrations SET lease_until = now() + $3::interval
		WHERE id = $1 AND lease_token = $2 AND status = 'scanning'`, id, token, lease.String())
}

func (r *toolMigrationRepository) FinishScan(ctx context.Context, id, token uuid.UUID, inventory []byte, status models.ToolMigrationStatus, errMsg string) (bool, error) {
	return r.exec(ctx, `
		UPDATE tool_migrations
		SET status = $3, inventory = COALESCE($4::jsonb, inventory), error = $5, lease_until = NULL, lease_token = NULL,
		    credentials_sealed = CASE WHEN $3 = 'ready' THEN credentials_sealed END,
		    finished_at = CASE WHEN $3 = 'ready' THEN NULL ELSE now() END, updated_at = now()
		WHERE id = $1 AND lease_token = $2 AND status = 'scanning'`, id, token, string(status), inventory, errMsg)
}

// exec runs one guarded write and reports whether it applied.
func (r *toolMigrationRepository) exec(ctx context.Context, query string, args ...any) (bool, error) {
	tag, err := r.DB.Exec(ctx, query, args...)
	if err != nil {
		db.CaptureError(err, query, nil, "exec")
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *toolMigrationRepository) ClaimStart(ctx context.Context, scope ToolMigrationScope, id uuid.UUID, hold time.Duration) (bool, map[string]uuid.UUID, error) {
	pred, owner, ok := scope.predicate()
	if !ok {
		return false, nil, ErrToolMigrationScope
	}
	query := `
		UPDATE tool_migrations m
		SET starting_until = now() + $3::interval, updated_at = now()
		WHERE ` + pred + ` AND m.id = $2 AND m.status = 'ready' AND m.credentials_sealed IS NOT NULL
		  AND (m.starting_until IS NULL OR m.starting_until < now())
		RETURNING m.created_clients`
	var raw []byte
	err := r.DB.QueryRow(ctx, query, owner, id, hold.String()).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil, nil
	}
	if err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return false, nil, err
	}
	created := map[string]uuid.UUID{}
	_ = json.Unmarshal(raw, &created)
	return true, created, nil
}

func (r *toolMigrationRepository) RecordClient(ctx context.Context, id uuid.UUID, key string, orgID uuid.UUID) error {
	_, err := r.exec(ctx, `UPDATE tool_migrations SET created_clients = created_clients || jsonb_build_object($2::text, $3::text), updated_at = now()
		WHERE id = $1`, id, key, orgID.String())
	return err
}

func (r *toolMigrationRepository) ReleaseStart(ctx context.Context, id uuid.UUID) error {
	_, err := r.exec(ctx, `UPDATE tool_migrations SET starting_until = NULL, updated_at = now() WHERE id = $1 AND status = 'ready'`, id)
	return err
}

func (r *toolMigrationRepository) Start(ctx context.Context, scope ToolMigrationScope, id uuid.UUID, options []byte, targets []ToolMigrationTargetInsert) (bool, error) {
	pred, owner, ok := scope.predicate()
	if !ok {
		return false, ErrToolMigrationScope
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return false, err
	}
	defer tx.Rollback(ctx)
	query := `
		UPDATE tool_migrations m
		SET status = 'running', options = $3, started_at = now(), attempts = 0, lease_until = NULL, lease_token = NULL,
		    starting_until = NULL, updated_at = now()
		WHERE ` + pred + ` AND m.id = $2 AND m.status = 'ready' AND m.credentials_sealed IS NOT NULL AND m.starting_until IS NOT NULL`
	tag, err := tx.Exec(ctx, query, owner, id, options)
	if err != nil {
		db.CaptureError(err, query, nil, "exec")
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	insert := `
		INSERT INTO tool_migration_targets (id, migration_id, organization_id, workspace_id, key_index, workspace_name, status)
		VALUES ($1, $2, $3, $4, $5, $6, 'pending')`
	for _, t := range targets {
		if _, err := tx.Exec(ctx, insert, t.ID, id, t.OrganizationID, t.WorkspaceID, t.KeyIndex, t.WorkspaceName); err != nil {
			db.CaptureError(err, insert, nil, "exec")
			return false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return false, err
	}
	return true, nil
}

func (r *toolMigrationRepository) Cancel(ctx context.Context, scope ToolMigrationScope, id uuid.UUID) (bool, error) {
	pred, owner, ok := scope.predicate()
	if !ok {
		return false, ErrToolMigrationScope
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return false, err
	}
	defer tx.Rollback(ctx)
	query := `
		UPDATE tool_migrations m
		SET status = 'canceled', credentials_sealed = NULL, lease_until = NULL, finished_at = now(), updated_at = now()
		WHERE ` + pred + ` AND m.id = $2 AND m.status IN ('scanning', 'ready', 'running')`
	tag, err := tx.Exec(ctx, query, owner, id)
	if err != nil {
		db.CaptureError(err, query, nil, "exec")
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	targets := `
		UPDATE tool_migration_targets
		SET status = 'canceled', lease_until = NULL, finished_at = now(), updated_at = now()
		WHERE migration_id = $1 AND status IN ('pending', 'running')`
	if _, err := tx.Exec(ctx, targets, id); err != nil {
		db.CaptureError(err, targets, nil, "exec")
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return false, err
	}
	return true, nil
}

func (r *toolMigrationRepository) Delete(ctx context.Context, scope ToolMigrationScope, id uuid.UUID) (bool, error) {
	pred, owner, ok := scope.predicate()
	if !ok {
		return false, ErrToolMigrationScope
	}
	query := `DELETE FROM tool_migrations m WHERE ` + pred + ` AND m.id = $2 AND m.status <> 'running'`
	tag, err := r.DB.Exec(ctx, query, owner, id)
	if err != nil {
		db.CaptureError(err, query, nil, "exec")
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *toolMigrationRepository) ClaimTarget(ctx context.Context, lease time.Duration) (*ToolMigrationTargetJob, error) {
	// SKIP LOCKED lets every backend run the loop; the lease hands a target back if its process dies.
	query := `
		WITH claimed AS (
			UPDATE tool_migration_targets t
			SET status = 'running', attempts = t.attempts + 1, lease_token = $2, lease_until = now() + $1::interval,
			    started_at = COALESCE(t.started_at, now()), updated_at = now()
			WHERE t.id = (
				SELECT tt.id FROM tool_migration_targets tt
				JOIN tool_migrations mm ON mm.id = tt.migration_id
				WHERE mm.status = 'running'
				  AND (tt.status = 'pending' OR (tt.status = 'running' AND tt.lease_until < now()))
				ORDER BY tt.created_at
				LIMIT 1
				FOR UPDATE OF tt SKIP LOCKED
			)
			RETURNING t.*
		)
		SELECT c.id, c.migration_id, c.organization_id, c.workspace_id, c.key_index, c.workspace_name, c.phase,
		       c.cursor, c.counts, c.followup, c.mailbox_import_id, c.category_id, c.attempts, c.lease_token,
		       m.provider, m.label, m.credentials_sealed, m.options, m.via_session, m.created_by_user_id, m.agency_id
		FROM claimed c
		JOIN tool_migrations m ON m.id = c.migration_id`
	var j ToolMigrationTargetJob
	err := r.DB.QueryRow(ctx, query, lease.String(), uuid.New()).Scan(&j.ID, &j.MigrationID, &j.OrgID, &j.WorkspaceID, &j.KeyIndex,
		&j.WorkspaceName, &j.Phase, &j.Cursor, &j.Counts, &j.Followup, &j.MailboxImportID, &j.CategoryID, &j.Attempts, &j.Token,
		&j.Provider, &j.Label, &j.Sealed, &j.Options, &j.ViaSession, &j.CreatedBy, &j.AgencyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return nil, err
	}
	return &j, nil
}

func (r *toolMigrationRepository) RenewTarget(ctx context.Context, id, token uuid.UUID, lease time.Duration) (bool, error) {
	return r.exec(ctx, `UPDATE tool_migration_targets SET lease_until = now() + $3::interval
		WHERE id = $1 AND lease_token = $2 AND status = 'running'`, id, token, lease.String())
}

func (r *toolMigrationRepository) SaveTarget(ctx context.Context, id, token uuid.UUID, lease time.Duration, p ToolMigrationProgress) (bool, error) {
	return r.exec(ctx, `
		UPDATE tool_migration_targets
		SET phase = $4, cursor = $5, counts = COALESCE($6::jsonb, counts), followup = COALESCE($7::jsonb, followup),
		    mailbox_import_id = COALESCE($8, mailbox_import_id), category_id = COALESCE($9, category_id),
		    lease_until = now() + $3::interval, updated_at = now()
		WHERE id = $1 AND lease_token = $2 AND status = 'running'`,
		id, token, lease.String(), p.Phase, p.Cursor, p.Counts, p.Followup, p.MailboxImportID, p.CategoryID)
}

func (r *toolMigrationRepository) FinishTarget(ctx context.Context, id, token uuid.UUID, status models.ToolMigrationTargetStatus, p ToolMigrationProgress, errMsg string) (bool, error) {
	return r.exec(ctx, `
		UPDATE tool_migration_targets
		SET status = $3, phase = $4, cursor = NULL, counts = COALESCE($5::jsonb, counts), followup = COALESCE($6::jsonb, followup),
		    mailbox_import_id = COALESCE($7, mailbox_import_id), category_id = COALESCE($8, category_id),
		    error = $9, lease_until = NULL, lease_token = NULL, finished_at = now(), updated_at = now()
		WHERE id = $1 AND lease_token = $2 AND status = 'running'`,
		id, token, string(status), p.Phase, p.Counts, p.Followup, p.MailboxImportID, p.CategoryID, errMsg)
}

func (r *toolMigrationRepository) Finalize(ctx context.Context, migrationID uuid.UUID) (models.ToolMigrationStatus, error) {
	query := `
		UPDATE tool_migrations m
		SET status = CASE WHEN EXISTS (SELECT 1 FROM tool_migration_targets t WHERE t.migration_id = m.id AND t.status = 'completed')
		                  THEN 'completed' ELSE 'failed' END,
		    error = CASE WHEN EXISTS (SELECT 1 FROM tool_migration_targets t WHERE t.migration_id = m.id AND t.status = 'completed')
		                 THEN '' ELSE 'Nothing could be brought over. Each workspace shows why.' END,
		    credentials_sealed = NULL, lease_until = NULL, finished_at = now(), updated_at = now()
		WHERE m.id = $1 AND m.status = 'running'
		  AND NOT EXISTS (SELECT 1 FROM tool_migration_targets t WHERE t.migration_id = m.id AND t.status IN ('pending', 'running'))
		RETURNING m.status`
	var status string
	err := r.DB.QueryRow(ctx, query, migrationID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return "", err
	}
	return models.ToolMigrationStatus(status), nil
}

func (r *toolMigrationRepository) Item(ctx context.Context, targetID uuid.UUID, kind, sourceID string) (uuid.UUID, bool, error) {
	query := `SELECT warmbly_id FROM tool_migration_items WHERE target_id = $1 AND kind = $2 AND source_id = $3 AND NOT pending`
	var id uuid.UUID
	err := r.DB.QueryRow(ctx, query, targetID, kind, sourceID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return uuid.Nil, false, err
	}
	return id, true, nil
}

// heldTarget is the guard every item write carries: the target is running under this holder's token.
const heldTarget = `EXISTS (SELECT 1 FROM tool_migration_targets t WHERE t.id = $1 AND t.lease_token = $2 AND t.status = 'running')`

// gone reports a write that raced the target's removal, which reads as a lost lease.
func gone(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

func (r *toolMigrationRepository) ReserveItem(ctx context.Context, targetID, token, orgID uuid.UUID, kind, sourceID string) (ToolMigrationItemState, uuid.UUID, error) {
	query := `
		WITH ins AS (
			INSERT INTO tool_migration_items (target_id, organization_id, kind, source_id, warmbly_id, pending)
			SELECT $1, $3, $4, $5, gen_random_uuid(), true
			WHERE ` + heldTarget + `
			ON CONFLICT (target_id, kind, source_id) DO NOTHING
			RETURNING 1
		)
		SELECT EXISTS (SELECT 1 FROM ins), ` + heldTarget + `,
		       (SELECT warmbly_id FROM tool_migration_items WHERE target_id = $1 AND kind = $4 AND source_id = $5),
		       (SELECT pending FROM tool_migration_items WHERE target_id = $1 AND kind = $4 AND source_id = $5)`
	var inserted, held bool
	var existing *uuid.UUID
	var pending *bool
	err := r.DB.QueryRow(ctx, query, targetID, token, orgID, kind, sourceID).Scan(&inserted, &held, &existing, &pending)
	if gone(err) {
		return ItemLost, uuid.Nil, nil
	}
	if err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return ItemLost, uuid.Nil, err
	}
	switch {
	case !held:
		return ItemLost, uuid.Nil, nil
	case inserted:
		return ItemReserved, uuid.Nil, nil
	case existing != nil && pending != nil && !*pending:
		return ItemDone, *existing, nil
	}
	return ItemPending, uuid.Nil, nil
}

func (r *toolMigrationRepository) CompleteItem(ctx context.Context, targetID, token uuid.UUID, kind, sourceID string, warmblyID uuid.UUID) (bool, error) {
	ok, err := r.exec(ctx, `
		UPDATE tool_migration_items SET warmbly_id = $5, pending = false
		WHERE target_id = $1 AND kind = $3 AND source_id = $4 AND `+heldTarget, targetID, token, kind, sourceID, warmblyID)
	if gone(err) {
		return false, nil
	}
	return ok, err
}

func (r *toolMigrationRepository) PutItems(ctx context.Context, targetID, token, orgID uuid.UUID, kind string, items map[string]uuid.UUID) (bool, error) {
	sources := make([]string, 0, len(items))
	ids := make([]uuid.UUID, 0, len(items))
	for s, id := range items {
		sources, ids = append(sources, s), append(ids, id)
	}
	query := `
		WITH ins AS (
			INSERT INTO tool_migration_items (target_id, organization_id, kind, source_id, warmbly_id)
			SELECT $1, $3, $4, v.source_id, v.warmbly_id
			FROM unnest($5::text[], $6::uuid[]) AS v(source_id, warmbly_id)
			WHERE ` + heldTarget + `
			ON CONFLICT (target_id, kind, source_id) DO NOTHING
			RETURNING 1
		)
		SELECT ` + heldTarget
	var held bool
	err := r.DB.QueryRow(ctx, query, targetID, token, orgID, kind, sources, ids).Scan(&held)
	if gone(err) {
		return false, nil
	}
	if err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return false, err
	}
	return held, nil
}

func (r *toolMigrationRepository) ItemsIn(ctx context.Context, targetID uuid.UUID, kind string, sourceIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(sourceIDs) == 0 {
		return out, nil
	}
	query := `SELECT source_id FROM tool_migration_items WHERE target_id = $1 AND kind = $2 AND source_id = ANY($3::text[]) AND NOT pending`
	rows, err := r.DB.Query(ctx, query, targetID, kind, sourceIDs)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	have, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	for _, s := range have {
		out[s] = true
	}
	return out, nil
}

func (r *toolMigrationRepository) CountItems(ctx context.Context, targetID uuid.UUID, kind string) (int, error) {
	query := `SELECT count(*) FROM tool_migration_items WHERE target_id = $1 AND kind = $2`
	var n int
	if err := r.DB.QueryRow(ctx, query, targetID, kind).Scan(&n); err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return 0, err
	}
	return n, nil
}

func (r *toolMigrationRepository) SetFollowup(ctx context.Context, migrationID, targetID uuid.UUID, followup []byte) error {
	query := `UPDATE tool_migration_targets SET followup = $3, updated_at = now() WHERE id = $2 AND migration_id = $1`
	if _, err := r.DB.Exec(ctx, query, migrationID, targetID, followup); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}

func (r *toolMigrationRepository) MailboxIDsByEmail(ctx context.Context, orgID uuid.UUID, emails []string) (map[string]uuid.UUID, error) {
	out := map[string]uuid.UUID{}
	if len(emails) == 0 {
		return out, nil
	}
	query := `
		SELECT DISTINCT ON (lower(email)) lower(email), id
		FROM email_accounts
		WHERE organization_id = $1 AND lower(email) = ANY($2::text[])
		ORDER BY lower(email), created_at`
	rows, err := r.DB.Query(ctx, query, orgID, emails)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var addr string
		var id uuid.UUID
		if err := rows.Scan(&addr, &id); err != nil {
			db.CaptureError(err, query, nil, "scan")
			return nil, err
		}
		out[addr] = id
	}
	return out, rows.Err()
}

func (r *toolMigrationRepository) Upkeep(ctx context.Context, idle time.Duration, maxAttempts int) ([]uuid.UUID, error) {
	ended := []uuid.UUID{}
	steps := []struct {
		query string
		args  []any
	}{
		// A migration nobody started within the idle window gives its keys up.
		{`UPDATE tool_migrations
		 SET status = 'canceled', credentials_sealed = NULL, error = 'This migration expired before it was started.',
		     finished_at = now(), updated_at = now()
		 WHERE status = 'ready' AND updated_at < now() - $1::interval
		   AND (starting_until IS NULL OR starting_until < now())
		 RETURNING id`, []any{idle.String()}},
		{`UPDATE tool_migrations
		 SET status = 'failed', credentials_sealed = NULL, lease_until = NULL,
		     error = 'Reading the other tool stopped unexpectedly several times. Start a new migration.',
		     finished_at = now(), updated_at = now()
		 WHERE status = 'scanning' AND attempts >= $1 AND lease_until < now()
		 RETURNING id`, []any{maxAttempts}},
		{`UPDATE tool_migrations
		 SET status = 'failed', credentials_sealed = NULL, error = 'The keys for this migration are gone.',
		     finished_at = now(), updated_at = now()
		 WHERE status = 'scanning' AND credentials_sealed IS NULL
		 RETURNING id`, nil},
	}
	for _, step := range steps {
		rows, err := r.DB.Query(ctx, step.query, step.args...)
		if err != nil {
			db.CaptureError(err, step.query, nil, "query")
			return ended, err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return ended, err
		}
		ended = append(ended, ids...)
	}
	// A target that keeps dying is failed rather than retried forever.
	failTargets := `
		UPDATE tool_migration_targets
		SET status = 'failed', lease_until = NULL, finished_at = now(), updated_at = now(),
		    error = 'This workspace stopped unexpectedly several times. What it brought over stays.'
		WHERE status = 'running' AND attempts >= $1 AND lease_until < now()`
	if _, err := r.DB.Exec(ctx, failTargets, maxAttempts); err != nil {
		db.CaptureError(err, failTargets, nil, "exec")
		return ended, err
	}
	orphans := `
		SELECT m.id FROM tool_migrations m
		WHERE m.status = 'running'
		  AND NOT EXISTS (SELECT 1 FROM tool_migration_targets t WHERE t.migration_id = m.id AND t.status IN ('pending', 'running'))`
	rows, err := r.DB.Query(ctx, orphans)
	if err != nil {
		db.CaptureError(err, orphans, nil, "query")
		return ended, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return ended, err
	}
	for _, id := range ids {
		if status, err := r.Finalize(ctx, id); err == nil && status != "" {
			ended = append(ended, id)
		}
	}
	// Belt and braces: no ended migration keeps a key.
	wipe := `UPDATE tool_migrations SET credentials_sealed = NULL, updated_at = now()
		WHERE credentials_sealed IS NOT NULL AND status IN ('completed', 'failed', 'canceled')`
	if _, err := r.DB.Exec(ctx, wipe); err != nil {
		db.CaptureError(err, wipe, nil, "exec")
		return ended, err
	}
	return ended, nil
}

func nonNilSlice[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
