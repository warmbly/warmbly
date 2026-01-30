package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

type SubscriptionRepository interface {
	Create(ctx context.Context, sub *models.Subscription) error
	Update(ctx context.Context, sub *models.Subscription) error
	GetByID(ctx context.Context, id uuid.UUID) (*models.Subscription, error)
	GetByUserID(ctx context.Context, userID uuid.UUID) (*models.Subscription, error)
	GetByOrganizationID(ctx context.Context, orgID uuid.UUID) (*models.Subscription, error)
	GetByStripeCustomerID(ctx context.Context, customerID string) (*models.Subscription, error)
	GetByStripeSubscriptionID(ctx context.Context, subscriptionID string) (*models.Subscription, error)

	// With limits - for realtime
	GetWithLimits(ctx context.Context, orgID uuid.UUID) (*models.SubscriptionWithLimits, error)

	// Enterprise
	SetEnterprise(ctx context.Context, orgID uuid.UUID, isEnterprise bool) error

	// Webhook idempotency
	WebhookEventExists(ctx context.Context, eventID string) (bool, error)
	RecordWebhookEvent(ctx context.Context, event *models.StripeWebhookEvent) error
}

type subscriptionRepository struct {
	db *pgxpool.Pool
}

func NewSubscriptionRepository(db *pgxpool.Pool) SubscriptionRepository {
	return &subscriptionRepository{db: db}
}

func (r *subscriptionRepository) Create(ctx context.Context, sub *models.Subscription) error {
	query := `
		INSERT INTO subscriptions (
			id, user_id, organization_id, plan_id, stripe_customer_id, stripe_subscription_id,
			stripe_price_id, status, current_period_start, current_period_end,
			cancel_at_period_end, canceled_at, trial_start, trial_end,
			free_trial_started_at, free_trial_ends_at,
			is_enterprise, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19
		)
	`

	now := time.Now()
	_, err := r.db.Exec(ctx, query,
		sub.ID, sub.UserID, sub.OrganizationID, sub.PlanID, sub.StripeCustomerID, sub.StripeSubscriptionID,
		sub.StripePriceID, sub.Status, sub.CurrentPeriodStart, sub.CurrentPeriodEnd,
		sub.CancelAtPeriodEnd, sub.CanceledAt, sub.TrialStart, sub.TrialEnd,
		sub.FreeTrialStartedAt, sub.FreeTrialEndsAt,
		sub.IsEnterprise, now, now,
	)
	return err
}

func (r *subscriptionRepository) Update(ctx context.Context, sub *models.Subscription) error {
	query := `
		UPDATE subscriptions SET
			organization_id = $2,
			plan_id = $3,
			stripe_customer_id = $4,
			stripe_subscription_id = $5,
			stripe_price_id = $6,
			status = $7,
			current_period_start = $8,
			current_period_end = $9,
			cancel_at_period_end = $10,
			canceled_at = $11,
			trial_start = $12,
			trial_end = $13,
			free_trial_started_at = $14,
			free_trial_ends_at = $15,
			is_enterprise = $16,
			updated_at = $17
		WHERE id = $1
	`

	_, err := r.db.Exec(ctx, query,
		sub.ID, sub.OrganizationID, sub.PlanID, sub.StripeCustomerID, sub.StripeSubscriptionID,
		sub.StripePriceID, sub.Status, sub.CurrentPeriodStart, sub.CurrentPeriodEnd,
		sub.CancelAtPeriodEnd, sub.CanceledAt, sub.TrialStart, sub.TrialEnd,
		sub.FreeTrialStartedAt, sub.FreeTrialEndsAt,
		sub.IsEnterprise, time.Now(),
	)
	return err
}

func (r *subscriptionRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.Subscription, error) {
	return r.scanSubscription(ctx, `SELECT * FROM subscriptions WHERE id = $1`, id)
}

func (r *subscriptionRepository) GetByUserID(ctx context.Context, userID uuid.UUID) (*models.Subscription, error) {
	return r.scanSubscription(ctx, `SELECT * FROM subscriptions WHERE user_id = $1 LIMIT 1`, userID)
}

func (r *subscriptionRepository) GetByOrganizationID(ctx context.Context, orgID uuid.UUID) (*models.Subscription, error) {
	return r.scanSubscription(ctx, `SELECT * FROM subscriptions WHERE organization_id = $1`, orgID)
}

func (r *subscriptionRepository) GetByStripeCustomerID(ctx context.Context, customerID string) (*models.Subscription, error) {
	return r.scanSubscription(ctx, `SELECT * FROM subscriptions WHERE stripe_customer_id = $1`, customerID)
}

func (r *subscriptionRepository) GetByStripeSubscriptionID(ctx context.Context, subscriptionID string) (*models.Subscription, error) {
	return r.scanSubscription(ctx, `SELECT * FROM subscriptions WHERE stripe_subscription_id = $1`, subscriptionID)
}

func (r *subscriptionRepository) scanSubscription(ctx context.Context, query string, args ...interface{}) (*models.Subscription, error) {
	row := r.db.QueryRow(ctx, query, args...)

	var sub models.Subscription
	err := row.Scan(
		&sub.ID, &sub.UserID, &sub.OrganizationID, &sub.PlanID, &sub.StripeCustomerID, &sub.StripeSubscriptionID,
		&sub.StripePriceID, &sub.Status, &sub.CurrentPeriodStart, &sub.CurrentPeriodEnd,
		&sub.CancelAtPeriodEnd, &sub.CanceledAt, &sub.TrialStart, &sub.TrialEnd,
		&sub.FreeTrialStartedAt, &sub.FreeTrialEndsAt,
		&sub.IsEnterprise, &sub.CreatedAt, &sub.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

func (r *subscriptionRepository) GetWithLimits(ctx context.Context, orgID uuid.UUID) (*models.SubscriptionWithLimits, error) {
	query := `
		SELECT
			s.id, s.user_id, s.organization_id, s.plan_id, s.stripe_customer_id, s.stripe_subscription_id,
			s.stripe_price_id, s.status, s.current_period_start, s.current_period_end,
			s.cancel_at_period_end, s.canceled_at, s.trial_start, s.trial_end,
			s.free_trial_started_at, s.free_trial_ends_at,
			s.is_enterprise, s.created_at, s.updated_at,
			COALESCE(url.limit_ws_message_pm, prl.limit_ws_message_pm, 120) as limit_ws_message_pm,
			COALESCE(url.limit_ws_join_pm, prl.limit_ws_join_pm, 30) as limit_ws_join_pm,
			COALESCE(url.limit_ws_event_pm, prl.limit_ws_event_pm, 60) as limit_ws_event_pm,
			COALESCE(url.max_connections, prl.max_connections, 10) as max_connections
		FROM subscriptions s
		LEFT JOIN plan_rate_limits prl ON prl.plan_id = s.plan_id
		LEFT JOIN user_rate_limits url ON url.user_id = s.user_id AND s.is_enterprise = true
		WHERE s.organization_id = $1
	`

	row := r.db.QueryRow(ctx, query, orgID)

	var result models.SubscriptionWithLimits
	var limits models.RealtimeRateLimits

	err := row.Scan(
		&result.ID, &result.UserID, &result.OrganizationID, &result.PlanID, &result.StripeCustomerID, &result.StripeSubscriptionID,
		&result.StripePriceID, &result.Status, &result.CurrentPeriodStart, &result.CurrentPeriodEnd,
		&result.CancelAtPeriodEnd, &result.CanceledAt, &result.TrialStart, &result.TrialEnd,
		&result.FreeTrialStartedAt, &result.FreeTrialEndsAt,
		&result.IsEnterprise, &result.CreatedAt, &result.UpdatedAt,
		&limits.LimitWSMessagePM, &limits.LimitWSJoinPM, &limits.LimitWSEventPM, &limits.MaxConnections,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	result.RateLimits = &limits
	return &result, nil
}

func (r *subscriptionRepository) SetEnterprise(ctx context.Context, orgID uuid.UUID, isEnterprise bool) error {
	query := `UPDATE subscriptions SET is_enterprise = $2, updated_at = $3 WHERE organization_id = $1`
	_, err := r.db.Exec(ctx, query, orgID, isEnterprise, time.Now())
	return err
}

func (r *subscriptionRepository) WebhookEventExists(ctx context.Context, eventID string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM stripe_webhook_events WHERE id = $1)`, eventID).Scan(&exists)
	return exists, err
}

func (r *subscriptionRepository) RecordWebhookEvent(ctx context.Context, event *models.StripeWebhookEvent) error {
	query := `INSERT INTO stripe_webhook_events (id, event_type, processed_at) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`
	_, err := r.db.Exec(ctx, query, event.ID, event.EventType, event.ProcessedAt)
	return err
}
