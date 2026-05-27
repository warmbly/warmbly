package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

type PlanRepository interface {
	Create(ctx context.Context, plan *models.Plan) error
	Update(ctx context.Context, plan *models.Plan) error
	GetByID(ctx context.Context, id uuid.UUID) (*models.Plan, error)
	GetByStripePriceID(ctx context.Context, priceID string) (*models.Plan, error)
	GetByStripeProductID(ctx context.Context, productID string) (*models.Plan, error)
	List(ctx context.Context, publicOnly bool) ([]*models.Plan, error)

	// Rate limits
	GetRateLimits(ctx context.Context, planID uuid.UUID) (*models.PlanRateLimits, error)
	SetRateLimits(ctx context.Context, limits *models.PlanRateLimits) error
}

type planRepository struct {
	db *pgxpool.Pool
}

func NewPlanRepository(db *pgxpool.Pool) PlanRepository {
	return &planRepository{db: db}
}

func (r *planRepository) Create(ctx context.Context, plan *models.Plan) error {
	query := `
		INSERT INTO plans (
			id, name, max_contacts, daily_emails, ai_generation, account_limit,
			price, discounted_price, duration_id, savings, public,
			stripe_price_id, stripe_product_id, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			(SELECT id FROM durations WHERE title = $9),
			$10, $11, $12, $13, $14, $15
		)
	`

	now := time.Now()
	_, err := r.db.Exec(ctx, query,
		plan.ID, plan.Name, plan.MaxContacts, plan.DailyEmails, plan.AIGeneration, plan.AccountLimit,
		plan.Price, plan.DiscountedPrice, string(plan.Duration), plan.Savings, plan.Public,
		plan.StripePriceID, plan.StripeProductID, now, now,
	)
	return err
}

func (r *planRepository) Update(ctx context.Context, plan *models.Plan) error {
	query := `
		UPDATE plans SET
			name = $2,
			max_contacts = $3,
			daily_emails = $4,
			ai_generation = $5,
			account_limit = $6,
			price = $7,
			discounted_price = $8,
			savings = $9,
			public = $10,
			stripe_price_id = $11,
			stripe_product_id = $12,
			updated_at = $13
		WHERE id = $1
	`

	_, err := r.db.Exec(ctx, query,
		plan.ID, plan.Name, plan.MaxContacts, plan.DailyEmails, plan.AIGeneration, plan.AccountLimit,
		plan.Price, plan.DiscountedPrice, plan.Savings, plan.Public,
		plan.StripePriceID, plan.StripeProductID, time.Now(),
	)
	return err
}

func (r *planRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.Plan, error) {
	return r.scanPlan(ctx, `
		SELECT p.id, p.name, p.max_contacts, p.daily_emails, p.ai_generation, p.account_limit,
			   p.price, p.discounted_price, d.title, p.savings, p.public,
			   p.stripe_price_id, p.stripe_product_id, p.dedicated_workers, p.daily_campaign_limit,
			   p.created_at, p.updated_at
		FROM plans p
		LEFT JOIN durations d ON d.id = p.duration_id
		WHERE p.id = $1
	`, id)
}

func (r *planRepository) GetByStripePriceID(ctx context.Context, priceID string) (*models.Plan, error) {
	return r.scanPlan(ctx, `
		SELECT p.id, p.name, p.max_contacts, p.daily_emails, p.ai_generation, p.account_limit,
			   p.price, p.discounted_price, d.title, p.savings, p.public,
			   p.stripe_price_id, p.stripe_product_id, p.dedicated_workers, p.daily_campaign_limit,
			   p.created_at, p.updated_at
		FROM plans p
		LEFT JOIN durations d ON d.id = p.duration_id
		WHERE p.stripe_price_id = $1
	`, priceID)
}

func (r *planRepository) GetByStripeProductID(ctx context.Context, productID string) (*models.Plan, error) {
	return r.scanPlan(ctx, `
		SELECT p.id, p.name, p.max_contacts, p.daily_emails, p.ai_generation, p.account_limit,
			   p.price, p.discounted_price, d.title, p.savings, p.public,
			   p.stripe_price_id, p.stripe_product_id, p.dedicated_workers, p.daily_campaign_limit,
			   p.created_at, p.updated_at
		FROM plans p
		LEFT JOIN durations d ON d.id = p.duration_id
		WHERE p.stripe_product_id = $1
	`, productID)
}

func (r *planRepository) scanPlan(ctx context.Context, query string, args ...interface{}) (*models.Plan, error) {
	row := r.db.QueryRow(ctx, query, args...)

	var plan models.Plan
	var duration *string
	err := row.Scan(
		&plan.ID, &plan.Name, &plan.MaxContacts, &plan.DailyEmails, &plan.AIGeneration, &plan.AccountLimit,
		&plan.Price, &plan.DiscountedPrice, &duration, &plan.Savings, &plan.Public,
		&plan.StripePriceID, &plan.StripeProductID, &plan.DedicatedWorkers, &plan.DailyCampaignLimit,
		&plan.CreatedAt, &plan.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if duration != nil {
		plan.Duration = models.Duration(*duration)
	}
	return &plan, nil
}

func (r *planRepository) List(ctx context.Context, publicOnly bool) ([]*models.Plan, error) {
	query := `
		SELECT p.id, p.name, p.max_contacts, p.daily_emails, p.ai_generation, p.account_limit,
			   p.price, p.discounted_price, d.title, p.savings, p.public,
			   p.stripe_price_id, p.stripe_product_id, p.dedicated_workers, p.daily_campaign_limit,
			   p.created_at, p.updated_at
		FROM plans p
		LEFT JOIN durations d ON d.id = p.duration_id
	`
	if publicOnly {
		query += " WHERE p.public = true"
	}
	query += " ORDER BY p.price ASC"

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var plans []*models.Plan
	for rows.Next() {
		var plan models.Plan
		var duration *string
		err := rows.Scan(
			&plan.ID, &plan.Name, &plan.MaxContacts, &plan.DailyEmails, &plan.AIGeneration, &plan.AccountLimit,
			&plan.Price, &plan.DiscountedPrice, &duration, &plan.Savings, &plan.Public,
			&plan.StripePriceID, &plan.StripeProductID, &plan.DedicatedWorkers, &plan.DailyCampaignLimit,
			&plan.CreatedAt, &plan.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		if duration != nil {
			plan.Duration = models.Duration(*duration)
		}
		plans = append(plans, &plan)
	}
	return plans, nil
}

func (r *planRepository) GetRateLimits(ctx context.Context, planID uuid.UUID) (*models.PlanRateLimits, error) {
	query := `SELECT * FROM plan_rate_limits WHERE plan_id = $1`
	row := r.db.QueryRow(ctx, query, planID)

	var limits models.PlanRateLimits
	err := row.Scan(
		&limits.PlanID,
		&limits.LimitReadPM, &limits.LimitWritePM, &limits.LimitBulkPM,
		&limits.LimitUniboxPM, &limits.LimitAnalyticsPM,
		&limits.LimitAPICallsDaily, &limits.LimitBulkOpsDaily,
		&limits.LimitWSMessagePM, &limits.LimitWSJoinPM, &limits.LimitWSEventPM, &limits.MaxConnections,
		&limits.CreatedAt, &limits.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &limits, nil
}

func (r *planRepository) SetRateLimits(ctx context.Context, limits *models.PlanRateLimits) error {
	query := `
		INSERT INTO plan_rate_limits (
			plan_id, limit_read_pm, limit_write_pm, limit_bulk_pm,
			limit_unibox_pm, limit_analytics_pm, limit_api_calls_daily, limit_bulk_ops_daily,
			limit_ws_message_pm, limit_ws_join_pm, limit_ws_event_pm, max_connections,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
		)
		ON CONFLICT (plan_id) DO UPDATE SET
			limit_read_pm = EXCLUDED.limit_read_pm,
			limit_write_pm = EXCLUDED.limit_write_pm,
			limit_bulk_pm = EXCLUDED.limit_bulk_pm,
			limit_unibox_pm = EXCLUDED.limit_unibox_pm,
			limit_analytics_pm = EXCLUDED.limit_analytics_pm,
			limit_api_calls_daily = EXCLUDED.limit_api_calls_daily,
			limit_bulk_ops_daily = EXCLUDED.limit_bulk_ops_daily,
			limit_ws_message_pm = EXCLUDED.limit_ws_message_pm,
			limit_ws_join_pm = EXCLUDED.limit_ws_join_pm,
			limit_ws_event_pm = EXCLUDED.limit_ws_event_pm,
			max_connections = EXCLUDED.max_connections,
			updated_at = EXCLUDED.updated_at
	`

	now := time.Now()
	_, err := r.db.Exec(ctx, query,
		limits.PlanID,
		limits.LimitReadPM, limits.LimitWritePM, limits.LimitBulkPM,
		limits.LimitUniboxPM, limits.LimitAnalyticsPM, limits.LimitAPICallsDaily, limits.LimitBulkOpsDaily,
		limits.LimitWSMessagePM, limits.LimitWSJoinPM, limits.LimitWSEventPM, limits.MaxConnections,
		now, now,
	)
	return err
}
