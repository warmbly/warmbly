package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// ErrApolloSourceRefs means an Apollo import source, its connection or its
// target campaign or categories are not in the organization.
var ErrApolloSourceRefs = errors.New("import source, connection, campaign or category not found")

// ApolloContactRef is the slice of a Warmbly contact enrichment reads.
type ApolloContactRef struct {
	ID           uuid.UUID
	Email        string
	FirstName    string
	LastName     string
	Company      string
	Phone        string
	CustomFields map[string]string
}

// ApolloRepository persists Apollo import sources, their members and
// enrichment runs. Every org-scoped read takes the organization.
type ApolloRepository interface {
	CreateSource(ctx context.Context, s *models.ApolloImportSource) error
	GetSource(ctx context.Context, orgID, id uuid.UUID) (*models.ApolloImportSource, error)
	ListSources(ctx context.Context, orgID, connID uuid.UUID) ([]models.ApolloImportSource, error)
	UpdateSource(ctx context.Context, s *models.ApolloImportSource) error
	DeleteSource(ctx context.Context, orgID, id uuid.UUID) (bool, error)
	ClaimSource(ctx context.Context, id uuid.UUID) (bool, error)
	FinishSource(ctx context.Context, id uuid.UUID, result *models.ApolloRunResult, errMsg string) error
	DueRecurringSources(ctx context.Context, every time.Duration) ([]models.ApolloImportSource, error)
	ReleaseStaleSources(ctx context.Context, after time.Duration) (int64, error)
	SourceMembers(ctx context.Context, sourceID uuid.UUID, apolloIDs []string) (map[string]bool, error)
	AddSourceMembers(ctx context.Context, sourceID uuid.UUID, members map[string]uuid.UUID) error

	ContactsByEmails(ctx context.Context, orgID uuid.UUID, emails []string) (map[string]uuid.UUID, error)
	SuppressedEmails(ctx context.Context, orgID uuid.UUID, emails []string) (map[string]bool, error)
	ContactsForEnrichment(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) ([]ApolloContactRef, error)

	CreateEnrichmentRun(ctx context.Context, r *models.ApolloEnrichmentRun) error
	GetEnrichmentRun(ctx context.Context, orgID, id uuid.UUID) (*models.ApolloEnrichmentRun, error)
	ListEnrichmentRuns(ctx context.Context, orgID, connID uuid.UUID, limit int) ([]models.ApolloEnrichmentRun, error)
	ClaimEnrichmentRun(ctx context.Context, id uuid.UUID) (*models.ApolloEnrichmentRun, error)
	DueEnrichmentRuns(ctx context.Context, limit int) ([]uuid.UUID, error)
	UpdateEnrichmentProgress(ctx context.Context, r *models.ApolloEnrichmentRun) error
	FinishEnrichmentRun(ctx context.Context, r *models.ApolloEnrichmentRun) error
}

type apolloRepository struct {
	db *pgxpool.Pool
}

// NewApolloRepository builds the Postgres Apollo repository.
func NewApolloRepository(db *pgxpool.Pool) ApolloRepository {
	return &apolloRepository{db: db}
}

// --- import sources ---------------------------------------------------------

const apolloSourceColumns = `id, organization_id, connection_id, created_by_user_id, name, list_id, list_name,
	campaign_id, category_ids, verified_only, recurring, enabled, status, last_run_at, last_result, last_error,
	total_imported, created_at, updated_at, segment_ids`

func scanApolloSource(row pgx.Row) (models.ApolloImportSource, error) {
	var s models.ApolloImportSource
	var result []byte
	err := row.Scan(&s.ID, &s.OrganizationID, &s.ConnectionID, &s.CreatedByUserID, &s.Name, &s.ListID, &s.ListName,
		&s.CampaignID, &s.CategoryIDs, &s.VerifiedOnly, &s.Recurring, &s.Enabled, &s.Status, &s.LastRunAt, &result,
		&s.LastError, &s.TotalImported, &s.CreatedAt, &s.UpdatedAt, &s.SegmentIDs)
	if err == nil && len(result) > 0 {
		var rr models.ApolloRunResult
		if json.Unmarshal(result, &rr) == nil {
			s.LastResult = &rr
		}
	}
	if s.CategoryIDs == nil {
		s.CategoryIDs = []uuid.UUID{}
	}
	if s.SegmentIDs == nil {
		s.SegmentIDs = []uuid.UUID{}
	}
	return s, err
}

func (r *apolloRepository) CreateSource(ctx context.Context, s *models.ApolloImportSource) error {
	if s.CategoryIDs == nil {
		s.CategoryIDs = []uuid.UUID{}
	}
	if s.SegmentIDs == nil {
		s.SegmentIDs = []uuid.UUID{}
	}
	row := r.db.QueryRow(ctx, `
		INSERT INTO apollo_import_sources (
			organization_id, connection_id, created_by_user_id, name, list_id, list_name,
			campaign_id, category_ids, verified_only, recurring, enabled, segment_ids
		)
		SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
		WHERE EXISTS (SELECT 1 FROM integration_connections WHERE id = $2 AND organization_id = $1 AND provider = 'apollo')
		  AND ($7::uuid IS NULL OR EXISTS (SELECT 1 FROM campaigns WHERE id = $7 AND organization_id = $1))
		  AND NOT EXISTS (SELECT 1 FROM unnest($8::uuid[]) c(id) WHERE NOT EXISTS (SELECT 1 FROM categories cat WHERE cat.id = c.id AND cat.organization_id = $1))
		  AND NOT EXISTS (SELECT 1 FROM unnest($12::uuid[]) g(id) WHERE NOT EXISTS (SELECT 1 FROM segments seg WHERE seg.id = g.id AND seg.organization_id = $1))
		RETURNING `+apolloSourceColumns,
		s.OrganizationID, s.ConnectionID, s.CreatedByUserID, s.Name, s.ListID, s.ListName,
		s.CampaignID, s.CategoryIDs, s.VerifiedOnly, s.Recurring, s.Enabled, s.SegmentIDs)
	out, err := scanApolloSource(row)
	if isNoRows(err) {
		return ErrApolloSourceRefs
	}
	if err != nil {
		return err
	}
	*s = out
	return nil
}

func (r *apolloRepository) GetSource(ctx context.Context, orgID, id uuid.UUID) (*models.ApolloImportSource, error) {
	s, err := scanApolloSource(r.db.QueryRow(ctx, `SELECT `+apolloSourceColumns+` FROM apollo_import_sources WHERE organization_id = $1 AND id = $2`, orgID, id))
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *apolloRepository) ListSources(ctx context.Context, orgID, connID uuid.UUID) ([]models.ApolloImportSource, error) {
	rows, err := r.db.Query(ctx, `SELECT `+apolloSourceColumns+` FROM apollo_import_sources WHERE organization_id = $1 AND connection_id = $2 ORDER BY created_at DESC`, orgID, connID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.ApolloImportSource{}
	for rows.Next() {
		s, err := scanApolloSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *apolloRepository) UpdateSource(ctx context.Context, s *models.ApolloImportSource) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE apollo_import_sources SET
			name = $3, campaign_id = $4, category_ids = $5, verified_only = $6, recurring = $7, enabled = $8,
			segment_ids = $9, updated_at = NOW()
		WHERE organization_id = $1 AND id = $2
		  AND ($4::uuid IS NULL OR EXISTS (SELECT 1 FROM campaigns WHERE id = $4 AND organization_id = $1))
		  AND NOT EXISTS (SELECT 1 FROM unnest($5::uuid[]) c(id) WHERE c.id <> ALL(category_ids) AND NOT EXISTS (SELECT 1 FROM categories cat WHERE cat.id = c.id AND cat.organization_id = $1))
		  AND NOT EXISTS (SELECT 1 FROM unnest($9::uuid[]) g(id) WHERE g.id <> ALL(segment_ids) AND NOT EXISTS (SELECT 1 FROM segments seg WHERE seg.id = g.id AND seg.organization_id = $1))`,
		s.OrganizationID, s.ID, s.Name, s.CampaignID, s.CategoryIDs, s.VerifiedOnly, s.Recurring, s.Enabled, s.SegmentIDs)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrApolloSourceRefs
	}
	return nil
}

func (r *apolloRepository) DeleteSource(ctx context.Context, orgID, id uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM apollo_import_sources WHERE organization_id = $1 AND id = $2`, orgID, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ClaimSource marks a source running unless it already is; a run that died
// mid-way is reclaimable after 30 minutes.
func (r *apolloRepository) ClaimSource(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE apollo_import_sources SET status = 'running', updated_at = NOW()
		WHERE id = $1 AND (status <> 'running' OR updated_at < NOW() - INTERVAL '30 minutes')`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ReleaseStaleSources marks runs that outlived any live run (a restart mid-import)
// as interrupted, so they show an error, can be run again and sync on schedule.
func (r *apolloRepository) ReleaseStaleSources(ctx context.Context, after time.Duration) (int64, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE apollo_import_sources
		SET status = 'error', last_error = 'The last run was interrupted. It runs again on its next check, or choose Run now.', updated_at = NOW()
		WHERE status = 'running' AND updated_at < NOW() - make_interval(secs => $1)`, after.Seconds())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *apolloRepository) FinishSource(ctx context.Context, id uuid.UUID, result *models.ApolloRunResult, errMsg string) error {
	var raw []byte
	added := 0
	if result != nil {
		raw, _ = json.Marshal(result)
		added = result.Imported
	}
	status := "idle"
	if errMsg != "" {
		status = "error"
	}
	_, err := r.db.Exec(ctx, `
		UPDATE apollo_import_sources SET
			status = $2, last_run_at = NOW(), last_result = COALESCE($3, last_result), last_error = $4,
			total_imported = total_imported + $5, updated_at = NOW()
		WHERE id = $1`, id, status, raw, truncateRunes(errMsg, 500), added)
	return err
}

func (r *apolloRepository) DueRecurringSources(ctx context.Context, every time.Duration) ([]models.ApolloImportSource, error) {
	rows, err := r.db.Query(ctx, `
		SELECT s.id, s.organization_id, s.connection_id, s.created_by_user_id, s.name, s.list_id, s.list_name,
			s.campaign_id, s.category_ids, s.verified_only, s.recurring, s.enabled, s.status, s.last_run_at,
			s.last_result, s.last_error, s.total_imported, s.created_at, s.updated_at, s.segment_ids
		FROM apollo_import_sources s
		JOIN integration_connections c ON c.id = s.connection_id
		WHERE s.recurring AND s.enabled
		  AND (s.status <> 'running' OR s.updated_at < NOW() - INTERVAL '30 minutes')
		  AND c.status IN ('connected', 'degraded')
		  AND (s.last_run_at IS NULL OR s.last_run_at < NOW() - make_interval(secs => $1))
		ORDER BY s.last_run_at NULLS FIRST
		LIMIT 50`, every.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.ApolloImportSource
	for rows.Next() {
		s, err := scanApolloSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *apolloRepository) SourceMembers(ctx context.Context, sourceID uuid.UUID, apolloIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(apolloIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `SELECT apollo_contact_id FROM apollo_import_members WHERE source_id = $1 AND apollo_contact_id = ANY($2)`, sourceID, apolloIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// AddSourceMembers records the Apollo contacts a source imported.
func (r *apolloRepository) AddSourceMembers(ctx context.Context, sourceID uuid.UUID, members map[string]uuid.UUID) error {
	if len(members) == 0 {
		return nil
	}
	ids := make([]string, 0, len(members))
	contacts := make([]string, 0, len(members))
	for rec, c := range members {
		ids = append(ids, rec)
		if c == uuid.Nil {
			contacts = append(contacts, "")
		} else {
			contacts = append(contacts, c.String())
		}
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO apollo_import_members (source_id, apollo_contact_id, contact_id)
		SELECT $1, rec, NULLIF(cid, '')::uuid FROM UNNEST($2::text[], $3::text[]) AS m(rec, cid)
		ON CONFLICT (source_id, apollo_contact_id) DO UPDATE SET contact_id = COALESCE(EXCLUDED.contact_id, apollo_import_members.contact_id)`,
		sourceID, ids, contacts)
	return err
}

// ContactsByEmails maps lowercased addresses to the organization's contacts.
func (r *apolloRepository) ContactsByEmails(ctx context.Context, orgID uuid.UUID, emails []string) (map[string]uuid.UUID, error) {
	out := map[string]uuid.UUID{}
	if len(emails) == 0 {
		return out, nil
	}
	lower := make([]string, 0, len(emails))
	for _, e := range emails {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			lower = append(lower, e)
		}
	}
	rows, err := r.db.Query(ctx, `
		SELECT DISTINCT ON (LOWER(email)) LOWER(email), id FROM contacts
		WHERE organization_id = $1 AND LOWER(email) = ANY($2)
		ORDER BY LOWER(email), created_at`, orgID, lower)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var email string
		var id uuid.UUID
		if err := rows.Scan(&email, &id); err != nil {
			return nil, err
		}
		out[email] = id
	}
	return out, rows.Err()
}

// SuppressedEmails reports which addresses the organization has suppressed,
// by address or by domain, with the same predicate the send gates use.
func (r *apolloRepository) SuppressedEmails(ctx context.Context, orgID uuid.UUID, emails []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(emails) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT addr FROM UNNEST($2::text[]) AS a(addr) WHERE recipient_suppressed($1, addr)`, orgID, emails)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var addr string
		if err := rows.Scan(&addr); err != nil {
			return nil, err
		}
		out[strings.ToLower(addr)] = true
	}
	return out, rows.Err()
}

// ContactsForEnrichment loads the organization's contacts among ids.
func (r *apolloRepository) ContactsForEnrichment(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID) ([]ApolloContactRef, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT id, email, COALESCE(first_name, ''), COALESCE(last_name, ''), COALESCE(company, ''),
		       COALESCE(phone, ''), COALESCE(custom_fields, '{}'::jsonb)
		FROM contacts
		WHERE organization_id = $1 AND id = ANY($2)`, orgID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ApolloContactRef
	for rows.Next() {
		var c ApolloContactRef
		var custom []byte
		if err := rows.Scan(&c.ID, &c.Email, &c.FirstName, &c.LastName, &c.Company, &c.Phone, &custom); err != nil {
			return nil, err
		}
		c.CustomFields = map[string]string{}
		if len(custom) > 0 {
			var raw map[string]any
			if json.Unmarshal(custom, &raw) == nil {
				for k, v := range raw {
					if s, ok := v.(string); ok {
						c.CustomFields[k] = s
					}
				}
			}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// --- enrichment runs --------------------------------------------------------

const apolloRunColumns = `id, organization_id, connection_id, requested_by_user_id, contact_ids, overwrite, status,
	total, processed, enriched, not_found, failed, credits_consumed, last_error, started_at, finished_at,
	created_at, updated_at`

func scanApolloRun(row pgx.Row) (models.ApolloEnrichmentRun, error) {
	var r models.ApolloEnrichmentRun
	err := row.Scan(&r.ID, &r.OrganizationID, &r.ConnectionID, &r.RequestedByUserID, &r.ContactIDs, &r.Overwrite,
		&r.Status, &r.Total, &r.Processed, &r.Enriched, &r.NotFound, &r.Failed, &r.CreditsConsumed, &r.LastError,
		&r.StartedAt, &r.FinishedAt, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

func (r *apolloRepository) CreateEnrichmentRun(ctx context.Context, run *models.ApolloEnrichmentRun) error {
	row := r.db.QueryRow(ctx, `
		INSERT INTO apollo_enrichment_runs (organization_id, connection_id, requested_by_user_id, contact_ids, overwrite, total)
		SELECT $1, $2, $3, $4, $5, $6
		WHERE EXISTS (SELECT 1 FROM integration_connections WHERE id = $2 AND organization_id = $1 AND provider = 'apollo')
		RETURNING `+apolloRunColumns,
		run.OrganizationID, run.ConnectionID, run.RequestedByUserID, run.ContactIDs, run.Overwrite, len(run.ContactIDs))
	out, err := scanApolloRun(row)
	if isNoRows(err) {
		return ErrApolloSourceRefs
	}
	if err != nil {
		return err
	}
	*run = out
	return nil
}

func (r *apolloRepository) GetEnrichmentRun(ctx context.Context, orgID, id uuid.UUID) (*models.ApolloEnrichmentRun, error) {
	out, err := scanApolloRun(r.db.QueryRow(ctx, `SELECT `+apolloRunColumns+` FROM apollo_enrichment_runs WHERE organization_id = $1 AND id = $2`, orgID, id))
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *apolloRepository) ListEnrichmentRuns(ctx context.Context, orgID, connID uuid.UUID, limit int) ([]models.ApolloEnrichmentRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := r.db.Query(ctx, `
		SELECT `+apolloRunColumns+` FROM apollo_enrichment_runs
		WHERE organization_id = $1 AND connection_id = $2
		ORDER BY created_at DESC LIMIT $3`, orgID, connID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.ApolloEnrichmentRun{}
	for rows.Next() {
		run, err := scanApolloRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

// ClaimEnrichmentRun moves a queued run, or a running one nobody has touched
// for 15 minutes, to running and returns it; nil when someone else holds it.
func (r *apolloRepository) ClaimEnrichmentRun(ctx context.Context, id uuid.UUID) (*models.ApolloEnrichmentRun, error) {
	out, err := scanApolloRun(r.db.QueryRow(ctx, `
		UPDATE apollo_enrichment_runs SET status = 'running', started_at = COALESCE(started_at, NOW()), updated_at = NOW()
		WHERE id = $1 AND (status = 'queued' OR (status = 'running' AND updated_at < NOW() - INTERVAL '15 minutes'))
		RETURNING `+apolloRunColumns, id))
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DueEnrichmentRuns lists runs waiting to start or abandoned mid-way.
func (r *apolloRepository) DueEnrichmentRuns(ctx context.Context, limit int) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id FROM apollo_enrichment_runs
		WHERE (status = 'queued' AND updated_at < NOW() - INTERVAL '1 minute')
		   OR (status = 'running' AND updated_at < NOW() - INTERVAL '15 minutes')
		ORDER BY created_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *apolloRepository) UpdateEnrichmentProgress(ctx context.Context, run *models.ApolloEnrichmentRun) error {
	_, err := r.db.Exec(ctx, `
		UPDATE apollo_enrichment_runs SET
			processed = $2, enriched = $3, not_found = $4, failed = $5, credits_consumed = $6, updated_at = NOW()
		WHERE id = $1 AND status = 'running'`,
		run.ID, run.Processed, run.Enriched, run.NotFound, run.Failed, run.CreditsConsumed)
	return err
}

func (r *apolloRepository) FinishEnrichmentRun(ctx context.Context, run *models.ApolloEnrichmentRun) error {
	_, err := r.db.Exec(ctx, `
		UPDATE apollo_enrichment_runs SET
			status = $2, processed = $3, enriched = $4, not_found = $5, failed = $6, credits_consumed = $7,
			last_error = $8, finished_at = NOW(), updated_at = NOW()
		WHERE id = $1`,
		run.ID, run.Status, run.Processed, run.Enriched, run.NotFound, run.Failed, run.CreditsConsumed,
		truncateRunes(run.LastError, 500))
	return err
}
