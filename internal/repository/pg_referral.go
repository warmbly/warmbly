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

// ReferralRepository owns the referral_codes, referral_attributions, and the
// referral_earnings_ledger/_transactions tables. The earnings ledger is mutated
// only through atomic, idempotency-keyed SQL so a retried Stripe webhook can
// never double-grant — this is the billing-correctness boundary.
type ReferralRepository interface {
	// Codes
	GetCodeByOwner(ctx context.Context, ownerUserID uuid.UUID) (*models.ReferralCode, error)
	GetCodeByCode(ctx context.Context, code string) (*models.ReferralCode, error)
	GetCodeByID(ctx context.Context, id uuid.UUID) (*models.ReferralCode, error)
	CreateCode(ctx context.Context, code *models.ReferralCode) error

	// Attributions
	CreateAttribution(ctx context.Context, a *models.ReferralAttribution) error
	GetAttributionByInviteeOrg(ctx context.Context, inviteeOrgID uuid.UUID) (*models.ReferralAttribution, error)
	MarkAttributionQualified(ctx context.Context, id uuid.UUID) error
	MarkAttributionRewarded(ctx context.Context, id uuid.UUID, rewardCents int64, currency string) error
	MarkAttributionVoid(ctx context.Context, id uuid.UUID, reason string) error
	ListAttributionsByReferrer(ctx context.Context, referrerOrgID uuid.UUID, limit, offset int) ([]models.ReferralAttribution, error)
	CountRewardedByReferrerSince(ctx context.Context, referrerOrgID uuid.UUID, since time.Time) (int, error)
	AttributionStats(ctx context.Context, referrerOrgID uuid.UUID) (total, pending, qualified, rewarded int, err error)

	// Earnings ledger
	GetLedger(ctx context.Context, orgID uuid.UUID) (*models.ReferralEarningsLedger, error)
	// ApplyReferralReward atomically, in one transaction: short-circuits if the
	// Stripe event was already processed (idempotencyKey), flips the attribution
	// pending|qualified -> rewarded (the conditional UPDATE is the one-time gate),
	// credits the referrer ledger, and appends a trail row. applied=false means
	// it was already rewarded/void or the event was replayed — no money moved.
	ApplyReferralReward(ctx context.Context, attr *models.ReferralAttribution, amountCents int64, currency, idempotencyKey string) (applied bool, err error)
	// ApplyReferralClawback is the reverse: short-circuits on replay, flips
	// rewarded -> void (the gate, so concurrent refund+cancel events can't
	// double-debit), debits the ledger, and appends a trail row.
	ApplyReferralClawback(ctx context.Context, attr *models.ReferralAttribution, amountCents int64, currency, idempotencyKey, reason string) (applied bool, err error)
	// SetStripePushed records how many net cents have been mirrored to the
	// Stripe customer balance (the sync watermark).
	SetStripePushed(ctx context.Context, orgID uuid.UUID, pushedCents int64) error
	ListEarnings(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]models.ReferralEarningsTransaction, error)
}

type referralRepository struct {
	db *pgxpool.Pool
}

func NewReferralRepository(db *pgxpool.Pool) ReferralRepository {
	return &referralRepository{db: db}
}

// --- Codes ---

const referralCodeCols = `id, owner_user_id, owner_org_id, code, discount_code_id, created_at, updated_at`

func scanReferralCode(row pgx.Row, c *models.ReferralCode) error {
	return row.Scan(&c.ID, &c.OwnerUserID, &c.OwnerOrgID, &c.Code, &c.DiscountCodeID, &c.CreatedAt, &c.UpdatedAt)
}

func (r *referralRepository) GetCodeByOwner(ctx context.Context, ownerUserID uuid.UUID) (*models.ReferralCode, error) {
	c := &models.ReferralCode{}
	err := scanReferralCode(r.db.QueryRow(ctx, `SELECT `+referralCodeCols+` FROM referral_codes WHERE owner_user_id = $1`, ownerUserID), c)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (r *referralRepository) GetCodeByCode(ctx context.Context, code string) (*models.ReferralCode, error) {
	c := &models.ReferralCode{}
	err := scanReferralCode(r.db.QueryRow(ctx, `SELECT `+referralCodeCols+` FROM referral_codes WHERE code = $1`, code), c)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (r *referralRepository) GetCodeByID(ctx context.Context, id uuid.UUID) (*models.ReferralCode, error) {
	c := &models.ReferralCode{}
	err := scanReferralCode(r.db.QueryRow(ctx, `SELECT `+referralCodeCols+` FROM referral_codes WHERE id = $1`, id), c)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (r *referralRepository) CreateCode(ctx context.Context, code *models.ReferralCode) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO referral_codes (id, owner_user_id, owner_org_id, code, discount_code_id)
		VALUES ($1, $2, $3, $4, $5)
	`, code.ID, code.OwnerUserID, code.OwnerOrgID, code.Code, code.DiscountCodeID)
	return err
}

// --- Attributions ---

const referralAttrCols = `id, referral_code_id, referrer_user_id, referrer_org_id, invitee_org_id, invitee_user_id,
	status, reward_cents, reward_currency, qualified_at, rewarded_at, voided_at, void_reason, created_at, updated_at`

func scanReferralAttribution(row pgx.Row, a *models.ReferralAttribution) error {
	return row.Scan(
		&a.ID, &a.ReferralCodeID, &a.ReferrerUserID, &a.ReferrerOrgID, &a.InviteeOrgID, &a.InviteeUserID,
		&a.Status, &a.RewardCents, &a.RewardCurrency, &a.QualifiedAt, &a.RewardedAt, &a.VoidedAt, &a.VoidReason,
		&a.CreatedAt, &a.UpdatedAt,
	)
}

func (r *referralRepository) CreateAttribution(ctx context.Context, a *models.ReferralAttribution) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO referral_attributions (
			id, referral_code_id, referrer_user_id, referrer_org_id, invitee_org_id, invitee_user_id, status
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, a.ID, a.ReferralCodeID, a.ReferrerUserID, a.ReferrerOrgID, a.InviteeOrgID, a.InviteeUserID, a.Status)
	return err
}

func (r *referralRepository) GetAttributionByInviteeOrg(ctx context.Context, inviteeOrgID uuid.UUID) (*models.ReferralAttribution, error) {
	a := &models.ReferralAttribution{}
	err := scanReferralAttribution(r.db.QueryRow(ctx, `SELECT `+referralAttrCols+` FROM referral_attributions WHERE invitee_org_id = $1`, inviteeOrgID), a)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (r *referralRepository) MarkAttributionQualified(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE referral_attributions
		SET status = 'qualified', qualified_at = COALESCE(qualified_at, now()), updated_at = now()
		WHERE id = $1 AND status = 'pending'
	`, id)
	return err
}

func (r *referralRepository) MarkAttributionRewarded(ctx context.Context, id uuid.UUID, rewardCents int64, currency string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE referral_attributions
		SET status = 'rewarded', reward_cents = $2, reward_currency = $3,
			qualified_at = COALESCE(qualified_at, now()), rewarded_at = now(), updated_at = now()
		WHERE id = $1
	`, id, rewardCents, currency)
	return err
}

func (r *referralRepository) MarkAttributionVoid(ctx context.Context, id uuid.UUID, reason string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE referral_attributions
		SET status = 'void', voided_at = now(), void_reason = $2, updated_at = now()
		WHERE id = $1
	`, id, reason)
	return err
}

func (r *referralRepository) ListAttributionsByReferrer(ctx context.Context, referrerOrgID uuid.UUID, limit, offset int) ([]models.ReferralAttribution, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+referralAttrCols+`
		FROM referral_attributions
		WHERE referrer_org_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, referrerOrgID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.ReferralAttribution, 0)
	for rows.Next() {
		var a models.ReferralAttribution
		if err := scanReferralAttribution(rows, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *referralRepository) CountRewardedByReferrerSince(ctx context.Context, referrerOrgID uuid.UUID, since time.Time) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `
		SELECT count(*) FROM referral_attributions
		WHERE referrer_org_id = $1 AND status = 'rewarded' AND rewarded_at >= $2
	`, referrerOrgID, since).Scan(&n)
	return n, err
}

func (r *referralRepository) AttributionStats(ctx context.Context, referrerOrgID uuid.UUID) (total, pending, qualified, rewarded int, err error) {
	err = r.db.QueryRow(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE status = 'pending'),
			count(*) FILTER (WHERE status = 'qualified'),
			count(*) FILTER (WHERE status = 'rewarded')
		FROM referral_attributions
		WHERE referrer_org_id = $1
	`, referrerOrgID).Scan(&total, &pending, &qualified, &rewarded)
	return total, pending, qualified, rewarded, err
}

// --- Earnings ledger ---

const referralLedgerCols = `org_id, balance_cents, lifetime_earned_cents, stripe_pushed_cents, currency, created_at, updated_at`

func scanReferralLedger(row pgx.Row, l *models.ReferralEarningsLedger) error {
	return row.Scan(&l.OrgID, &l.BalanceCents, &l.LifetimeEarnedCents, &l.StripePushedCents, &l.Currency, &l.CreatedAt, &l.UpdatedAt)
}

func (r *referralRepository) GetLedger(ctx context.Context, orgID uuid.UUID) (*models.ReferralEarningsLedger, error) {
	l := &models.ReferralEarningsLedger{}
	err := scanReferralLedger(r.db.QueryRow(ctx, `SELECT `+referralLedgerCols+` FROM referral_earnings_ledger WHERE org_id = $1`, orgID), l)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return l, nil
}

func (r *referralRepository) ApplyReferralReward(ctx context.Context, attr *models.ReferralAttribution, amountCents int64, currency, idempotencyKey string) (bool, error) {
	return r.applyAttributionLedger(ctx, attr, amountCents, currency, "referral_reward", idempotencyKey,
		`UPDATE referral_attributions
		 SET status = 'rewarded', reward_cents = $2, reward_currency = $3,
		     qualified_at = COALESCE(qualified_at, now()), rewarded_at = now(), updated_at = now()
		 WHERE id = $1 AND status IN ('pending', 'qualified')`,
		[]any{attr.ID, amountCents, currency})
}

func (r *referralRepository) ApplyReferralClawback(ctx context.Context, attr *models.ReferralAttribution, amountCents int64, currency, idempotencyKey, reason string) (bool, error) {
	return r.applyAttributionLedger(ctx, attr, -amountCents, currency, "referral_clawback:"+reason, idempotencyKey,
		`UPDATE referral_attributions
		 SET status = 'void', voided_at = now(), void_reason = $2, updated_at = now()
		 WHERE id = $1 AND status = 'rewarded'`,
		[]any{attr.ID, reason})
}

// applyAttributionLedger is the shared atomic primitive for reward + clawback.
// In one transaction it: short-circuits on a replayed Stripe event
// (idempotencyKey); runs the conditional attribution status transition (the
// one-time / mutual-exclusion gate — 0 rows affected means another event
// already handled it, so no money moves); then mutates the ledger and appends a
// trail row. ledgerDelta is signed (positive = reward, negative = clawback).
func (r *referralRepository) applyAttributionLedger(ctx context.Context, attr *models.ReferralAttribution, ledgerDelta int64, currency, reason, idempotencyKey, statusSQL string, statusArgs []any) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	if idempotencyKey != "" {
		var exists bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM referral_earnings_transactions WHERE idempotency_key = $1)`, idempotencyKey).Scan(&exists); err != nil {
			return false, err
		}
		if exists {
			return false, tx.Commit(ctx)
		}
	}

	// Conditional status transition. Postgres serializes concurrent UPDATEs on
	// the row, so exactly one of two racing events (e.g. refund + cancel) wins;
	// the loser matches 0 rows and applies nothing.
	tag, err := tx.Exec(ctx, statusSQL, statusArgs...)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, tx.Commit(ctx)
	}

	// Apply the signed delta. lifetime_earned only grows on positive amounts;
	// balance may go negative on a clawback that exceeds available credit.
	var balanceAfter int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO referral_earnings_ledger (org_id, balance_cents, lifetime_earned_cents, currency)
		VALUES ($1, $2, GREATEST($2, 0), $3)
		ON CONFLICT (org_id) DO UPDATE SET
			balance_cents = referral_earnings_ledger.balance_cents + EXCLUDED.balance_cents,
			lifetime_earned_cents = referral_earnings_ledger.lifetime_earned_cents + GREATEST(EXCLUDED.balance_cents, 0),
			updated_at = now()
		RETURNING balance_cents`, attr.ReferrerOrgID, ledgerDelta, currency).Scan(&balanceAfter); err != nil {
		return false, err
	}

	attrID := attr.ID
	var keyArg *string
	if idempotencyKey != "" {
		keyArg = &idempotencyKey
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO referral_earnings_transactions
			(org_id, attribution_id, amount_cents, currency, reason, balance_after_cents, idempotency_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, attr.ReferrerOrgID, attrID, ledgerDelta, currency, reason, balanceAfter, keyArg); err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (r *referralRepository) SetStripePushed(ctx context.Context, orgID uuid.UUID, pushedCents int64) error {
	_, err := r.db.Exec(ctx, `
		UPDATE referral_earnings_ledger SET stripe_pushed_cents = $2, updated_at = now() WHERE org_id = $1
	`, orgID, pushedCents)
	return err
}

func (r *referralRepository) ListEarnings(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]models.ReferralEarningsTransaction, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, org_id, attribution_id, amount_cents, currency, reason, balance_after_cents,
			   stripe_customer_balance_txn_id, idempotency_key, created_at
		FROM referral_earnings_transactions
		WHERE org_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, orgID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.ReferralEarningsTransaction, 0)
	for rows.Next() {
		var t models.ReferralEarningsTransaction
		if err := rows.Scan(
			&t.ID, &t.OrgID, &t.AttributionID, &t.AmountCents, &t.Currency, &t.Reason, &t.BalanceAfterCents,
			&t.StripeCustomerBalanceTxnID, &t.IdempotencyKey, &t.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
