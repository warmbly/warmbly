package seed

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

func seedSubscriptions(ctx context.Context, pool *pgxpool.Pool, _ *Result) error {
	// Acme: paid Pro Monthly, active right now with a 30-day window.
	_, err := pool.Exec(ctx, `
		INSERT INTO subscriptions (
			id, user_id, organization_id, plan_id,
			stripe_customer_id, stripe_subscription_id, stripe_price_id,
			status, current_period_start, current_period_end, cancel_at_period_end,
			is_enterprise, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4,
			'cus_seed_acme', 'sub_seed_acme', 'price_pro_monthly_seed',
			'active', NOW() - INTERVAL '5 days', NOW() + INTERVAL '25 days', FALSE,
			FALSE, NOW(), NOW()
		)
		ON CONFLICT (id) DO UPDATE SET
			status = 'active',
			current_period_start = EXCLUDED.current_period_start,
			current_period_end = EXCLUDED.current_period_end,
			updated_at = NOW()
	`, SubAcmeID, UserOwnerID, OrgAcmeID, PlanProMonthlyID)
	if err != nil {
		return err
	}

	// Globex: trialing on Free Trial plan with 14 days left.
	_, err = pool.Exec(ctx, `
		INSERT INTO subscriptions (
			id, user_id, organization_id, plan_id,
			stripe_customer_id, status,
			free_trial_started_at, free_trial_ends_at,
			is_enterprise, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4,
			'', 'trialing',
			NOW(), NOW() + INTERVAL '14 days',
			FALSE, NOW(), NOW()
		)
		ON CONFLICT (id) DO UPDATE SET
			status = 'trialing',
			free_trial_started_at = EXCLUDED.free_trial_started_at,
			free_trial_ends_at = EXCLUDED.free_trial_ends_at,
			updated_at = NOW()
	`, SubGlobexID, UserFounderID, OrgGlobexID, PlanFreeTrialID)
	return err
}
