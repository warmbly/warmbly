package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// LeadSyncRepository owns persistence for on-demand Google Sheets -> leads
// sync sources. Every query is organization-scoped; a source is only ever
// reachable by the org that created it.
type LeadSyncRepository interface {
	Create(ctx context.Context, src *models.LeadSyncSource) error
	List(ctx context.Context, orgID uuid.UUID, campaignID *uuid.UUID) ([]models.LeadSyncSource, error)
	Get(ctx context.Context, orgID, id uuid.UUID) (*models.LeadSyncSource, error)
	Update(ctx context.Context, src *models.LeadSyncSource) error
	Delete(ctx context.Context, orgID, id uuid.UUID) error
	// SetResult records the outcome of a "Sync now" run. resultJSON is the
	// marshalled *models.ContactImportResult (nil-safe).
	SetResult(ctx context.Context, id uuid.UUID, status models.LeadSyncStatus, lastSyncedAt *time.Time, resultJSON []byte, lastErr string) error
}

type leadSyncRepository struct {
	db *pgxpool.Pool
}

func NewLeadSyncRepository(db *pgxpool.Pool) LeadSyncRepository {
	return &leadSyncRepository{db: db}
}

const leadSyncCols = `
	id, organization_id, created_by_user_id, provider, connection_id,
	sheet_id, COALESCE(sheet_title, ''), COALESCE(tab_title, ''), COALESCE(a1_range, ''),
	has_header, column_mapping, dedup, target_campaign_id, category_ids,
	subscribed_default, COALESCE(label, ''), status, last_synced_at, last_result,
	COALESCE(last_error, ''), created_at, updated_at`

func (r *leadSyncRepository) Create(ctx context.Context, src *models.LeadSyncSource) error {
	if src.ID == uuid.Nil {
		src.ID = uuid.New()
	}
	now := time.Now().UTC()
	src.CreatedAt = now
	src.UpdatedAt = now
	if src.Provider == "" {
		src.Provider = string(models.IntegrationGoogleSheets)
	}
	if src.Status == "" {
		src.Status = models.LeadSyncStatusIdle
	}

	mapping := marshalJSONDefault(src.ColumnMapping, "[]")
	cats := marshalJSONDefault(src.CategoryIDs, "[]")

	_, err := r.db.Exec(ctx, `
		INSERT INTO lead_sync_sources (
			id, organization_id, created_by_user_id, provider, connection_id,
			sheet_id, sheet_title, tab_title, a1_range, has_header,
			column_mapping, dedup, target_campaign_id, category_ids,
			subscribed_default, label, status, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10,
			$11, $12, $13, $14,
			$15, $16, $17, $18, $18
		)`,
		src.ID, src.OrganizationID, src.CreatedByUserID, src.Provider, src.ConnectionID,
		src.SheetID, nullIfEmptyStr(src.SheetTitle), nullIfEmptyStr(src.TabTitle), nullIfEmptyStr(src.A1Range), src.HasHeader,
		mapping, string(src.Dedup), src.TargetCampaignID, cats,
		src.SubscribedDefault, nullIfEmptyStr(src.Label), string(src.Status), now,
	)
	return err
}

func (r *leadSyncRepository) List(ctx context.Context, orgID uuid.UUID, campaignID *uuid.UUID) ([]models.LeadSyncSource, error) {
	rows, err := r.db.Query(ctx, `SELECT `+leadSyncCols+`
		FROM lead_sync_sources
		WHERE organization_id = $1 AND ($2::uuid IS NULL OR target_campaign_id = $2)
		ORDER BY created_at DESC`, orgID, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.LeadSyncSource{}
	for rows.Next() {
		var s models.LeadSyncSource
		if err := scanLeadSyncInto(rows, &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *leadSyncRepository) Get(ctx context.Context, orgID, id uuid.UUID) (*models.LeadSyncSource, error) {
	row := r.db.QueryRow(ctx, `SELECT `+leadSyncCols+`
		FROM lead_sync_sources WHERE organization_id = $1 AND id = $2`, orgID, id)
	var s models.LeadSyncSource
	if err := scanLeadSyncInto(row, &s); err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

func (r *leadSyncRepository) Update(ctx context.Context, src *models.LeadSyncSource) error {
	now := time.Now().UTC()
	src.UpdatedAt = now
	mapping := marshalJSONDefault(src.ColumnMapping, "[]")
	cats := marshalJSONDefault(src.CategoryIDs, "[]")

	_, err := r.db.Exec(ctx, `
		UPDATE lead_sync_sources SET
			sheet_id = $1, sheet_title = $2, tab_title = $3, a1_range = $4,
			has_header = $5, column_mapping = $6, dedup = $7, target_campaign_id = $8,
			category_ids = $9, subscribed_default = $10, label = $11, updated_at = $12
		WHERE organization_id = $13 AND id = $14`,
		src.SheetID, nullIfEmptyStr(src.SheetTitle), nullIfEmptyStr(src.TabTitle), nullIfEmptyStr(src.A1Range),
		src.HasHeader, mapping, string(src.Dedup), src.TargetCampaignID,
		cats, src.SubscribedDefault, nullIfEmptyStr(src.Label), now,
		src.OrganizationID, src.ID,
	)
	return err
}

func (r *leadSyncRepository) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	_, err := r.db.Exec(ctx,
		`DELETE FROM lead_sync_sources WHERE organization_id = $1 AND id = $2`, orgID, id)
	return err
}

func (r *leadSyncRepository) SetResult(ctx context.Context, id uuid.UUID, status models.LeadSyncStatus, lastSyncedAt *time.Time, resultJSON []byte, lastErr string) error {
	now := time.Now().UTC()
	_, err := r.db.Exec(ctx, `
		UPDATE lead_sync_sources SET
			status = $1, last_synced_at = $2, last_result = $3, last_error = $4, updated_at = $5
		WHERE id = $6`,
		string(status), lastSyncedAt, nullIfEmptyBytes(resultJSON), nullIfEmptyStr(lastErr), now, id)
	return err
}

// marshalJSONDefault marshals v to JSON bytes, falling back to a literal
// default (e.g. "[]") if v is nil or marshalling fails — JSONB columns are
// NOT NULL with a '[]' default, so we never write a SQL NULL there.
func marshalJSONDefault(v any, def string) []byte {
	if v == nil {
		return []byte(def)
	}
	b, err := json.Marshal(v)
	if err != nil || len(b) == 0 || string(b) == "null" {
		return []byte(def)
	}
	return b
}

func scanLeadSyncInto(row scanner, s *models.LeadSyncSource) error {
	var (
		mapping    []byte
		cats       []byte
		lastResult []byte
		status     string
		dedup      string
	)
	if err := row.Scan(
		&s.ID, &s.OrganizationID, &s.CreatedByUserID, &s.Provider, &s.ConnectionID,
		&s.SheetID, &s.SheetTitle, &s.TabTitle, &s.A1Range,
		&s.HasHeader, &mapping, &dedup, &s.TargetCampaignID, &cats,
		&s.SubscribedDefault, &s.Label, &status, &s.LastSyncedAt, &lastResult,
		&s.LastError, &s.CreatedAt, &s.UpdatedAt,
	); err != nil {
		return err
	}
	s.Status = models.LeadSyncStatus(status)
	s.Dedup = models.ContactImportDedupStrategy(dedup)

	s.ColumnMapping = []models.ContactImportColumnMapping{}
	if len(mapping) > 0 {
		_ = json.Unmarshal(mapping, &s.ColumnMapping)
	}
	s.CategoryIDs = []string{}
	if len(cats) > 0 {
		_ = json.Unmarshal(cats, &s.CategoryIDs)
	}
	if len(lastResult) > 0 {
		var res models.ContactImportResult
		if err := json.Unmarshal(lastResult, &res); err == nil {
			s.LastResult = &res
		}
	}
	return nil
}
