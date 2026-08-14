package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// ConfengePolicyRepository stores CAMPAIGN_POLICY_AUTHORIZATION grants.
type ConfengePolicyRepository interface {
	InsertCampaignPolicy(ctx context.Context, orgID uuid.UUID, auth *models.CampaignPolicyAuthorization) (uuid.UUID, error)
	GetActiveCampaignPolicy(ctx context.Context, orgID, campaignID uuid.UUID, now time.Time) (*models.CampaignPolicyAuthorization, error)
	GetCampaignPolicyByID(ctx context.Context, orgID, authID uuid.UUID) (*models.CampaignPolicyAuthorization, error)
	RevokeCampaignPolicy(ctx context.Context, orgID, campaignID, actor uuid.UUID, now time.Time) (bool, error)
	ListCampaignPolicies(ctx context.Context, orgID, campaignID uuid.UUID, limit int) ([]models.CampaignPolicyAuthorization, error)
}

type confengePolicyRepository struct {
	db *pgxpool.Pool
}

// NewConfengePolicyRepository returns a postgres-backed policy store.
func NewConfengePolicyRepository(db *pgxpool.Pool) ConfengePolicyRepository {
	return &confengePolicyRepository{db: db}
}

func (r *confengePolicyRepository) InsertCampaignPolicy(ctx context.Context, orgID uuid.UUID, auth *models.CampaignPolicyAuthorization) (uuid.UUID, error) {
	if auth == nil {
		return uuid.Nil, errors.New("nil policy")
	}
	id := auth.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	now := time.Now().UTC()
	ch := auth.Channel
	if ch == "" {
		ch = "EMAIL"
	}
	rc := auth.AllowedRiskClass
	if rc == "" {
		rc = "GREEN"
	}
	rate := auth.MaxRatePerHour
	if rate < 1 {
		rate = 20
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO confenge_campaign_policy_authorizations (
			id, organization_id, campaign_id,
			prompt_policy_version, validator_version, contact_policy_version, template_policy_version,
			sender_mailbox, channel, allowed_risk_class, max_rate_per_hour, allow_policy_template_green,
			effective_at, authorized_by, authorized_by_label, revoked_at, created_at, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18
		)`,
		id, orgID, auth.CampaignID,
		auth.PromptPolicyVersion, auth.ValidatorVersion, auth.ContactPolicyVersion, auth.TemplatePolicyVersion,
		auth.SenderMailbox, ch, rc, rate, auth.AllowPolicyTemplateGREEN,
		auth.EffectiveAt, auth.AuthorizedBy, auth.AuthorizedByLabel, auth.RevokedAt, now, now,
	)
	if err == nil {
		auth.ID = id
	}
	return id, err
}

func (r *confengePolicyRepository) GetActiveCampaignPolicy(ctx context.Context, orgID, campaignID uuid.UUID, now time.Time) (*models.CampaignPolicyAuthorization, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	row := r.db.QueryRow(ctx, `
		SELECT id, campaign_id, prompt_policy_version, validator_version, contact_policy_version, template_policy_version,
			sender_mailbox, channel, allowed_risk_class, max_rate_per_hour, allow_policy_template_green,
			effective_at, authorized_by, authorized_by_label, revoked_at
		FROM confenge_campaign_policy_authorizations
		WHERE organization_id=$1 AND campaign_id=$2
		  AND revoked_at IS NULL
		  AND effective_at <= $3
		ORDER BY effective_at DESC
		LIMIT 1`, orgID, campaignID, now)
	var a models.CampaignPolicyAuthorization
	err := row.Scan(
		&a.ID, &a.CampaignID, &a.PromptPolicyVersion, &a.ValidatorVersion, &a.ContactPolicyVersion, &a.TemplatePolicyVersion,
		&a.SenderMailbox, &a.Channel, &a.AllowedRiskClass, &a.MaxRatePerHour, &a.AllowPolicyTemplateGREEN,
		&a.EffectiveAt, &a.AuthorizedBy, &a.AuthorizedByLabel, &a.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *confengePolicyRepository) GetCampaignPolicyByID(ctx context.Context, orgID, authID uuid.UUID) (*models.CampaignPolicyAuthorization, error) {
	if authID == uuid.Nil {
		return nil, nil
	}
	row := r.db.QueryRow(ctx, `
		SELECT id, campaign_id, prompt_policy_version, validator_version, contact_policy_version, template_policy_version,
			sender_mailbox, channel, allowed_risk_class, max_rate_per_hour, allow_policy_template_green,
			effective_at, authorized_by, authorized_by_label, revoked_at
		FROM confenge_campaign_policy_authorizations
		WHERE organization_id=$1 AND id=$2`, orgID, authID)
	var a models.CampaignPolicyAuthorization
	err := row.Scan(
		&a.ID, &a.CampaignID, &a.PromptPolicyVersion, &a.ValidatorVersion, &a.ContactPolicyVersion, &a.TemplatePolicyVersion,
		&a.SenderMailbox, &a.Channel, &a.AllowedRiskClass, &a.MaxRatePerHour, &a.AllowPolicyTemplateGREEN,
		&a.EffectiveAt, &a.AuthorizedBy, &a.AuthorizedByLabel, &a.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *confengePolicyRepository) RevokeCampaignPolicy(ctx context.Context, orgID, campaignID, actor uuid.UUID, now time.Time) (bool, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	_ = actor
	ct, err := r.db.Exec(ctx, `
		UPDATE confenge_campaign_policy_authorizations
		SET revoked_at=$3, updated_at=$3
		WHERE organization_id=$1 AND campaign_id=$2 AND revoked_at IS NULL`,
		orgID, campaignID, now)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}

func (r *confengePolicyRepository) ListCampaignPolicies(ctx context.Context, orgID, campaignID uuid.UUID, limit int) ([]models.CampaignPolicyAuthorization, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := r.db.Query(ctx, `
		SELECT id, campaign_id, prompt_policy_version, validator_version, contact_policy_version, template_policy_version,
			sender_mailbox, channel, allowed_risk_class, max_rate_per_hour, allow_policy_template_green,
			effective_at, authorized_by, authorized_by_label, revoked_at
		FROM confenge_campaign_policy_authorizations
		WHERE organization_id=$1 AND campaign_id=$2
		ORDER BY created_at DESC
		LIMIT $3`, orgID, campaignID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.CampaignPolicyAuthorization, 0)
	for rows.Next() {
		var a models.CampaignPolicyAuthorization
		if err := rows.Scan(
			&a.ID, &a.CampaignID, &a.PromptPolicyVersion, &a.ValidatorVersion, &a.ContactPolicyVersion, &a.TemplatePolicyVersion,
			&a.SenderMailbox, &a.Channel, &a.AllowedRiskClass, &a.MaxRatePerHour, &a.AllowPolicyTemplateGREEN,
			&a.EffectiveAt, &a.AuthorizedBy, &a.AuthorizedByLabel, &a.RevokedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
