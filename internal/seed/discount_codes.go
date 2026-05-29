package seed

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// seedDiscountCodes inserts a sample, all-plans percentage code so the discount
// flow can be exercised end-to-end in development. Idempotent via the stable ID.
func seedDiscountCodes(ctx context.Context, pool *pgxpool.Pool, _ *Result) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO discount_codes (
			id, code, description, type, percent_off, duration,
			applies_to_all_plans, per_account_limit, status, created_by
		) VALUES ($1, $2, $3, 'percent', 10, 'once', true, 1, 'active', $4)
		ON CONFLICT (id) DO UPDATE SET
			code = EXCLUDED.code,
			description = EXCLUDED.description,
			percent_off = EXCLUDED.percent_off,
			status = EXCLUDED.status,
			updated_at = NOW()
	`, DiscountWelcome10ID, "WELCOME10", "10% off your first plan (sample seed code)", UserAdminID)
	return err
}
