package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// CRMProviderRepository stores a workspace's CRM mode and the mirror that keeps
// a connected CRM's records readable from Postgres.
type CRMProviderRepository interface {
	GetSettings(ctx context.Context, orgID uuid.UUID) (*CRMSettingsRow, error)
	UpsertSettings(ctx context.Context, row *CRMSettingsRow) error
	ListProviderOrgs(ctx context.Context, provider models.CRMProvider) ([]CRMSettingsRow, error)
	OrgsForAccount(ctx context.Context, provider models.CRMProvider, externalAccountID string) ([]uuid.UUID, error)
	OrgForConnection(ctx context.Context, provider models.CRMProvider, connID uuid.UUID) (uuid.UUID, error)
	OrgsForCompany(ctx context.Context, provider models.CRMProvider, companyID string) ([]uuid.UUID, error)

	GetLinkByLocal(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, objectType string, localID uuid.UUID) (*models.CRMExternalLink, error)
	GetLinkByExternal(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, objectType, externalID string) (*models.CRMExternalLink, error)
	LinksForLocal(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, objectType string, ids []uuid.UUID) (map[uuid.UUID]models.CRMExternalLink, error)
	ListLinks(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, objectType string) ([]models.CRMExternalLink, error)
	UpsertLink(ctx context.Context, l *models.CRMExternalLink) error
	ClaimLink(ctx context.Context, l *models.CRMExternalLink) error
	DeleteLinkByLocal(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, objectType string, localID uuid.UUID) error

	GetContactRecord(ctx context.Context, orgID, contactID uuid.UUID, provider models.CRMProvider) (*models.CRMContactRecord, error)
	GetContactRecordByExternal(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, externalID string) (*models.CRMContactRecord, error)
	UpsertContactRecord(ctx context.Context, rec *models.CRMContactRecord) error
	DeleteContactRecord(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, externalID string) error
	ContactIDsByEmail(ctx context.Context, orgID uuid.UUID, emails []string) (map[string]uuid.UUID, error)
	UpdateContactFields(ctx context.Context, orgID, contactID uuid.UUID, fields map[string]string, custom map[string]string) error
	MailboxUser(ctx context.Context, orgID, emailAccountID uuid.UUID) (*uuid.UUID, error)
	CampaignName(ctx context.Context, orgID, campaignID uuid.UUID) (string, error)
	CampaignOptions(ctx context.Context, orgID uuid.UUID, query string, limit int) ([]CampaignOption, error)
	ResumeHeldEverywhere(ctx context.Context, orgID, contactID uuid.UUID) (int64, error)

	ReplaceOwners(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, owners []models.CRMOwner) error
	ListOwners(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider) ([]models.CRMOwner, error)
	SetOwnerUser(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, externalID string, userID *uuid.UUID) error
	OwnerForUser(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, userID uuid.UUID) (string, error)
	UserForOwner(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, externalID string) (*uuid.UUID, error)
	FallbackActor(ctx context.Context, orgID uuid.UUID) (uuid.UUID, error)
	MemberForOwnerEmail(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, email string) (*uuid.UUID, models.OrganizationPermission, error)
	MemberForOwner(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, externalID string) (*uuid.UUID, models.OrganizationPermission, error)

	EnqueueJob(ctx context.Context, job *models.CRMSyncJob) error
	ClaimJobs(ctx context.Context, provider models.CRMProvider, limit int, lease time.Duration) ([]models.CRMSyncJob, error)
	CompleteJob(ctx context.Context, job *models.CRMSyncJob) error
	FailJob(ctx context.Context, job *models.CRMSyncJob, msg string, retryAt *time.Time) error
	ContinueJob(ctx context.Context, job *models.CRMSyncJob, payload map[string]any) error
	RetryFailedJobs(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, ids []uuid.UUID) (int64, error)
	DiscardFailedJobs(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, ids []uuid.UUID) (int64, error)
	PurgeJobs(ctx context.Context) error
	SyncHealth(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider) (*models.CRMSyncHealth, error)

	GetCursor(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, objectType string) (*time.Time, error)
	SetCursor(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, objectType string, at *time.Time, runErr string) error

	MirrorPipeline(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, externalID, name string, position int) (uuid.UUID, error)
	MirrorStage(ctx context.Context, orgID, pipelineID uuid.UUID, provider models.CRMProvider, externalID, name, color string, position int, meta map[string]any) (uuid.UUID, error)
	PruneStages(ctx context.Context, orgID, pipelineID uuid.UUID, provider models.CRMProvider, keepExternal []string) error
	PrunePipelines(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, keepExternal []string) error
	MirrorDeal(ctx context.Context, in *MirrorDeal) (uuid.UUID, bool, error)
	MirrorTask(ctx context.Context, in *MirrorTask) (uuid.UUID, bool, error)
	MirrorNote(ctx context.Context, in *MirrorNote) (uuid.UUID, bool, error)
	DeleteMirrored(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, objectType, externalID string) error
	OpenDealContactIDs(ctx context.Context, orgID uuid.UUID, contactIDs []uuid.UUID) (map[uuid.UUID]bool, error)
	MoveDeal(ctx context.Context, orgID, dealID, pipelineID, stageID uuid.UUID) error

	UnlinkedNative(ctx context.Context, orgID uuid.UUID, objectType string, after uuid.UUID, limit int) ([]uuid.UUID, error)
	CountUnlinkedNative(ctx context.Context, orgID uuid.UUID) (*models.CRMBackfillPreview, error)
}

// CRMSettingsRow is crm_settings with the config still raw.
type CRMSettingsRow struct {
	OrganizationID   uuid.UUID
	Provider         models.CRMProvider
	ConnectionID     *uuid.UUID
	Config           models.CRMProviderConfig
	SetupCompletedAt *time.Time
	UpdatedAt        time.Time
}

// MirrorDeal is a provider deal written into the local deals table.
type MirrorDeal struct {
	OrganizationID uuid.UUID
	Provider       models.CRMProvider
	ExternalID     string
	PipelineID     uuid.UUID
	StageID        uuid.UUID
	ContactID      *uuid.UUID
	Name           string
	Value          *float64
	Currency       string
	Status         models.DealStatus
	CloseDate      *time.Time
	AssignedTo     *uuid.UUID
	CreatedAt      *time.Time
	ClosedAt       *time.Time
	Meta           map[string]any
}

// MirrorTask is a provider task written into the local crm_tasks table.
type MirrorTask struct {
	OrganizationID uuid.UUID
	Provider       models.CRMProvider
	ExternalID     string
	ContactID      *uuid.UUID
	DealID         *uuid.UUID
	AssignedTo     *uuid.UUID
	CreatedBy      uuid.UUID
	Title          string
	Description    *string
	DueDate        *time.Time
	Priority       models.CRMTaskPriority
	Type           string
	Status         models.CRMTaskStatus
	CompletedAt    *time.Time
	CreatedAt      *time.Time
	Meta           map[string]any
}

// MirrorNote is a provider note written into the local contact_notes table.
type MirrorNote struct {
	OrganizationID uuid.UUID
	Provider       models.CRMProvider
	ExternalID     string
	ContactID      uuid.UUID
	UserID         uuid.UUID
	Content        string
	CreatedAt      *time.Time
	Meta           map[string]any
}

// ErrCRMRecordNotFound is a mirror lookup that names nothing in this workspace.
var ErrCRMRecordNotFound = errors.New("crm record not found")

type crmProviderRepository struct {
	db *pgxpool.Pool
}

func NewCRMProviderRepository(db *pgxpool.Pool) CRMProviderRepository {
	return &crmProviderRepository{db: db}
}

// ---------- settings ----------

func (r *crmProviderRepository) GetSettings(ctx context.Context, orgID uuid.UUID) (*CRMSettingsRow, error) {
	var row CRMSettingsRow
	var raw []byte
	err := r.db.QueryRow(ctx, `
		SELECT organization_id, provider, connection_id, config, setup_completed_at, updated_at
		FROM crm_settings WHERE organization_id = $1`, orgID).
		Scan(&row.OrganizationID, &row.Provider, &row.ConnectionID, &raw, &row.SetupCompletedAt, &row.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cfg, err := decodeCRMConfig(row.Provider, raw)
	if err != nil {
		return nil, err
	}
	row.Config = cfg
	return &row, nil
}

// decodeCRMConfig reads stored choices over the provider's defaults. A stored
// field map replaces the default one whole, so a mapping someone removed does
// not come back from the defaults.
func decodeCRMConfig(provider models.CRMProvider, raw []byte) (models.CRMProviderConfig, error) {
	cfg := models.DefaultCRMProviderConfigFor(provider)
	if len(raw) <= 2 {
		return cfg, nil
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return cfg, err
	}
	if _, ok := keys["field_map"]; ok {
		cfg.FieldMap = nil
	}
	if _, ok := keys["field_direction"]; ok {
		cfg.FieldDirection = nil
	}
	err := json.Unmarshal(raw, &cfg)
	return cfg, err
}

func (r *crmProviderRepository) UpsertSettings(ctx context.Context, row *CRMSettingsRow) error {
	raw, err := json.Marshal(row.Config)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `
		INSERT INTO crm_settings (organization_id, provider, connection_id, config, setup_completed_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (organization_id) DO UPDATE
		SET provider = EXCLUDED.provider, connection_id = EXCLUDED.connection_id, config = EXCLUDED.config,
		    setup_completed_at = EXCLUDED.setup_completed_at, updated_at = NOW()`,
		row.OrganizationID, row.Provider, row.ConnectionID, raw, row.SetupCompletedAt)
	return err
}

func (r *crmProviderRepository) ListProviderOrgs(ctx context.Context, provider models.CRMProvider) ([]CRMSettingsRow, error) {
	rows, err := r.db.Query(ctx, `
		SELECT s.organization_id, s.provider, s.connection_id, s.config, s.setup_completed_at, s.updated_at
		FROM crm_settings s
		JOIN integration_connections c ON c.id = s.connection_id AND c.organization_id = s.organization_id
		WHERE s.provider = $1 AND c.status IN ('connected', 'degraded')`, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CRMSettingsRow
	for rows.Next() {
		var row CRMSettingsRow
		var raw []byte
		if err := rows.Scan(&row.OrganizationID, &row.Provider, &row.ConnectionID, &raw, &row.SetupCompletedAt, &row.UpdatedAt); err != nil {
			return nil, err
		}
		row.Config, _ = decodeCRMConfig(row.Provider, raw)
		out = append(out, row)
	}
	return out, rows.Err()
}

// OrgsForAccount lists the workspaces in provider mode with a live connection
// to one provider account (a HubSpot portal).
func (r *crmProviderRepository) OrgsForAccount(ctx context.Context, provider models.CRMProvider, externalAccountID string) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `
		SELECT s.organization_id FROM crm_settings s
		JOIN integration_connections c ON c.id = s.connection_id AND c.organization_id = s.organization_id
		WHERE s.provider = $1 AND c.provider = $1 AND c.external_account_id = $2
		  AND c.status IN ('connected', 'degraded')
		ORDER BY s.created_at`, provider, externalAccountID)
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

// OrgForConnection is the workspace whose CRM runs on one connection, or
// uuid.Nil when none does.
func (r *crmProviderRepository) OrgForConnection(ctx context.Context, provider models.CRMProvider, connID uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.QueryRow(ctx, `
		SELECT s.organization_id FROM crm_settings s
		JOIN integration_connections c ON c.id = s.connection_id AND c.organization_id = s.organization_id
		WHERE s.provider = $1 AND s.connection_id = $2 AND c.provider = $1 AND c.status IN ('connected', 'degraded')`,
		provider, connID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	return id, err
}

// OrgsForCompany lists the workspaces in provider mode with a live connection
// to one provider company (a Pipedrive company id, kept on the connection).
func (r *crmProviderRepository) OrgsForCompany(ctx context.Context, provider models.CRMProvider, companyID string) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `
		SELECT s.organization_id FROM crm_settings s
		JOIN integration_connections c ON c.id = s.connection_id AND c.organization_id = s.organization_id
		WHERE s.provider = $1 AND c.provider = $1 AND c.display_fields->>'company_id' = $2
		  AND c.status IN ('connected', 'degraded')
		ORDER BY s.created_at`, provider, companyID)
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

// ---------- links ----------

const linkCols = `organization_id, provider, object_type, local_id, external_id, meta, synced_at`

func scanLink(row pgx.Row) (*models.CRMExternalLink, error) {
	var l models.CRMExternalLink
	var meta []byte
	if err := row.Scan(&l.OrganizationID, &l.Provider, &l.ObjectType, &l.LocalID, &l.ExternalID, &meta, &l.SyncedAt); err != nil {
		return nil, err
	}
	l.Meta = map[string]any{}
	if len(meta) > 0 {
		_ = json.Unmarshal(meta, &l.Meta)
	}
	return &l, nil
}

func (r *crmProviderRepository) GetLinkByLocal(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, objectType string, localID uuid.UUID) (*models.CRMExternalLink, error) {
	l, err := scanLink(r.db.QueryRow(ctx, `SELECT `+linkCols+` FROM crm_external_links
		WHERE organization_id = $1 AND provider = $2 AND object_type = $3 AND local_id = $4`, orgID, provider, objectType, localID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return l, err
}

func (r *crmProviderRepository) GetLinkByExternal(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, objectType, externalID string) (*models.CRMExternalLink, error) {
	l, err := scanLink(r.db.QueryRow(ctx, `SELECT `+linkCols+` FROM crm_external_links
		WHERE organization_id = $1 AND provider = $2 AND object_type = $3 AND external_id = $4`, orgID, provider, objectType, externalID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return l, err
}

func (r *crmProviderRepository) LinksForLocal(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, objectType string, ids []uuid.UUID) (map[uuid.UUID]models.CRMExternalLink, error) {
	out := map[uuid.UUID]models.CRMExternalLink{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `SELECT `+linkCols+` FROM crm_external_links
		WHERE organization_id = $1 AND provider = $2 AND object_type = $3 AND local_id = ANY($4)`, orgID, provider, objectType, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			return nil, err
		}
		out[l.LocalID] = *l
	}
	return out, rows.Err()
}

func (r *crmProviderRepository) ListLinks(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, objectType string) ([]models.CRMExternalLink, error) {
	rows, err := r.db.Query(ctx, `SELECT `+linkCols+` FROM crm_external_links
		WHERE organization_id = $1 AND provider = $2 AND object_type = $3`, orgID, provider, objectType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.CRMExternalLink
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

func (r *crmProviderRepository) UpsertLink(ctx context.Context, l *models.CRMExternalLink) error {
	return upsertLink(ctx, r.db, l)
}

func upsertLink(ctx context.Context, db interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, l *models.CRMExternalLink) error {
	meta := l.Meta
	if meta == nil {
		meta = map[string]any{}
	}
	raw, _ := json.Marshal(meta)
	var id uuid.UUID
	return db.QueryRow(ctx, `
		INSERT INTO crm_external_links (organization_id, provider, object_type, local_id, external_id, meta, synced_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (organization_id, provider, object_type, external_id) DO UPDATE
		SET local_id = EXCLUDED.local_id, meta = EXCLUDED.meta, synced_at = NOW()
		RETURNING id`,
		l.OrganizationID, l.Provider, l.ObjectType, l.LocalID, l.ExternalID, raw).Scan(&id)
}

// ClaimLink links a record Warmbly just created in the provider. Under the
// same lock as the mirror, so a pull that met the new record first and
// mirrored it as a second local row has that row removed.
func (r *crmProviderRepository) ClaimLink(ctx context.Context, l *models.CRMExternalLink) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockExternal(ctx, tx, l.OrganizationID, l.ObjectType, l.ExternalID); err != nil {
		return err
	}
	var existing uuid.UUID
	err = tx.QueryRow(ctx, `SELECT local_id FROM crm_external_links
		WHERE organization_id = $1 AND provider = $2 AND object_type = $3 AND external_id = $4`,
		l.OrganizationID, l.Provider, l.ObjectType, l.ExternalID).Scan(&existing)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil && existing != l.LocalID {
		if table, ok := mirrorTables[l.ObjectType]; ok {
			if _, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE organization_id = $1 AND id = $2`, l.OrganizationID, existing); err != nil {
				return err
			}
		}
	}
	if err := upsertLink(ctx, tx, l); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *crmProviderRepository) DeleteLinkByLocal(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, objectType string, localID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM crm_external_links WHERE organization_id = $1 AND provider = $2 AND object_type = $3 AND local_id = $4`,
		orgID, provider, objectType, localID)
	return err
}

// ---------- contact records ----------

const contactRecordCols = `organization_id, contact_id, provider, external_id, COALESCE(owner_external_id, ''), COALESCE(lifecycle_stage, ''),
	COALESCE(lead_status, ''), COALESCE(company_external_id, ''), COALESCE(company_name, ''), COALESCE(company_domain, ''),
	opted_out, properties, external_updated_at, synced_at`

func scanContactRecord(row pgx.Row) (*models.CRMContactRecord, error) {
	var rec models.CRMContactRecord
	var props []byte
	if err := row.Scan(&rec.OrganizationID, &rec.ContactID, &rec.Provider, &rec.ExternalID, &rec.OwnerExternalID,
		&rec.LifecycleStage, &rec.LeadStatus, &rec.CompanyExternalID, &rec.CompanyName, &rec.CompanyDomain,
		&rec.OptedOut, &props, &rec.ExternalUpdatedAt, &rec.SyncedAt); err != nil {
		return nil, err
	}
	rec.Properties = map[string]string{}
	if len(props) > 0 {
		_ = json.Unmarshal(props, &rec.Properties)
	}
	return &rec, nil
}

func (r *crmProviderRepository) GetContactRecord(ctx context.Context, orgID, contactID uuid.UUID, provider models.CRMProvider) (*models.CRMContactRecord, error) {
	rec, err := scanContactRecord(r.db.QueryRow(ctx, `SELECT `+contactRecordCols+` FROM crm_contact_records
		WHERE organization_id = $1 AND contact_id = $2 AND provider = $3`, orgID, contactID, provider))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return rec, err
}

func (r *crmProviderRepository) GetContactRecordByExternal(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, externalID string) (*models.CRMContactRecord, error) {
	rec, err := scanContactRecord(r.db.QueryRow(ctx, `SELECT `+contactRecordCols+` FROM crm_contact_records
		WHERE organization_id = $1 AND provider = $2 AND external_id = $3`, orgID, provider, externalID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return rec, err
}

func (r *crmProviderRepository) UpsertContactRecord(ctx context.Context, rec *models.CRMContactRecord) error {
	props := rec.Properties
	if props == nil {
		props = map[string]string{}
	}
	raw, _ := json.Marshal(props)
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// A provider record re-pointed at another contact (a merge in the
	// provider) moves rather than conflicts.
	if _, err := tx.Exec(ctx, `DELETE FROM crm_contact_records
		WHERE organization_id = $1 AND provider = $2 AND external_id = $3 AND contact_id <> $4`,
		rec.OrganizationID, rec.Provider, rec.ExternalID, rec.ContactID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO crm_contact_records (organization_id, contact_id, provider, external_id, owner_external_id,
			lifecycle_stage, lead_status, company_external_id, company_name, company_domain, opted_out, properties,
			external_updated_at, synced_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''),
			NULLIF($10, ''), $11, $12, $13, NOW())
		ON CONFLICT (contact_id, provider) DO UPDATE
		SET external_id = EXCLUDED.external_id, owner_external_id = EXCLUDED.owner_external_id,
		    lifecycle_stage = EXCLUDED.lifecycle_stage, lead_status = EXCLUDED.lead_status,
		    company_external_id = EXCLUDED.company_external_id, company_name = EXCLUDED.company_name,
		    company_domain = EXCLUDED.company_domain, opted_out = EXCLUDED.opted_out,
		    properties = EXCLUDED.properties, external_updated_at = EXCLUDED.external_updated_at, synced_at = NOW()`,
		rec.OrganizationID, rec.ContactID, rec.Provider, rec.ExternalID, rec.OwnerExternalID, rec.LifecycleStage,
		rec.LeadStatus, rec.CompanyExternalID, rec.CompanyName, rec.CompanyDomain, rec.OptedOut, raw, rec.ExternalUpdatedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *crmProviderRepository) DeleteContactRecord(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, externalID string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM crm_contact_records WHERE organization_id = $1 AND provider = $2 AND external_id = $3`,
		orgID, provider, externalID)
	return err
}

func (r *crmProviderRepository) ContactIDsByEmail(ctx context.Context, orgID uuid.UUID, emails []string) (map[string]uuid.UUID, error) {
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
	rows, err := r.db.Query(ctx, `SELECT LOWER(email), id FROM contacts
		WHERE organization_id = $1 AND LOWER(email) = ANY($2)`, orgID, lower)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e string
		var id uuid.UUID
		if err := rows.Scan(&e, &id); err != nil {
			return nil, err
		}
		out[e] = id
	}
	return out, rows.Err()
}

// UpdateContactFields writes provider values onto a Warmbly contact: the
// standard columns named in fields and keys merged into custom_fields.
func (r *crmProviderRepository) UpdateContactFields(ctx context.Context, orgID, contactID uuid.UUID, fields map[string]string, custom map[string]string) error {
	cols := map[string]string{"first_name": "first_name", "last_name": "last_name", "company": "company", "phone": "phone"}
	sets := []string{}
	args := []any{orgID, contactID}
	for k, v := range fields {
		col, ok := cols[k]
		if !ok {
			continue
		}
		args = append(args, truncate(v, 255))
		sets = append(sets, col+" = $"+strconv.Itoa(len(args)))
	}
	if len(custom) > 0 {
		raw, _ := json.Marshal(custom)
		args = append(args, raw)
		sets = append(sets, "custom_fields = COALESCE(custom_fields, '{}'::jsonb) || $"+strconv.Itoa(len(args))+"::jsonb")
	}
	if len(sets) == 0 {
		return nil
	}
	// Only a real change touches the row, so a pull every minute does not
	// rewrite every active contact.
	changed := make([]string, 0, len(sets))
	for _, set := range sets {
		col, val, _ := strings.Cut(set, " = ")
		if col == "custom_fields" {
			changed = append(changed, "NOT (COALESCE(custom_fields, '{}'::jsonb) @> "+strings.TrimPrefix(val, "COALESCE(custom_fields, '{}'::jsonb) || ")+")")
			continue
		}
		changed = append(changed, col+" IS DISTINCT FROM "+val)
	}
	_, err := r.db.Exec(ctx, `UPDATE contacts SET `+strings.Join(sets, ", ")+`, updated_at = NOW()
		WHERE organization_id = $1 AND id = $2 AND (`+strings.Join(changed, " OR ")+`)`, args...)
	return err
}

// MailboxUser is the member who owns a mailbox in the workspace.
func (r *crmProviderRepository) MailboxUser(ctx context.Context, orgID, emailAccountID uuid.UUID) (*uuid.UUID, error) {
	var id *uuid.UUID
	err := r.db.QueryRow(ctx, `SELECT user_id FROM email_accounts WHERE organization_id = $1 AND id = $2`,
		orgID, emailAccountID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return id, err
}

func (r *crmProviderRepository) CampaignName(ctx context.Context, orgID, campaignID uuid.UUID) (string, error) {
	var name string
	err := r.db.QueryRow(ctx, `SELECT name FROM campaigns WHERE organization_id = $1 AND id = $2`, orgID, campaignID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return name, err
}

// CampaignOption is a campaign a contact can be added to from HubSpot.
type CampaignOption struct {
	ID     uuid.UUID
	Name   string
	Status string
}

// CampaignOptions lists the workspace's campaigns that still send, newest first.
func (r *crmProviderRepository) CampaignOptions(ctx context.Context, orgID uuid.UUID, query string, limit int) ([]CampaignOption, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, name, status::text FROM campaigns
		WHERE organization_id = $1 AND status::text NOT IN ('completed', 'archived')
		  AND ($2 = '' OR name ILIKE '%' || $2 || '%')
		ORDER BY (status::text = 'active') DESC, created_at DESC
		LIMIT $3`, orgID, strings.TrimSpace(query), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CampaignOption
	for rows.Next() {
		var c CampaignOption
		if err := rows.Scan(&c.ID, &c.Name, &c.Status); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ResumeHeldEverywhere lifts every hold on a contact's leads in the workspace.
func (r *crmProviderRepository) ResumeHeldEverywhere(ctx context.Context, orgID, contactID uuid.UUID) (int64, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE campaign_leads cl
		SET paused_at = NULL, paused_until = NULL, pause_reason = NULL, pause_source = NULL
		FROM campaigns c
		WHERE cl.contact_id = $2 AND c.id = cl.campaign_id AND c.organization_id = $1 AND cl.paused_at IS NOT NULL`,
		orgID, contactID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ---------- owners ----------

// ReplaceOwners upserts the provider's owners, archives the ones it no longer
// returns, and matches every unpinned owner to the member with the same address.
func (r *crmProviderRepository) ReplaceOwners(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, owners []models.CRMOwner) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	keep := make([]string, 0, len(owners))
	for _, o := range owners {
		keep = append(keep, o.ExternalID)
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm_owners (organization_id, provider, external_id, email, first_name, last_name, archived, synced_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
			ON CONFLICT (organization_id, provider, external_id) DO UPDATE
			SET email = EXCLUDED.email, first_name = EXCLUDED.first_name, last_name = EXCLUDED.last_name,
			    archived = EXCLUDED.archived, synced_at = NOW()`,
			orgID, provider, o.ExternalID, strings.ToLower(strings.TrimSpace(o.Email)), o.FirstName, o.LastName, o.Archived); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE crm_owners SET archived = true
		WHERE organization_id = $1 AND provider = $2 AND NOT (external_id = ANY($3))`, orgID, provider, keep); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE crm_owners o SET user_id = m.user_id
		FROM (
			SELECT DISTINCT ON (LOWER(u.email)) LOWER(u.email) AS email, u.id AS user_id
			FROM organization_members om JOIN users u ON u.id = om.user_id
			WHERE om.organization_id = $1
		) m
		WHERE o.organization_id = $1 AND o.provider = $2 AND NOT o.user_pinned
		  AND o.email = m.email AND o.user_id IS DISTINCT FROM m.user_id`, orgID, provider); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *crmProviderRepository) ListOwners(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider) ([]models.CRMOwner, error) {
	rows, err := r.db.Query(ctx, `
		SELECT external_id, email, first_name, last_name, user_id, user_pinned, archived
		FROM crm_owners WHERE organization_id = $1 AND provider = $2
		ORDER BY archived, LOWER(first_name || ' ' || last_name), email`, orgID, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.CRMOwner{}
	for rows.Next() {
		var o models.CRMOwner
		if err := rows.Scan(&o.ExternalID, &o.Email, &o.FirstName, &o.LastName, &o.UserID, &o.UserPinned, &o.Archived); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (r *crmProviderRepository) SetOwnerUser(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, externalID string, userID *uuid.UUID) error {
	if userID != nil {
		var ok bool
		if err := r.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM organization_members WHERE organization_id = $1 AND user_id = $2)`,
			orgID, *userID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return ErrCRMRecordNotFound
		}
	}
	tag, err := r.db.Exec(ctx, `UPDATE crm_owners SET user_id = $4, user_pinned = true
		WHERE organization_id = $1 AND provider = $2 AND external_id = $3`, orgID, provider, externalID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCRMRecordNotFound
	}
	return nil
}

func (r *crmProviderRepository) OwnerForUser(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, userID uuid.UUID) (string, error) {
	var ext string
	err := r.db.QueryRow(ctx, `SELECT external_id FROM crm_owners
		WHERE organization_id = $1 AND provider = $2 AND user_id = $3 AND NOT archived
		ORDER BY user_pinned DESC LIMIT 1`, orgID, provider, userID).Scan(&ext)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return ext, err
}

func (r *crmProviderRepository) UserForOwner(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, externalID string) (*uuid.UUID, error) {
	if externalID == "" {
		return nil, nil
	}
	var id *uuid.UUID
	err := r.db.QueryRow(ctx, `SELECT user_id FROM crm_owners
		WHERE organization_id = $1 AND provider = $2 AND external_id = $3`, orgID, provider, externalID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return id, err
}

// MemberForOwnerEmail is the accepted, sign-in-allowed member matched to the
// active provider owner with this address, with their permissions.
func (r *crmProviderRepository) MemberForOwnerEmail(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, email string) (*uuid.UUID, models.OrganizationPermission, error) {
	var id uuid.UUID
	var perms models.OrganizationPermission
	var role string
	err := r.db.QueryRow(ctx, `
		SELECT m.user_id, m.permissions, m.role FROM crm_owners o
		JOIN organization_members m ON m.organization_id = o.organization_id AND m.user_id = o.user_id
		JOIN users u ON u.id = m.user_id
		WHERE o.organization_id = $1 AND o.provider = $2 AND NOT o.archived
		  AND LOWER(o.email) = LOWER($3) AND m.accepted_at IS NOT NULL AND (u.ban_scope & 1) = 0
		ORDER BY o.user_pinned DESC, o.external_id LIMIT 1`, orgID, provider, email).Scan(&id, &perms, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	// The workspace owner holds every permission, whatever its stored mask says.
	if role == "owner" {
		perms = models.AllPermissions
	}
	return &id, perms, nil
}

// MemberForOwner is the active member matched to a provider user, with that
// member's permissions; nil when the user is not matched.
func (r *crmProviderRepository) MemberForOwner(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, externalID string) (*uuid.UUID, models.OrganizationPermission, error) {
	var id uuid.UUID
	var perms models.OrganizationPermission
	var role string
	err := r.db.QueryRow(ctx, `
		SELECT m.user_id, m.permissions, m.role FROM crm_owners o
		JOIN organization_members m ON m.organization_id = o.organization_id AND m.user_id = o.user_id
		JOIN users u ON u.id = m.user_id
		WHERE o.organization_id = $1 AND o.provider = $2 AND o.external_id = $3 AND NOT o.archived
		  AND m.accepted_at IS NOT NULL AND (u.ban_scope & 1) = 0`, orgID, provider, externalID).Scan(&id, &perms, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	// The workspace owner holds every permission, whatever its stored mask says.
	if role == "owner" {
		perms = models.AllPermissions
	}
	return &id, perms, nil
}

// FallbackActor is who a mirrored note or task is recorded as created by when
// its provider owner is not a member: the workspace owner.
func (r *crmProviderRepository) FallbackActor(ctx context.Context, orgID uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.QueryRow(ctx, `
		SELECT user_id FROM organization_members WHERE organization_id = $1
		ORDER BY (role = 'owner') DESC, invited_at LIMIT 1`, orgID).Scan(&id)
	return id, err
}

// ---------- jobs ----------

func (r *crmProviderRepository) EnqueueJob(ctx context.Context, job *models.CRMSyncJob) error {
	payload := job.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var dedupe *string
	if job.DedupeKey != "" {
		dedupe = &job.DedupeKey
	}
	_, err = r.db.Exec(ctx, `
		INSERT INTO crm_sync_jobs (organization_id, provider, kind, dedupe_key, subject, payload, next_attempt_at)
		VALUES ($1, $2, $3, $4, $5, $6, COALESCE($7, NOW()))
		ON CONFLICT (organization_id, provider, dedupe_key) WHERE dedupe_key IS NOT NULL AND status IN ('pending', 'running')
		DO NOTHING`,
		job.OrganizationID, job.Provider, job.Kind, dedupe, truncate(job.Subject, 300), raw, nullTime(job.NextAttemptAt))
	return err
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

const jobCols = `id, organization_id, provider, kind, COALESCE(dedupe_key, ''), subject, payload, status, attempts,
	next_attempt_at, COALESCE(last_error, ''), created_at, updated_at, finished_at, lease_token`

func scanJob(row pgx.Row) (*models.CRMSyncJob, error) {
	var j models.CRMSyncJob
	var raw []byte
	var lease *uuid.UUID
	if err := row.Scan(&j.ID, &j.OrganizationID, &j.Provider, &j.Kind, &j.DedupeKey, &j.Subject, &raw, &j.Status,
		&j.Attempts, &j.NextAttemptAt, &j.LastError, &j.CreatedAt, &j.UpdatedAt, &j.FinishedAt, &lease); err != nil {
		return nil, err
	}
	if lease != nil {
		j.LeaseToken = *lease
	}
	j.Payload = map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &j.Payload)
	}
	return &j, nil
}

// ClaimJobs leases due jobs. A lease that lapses (a crashed drainer) makes the
// job claimable again, so nothing is stranded in running. Each arm is its own
// select so the due and the running-lease partial indexes serve it.
func (r *crmProviderRepository) ClaimJobs(ctx context.Context, provider models.CRMProvider, limit int, lease time.Duration) ([]models.CRMSyncJob, error) {
	rows, err := r.db.Query(ctx, `
		WITH lapsed AS (
			SELECT id, next_attempt_at FROM crm_sync_jobs
			WHERE provider = $3 AND status = 'running' AND locked_until < NOW()
			ORDER BY locked_until
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		), due AS (
			SELECT id, next_attempt_at FROM crm_sync_jobs
			WHERE provider = $3 AND status = 'pending' AND next_attempt_at <= NOW()
			ORDER BY next_attempt_at
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		), picked AS (
			SELECT id AS pick_id, next_attempt_at AS pick_at FROM lapsed
			UNION ALL
			SELECT id, next_attempt_at FROM due
			ORDER BY pick_at
			LIMIT $1
		)
		UPDATE crm_sync_jobs j
		SET status = 'running', attempts = j.attempts + 1, locked_until = NOW() + make_interval(secs => $2),
		    lease_token = gen_random_uuid(), updated_at = NOW()
		FROM picked
		WHERE j.id = picked.pick_id
		RETURNING `+jobCols, limit, lease.Seconds(), provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.CRMSyncJob
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

func (r *crmProviderRepository) CompleteJob(ctx context.Context, job *models.CRMSyncJob) error {
	_, err := r.db.Exec(ctx, `UPDATE crm_sync_jobs SET status = 'done', last_error = NULL, locked_until = NULL,
		lease_token = NULL, finished_at = NOW(), updated_at = NOW() WHERE id = $1 AND lease_token = $2`, job.ID, job.LeaseToken)
	return err
}

func (r *crmProviderRepository) FailJob(ctx context.Context, job *models.CRMSyncJob, msg string, retryAt *time.Time) error {
	if retryAt != nil {
		_, err := r.db.Exec(ctx, `UPDATE crm_sync_jobs SET status = 'pending', last_error = $3, next_attempt_at = $4,
			locked_until = NULL, lease_token = NULL, updated_at = NOW() WHERE id = $1 AND lease_token = $2`,
			job.ID, job.LeaseToken, truncate(msg, 1000), *retryAt)
		return err
	}
	_, err := r.db.Exec(ctx, `UPDATE crm_sync_jobs SET status = 'failed', last_error = $3, locked_until = NULL,
		lease_token = NULL, finished_at = NOW(), updated_at = NOW() WHERE id = $1 AND lease_token = $2`,
		job.ID, job.LeaseToken, truncate(msg, 1000))
	return err
}

// ContinueJob hands a long job (a backfill) its next step: same row, same
// dedupe key, new payload, due again now.
func (r *crmProviderRepository) ContinueJob(ctx context.Context, job *models.CRMSyncJob, payload map[string]any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `UPDATE crm_sync_jobs SET status = 'pending', payload = $3, attempts = 0, last_error = NULL,
		next_attempt_at = NOW(), locked_until = NULL, lease_token = NULL, updated_at = NOW()
		WHERE id = $1 AND lease_token = $2`, job.ID, job.LeaseToken, raw)
	return err
}

// RetryFailedJobs requeues failed jobs (all when ids is empty). A failure whose
// work is already queued again under the same key stays failed.
func (r *crmProviderRepository) RetryFailedJobs(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, ids []uuid.UUID) (int64, error) {
	if ids == nil {
		ids = []uuid.UUID{}
	}
	tag, err := r.db.Exec(ctx, `UPDATE crm_sync_jobs j SET status = 'pending', attempts = 0, next_attempt_at = NOW(),
		finished_at = NULL, updated_at = NOW()
		WHERE j.organization_id = $1 AND j.provider = $3 AND j.status = 'failed' AND (cardinality($2::uuid[]) = 0 OR j.id = ANY($2))
		  AND (j.dedupe_key IS NULL OR NOT EXISTS (
			SELECT 1 FROM crm_sync_jobs o WHERE o.organization_id = j.organization_id AND o.provider = j.provider
			  AND o.dedupe_key = j.dedupe_key AND o.status IN ('pending', 'running')))`, orgID, ids, provider)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *crmProviderRepository) DiscardFailedJobs(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, ids []uuid.UUID) (int64, error) {
	if ids == nil {
		ids = []uuid.UUID{}
	}
	tag, err := r.db.Exec(ctx, `DELETE FROM crm_sync_jobs
		WHERE organization_id = $1 AND provider = $3 AND status = 'failed' AND (cardinality($2::uuid[]) = 0 OR id = ANY($2))`,
		orgID, ids, provider)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// PurgeJobs keeps a week of finished jobs and a month of failures.
func (r *crmProviderRepository) PurgeJobs(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `DELETE FROM crm_sync_jobs
		WHERE (status = 'done' AND finished_at < NOW() - interval '7 days')
		   OR (status = 'failed' AND finished_at < NOW() - interval '30 days')`)
	return err
}

func (r *crmProviderRepository) SyncHealth(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider) (*models.CRMSyncHealth, error) {
	h := &models.CRMSyncHealth{Cursors: []models.CRMSyncCursor{}, Failures: []models.CRMSyncJob{}}
	if err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE status IN ('pending', 'running')),
		       COUNT(*) FILTER (WHERE status = 'failed'),
		       COUNT(*) FILTER (WHERE status = 'done' AND finished_at > NOW() - interval '24 hours'),
		       MAX(finished_at) FILTER (WHERE status = 'done')
		FROM crm_sync_jobs WHERE organization_id = $1 AND provider = $2`, orgID, provider).
		Scan(&h.Pending, &h.Failed, &h.Done24h, &h.LastSyncedAt); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `SELECT `+jobCols+` FROM crm_sync_jobs
		WHERE organization_id = $1 AND provider = $2 AND status = 'failed'
		ORDER BY updated_at DESC LIMIT 50`, orgID, provider)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		h.Failures = append(h.Failures, *j)
	}
	rows.Close()
	crows, err := r.db.Query(ctx, `SELECT object_type, cursor_at, last_run_at, COALESCE(last_error, '')
		FROM crm_sync_cursors WHERE organization_id = $1 AND provider = $2 ORDER BY object_type`, orgID, provider)
	if err != nil {
		return nil, err
	}
	for crows.Next() {
		var c models.CRMSyncCursor
		if err := crows.Scan(&c.ObjectType, &c.CursorAt, &c.LastRunAt, &c.LastError); err != nil {
			crows.Close()
			return nil, err
		}
		if h.LastSyncedAt == nil || (c.LastRunAt != nil && c.LastRunAt.After(*h.LastSyncedAt) && c.LastError == "") {
			h.LastSyncedAt = c.LastRunAt
		}
		h.Cursors = append(h.Cursors, c)
	}
	crows.Close()
	if err := r.db.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM crm_contact_records WHERE organization_id = $1 AND provider = $2),
		       (SELECT COUNT(*) FROM crm_external_links WHERE organization_id = $1 AND provider = $2 AND object_type = 'deal'),
		       (SELECT COUNT(*) FROM crm_external_links WHERE organization_id = $1 AND provider = $2 AND object_type = 'task'),
		       (SELECT COUNT(*) FROM crm_external_links WHERE organization_id = $1 AND provider = $2 AND object_type = 'pipeline'),
		       (SELECT COUNT(*) FROM crm_owners WHERE organization_id = $1 AND provider = $2 AND NOT archived)`,
		orgID, provider).Scan(&h.Counts.Contacts, &h.Counts.Deals, &h.Counts.Tasks, &h.Counts.Pipelines, &h.Counts.Owners); err != nil {
		return nil, err
	}
	return h, nil
}

// ---------- cursors ----------

func (r *crmProviderRepository) GetCursor(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, objectType string) (*time.Time, error) {
	var at *time.Time
	err := r.db.QueryRow(ctx, `SELECT cursor_at FROM crm_sync_cursors
		WHERE organization_id = $1 AND provider = $2 AND object_type = $3`, orgID, provider, objectType).Scan(&at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return at, err
}

// SetCursor records a pull. A nil at keeps the previous checkpoint, which is
// what a failed run wants.
func (r *crmProviderRepository) SetCursor(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, objectType string, at *time.Time, runErr string) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO crm_sync_cursors (organization_id, provider, object_type, cursor_at, last_run_at, last_error)
		VALUES ($1, $2, $3, $4, NOW(), NULLIF($5, ''))
		ON CONFLICT (organization_id, provider, object_type) DO UPDATE
		SET cursor_at = COALESCE(EXCLUDED.cursor_at, crm_sync_cursors.cursor_at), last_run_at = NOW(),
		    last_error = EXCLUDED.last_error`, orgID, provider, objectType, at, truncate(runErr, 1000))
	return err
}

// ---------- mirror ----------

func (r *crmProviderRepository) MirrorPipeline(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, externalID, name string, position int) (uuid.UUID, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockExternal(ctx, tx, orgID, models.CRMObjectPipeline, externalID); err != nil {
		return uuid.Nil, err
	}
	var localID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT local_id FROM crm_external_links
		WHERE organization_id = $1 AND provider = $2 AND object_type = 'pipeline' AND external_id = $3`,
		orgID, provider, externalID).Scan(&localID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if err := tx.QueryRow(ctx, `INSERT INTO pipelines (organization_id, name, position) VALUES ($1, $2, $3) RETURNING id`,
			orgID, truncate(name, 255), position).Scan(&localID); err != nil {
			return uuid.Nil, err
		}
	case err != nil:
		return uuid.Nil, err
	default:
		tag, err := tx.Exec(ctx, `UPDATE pipelines SET name = $3, position = $4, updated_at = NOW()
			WHERE organization_id = $1 AND id = $2`, orgID, localID, truncate(name, 255), position)
		if err != nil {
			return uuid.Nil, err
		}
		if tag.RowsAffected() == 0 {
			if err := tx.QueryRow(ctx, `INSERT INTO pipelines (organization_id, name, position) VALUES ($1, $2, $3) RETURNING id`,
				orgID, truncate(name, 255), position).Scan(&localID); err != nil {
				return uuid.Nil, err
			}
		}
	}
	if err := upsertLink(ctx, tx, &models.CRMExternalLink{OrganizationID: orgID, Provider: provider,
		ObjectType: models.CRMObjectPipeline, LocalID: localID, ExternalID: externalID}); err != nil {
		return uuid.Nil, err
	}
	return localID, tx.Commit(ctx)
}

func (r *crmProviderRepository) MirrorStage(ctx context.Context, orgID, pipelineID uuid.UUID, provider models.CRMProvider, externalID, name, color string, position int, meta map[string]any) (uuid.UUID, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockExternal(ctx, tx, orgID, models.CRMObjectStage, externalID); err != nil {
		return uuid.Nil, err
	}
	var localID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT local_id FROM crm_external_links
		WHERE organization_id = $1 AND provider = $2 AND object_type = 'stage' AND external_id = $3`,
		orgID, provider, externalID).Scan(&localID)
	insert := func() error {
		return tx.QueryRow(ctx, `INSERT INTO pipeline_stages (pipeline_id, name, color, position) VALUES ($1, $2, $3, $4) RETURNING id`,
			pipelineID, truncate(name, 255), color, position).Scan(&localID)
	}
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if err := insert(); err != nil {
			return uuid.Nil, err
		}
	case err != nil:
		return uuid.Nil, err
	default:
		tag, err := tx.Exec(ctx, `UPDATE pipeline_stages SET pipeline_id = $2, name = $3, position = $4, updated_at = NOW()
			WHERE id = $1`, localID, pipelineID, truncate(name, 255), position)
		if err != nil {
			return uuid.Nil, err
		}
		if tag.RowsAffected() == 0 {
			if err := insert(); err != nil {
				return uuid.Nil, err
			}
		}
	}
	if err := upsertLink(ctx, tx, &models.CRMExternalLink{OrganizationID: orgID, Provider: provider,
		ObjectType: models.CRMObjectStage, LocalID: localID, ExternalID: externalID, Meta: meta}); err != nil {
		return uuid.Nil, err
	}
	return localID, tx.Commit(ctx)
}

// PruneStages removes mirrored stages the provider no longer has. A stage that
// still holds deals stays until the next deal pull moves them.
func (r *crmProviderRepository) PruneStages(ctx context.Context, orgID, pipelineID uuid.UUID, provider models.CRMProvider, keepExternal []string) error {
	_, err := r.db.Exec(ctx, `
		WITH gone AS (
			SELECT l.local_id FROM crm_external_links l
			JOIN pipeline_stages s ON s.id = l.local_id
			WHERE l.organization_id = $1 AND l.provider = $3 AND l.object_type = 'stage'
			  AND s.pipeline_id = $2 AND NOT (l.external_id = ANY($4))
			  AND NOT EXISTS (SELECT 1 FROM deals d WHERE d.stage_id = s.id)
		), del AS (
			DELETE FROM pipeline_stages WHERE id IN (SELECT local_id FROM gone)
		)
		DELETE FROM crm_external_links WHERE organization_id = $1 AND object_type = 'stage'
		  AND local_id IN (SELECT local_id FROM gone)`, orgID, pipelineID, provider, keepExternal)
	return err
}

func (r *crmProviderRepository) PrunePipelines(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, keepExternal []string) error {
	_, err := r.db.Exec(ctx, `
		WITH gone AS (
			SELECT local_id FROM crm_external_links
			WHERE organization_id = $1 AND provider = $2 AND object_type = 'pipeline' AND NOT (external_id = ANY($3))
		), del AS (
			DELETE FROM pipelines WHERE organization_id = $1 AND id IN (SELECT local_id FROM gone)
		)
		DELETE FROM crm_external_links WHERE organization_id = $1 AND object_type = 'pipeline'
		  AND local_id IN (SELECT local_id FROM gone)`, orgID, provider, keepExternal)
	return err
}

// MirrorDeal upserts a provider deal by its external id and reports whether it
// was new to Warmbly.
func (r *crmProviderRepository) MirrorDeal(ctx context.Context, in *MirrorDeal) (uuid.UUID, bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return uuid.Nil, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockExternal(ctx, tx, in.OrganizationID, models.CRMObjectDeal, in.ExternalID); err != nil {
		return uuid.Nil, false, err
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if len(currency) != 3 {
		currency = "USD"
	}
	var wonAt, lostAt *time.Time
	switch in.Status {
	case models.DealStatusWon:
		wonAt = coalesceTime(in.ClosedAt)
	case models.DealStatusLost:
		lostAt = coalesceTime(in.ClosedAt)
	}
	var localID uuid.UUID
	created := false
	err = tx.QueryRow(ctx, `SELECT local_id FROM crm_external_links
		WHERE organization_id = $1 AND provider = $2 AND object_type = 'deal' AND external_id = $3`,
		in.OrganizationID, in.Provider, in.ExternalID).Scan(&localID)
	insert := func() error {
		created = true
		return tx.QueryRow(ctx, `
			INSERT INTO deals (organization_id, pipeline_id, stage_id, contact_id, name, value, currency, status,
				expected_close_date, won_at, lost_at, assigned_to, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, COALESCE($13, NOW()))
			RETURNING id`,
			in.OrganizationID, in.PipelineID, in.StageID, in.ContactID, truncate(in.Name, 255), in.Value, currency,
			in.Status, in.CloseDate, wonAt, lostAt, in.AssignedTo, in.CreatedAt).Scan(&localID)
	}
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if err := insert(); err != nil {
			return uuid.Nil, false, err
		}
	case err != nil:
		return uuid.Nil, false, err
	default:
		tag, err := tx.Exec(ctx, `
			UPDATE deals SET pipeline_id = $3, stage_id = $4, contact_id = COALESCE($5, contact_id), name = $6,
				value = $7, currency = $8, status = $9::text::deal_status, expected_close_date = $10,
				won_at = CASE WHEN $9::text = 'won' THEN COALESCE(won_at, $11) END,
				lost_at = CASE WHEN $9::text = 'lost' THEN COALESCE(lost_at, $12) END,
				assigned_to = $13, updated_at = NOW()
			WHERE organization_id = $1 AND id = $2`,
			in.OrganizationID, localID, in.PipelineID, in.StageID, in.ContactID, truncate(in.Name, 255), in.Value,
			currency, in.Status, in.CloseDate, wonAt, lostAt, in.AssignedTo)
		if err != nil {
			return uuid.Nil, false, err
		}
		if tag.RowsAffected() == 0 {
			if err := insert(); err != nil {
				return uuid.Nil, false, err
			}
		}
	}
	if err := upsertLink(ctx, tx, &models.CRMExternalLink{OrganizationID: in.OrganizationID, Provider: in.Provider,
		ObjectType: models.CRMObjectDeal, LocalID: localID, ExternalID: in.ExternalID, Meta: in.Meta}); err != nil {
		return uuid.Nil, false, err
	}
	return localID, created, tx.Commit(ctx)
}

// lockExternal serializes mirror writes of one provider record across
// processes, so two pulls meeting the same new record cannot both insert it.
func lockExternal(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, objectType, externalID string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		orgID.String()+":"+objectType+":"+externalID)
	return err
}

func coalesceTime(t *time.Time) *time.Time {
	if t != nil {
		return t
	}
	now := time.Now()
	return &now
}

func (r *crmProviderRepository) MirrorTask(ctx context.Context, in *MirrorTask) (uuid.UUID, bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return uuid.Nil, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockExternal(ctx, tx, in.OrganizationID, models.CRMObjectTask, in.ExternalID); err != nil {
		return uuid.Nil, false, err
	}
	var localID uuid.UUID
	created := false
	err = tx.QueryRow(ctx, `SELECT local_id FROM crm_external_links
		WHERE organization_id = $1 AND provider = $2 AND object_type = 'task' AND external_id = $3`,
		in.OrganizationID, in.Provider, in.ExternalID).Scan(&localID)
	insert := func() error {
		created = true
		return tx.QueryRow(ctx, `
			INSERT INTO crm_tasks (organization_id, contact_id, deal_id, assigned_to, created_by, title, description,
				due_date, priority, type, status, completed_at, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, COALESCE($13, NOW()))
			RETURNING id`,
			in.OrganizationID, in.ContactID, in.DealID, in.AssignedTo, in.CreatedBy, truncate(in.Title, 255), in.Description,
			in.DueDate, in.Priority, in.Type, in.Status, in.CompletedAt, in.CreatedAt).Scan(&localID)
	}
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if err := insert(); err != nil {
			return uuid.Nil, false, err
		}
	case err != nil:
		return uuid.Nil, false, err
	default:
		tag, err := tx.Exec(ctx, `
			UPDATE crm_tasks SET contact_id = COALESCE($3, contact_id), deal_id = COALESCE($4, deal_id),
				assigned_to = $5, title = $6, description = $7, due_date = $8, priority = $9, type = $10,
				status = $11, completed_at = $12, updated_at = NOW()
			WHERE organization_id = $1 AND id = $2`,
			in.OrganizationID, localID, in.ContactID, in.DealID, in.AssignedTo, truncate(in.Title, 255), in.Description,
			in.DueDate, in.Priority, in.Type, in.Status, in.CompletedAt)
		if err != nil {
			return uuid.Nil, false, err
		}
		if tag.RowsAffected() == 0 {
			if err := insert(); err != nil {
				return uuid.Nil, false, err
			}
		}
	}
	if err := upsertLink(ctx, tx, &models.CRMExternalLink{OrganizationID: in.OrganizationID, Provider: in.Provider,
		ObjectType: models.CRMObjectTask, LocalID: localID, ExternalID: in.ExternalID, Meta: in.Meta}); err != nil {
		return uuid.Nil, false, err
	}
	return localID, created, tx.Commit(ctx)
}

func (r *crmProviderRepository) MirrorNote(ctx context.Context, in *MirrorNote) (uuid.UUID, bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return uuid.Nil, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockExternal(ctx, tx, in.OrganizationID, models.CRMObjectNote, in.ExternalID); err != nil {
		return uuid.Nil, false, err
	}
	var localID uuid.UUID
	created := false
	err = tx.QueryRow(ctx, `SELECT local_id FROM crm_external_links
		WHERE organization_id = $1 AND provider = $2 AND object_type = 'note' AND external_id = $3`,
		in.OrganizationID, in.Provider, in.ExternalID).Scan(&localID)
	insert := func() error {
		created = true
		return tx.QueryRow(ctx, `
			INSERT INTO contact_notes (contact_id, organization_id, user_id, content, created_at, updated_at)
			VALUES ($1, $2, $3, $4, COALESCE($5, NOW()), NOW()) RETURNING id`,
			in.ContactID, in.OrganizationID, in.UserID, in.Content, in.CreatedAt).Scan(&localID)
	}
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if err := insert(); err != nil {
			return uuid.Nil, false, err
		}
	case err != nil:
		return uuid.Nil, false, err
	default:
		tag, err := tx.Exec(ctx, `UPDATE contact_notes SET content = $3, updated_at = NOW()
			WHERE organization_id = $1 AND id = $2 AND content <> $3`, in.OrganizationID, localID, in.Content)
		if err != nil {
			return uuid.Nil, false, err
		}
		if tag.RowsAffected() == 0 {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM contact_notes WHERE id = $1)`, localID).Scan(&exists); err != nil {
				return uuid.Nil, false, err
			}
			if !exists {
				if err := insert(); err != nil {
					return uuid.Nil, false, err
				}
			}
		}
	}
	if err := upsertLink(ctx, tx, &models.CRMExternalLink{OrganizationID: in.OrganizationID, Provider: in.Provider,
		ObjectType: models.CRMObjectNote, LocalID: localID, ExternalID: in.ExternalID, Meta: in.Meta}); err != nil {
		return uuid.Nil, false, err
	}
	return localID, created, tx.Commit(ctx)
}

var mirrorTables = map[string]string{
	models.CRMObjectDeal: "deals",
	models.CRMObjectTask: "crm_tasks",
	models.CRMObjectNote: "contact_notes",
}

// DeleteMirrored removes a local row whose provider record was deleted.
func (r *crmProviderRepository) DeleteMirrored(ctx context.Context, orgID uuid.UUID, provider models.CRMProvider, objectType, externalID string) error {
	table, ok := mirrorTables[objectType]
	if !ok {
		return nil
	}
	_, err := r.db.Exec(ctx, `
		WITH l AS (
			DELETE FROM crm_external_links
			WHERE organization_id = $1 AND provider = $2 AND object_type = $3 AND external_id = $4
			RETURNING local_id
		)
		DELETE FROM `+table+` WHERE organization_id = $1 AND id IN (SELECT local_id FROM l)`,
		orgID, provider, objectType, externalID)
	return err
}

func (r *crmProviderRepository) OpenDealContactIDs(ctx context.Context, orgID uuid.UUID, contactIDs []uuid.UUID) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	if len(contactIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `SELECT DISTINCT contact_id FROM deals
		WHERE organization_id = $1 AND status = 'open' AND contact_id = ANY($2)`, orgID, contactIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// MoveDeal re-homes a deal onto another pipeline and stage of the workspace.
func (r *crmProviderRepository) MoveDeal(ctx context.Context, orgID, dealID, pipelineID, stageID uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE deals SET pipeline_id = $3, stage_id = $4, updated_at = NOW()
		WHERE organization_id = $1 AND id = $2
		  AND EXISTS (SELECT 1 FROM pipelines p JOIN pipeline_stages s ON s.pipeline_id = p.id
		              WHERE p.organization_id = $1 AND p.id = $3 AND s.id = $4)`,
		orgID, dealID, pipelineID, stageID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCRMRecordNotFound
	}
	return nil
}

// UnlinkedNative lists Warmbly-only rows of one type past a cursor, for the
// one-time copy into a provider.
func (r *crmProviderRepository) UnlinkedNative(ctx context.Context, orgID uuid.UUID, objectType string, after uuid.UUID, limit int) ([]uuid.UUID, error) {
	table, ok := mirrorTables[objectType]
	if !ok {
		return nil, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT t.id FROM `+table+` t
		WHERE t.organization_id = $1 AND t.id > $4
		  AND NOT EXISTS (SELECT 1 FROM crm_external_links l
		                  WHERE l.organization_id = $1 AND l.object_type = $2 AND l.local_id = t.id)
		ORDER BY t.id LIMIT $3`, orgID, objectType, limit, after)
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

func (r *crmProviderRepository) CountUnlinkedNative(ctx context.Context, orgID uuid.UUID) (*models.CRMBackfillPreview, error) {
	var p models.CRMBackfillPreview
	err := r.db.QueryRow(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM deals t WHERE t.organization_id = $1 AND NOT EXISTS
		     (SELECT 1 FROM crm_external_links l WHERE l.organization_id = $1 AND l.object_type = 'deal' AND l.local_id = t.id)),
		  (SELECT COUNT(*) FROM crm_tasks t WHERE t.organization_id = $1 AND NOT EXISTS
		     (SELECT 1 FROM crm_external_links l WHERE l.organization_id = $1 AND l.object_type = 'task' AND l.local_id = t.id)),
		  (SELECT COUNT(*) FROM contact_notes t WHERE t.organization_id = $1 AND NOT EXISTS
		     (SELECT 1 FROM crm_external_links l WHERE l.organization_id = $1 AND l.object_type = 'note' AND l.local_id = t.id))`,
		orgID).Scan(&p.Deals, &p.Tasks, &p.Notes)
	return &p, err
}
