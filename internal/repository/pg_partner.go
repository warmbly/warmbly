package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

var (
	// ErrPartnerOrgExists is a second create for an external id another request just created.
	ErrPartnerOrgExists = errors.New("partner organization already exists")
	// ErrPartnerIdentityExists is the same race for a person.
	ErrPartnerIdentityExists = errors.New("partner identity already exists")
	// ErrPartnerSlugTaken refuses a second partner under one slug.
	ErrPartnerSlugTaken = errors.New("partner slug taken")
)

// PartnerUpdate changes what is set; nil leaves a field alone.
type PartnerUpdate struct {
	Name, DisplayName          *string
	Status                     *models.PartnerStatus
	AllowedPlans, AllowedHosts []string
	SetAllowedPlans            bool
	SetAllowedHosts            bool
	DelegationDomains          []string
	SetDelegationDomains       bool
	WebhookURL                 *string // "" clears
	CallbackBaseURL            *string // "" clears
	WholesaleSendingWarmupDay  *int64
	WholesaleWarmupOnlyDay     *int64
	RateLimitPerSecond         *int
}

// ManagedOrgCreate is everything a new managed workspace starts with.
type ManagedOrgCreate struct {
	OrganizationID uuid.UUID
	Name, Slug     string
	Timezone       string
	ServiceUserID  uuid.UUID
	PartnerName    string
	Link           models.PartnerOrganization
}

// PartnerOrgCursor pages a partner's workspaces newest first.
type PartnerOrgCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// PartnerWarmupSummary counts a workspace's warming mailboxes by health.
type PartnerWarmupSummary struct {
	Enabled, Healthy, Watch, Problem int
}

// PartnerRepository stores partners, their keys, the workspaces they manage, their people and their events.
type PartnerRepository interface {
	CreatePartner(ctx context.Context, p *models.Partner, serviceEmail string, createdBy *uuid.UUID) (*models.Partner, error)
	ListPartners(ctx context.Context) ([]models.Partner, error)
	GetPartner(ctx context.Context, id uuid.UUID) (*models.Partner, error)
	GetPartnerBySlug(ctx context.Context, slug string) (*models.Partner, error)
	UpdatePartner(ctx context.Context, id uuid.UUID, u PartnerUpdate) (*models.Partner, error)
	GetPartnerSecrets(ctx context.Context, id uuid.UUID) (*models.PartnerSecrets, error)
	// RotatePartnerWebhookSecret keeps the current secret as the previous one.
	RotatePartnerWebhookSecret(ctx context.Context, id uuid.UUID, sealed string) error
	SetPartnerCallbackKey(ctx context.Context, id uuid.UUID, sealed string) error

	CreatePartnerKey(ctx context.Context, k *models.PartnerAPIKey, hash string, createdBy *uuid.UUID) error
	ListPartnerKeys(ctx context.Context, partnerID uuid.UUID) ([]models.PartnerAPIKey, error)
	RevokePartnerKey(ctx context.Context, partnerID, keyID uuid.UUID) (bool, error)
	// AuthenticatePartnerKey resolves a live key by its hash; nil when there is none.
	AuthenticatePartnerKey(ctx context.Context, hash string) (*models.Partner, *models.PartnerAPIKey, error)
	TouchPartnerKey(ctx context.Context, keyID uuid.UUID) error

	GetPartnerOrg(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, externalID string) (*models.PartnerOrganization, error)
	// GetPartnerOrgByOrganization is the partner link of a workspace and the partner's display name, or nil.
	GetPartnerOrgByOrganization(ctx context.Context, orgID uuid.UUID) (*models.PartnerOrganization, string, error)
	// HasPartnerOrganizations reports whether any workspace on the instance is linked to a partner.
	HasPartnerOrganizations(ctx context.Context) (bool, error)
	ListPartnerOrgs(ctx context.Context, partnerID uuid.UUID, mode *models.PartnerMode, after *PartnerOrgCursor, limit int) ([]models.PartnerOrganization, error)
	CreateManagedOrganization(ctx context.Context, in ManagedOrgCreate) (*models.PartnerOrganization, error)
	SavePartnerOrg(ctx context.Context, po *models.PartnerOrganization, orgName string) error
	// TakeDeletedPartnerOrgs removes and returns links whose workspace is gone.
	TakeDeletedPartnerOrgs(ctx context.Context, limit int) ([]models.PartnerOrganization, error)
	PartnerWarmupSummary(ctx context.Context, orgID uuid.UUID) (PartnerWarmupSummary, error)

	// GetPartnerIdentity resolves a person only within the key mode that created them.
	GetPartnerIdentity(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, externalID string) (*models.PartnerIdentity, error)
	CreatePartnerUser(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, externalID, email, name, firstName, lastName, accountEmail string) (*models.PartnerIdentity, error)
	RefreshPartnerIdentity(ctx context.Context, id *models.PartnerIdentity, firstName, lastName string) error
	IsPartnerBoundUser(ctx context.Context, userID uuid.UUID) (bool, error)

	// ClaimIdempotency claims a key, taking over one expired or abandoned (same request, processing, older than abandonAfter)
	// in the same statement. It returns the claim's token, or the stored row when the key is held or answered.
	ClaimIdempotency(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, key, requestHash string, abandonAfter time.Duration) (claim uuid.UUID, prev *PartnerIdempotencyRecord, err error)
	// CompleteIdempotency and ReleaseIdempotency act only while the claim is still the one token names.
	CompleteIdempotency(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, key string, claim uuid.UUID, status int, sealed string) error
	ReleaseIdempotency(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, key string, claim uuid.UUID) error

	InsertPartnerEvent(ctx context.Context, e *models.PartnerEvent) error
	ListPartnerEvents(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, afterID string, limit int) ([]models.PartnerEvent, bool, error)
	// ClaimDueWebhookEvents leases undelivered events of partners with a webhook, oldest first.
	ClaimDueWebhookEvents(ctx context.Context, limit int, lease time.Duration) ([]models.PartnerEvent, error)
	MarkPartnerEventDelivered(ctx context.Context, seq int64) error
	MarkPartnerEventFailed(ctx context.Context, seq int64, next time.Time, lastErr string, abandon bool) error
	// PrunePartnerRecords drops idempotency keys and events past their retention.
	PrunePartnerRecords(ctx context.Context, keysBefore, eventsBefore time.Time) error
}

// PartnerIdempotencyRecord is a stored answer.
type PartnerIdempotencyRecord struct {
	RequestHash string
	Completed   bool
	StatusCode  int
	Sealed      string
	CreatedAt   time.Time
}

type partnerRepository struct{ db *pgxpool.Pool }

func NewPartnerRepository(db *pgxpool.Pool) PartnerRepository { return &partnerRepository{db: db} }

const partnerColumns = `p.id, p.slug, p.name, p.display_name, p.status, p.service_user_id, p.allowed_plans, p.allowed_hosts, p.delegation_domains,
	p.webhook_url, COALESCE(p.webhook_secret, '') <> '', p.webhook_secret_rotated_at, p.callback_base_url, COALESCE(p.callback_key, '') <> '',
	p.wholesale_sending_warmup_day_micros, p.wholesale_warmup_only_day_micros, p.rate_limit_per_second, p.created_at, p.updated_at`

func scanPartner(row pgx.Row) (*models.Partner, error) {
	var p models.Partner
	err := row.Scan(&p.ID, &p.Slug, &p.Name, &p.DisplayName, &p.Status, &p.ServiceUserID, &p.AllowedPlans, &p.AllowedHosts, &p.DelegationDomains,
		&p.WebhookURL, &p.WebhookSecretSet, &p.WebhookSecretRotatedAt, &p.CallbackBaseURL, &p.CallbackKeySet,
		&p.WholesaleSendingWarmupDay, &p.WholesaleWarmupOnlyDay, &p.RateLimitPerSecond, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *partnerRepository) CreatePartner(ctx context.Context, p *models.Partner, serviceEmail string, createdBy *uuid.UUID) (*models.Partner, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	serviceID := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, first_name, last_name, created_at, updated_at, onboarding_completed_at)
		VALUES ($1, $2, NULL, $3, '', now(), now(), now())`, serviceID, serviceEmail, p.DisplayName); err != nil {
		return nil, err
	}
	id := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO partners (id, slug, name, display_name, service_user_id, allowed_hosts, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, id, p.Slug, p.Name, p.DisplayName, serviceID, nonNilStrings(p.AllowedHosts), createdBy); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrPartnerSlugTaken
		}
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetPartner(ctx, id)
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (r *partnerRepository) ListPartners(ctx context.Context) ([]models.Partner, error) {
	rows, err := r.db.Query(ctx, `SELECT `+partnerColumns+` FROM partners p ORDER BY p.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Partner{}
	for rows.Next() {
		p, err := scanPartner(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (r *partnerRepository) GetPartner(ctx context.Context, id uuid.UUID) (*models.Partner, error) {
	return scanPartner(r.db.QueryRow(ctx, `SELECT `+partnerColumns+` FROM partners p WHERE p.id = $1`, id))
}

func (r *partnerRepository) GetPartnerBySlug(ctx context.Context, slug string) (*models.Partner, error) {
	return scanPartner(r.db.QueryRow(ctx, `SELECT `+partnerColumns+` FROM partners p WHERE p.slug = $1`, slug))
}

func (r *partnerRepository) UpdatePartner(ctx context.Context, id uuid.UUID, u PartnerUpdate) (*models.Partner, error) {
	var status *string
	if u.Status != nil {
		v := string(*u.Status)
		status = &v
	}
	_, err := r.db.Exec(ctx, `
		UPDATE partners SET
			name = COALESCE($2, name),
			display_name = COALESCE($3, display_name),
			status = COALESCE($4, status),
			allowed_plans = CASE WHEN $5 THEN $6::text[] ELSE allowed_plans END,
			allowed_hosts = CASE WHEN $7 THEN $8::text[] ELSE allowed_hosts END,
			webhook_url = CASE WHEN $9::text IS NULL THEN webhook_url ELSE NULLIF($9, '') END,
			callback_base_url = CASE WHEN $10::text IS NULL THEN callback_base_url ELSE NULLIF($10, '') END,
			wholesale_sending_warmup_day_micros = COALESCE($11, wholesale_sending_warmup_day_micros),
			wholesale_warmup_only_day_micros = COALESCE($12, wholesale_warmup_only_day_micros),
			rate_limit_per_second = COALESCE($13, rate_limit_per_second),
			delegation_domains = CASE WHEN $14 THEN $15::text[] ELSE delegation_domains END,
			updated_at = now()
		WHERE id = $1`,
		id, u.Name, u.DisplayName, status, u.SetAllowedPlans, nonNilStrings(u.AllowedPlans), u.SetAllowedHosts, nonNilStrings(u.AllowedHosts),
		u.WebhookURL, u.CallbackBaseURL, u.WholesaleSendingWarmupDay, u.WholesaleWarmupOnlyDay, u.RateLimitPerSecond,
		u.SetDelegationDomains, nonNilStrings(u.DelegationDomains))
	if err != nil {
		return nil, err
	}
	if u.DisplayName != nil {
		// The service account is named after the partner in member lists and API key lists.
		if _, err := r.db.Exec(ctx, `UPDATE users u SET first_name = $2, updated_at = now() FROM partners p WHERE p.id = $1 AND u.id = p.service_user_id`, id, *u.DisplayName); err != nil {
			return nil, err
		}
	}
	return r.GetPartner(ctx, id)
}

func (r *partnerRepository) GetPartnerSecrets(ctx context.Context, id uuid.UUID) (*models.PartnerSecrets, error) {
	var s models.PartnerSecrets
	err := r.db.QueryRow(ctx, `
		SELECT COALESCE(webhook_secret, ''), COALESCE(webhook_secret_previous, ''), COALESCE(callback_key, ''), webhook_secret_rotated_at
		FROM partners WHERE id = $1`, id).Scan(&s.WebhookSecret, &s.WebhookSecretPrevious, &s.CallbackKey, &s.WebhookSecretRotatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &s, err
}

func (r *partnerRepository) RotatePartnerWebhookSecret(ctx context.Context, id uuid.UUID, sealed string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE partners SET webhook_secret_previous = webhook_secret, webhook_secret = $2,
			webhook_secret_rotated_at = CASE WHEN webhook_secret IS NULL THEN NULL ELSE now() END, updated_at = now()
		WHERE id = $1`, id, sealed)
	return err
}

func (r *partnerRepository) SetPartnerCallbackKey(ctx context.Context, id uuid.UUID, sealed string) error {
	_, err := r.db.Exec(ctx, `UPDATE partners SET callback_key = NULLIF($2, ''), updated_at = now() WHERE id = $1`, id, sealed)
	return err
}

const partnerKeyColumns = `k.id, k.partner_id, k.mode, k.name, k.key_prefix, k.key_suffix, k.last_used_at, k.revoked_at, k.created_at`

func scanPartnerKey(row pgx.Row, k *models.PartnerAPIKey) error {
	return row.Scan(&k.ID, &k.PartnerID, &k.Mode, &k.Name, &k.KeyPrefix, &k.KeySuffix, &k.LastUsedAt, &k.RevokedAt, &k.CreatedAt)
}

func (r *partnerRepository) CreatePartnerKey(ctx context.Context, k *models.PartnerAPIKey, hash string, createdBy *uuid.UUID) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO partner_api_keys (id, partner_id, mode, name, key_prefix, key_suffix, key_hash, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING created_at`,
		k.ID, k.PartnerID, k.Mode, k.Name, k.KeyPrefix, k.KeySuffix, hash, createdBy).Scan(&k.CreatedAt)
}

func (r *partnerRepository) ListPartnerKeys(ctx context.Context, partnerID uuid.UUID) ([]models.PartnerAPIKey, error) {
	rows, err := r.db.Query(ctx, `SELECT `+partnerKeyColumns+` FROM partner_api_keys k WHERE k.partner_id = $1 ORDER BY k.created_at DESC`, partnerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.PartnerAPIKey{}
	for rows.Next() {
		var k models.PartnerAPIKey
		if err := scanPartnerKey(rows, &k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (r *partnerRepository) RevokePartnerKey(ctx context.Context, partnerID, keyID uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `UPDATE partner_api_keys SET revoked_at = now() WHERE id = $1 AND partner_id = $2 AND revoked_at IS NULL`, keyID, partnerID)
	return tag.RowsAffected() == 1, err
}

func (r *partnerRepository) AuthenticatePartnerKey(ctx context.Context, hash string) (*models.Partner, *models.PartnerAPIKey, error) {
	var k models.PartnerAPIKey
	err := scanPartnerKey(r.db.QueryRow(ctx, `SELECT `+partnerKeyColumns+` FROM partner_api_keys k WHERE k.key_hash = $1 AND k.revoked_at IS NULL`, hash), &k)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	p, err := r.GetPartner(ctx, k.PartnerID)
	if err != nil || p == nil {
		return nil, nil, err
	}
	return p, &k, nil
}

func (r *partnerRepository) TouchPartnerKey(ctx context.Context, keyID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE partner_api_keys SET last_used_at = now()
		WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute')`, keyID)
	return err
}

const partnerOrgColumns = `po.id, po.partner_id, po.mode, po.external_id, po.organization_id, po.plan_key, po.state, po.suspended_reason,
	po.campaign_sends_per_mailbox_per_day, po.group_external_id, po.group_name, po.group_branding, po.cancelled_at, po.deletes_at,
	po.created_at, po.updated_at, COALESCE(o.name, '')`

const partnerOrgFrom = ` FROM partner_organizations po LEFT JOIN organizations o ON o.id = po.organization_id`

func scanPartnerOrg(row pgx.Row, extra ...any) (*models.PartnerOrganization, error) {
	var po models.PartnerOrganization
	var branding []byte
	err := row.Scan(append([]any{&po.ID, &po.PartnerID, &po.Mode, &po.ExternalID, &po.OrganizationID, &po.PlanKey, &po.State, &po.SuspendedReason,
		&po.CampaignSendsPerMailboxPerDay, &po.GroupExternalID, &po.GroupName, &branding, &po.CancelledAt, &po.DeletesAt,
		&po.CreatedAt, &po.UpdatedAt, &po.OrganizationName}, extra...)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(branding) > 0 {
		po.GroupBranding = json.RawMessage(branding)
	}
	return &po, nil
}

func (r *partnerRepository) GetPartnerOrg(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, externalID string) (*models.PartnerOrganization, error) {
	return scanPartnerOrg(r.db.QueryRow(ctx, `SELECT `+partnerOrgColumns+partnerOrgFrom+`
		WHERE po.partner_id = $1 AND po.mode = $2 AND po.external_id = $3`, partnerID, mode, externalID))
}

func (r *partnerRepository) GetPartnerOrgByOrganization(ctx context.Context, orgID uuid.UUID) (*models.PartnerOrganization, string, error) {
	var display string
	po, err := scanPartnerOrg(r.db.QueryRow(ctx, `SELECT `+partnerOrgColumns+`, p.display_name`+partnerOrgFrom+`
		JOIN partners p ON p.id = po.partner_id
		WHERE po.organization_id = $1`, orgID), &display)
	return po, display, err
}

func (r *partnerRepository) HasPartnerOrganizations(ctx context.Context) (bool, error) {
	var ok bool
	err := r.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM partner_organizations WHERE organization_id IS NOT NULL)`).Scan(&ok)
	return ok, err
}

func (r *partnerRepository) ListPartnerOrgs(ctx context.Context, partnerID uuid.UUID, mode *models.PartnerMode, after *PartnerOrgCursor, limit int) ([]models.PartnerOrganization, error) {
	var at *time.Time
	var id *uuid.UUID
	if after != nil {
		at, id = &after.CreatedAt, &after.ID
	}
	var modeText *string
	if mode != nil {
		m := string(*mode)
		modeText = &m
	}
	rows, err := r.db.Query(ctx, `SELECT `+partnerOrgColumns+partnerOrgFrom+`
		WHERE po.partner_id = $1 AND ($2::text IS NULL OR po.mode = $2)
		  AND po.organization_id IS NOT NULL
		  AND ($3::timestamptz IS NULL OR (po.created_at, po.id) < ($3::timestamptz, $4::uuid))
		ORDER BY po.created_at DESC, po.id DESC LIMIT $5`, partnerID, modeText, at, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.PartnerOrganization{}
	for rows.Next() {
		po, err := scanPartnerOrg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *po)
	}
	return out, rows.Err()
}

func (r *partnerRepository) CreateManagedOrganization(ctx context.Context, in ManagedOrgCreate) (*models.PartnerOrganization, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	category := models.OrganizationCategoryStandard
	if in.Link.Mode == models.PartnerModeTest {
		category = models.OrganizationCategoryTest
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO organizations (id, name, slug, owner_user_id, created_at, updated_at, timezone, category)
		VALUES ($1, $2, $3, $4, now(), now(), $5, $6)`,
		in.OrganizationID, in.Name, in.Slug, in.ServiceUserID, in.Timezone, category); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO organization_members (id, organization_id, user_id, role, permissions, invited_at, accepted_at, access_scope)
		VALUES ($1, $2, $3, $4, $5, now(), now(), $6)`,
		uuid.New(), in.OrganizationID, in.ServiceUserID, string(models.RoleOwner), models.RolePermissions[models.RoleOwner], models.AccessScopeWorkspace); err != nil {
		return nil, err
	}
	for _, seed := range models.DefaultSeedRoles() {
		if _, err := tx.Exec(ctx, `
			INSERT INTO organization_roles (id, organization_id, name, description, color, permissions)
			VALUES ($1, $2, $3, $4, $5, $6)`, uuid.New(), in.OrganizationID, seed.Name, seed.Description, seed.Color, seed.Permissions); err != nil {
			return nil, err
		}
	}
	planID := models.PartnerPlanIDs[in.Link.PlanKey]
	if _, err := tx.Exec(ctx, `
		INSERT INTO subscriptions (user_id, organization_id, plan_id, stripe_customer_id,
			managed_at, managed_by, managed_reason, managed_plan_id)
		VALUES ($1, $2, $3, '', now(), $1, $4, $3)`,
		in.ServiceUserID, in.OrganizationID, planID, "Managed by partner "+in.PartnerName); err != nil {
		return nil, err
	}
	l := in.Link
	if _, err := tx.Exec(ctx, `
		INSERT INTO partner_organizations (id, partner_id, mode, external_id, organization_id, plan_key, state, suspended_reason,
			campaign_sends_per_mailbox_per_day, group_external_id, group_name, group_branding)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		l.ID, l.PartnerID, l.Mode, l.ExternalID, in.OrganizationID, l.PlanKey, l.State, l.SuspendedReason,
		l.CampaignSendsPerMailboxPerDay, l.GroupExternalID, l.GroupName, nullJSON(l.GroupBranding)); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrPartnerOrgExists
		}
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	if err := SeedDefaultTaskTypes(ctx, r.db, in.OrganizationID); err != nil {
		return nil, err
	}
	return r.GetPartnerOrg(ctx, l.PartnerID, l.Mode, l.ExternalID)
}

func nullJSON(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return []byte(raw)
}

func (r *partnerRepository) SavePartnerOrg(ctx context.Context, po *models.PartnerOrganization, orgName string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := tx.QueryRow(ctx, `
		UPDATE partner_organizations SET plan_key = $2, state = $3, suspended_reason = $4, campaign_sends_per_mailbox_per_day = $5,
			group_external_id = $6, group_name = $7, group_branding = $8, cancelled_at = $9, deletes_at = $10, updated_at = now()
		WHERE id = $1 RETURNING updated_at`,
		po.ID, po.PlanKey, po.State, po.SuspendedReason, po.CampaignSendsPerMailboxPerDay,
		po.GroupExternalID, po.GroupName, nullJSON(po.GroupBranding), po.CancelledAt, po.DeletesAt).Scan(&po.UpdatedAt); err != nil {
		return err
	}
	if po.OrganizationID != nil && orgName != "" {
		if _, err := tx.Exec(ctx, `UPDATE organizations SET name = $2, updated_at = now() WHERE id = $1 AND name <> $2`, *po.OrganizationID, orgName); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *partnerRepository) TakeDeletedPartnerOrgs(ctx context.Context, limit int) ([]models.PartnerOrganization, error) {
	rows, err := r.db.Query(ctx, `
		WITH gone AS (
			DELETE FROM partner_organizations WHERE id IN (
				SELECT id FROM partner_organizations WHERE organization_id IS NULL ORDER BY updated_at LIMIT $1 FOR UPDATE SKIP LOCKED)
			RETURNING *)
		SELECT po.id, po.partner_id, po.mode, po.external_id, po.organization_id, po.plan_key, po.state, po.suspended_reason,
			po.campaign_sends_per_mailbox_per_day, po.group_external_id, po.group_name, po.group_branding, po.cancelled_at, po.deletes_at,
			po.created_at, po.updated_at, ''
		FROM gone po`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.PartnerOrganization{}
	for rows.Next() {
		po, err := scanPartnerOrg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *po)
	}
	return out, rows.Err()
}

func (r *partnerRepository) PartnerWarmupSummary(ctx context.Context, orgID uuid.UUID) (PartnerWarmupSummary, error) {
	var s PartnerWarmupSummary
	err := r.db.QueryRow(ctx, `
		SELECT count(*),
			count(*) FILTER (WHERE wpp.health_state = 'healthy'),
			count(*) FILTER (WHERE wpp.health_state = 'watch'),
			count(*) FILTER (WHERE wpp.health_state IN ('throttled', 'quarantined', 'blocked'))
		FROM email_accounts ea
		LEFT JOIN LATERAL (
			SELECT health_state FROM warmup_pool_participants p WHERE p.email_account_id = ea.id LIMIT 1
		) wpp ON true
		WHERE ea.organization_id = $1 AND ea.warmup IS NOT NULL AND ea.warmup_paused_at IS NULL`, orgID).
		Scan(&s.Enabled, &s.Healthy, &s.Watch, &s.Problem)
	return s, err
}

const partnerIdentityColumns = `id, partner_id, mode, external_id, user_id, email, name`

func scanPartnerIdentity(row pgx.Row) (*models.PartnerIdentity, error) {
	var p models.PartnerIdentity
	err := row.Scan(&p.ID, &p.PartnerID, &p.Mode, &p.ExternalID, &p.UserID, &p.Email, &p.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *partnerRepository) GetPartnerIdentity(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, externalID string) (*models.PartnerIdentity, error) {
	return scanPartnerIdentity(r.db.QueryRow(ctx, `SELECT `+partnerIdentityColumns+` FROM partner_identities
		WHERE partner_id = $1 AND mode = $2 AND external_id = $3`, partnerID, mode, externalID))
}

func (r *partnerRepository) CreatePartnerUser(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, externalID, email, name, firstName, lastName, accountEmail string) (*models.PartnerIdentity, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	userID := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO users (id, email, contact_email, password_hash, first_name, last_name, created_at, updated_at, onboarding_completed_at)
		VALUES ($1, $2, NULLIF($3, ''), NULL, $4, $5, now(), now(), now())`, userID, accountEmail, email, firstName, lastName); err != nil {
		return nil, err
	}
	id := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO partner_identities (id, partner_id, mode, external_id, user_id, email, name)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, id, partnerID, mode, externalID, userID, email, name); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrPartnerIdentityExists
		}
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &models.PartnerIdentity{ID: id, PartnerID: partnerID, Mode: mode, ExternalID: externalID, UserID: userID, Email: email, Name: name}, nil
}

func (r *partnerRepository) RefreshPartnerIdentity(ctx context.Context, id *models.PartnerIdentity, firstName, lastName string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `
		UPDATE partner_identities SET email = $2, name = $3, last_login_at = now(), updated_at = now() WHERE id = $1`,
		id.ID, id.Email, id.Name); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE users SET first_name = $2, last_name = $3, contact_email = NULLIF($4, ''), updated_at = now()
		WHERE id = $1 AND (first_name <> $2 OR last_name <> $3 OR contact_email IS DISTINCT FROM NULLIF($4, ''))`,
		id.UserID, firstName, lastName, id.Email); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *partnerRepository) IsPartnerBoundUser(ctx context.Context, userID uuid.UUID) (bool, error) {
	var bound bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM partner_identities WHERE user_id = $1)
		    OR EXISTS (SELECT 1 FROM partners WHERE service_user_id = $1)`, userID).Scan(&bound)
	return bound, err
}

func (r *partnerRepository) ClaimIdempotency(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, key, requestHash string, abandonAfter time.Duration) (uuid.UUID, *PartnerIdempotencyRecord, error) {
	for attempt := 0; ; attempt++ {
		var claim uuid.UUID
		err := r.db.QueryRow(ctx, `
			INSERT INTO partner_idempotency_keys AS k (partner_id, mode, key, request_hash) VALUES ($1, $2, $3, $4)
			ON CONFLICT (partner_id, mode, key) DO UPDATE
				SET request_hash = EXCLUDED.request_hash, status = 'processing', status_code = NULL, response_sealed = NULL,
					claim_token = gen_random_uuid(), created_at = now()
				WHERE k.created_at < now() - interval '24 hours'
				   OR (k.status = 'processing' AND k.request_hash = EXCLUDED.request_hash
				       AND k.created_at < now() - make_interval(secs => $5))
			RETURNING claim_token`,
			partnerID, mode, key, requestHash, abandonAfter.Seconds()).Scan(&claim)
		if err == nil {
			return claim, nil, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, nil, err
		}
		var rec PartnerIdempotencyRecord
		var code *int
		var sealed *string
		var status string
		err = r.db.QueryRow(ctx, `
			SELECT request_hash, status, status_code, response_sealed, created_at FROM partner_idempotency_keys
			WHERE partner_id = $1 AND mode = $2 AND key = $3`, partnerID, mode, key).
			Scan(&rec.RequestHash, &status, &code, &sealed, &rec.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) && attempt == 0 {
			// Released between the claim and the read: claim again once.
			continue
		}
		if err != nil {
			return uuid.Nil, nil, err
		}
		rec.Completed = status == "completed"
		if code != nil {
			rec.StatusCode = *code
		}
		if sealed != nil {
			rec.Sealed = *sealed
		}
		return uuid.Nil, &rec, nil
	}
}

func (r *partnerRepository) CompleteIdempotency(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, key string, claim uuid.UUID, status int, sealed string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE partner_idempotency_keys SET status = 'completed', status_code = $5, response_sealed = $6
		WHERE partner_id = $1 AND mode = $2 AND key = $3 AND claim_token = $4 AND status = 'processing'`,
		partnerID, mode, key, claim, status, sealed)
	return err
}

func (r *partnerRepository) ReleaseIdempotency(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, key string, claim uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM partner_idempotency_keys
		WHERE partner_id = $1 AND mode = $2 AND key = $3 AND claim_token = $4 AND status = 'processing'`, partnerID, mode, key, claim)
	return err
}

func (r *partnerRepository) InsertPartnerEvent(ctx context.Context, e *models.PartnerEvent) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO partner_events (id, partner_id, mode, type, organization_external_id, data)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING seq, created_at`,
		e.ID, e.PartnerID, e.Mode, e.Type, e.OrganizationExternalID, []byte(e.Data)).Scan(&e.Seq, &e.CreatedAt)
}

const partnerEventColumns = `seq, id, partner_id, mode, type, organization_external_id, data, created_at, attempts`

func scanPartnerEvents(rows pgx.Rows) ([]models.PartnerEvent, error) {
	defer rows.Close()
	out := []models.PartnerEvent{}
	for rows.Next() {
		var e models.PartnerEvent
		var data []byte
		if err := rows.Scan(&e.Seq, &e.ID, &e.PartnerID, &e.Mode, &e.Type, &e.OrganizationExternalID, &data, &e.CreatedAt, &e.Attempts); err != nil {
			return nil, err
		}
		e.Data = json.RawMessage(data)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ErrPartnerEventCursor is an `after` that names no event of this partner and mode.
var ErrPartnerEventCursor = errors.New("unknown event cursor")

func (r *partnerRepository) ListPartnerEvents(ctx context.Context, partnerID uuid.UUID, mode models.PartnerMode, afterID string, limit int) ([]models.PartnerEvent, bool, error) {
	var after int64
	if afterID != "" {
		err := r.db.QueryRow(ctx, `SELECT seq FROM partner_events WHERE id = $1 AND partner_id = $2 AND mode = $3`, afterID, partnerID, mode).Scan(&after)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, ErrPartnerEventCursor
		}
		if err != nil {
			return nil, false, err
		}
	}
	rows, err := r.db.Query(ctx, `SELECT `+partnerEventColumns+` FROM partner_events
		WHERE partner_id = $1 AND mode = $2 AND seq > $3 ORDER BY seq LIMIT $4`, partnerID, mode, after, limit+1)
	if err != nil {
		return nil, false, err
	}
	out, err := scanPartnerEvents(rows)
	if err != nil {
		return nil, false, err
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	return out, more, nil
}

func (r *partnerRepository) ClaimDueWebhookEvents(ctx context.Context, limit int, lease time.Duration) ([]models.PartnerEvent, error) {
	rows, err := r.db.Query(ctx, `
		UPDATE partner_events e SET next_attempt_at = now() + make_interval(secs => $2)
		WHERE e.seq IN (
			SELECT ev.seq FROM partner_events ev JOIN partners p ON p.id = ev.partner_id
			WHERE ev.delivered_at IS NULL AND ev.abandoned_at IS NULL AND ev.next_attempt_at <= now()
			  AND p.webhook_url IS NOT NULL AND p.webhook_secret IS NOT NULL AND p.status = 'active'
			ORDER BY ev.seq LIMIT $1 FOR UPDATE OF ev SKIP LOCKED)
		RETURNING `+partnerEventColumns, limit, lease.Seconds())
	if err != nil {
		return nil, err
	}
	return scanPartnerEvents(rows)
}

func (r *partnerRepository) MarkPartnerEventDelivered(ctx context.Context, seq int64) error {
	_, err := r.db.Exec(ctx, `UPDATE partner_events SET delivered_at = now(), attempts = attempts + 1, last_error = NULL WHERE seq = $1`, seq)
	return err
}

func (r *partnerRepository) MarkPartnerEventFailed(ctx context.Context, seq int64, next time.Time, lastErr string, abandon bool) error {
	_, err := r.db.Exec(ctx, `
		UPDATE partner_events SET attempts = attempts + 1, next_attempt_at = $2, last_error = $3,
			abandoned_at = CASE WHEN $4 THEN now() ELSE NULL END
		WHERE seq = $1`, seq, next, lastErr, abandon)
	return err
}

func (r *partnerRepository) PrunePartnerRecords(ctx context.Context, keysBefore, eventsBefore time.Time) error {
	if _, err := r.db.Exec(ctx, `DELETE FROM partner_idempotency_keys WHERE created_at < $1`, keysBefore); err != nil {
		return err
	}
	_, err := r.db.Exec(ctx, `DELETE FROM partner_events WHERE created_at < $1`, eventsBefore)
	return err
}
