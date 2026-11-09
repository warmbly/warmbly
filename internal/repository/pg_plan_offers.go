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

// PlanOfferRepository stores the private plans an operator lets one workspace buy.
type PlanOfferRepository interface {
	ListPlanOffers(ctx context.Context, orgID uuid.UUID) ([]*models.PlanOffer, error)
	GetPlanOffer(ctx context.Context, orgID, planID uuid.UUID) (*models.PlanOffer, error)
	UpsertPlanOffer(ctx context.Context, offer *models.PlanOffer) (*models.PlanOffer, error)
	DeletePlanOffer(ctx context.Context, orgID, planID uuid.UUID) (bool, error)
}

type planOfferRepository struct {
	db *pgxpool.Pool
}

// NewPlanOfferRepository returns the Postgres plan offer store.
func NewPlanOfferRepository(db *pgxpool.Pool) PlanOfferRepository {
	return &planOfferRepository{db: db}
}

const planOfferColumns = `organization_id, plan_id, granted_by, reason, granted_at, expires_at`

func scanPlanOffer(row pgx.Row) (*models.PlanOffer, error) {
	var o models.PlanOffer
	if err := row.Scan(&o.OrganizationID, &o.PlanID, &o.GrantedBy, &o.Reason, &o.GrantedAt, &o.ExpiresAt); err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *planOfferRepository) ListPlanOffers(ctx context.Context, orgID uuid.UUID) ([]*models.PlanOffer, error) {
	rows, err := r.db.Query(ctx, `SELECT `+planOfferColumns+`
		FROM organization_plan_offers WHERE organization_id = $1 ORDER BY granted_at`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	offers := []*models.PlanOffer{}
	for rows.Next() {
		o, err := scanPlanOffer(rows)
		if err != nil {
			return nil, err
		}
		offers = append(offers, o)
	}
	return offers, rows.Err()
}

func (r *planOfferRepository) GetPlanOffer(ctx context.Context, orgID, planID uuid.UUID) (*models.PlanOffer, error) {
	o, err := scanPlanOffer(r.db.QueryRow(ctx, `SELECT `+planOfferColumns+`
		FROM organization_plan_offers WHERE organization_id = $1 AND plan_id = $2`, orgID, planID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return o, err
}

func (r *planOfferRepository) UpsertPlanOffer(ctx context.Context, offer *models.PlanOffer) (*models.PlanOffer, error) {
	return scanPlanOffer(r.db.QueryRow(ctx, `
		INSERT INTO organization_plan_offers (organization_id, plan_id, granted_by, reason, granted_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (organization_id, plan_id) DO UPDATE SET
			granted_by = EXCLUDED.granted_by,
			reason = EXCLUDED.reason,
			granted_at = EXCLUDED.granted_at,
			expires_at = EXCLUDED.expires_at
		RETURNING `+planOfferColumns,
		offer.OrganizationID, offer.PlanID, offer.GrantedBy, offer.Reason, grantTime(offer.GrantedAt), offer.ExpiresAt))
}

func (r *planOfferRepository) DeletePlanOffer(ctx context.Context, orgID, planID uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM organization_plan_offers WHERE organization_id = $1 AND plan_id = $2`, orgID, planID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func grantTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t
}
