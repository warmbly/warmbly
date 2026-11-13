package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// PartnerReferralRepository stores the partner-program click a workspace signed up through.
type PartnerReferralRepository interface {
	// RecordPartnerReferral keeps the first click; a workspace has one origin.
	RecordPartnerReferral(ctx context.Context, orgID uuid.UUID, dubClickID string) error
	// GetPartnerReferral returns nil when the workspace did not come from a partner.
	GetPartnerReferral(ctx context.Context, orgID uuid.UUID) (*models.PartnerReferral, error)
	MarkPartnerLeadReported(ctx context.Context, orgID uuid.UUID) error
}

type partnerReferralRepository struct {
	db *pgxpool.Pool
}

func NewPartnerReferralRepository(db *pgxpool.Pool) PartnerReferralRepository {
	return &partnerReferralRepository{db: db}
}

func (r *partnerReferralRepository) RecordPartnerReferral(ctx context.Context, orgID uuid.UUID, dubClickID string) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO organization_partner_referrals (organization_id, dub_click_id)
		VALUES ($1, $2)
		ON CONFLICT (organization_id) DO NOTHING`, orgID, dubClickID)
	return err
}

func (r *partnerReferralRepository) GetPartnerReferral(ctx context.Context, orgID uuid.UUID) (*models.PartnerReferral, error) {
	ref := &models.PartnerReferral{OrganizationID: orgID}
	err := r.db.QueryRow(ctx, `
		SELECT dub_click_id, lead_reported_at
		FROM organization_partner_referrals WHERE organization_id = $1`, orgID).
		Scan(&ref.DubClickID, &ref.LeadReportedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ref, nil
}

func (r *partnerReferralRepository) MarkPartnerLeadReported(ctx context.Context, orgID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE organization_partner_referrals SET lead_reported_at = now()
		WHERE organization_id = $1 AND lead_reported_at IS NULL`, orgID)
	return err
}
