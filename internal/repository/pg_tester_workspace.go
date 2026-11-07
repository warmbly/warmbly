package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// ProvisionTesterWorkspace grants access only to a dedicated reviewer's workspace.
func (r *organizationRepository) ProvisionTesterWorkspace(ctx context.Context, orgID, ownerID, adminID uuid.UUID, reason string, until time.Time) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id uuid.UUID
	if err := tx.QueryRow(ctx, `
		SELECT o.id FROM organizations o JOIN users u ON u.id = o.owner_user_id
		WHERE o.id = $1 AND o.owner_user_id = $2 AND u.login_code_exempt
		  AND u.password_expires_at = $3 AND u.password_expires_at > now()
		  AND NOT EXISTS (SELECT 1 FROM subscriptions s WHERE s.organization_id = o.id)
		FOR UPDATE OF o`, orgID, ownerID, until).Scan(&id); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `UPDATE organizations SET category = 'test', updated_at = now() WHERE id = $1`, orgID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO subscriptions (user_id, organization_id, plan_id, stripe_customer_id,
			managed_at, managed_by, managed_reason, managed_until, managed_plan_id)
		VALUES ($1, $2, '00000000-0000-0000-0000-000000000001', '', now(), $3, $4, $5, $6)`,
		ownerID, orgID, adminID, reason, until, models.TestPlanID); err != nil {
		return err
	}

	var allowance models.Micro
	if err := tx.QueryRow(ctx, `SELECT monthly_allowance_micro FROM plans WHERE id = $1`, models.TestPlanID).Scan(&allowance); err != nil {
		return err
	}
	if allowance <= 0 {
		return errors.New("the test plan needs a monthly allowance")
	}
	var balance, purchased models.Micro
	if err := tx.QueryRow(ctx, `
		INSERT INTO credit_ledger (org_id, balance_micro) VALUES ($1, $2)
		ON CONFLICT (org_id) DO UPDATE SET balance_micro = credit_ledger.balance_micro + EXCLUDED.balance_micro, updated_at = now()
		RETURNING balance_micro, purchased_micro`, orgID, allowance).Scan(&balance, &purchased); err != nil {
		return err
	}
	if _, err := insertTxn(ctx, tx, orgID, models.CreditTransaction{
		Amount: allowance, Reason: "tester_grant", MonthlyAfter: balance, PurchasedAfter: purchased,
		IdempotencyKey: keyPtr(scopeKey(orgID, "tester_grant")),
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
