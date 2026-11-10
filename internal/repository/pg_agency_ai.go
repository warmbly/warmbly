package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// ErrAgencyAIBalanceShort refuses a move of more than the agency balance holds.
var ErrAgencyAIBalanceShort = errors.New("agency AI balance is short")

// ErrAgencyAIClientNotFound refuses a move into a workspace that is not the agency's client.
var ErrAgencyAIClientNotFound = errors.New("workspace is not a client of this agency")

// Kinds of agency_ai_transactions rows.
const (
	agencyAIPeriodReset = "period_reset"
	agencyAISpend       = "spend"
	agencyAIRefund      = "refund"
	agencyAITransfer    = "transfer"
	agencyAITopup       = "topup"
	agencyAITopupRefund = "topup_refund"
	agencyAICredit      = "credit"
)

// AgencyAIRepository is the agency side of the shared AI balance. Every
// change row-locks the agency's ledger after any workspace ledger it touches,
// the same order a client charge takes, and CHECK constraints keep both pools
// from going negative.
type AgencyAIRepository interface {
	// Ledger is the stored balance, or a zero one when the agency has none yet.
	Ledger(ctx context.Context, agencyID uuid.UUID) (models.AgencyAILedger, error)
	// PeriodUse is each client's spend and the moves into it in the period
	// that starts at periodStart, and what was bought then and not refunded.
	PeriodUse(ctx context.Context, agencyID uuid.UUID, periodStart time.Time) (map[uuid.UUID]models.AgencyAIClientPeriod, models.Micro, error)
	// SetClientAILimit caps a client's spend from the agency balance; nil removes it.
	SetClientAILimit(ctx context.Context, agencyID, orgID uuid.UUID, limit *models.Micro) (bool, error)
	// Transfer moves amount from the agency balance into the client's own
	// purchased balance, once per key.
	Transfer(ctx context.Context, agencyID, orgID uuid.UUID, amount models.Micro, period models.AgencyAIPeriod, key string, actor uuid.UUID) (*models.AgencyAITransfer, error)
	// GrantPurchased adds a paid top-up, once per payment intent.
	GrantPurchased(ctx context.Context, agencyID uuid.UUID, amount models.Micro, period models.AgencyAIPeriod, paymentIntent string, actor *uuid.UUID) (bool, error)
	// Credit adds money an operator gave, which never expires, once per key.
	Credit(ctx context.Context, agencyID uuid.UUID, amount models.Micro, period models.AgencyAIPeriod, reason, key string, actor uuid.UUID) (bool, error)
	// RefundPurchase takes a refunded top-up back out of the bought balance, as
	// much of it as is left, once. Found is false for a payment that bought none.
	RefundPurchase(ctx context.Context, paymentIntent string) (agencyID uuid.UUID, found bool, err error)
}

type agencyAIRepository struct {
	DB *db.DB
}

func NewAgencyAIRepository(database *db.DB) AgencyAIRepository {
	return &agencyAIRepository{DB: database}
}

// agencyFunding is the agency that pays what a client's own balance cannot.
type agencyFunding struct {
	agencyID uuid.UUID
	period   models.AgencyAIPeriod
	limit    *models.Micro
}

// agencyFundingFor reads the workspace's agency inside the caller's
// transaction, so a client detached a moment ago is never charged to it.
func agencyFundingFor(ctx context.Context, q queryRower, orgID uuid.UUID, billed bool, now time.Time) (*agencyFunding, error) {
	var a models.Agency
	var allowance, granted, limit *models.Micro
	err := q.QueryRow(ctx, `
		SELECT a.id, a.plan_id, a.status, a.current_period_start, a.current_period_end,
		       a.managed_plan_id, a.managed_at, a.managed_until, a.ai_allowance_override_micro,
		       p.agency_ai_allowance_micro, mp.agency_ai_allowance_micro, c.monthly_ai_limit_micro
		FROM agency_clients c
		JOIN agencies a ON a.id = c.agency_id
		LEFT JOIN plans p ON p.id = a.plan_id
		LEFT JOIN plans mp ON mp.id = a.managed_plan_id
		WHERE c.organization_id = $1`, orgID).
		Scan(&a.ID, &a.PlanID, &a.Status, &a.CurrentPeriodStart, &a.CurrentPeriodEnd,
			&a.ManagedPlanID, &a.ManagedAt, &a.ManagedUntil, &a.AIAllowanceOverride, &allowance, &granted, &limit)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// An operator's plan in force is the one whose allowance refills the balance.
	if a.Granted(now) {
		allowance = granted
	}
	return &agencyFunding{agencyID: a.ID, period: models.AgencyAIPeriodOf(&a, allowance, billed, now), limit: limit}, nil
}

func readAgencyAILedger(ctx context.Context, q queryRower, agencyID uuid.UUID) (models.AgencyAILedger, error) {
	l := models.AgencyAILedger{AgencyID: agencyID}
	err := q.QueryRow(ctx, `SELECT allowance_micro, purchased_micro, total_purchased_micro, period_start FROM agency_ai_ledger WHERE agency_id = $1`, agencyID).
		Scan(&l.Allowance, &l.Purchased, &l.TotalPurchased, &l.PeriodStart)
	if errors.Is(err, pgx.ErrNoRows) {
		return l, nil
	}
	return l, err
}

// lockAgencyAI row-locks the agency's ledger and, given a period newer than
// the stored one, refills the allowance for it. A period older than the
// stored one (a stale read of the agency) changes nothing.
func lockAgencyAI(ctx context.Context, tx pgx.Tx, agencyID uuid.UUID, p *models.AgencyAIPeriod) (models.AgencyAILedger, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO agency_ai_ledger (agency_id) VALUES ($1) ON CONFLICT (agency_id) DO NOTHING`, agencyID); err != nil {
		return models.AgencyAILedger{}, err
	}
	l := models.AgencyAILedger{AgencyID: agencyID}
	if err := tx.QueryRow(ctx, `SELECT allowance_micro, purchased_micro, total_purchased_micro, period_start FROM agency_ai_ledger WHERE agency_id = $1 FOR UPDATE`, agencyID).
		Scan(&l.Allowance, &l.Purchased, &l.TotalPurchased, &l.PeriodStart); err != nil {
		return l, err
	}
	if p == nil || (l.PeriodStart != nil && !l.PeriodStart.Before(p.Start)) {
		return l, nil
	}
	next := l.In(*p)
	if _, err := tx.Exec(ctx, `UPDATE agency_ai_ledger SET allowance_micro = $2, period_start = $3, updated_at = now() WHERE agency_id = $1`,
		agencyID, next.Allowance, *next.PeriodStart); err != nil {
		return l, err
	}
	delta := next.Allowance - l.Allowance
	if err := insertAgencyTxn(ctx, tx, agencyTxn{agencyID: agencyID, kind: agencyAIPeriodReset, amount: delta, allowanceDelta: delta, after: next}); err != nil {
		return l, err
	}
	return next, nil
}

// periodOf is the period a ledger's rows count toward.
func periodOf(l models.AgencyAILedger) time.Time {
	if l.PeriodStart != nil {
		return *l.PeriodStart
	}
	return time.Now()
}

type agencyTxn struct {
	agencyID       uuid.UUID
	orgID          *uuid.UUID
	kind           string
	amount         models.Micro
	allowanceDelta models.Micro
	after          models.AgencyAILedger
	period         *time.Time
	reason         string
	key            *string
	paymentIntent  *string
	actor          *uuid.UUID
}

func insertAgencyTxn(ctx context.Context, tx pgx.Tx, t agencyTxn) error {
	period := periodOf(t.after)
	if t.period != nil {
		period = *t.period
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO agency_ai_transactions
			(agency_id, organization_id, kind, amount_micro, allowance_delta_micro, allowance_after_micro, purchased_after_micro,
			 period_start, reason, idempotency_key, stripe_payment_intent_id, actor_user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		t.agencyID, t.orgID, t.kind, t.amount, t.allowanceDelta, t.after.Allowance, t.after.Purchased,
		period, t.reason, t.key, t.paymentIntent, t.actor)
	return err
}

// clientSpent is the client's net spend from the agency balance in a period.
func clientSpent(ctx context.Context, q queryRower, agencyID, orgID uuid.UUID, periodStart time.Time) (models.Micro, error) {
	var spent models.Micro
	err := q.QueryRow(ctx, `
		SELECT COALESCE(-SUM(amount_micro), 0) FROM agency_ai_transactions
		WHERE agency_id = $1 AND organization_id = $2 AND period_start = $3 AND kind IN ('spend', 'refund')`,
		agencyID, orgID, periodStart).Scan(&spent)
	return spent, err
}

// clientAvailable is what the agency balance l still pays for this client,
// held to the client's cap for the period l is in.
func clientAvailable(ctx context.Context, q queryRower, orgID uuid.UUID, f *agencyFunding, l models.AgencyAILedger) (models.Micro, error) {
	avail := l.Allowance + l.Purchased
	if f.limit == nil {
		return avail, nil
	}
	spent, err := clientSpent(ctx, q, f.agencyID, orgID, periodOf(l))
	if err != nil {
		return 0, err
	}
	return min(avail, max(0, *f.limit-spent)), nil
}

// agencyCanPay locks the agency's ledger in the charge's transaction and
// returns it with what it may pay for this client.
func agencyCanPay(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, f *agencyFunding) (models.AgencyAILedger, models.Micro, error) {
	l, err := lockAgencyAI(ctx, tx, f.agencyID, &f.period)
	if err != nil {
		return l, 0, err
	}
	avail, err := clientAvailable(ctx, tx, orgID, f, l)
	return l, avail, err
}

// spendAgency takes amount from a ledger agencyCanPay locked: allowance first, then bought money.
func spendAgency(ctx context.Context, tx pgx.Tx, orgID, agencyID uuid.UUID, l models.AgencyAILedger, amount models.Micro, reason string, key *string) error {
	fromAllowance := min(amount, l.Allowance)
	l.Allowance -= fromAllowance
	l.Purchased -= amount - fromAllowance
	if _, err := tx.Exec(ctx, `UPDATE agency_ai_ledger SET allowance_micro = $2, purchased_micro = $3, updated_at = now() WHERE agency_id = $1`,
		agencyID, l.Allowance, l.Purchased); err != nil {
		return err
	}
	return insertAgencyTxn(ctx, tx, agencyTxn{
		agencyID: agencyID, orgID: &orgID, kind: agencyAISpend, amount: -amount, allowanceDelta: -fromAllowance,
		after: l, reason: reason, key: key, actor: actorOf(ctx),
	})
}

// refundAgencySpend gives the agency back its part of a charge: the allowance
// part only while the charge's period is still the stored one.
func refundAgencySpend(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, spendKey, refundKey, reason string) (models.Micro, error) {
	var agencyID uuid.UUID
	var amount, allowanceDelta models.Micro
	var period time.Time
	err := tx.QueryRow(ctx, `
		SELECT agency_id, amount_micro, allowance_delta_micro, period_start FROM agency_ai_transactions
		WHERE idempotency_key = $1 AND kind = 'spend' AND organization_id = $2`, spendKey, orgID).
		Scan(&agencyID, &amount, &allowanceDelta, &period)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	l, err := lockAgencyAI(ctx, tx, agencyID, nil)
	if err != nil {
		return 0, err
	}
	toPurchased := -amount + allowanceDelta
	toAllowance := models.Micro(0)
	if l.PeriodStart != nil && l.PeriodStart.Equal(period) {
		toAllowance = -allowanceDelta
	}
	total := toAllowance + toPurchased
	if total == 0 {
		return 0, nil
	}
	l.Allowance += toAllowance
	l.Purchased += toPurchased
	if _, err := tx.Exec(ctx, `UPDATE agency_ai_ledger SET allowance_micro = $2, purchased_micro = $3, updated_at = now() WHERE agency_id = $1`,
		agencyID, l.Allowance, l.Purchased); err != nil {
		return 0, err
	}
	if err := insertAgencyTxn(ctx, tx, agencyTxn{
		agencyID: agencyID, orgID: &orgID, kind: agencyAIRefund, amount: total, allowanceDelta: toAllowance,
		after: l, period: &period, reason: reason, key: keyPtr(refundKey),
	}); err != nil {
		return 0, err
	}
	return total, nil
}

func actorOf(ctx context.Context) *uuid.UUID {
	if id := models.CreditMetaFrom(ctx).ActorID; id != uuid.Nil {
		return &id
	}
	return nil
}

func (r *agencyAIRepository) Ledger(ctx context.Context, agencyID uuid.UUID) (models.AgencyAILedger, error) {
	return readAgencyAILedger(ctx, r.DB, agencyID)
}

func (r *agencyAIRepository) PeriodUse(ctx context.Context, agencyID uuid.UUID, periodStart time.Time) (map[uuid.UUID]models.AgencyAIClientPeriod, models.Micro, error) {
	rows, err := r.DB.Query(ctx, `
		SELECT organization_id, kind, SUM(amount_micro) FROM agency_ai_transactions
		WHERE agency_id = $1 AND period_start = $2 AND organization_id IS NOT NULL AND kind IN ('spend', 'refund', 'transfer')
		GROUP BY 1, 2`, agencyID, periodStart)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := map[uuid.UUID]models.AgencyAIClientPeriod{}
	for rows.Next() {
		var orgID uuid.UUID
		var kind string
		var sum models.Micro
		if err := rows.Scan(&orgID, &kind, &sum); err != nil {
			return nil, 0, err
		}
		u := out[orgID]
		if kind == agencyAITransfer {
			u.Moved -= sum
		} else {
			u.Spent -= sum
		}
		out[orgID] = u
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var bought models.Micro
	err = r.DB.QueryRow(ctx, `
		SELECT COALESCE(SUM(t.amount_micro), 0) FROM agency_ai_transactions t
		WHERE t.agency_id = $1 AND t.period_start = $2 AND t.kind = 'topup'
		  AND NOT EXISTS (SELECT 1 FROM agency_ai_transactions r WHERE r.idempotency_key = t.idempotency_key || ':refund')`,
		agencyID, periodStart).Scan(&bought)
	return out, bought, err
}

func (r *agencyAIRepository) SetClientAILimit(ctx context.Context, agencyID, orgID uuid.UUID, limit *models.Micro) (bool, error) {
	tag, err := r.DB.Exec(ctx, `UPDATE agency_clients SET monthly_ai_limit_micro = $3 WHERE agency_id = $1 AND organization_id = $2`, agencyID, orgID, limit)
	return tag.RowsAffected() == 1, err
}

func (r *agencyAIRepository) Transfer(ctx context.Context, agencyID, orgID uuid.UUID, amount models.Micro, period models.AgencyAIPeriod, key string, actor uuid.UUID) (*models.AgencyAITransfer, error) {
	if amount <= 0 {
		return nil, errors.New("transfer amount must be positive")
	}
	var agencyKey *string
	if key != "" {
		k := "transfer:" + agencyID.String() + ":" + key
		agencyKey = &k
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if agencyKey != nil {
		var prev models.Micro
		err := tx.QueryRow(ctx, `SELECT -amount_micro FROM agency_ai_transactions WHERE idempotency_key = $1`, *agencyKey).Scan(&prev)
		if err == nil {
			out := &models.AgencyAITransfer{Amount: prev, Replayed: true}
			l, lerr := readAgencyAILedger(ctx, tx, agencyID)
			if lerr != nil {
				return nil, lerr
			}
			l = l.In(period)
			out.AgencyBalance = l.Allowance + l.Purchased
			if own, oerr := r.ownBalance(ctx, tx, orgID); oerr == nil {
				out.WorkspaceBalance = own
			}
			return out, tx.Commit(ctx)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}

	if _, _, _, err := lockOrgLedger(ctx, tx, orgID, true); err != nil {
		return nil, err
	}
	var isClient bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agency_clients WHERE organization_id = $1 AND agency_id = $2)`, orgID, agencyID).Scan(&isClient); err != nil {
		return nil, err
	}
	if !isClient {
		return nil, ErrAgencyAIClientNotFound
	}
	l, err := lockAgencyAI(ctx, tx, agencyID, &period)
	if err != nil {
		return nil, err
	}
	if l.Allowance+l.Purchased < amount {
		return nil, ErrAgencyAIBalanceShort
	}
	fromAllowance := min(amount, l.Allowance)
	l.Allowance -= fromAllowance
	l.Purchased -= amount - fromAllowance
	if _, err := tx.Exec(ctx, `UPDATE agency_ai_ledger SET allowance_micro = $2, purchased_micro = $3, updated_at = now() WHERE agency_id = $1`,
		agencyID, l.Allowance, l.Purchased); err != nil {
		return nil, err
	}
	actorArg := &actor
	if actor == uuid.Nil {
		actorArg = nil
	}
	if err := insertAgencyTxn(ctx, tx, agencyTxn{
		agencyID: agencyID, orgID: &orgID, kind: agencyAITransfer, amount: -amount, allowanceDelta: -fromAllowance,
		after: l, reason: "agency_transfer", key: agencyKey, actor: actorArg,
	}); err != nil {
		return nil, err
	}

	// Moved money is the workspace's to keep, so it lands in the pool that never expires.
	var monthly, purchased models.Micro
	if err := tx.QueryRow(ctx, `
		UPDATE credit_ledger SET purchased_micro = purchased_micro + $2, updated_at = now() WHERE org_id = $1
		RETURNING balance_micro, purchased_micro`, orgID, amount).Scan(&monthly, &purchased); err != nil {
		return nil, err
	}
	var orgKey string
	if agencyKey != nil {
		orgKey = scopeKey(orgID, *agencyKey)
	}
	if _, err := insertTxn(models.WithCreditMeta(ctx, models.CreditMeta{ActorID: actor}), tx, orgID, models.CreditTransaction{
		Amount: amount, Reason: "agency_transfer",
		MonthlyAfter: monthly, PurchasedDelta: amount, PurchasedAfter: purchased,
		IdempotencyKey: keyPtr(orgKey),
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &models.AgencyAITransfer{Amount: amount, AgencyBalance: l.Allowance + l.Purchased, WorkspaceBalance: monthly + purchased}, nil
}

func (r *agencyAIRepository) ownBalance(ctx context.Context, q queryRower, orgID uuid.UUID) (models.Micro, error) {
	var total models.Micro
	err := q.QueryRow(ctx, `SELECT balance_micro + purchased_micro FROM credit_ledger WHERE org_id = $1`, orgID).Scan(&total)
	return total, err
}

func (r *agencyAIRepository) GrantPurchased(ctx context.Context, agencyID uuid.UUID, amount models.Micro, period models.AgencyAIPeriod, paymentIntent string, actor *uuid.UUID) (bool, error) {
	if amount <= 0 || paymentIntent == "" {
		return false, errors.New("a top-up needs a positive amount and its payment")
	}
	key := "topup:" + paymentIntent
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var seen bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agency_ai_transactions WHERE idempotency_key = $1)`, key).Scan(&seen); err != nil {
		return false, err
	}
	if seen {
		return false, tx.Commit(ctx)
	}
	l, err := lockAgencyAI(ctx, tx, agencyID, &period)
	if err != nil {
		return false, err
	}
	l.Purchased += amount
	l.TotalPurchased += amount
	if _, err := tx.Exec(ctx, `UPDATE agency_ai_ledger SET purchased_micro = $2, total_purchased_micro = $3, updated_at = now() WHERE agency_id = $1`,
		agencyID, l.Purchased, l.TotalPurchased); err != nil {
		return false, err
	}
	if err := insertAgencyTxn(ctx, tx, agencyTxn{
		agencyID: agencyID, kind: agencyAITopup, amount: amount, after: l, reason: "agency_ai_topup",
		key: &key, paymentIntent: &paymentIntent, actor: actor,
	}); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (r *agencyAIRepository) Credit(ctx context.Context, agencyID uuid.UUID, amount models.Micro, period models.AgencyAIPeriod, reason, key string, actor uuid.UUID) (bool, error) {
	if amount <= 0 || key == "" {
		return false, errors.New("a credit needs a positive amount and a key")
	}
	key = "credit:" + key
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var seen bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agency_ai_transactions WHERE idempotency_key = $1)`, key).Scan(&seen); err != nil {
		return false, err
	}
	if seen {
		return false, tx.Commit(ctx)
	}
	l, err := lockAgencyAI(ctx, tx, agencyID, &period)
	if err != nil {
		return false, err
	}
	l.Purchased += amount
	if _, err := tx.Exec(ctx, `UPDATE agency_ai_ledger SET purchased_micro = $2, updated_at = now() WHERE agency_id = $1`, agencyID, l.Purchased); err != nil {
		return false, err
	}
	if err := insertAgencyTxn(ctx, tx, agencyTxn{
		agencyID: agencyID, kind: agencyAICredit, amount: amount, after: l, reason: reason, key: &key, actor: &actor,
	}); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (r *agencyAIRepository) RefundPurchase(ctx context.Context, paymentIntent string) (uuid.UUID, bool, error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return uuid.Nil, false, err
	}
	defer tx.Rollback(ctx)
	var agencyID uuid.UUID
	var amount models.Micro
	var key string
	err = tx.QueryRow(ctx, `SELECT agency_id, amount_micro, idempotency_key FROM agency_ai_transactions WHERE stripe_payment_intent_id = $1 AND kind = 'topup'`, paymentIntent).
		Scan(&agencyID, &amount, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	refundKey := key + ":refund"
	l, err := lockAgencyAI(ctx, tx, agencyID, nil)
	if err != nil {
		return uuid.Nil, false, err
	}
	var done bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agency_ai_transactions WHERE idempotency_key = $1)`, refundKey).Scan(&done); err != nil {
		return uuid.Nil, false, err
	}
	if done {
		return agencyID, true, tx.Commit(ctx)
	}
	// What was already spent cannot be taken back; the rest leaves with the refund.
	take := min(amount, l.Purchased)
	l.Purchased -= take
	if _, err := tx.Exec(ctx, `UPDATE agency_ai_ledger SET purchased_micro = $2, updated_at = now() WHERE agency_id = $1`, agencyID, l.Purchased); err != nil {
		return uuid.Nil, false, err
	}
	if err := insertAgencyTxn(ctx, tx, agencyTxn{
		agencyID: agencyID, kind: agencyAITopupRefund, amount: -take, after: l, reason: "agency_ai_topup_refund", key: &refundKey,
	}); err != nil {
		return uuid.Nil, false, err
	}
	return agencyID, true, tx.Commit(ctx)
}
