package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// ErrInsufficientCredits is returned by a strict Debit when the org's combined
// balance (monthly + purchased) is lower than the amount. Callers map it to a
// 402.
var ErrInsufficientCredits = errors.New("insufficient balance")

// CreditRepository persists the balance ledger and its append-only transaction
// log, in micro-dollars. The ledger has two pools: the monthly allowance (reset
// each billing cycle) and the purchased balance (never expires). Every balance
// change runs inside a transaction that row-locks the ledger and is backstopped
// by CHECK constraints, so neither pool can go negative under concurrency.
type CreditRepository interface {
	// GetBalance returns the ledger for an org, or nil if it has none yet.
	GetBalance(ctx context.Context, orgID uuid.UUID) (*models.CreditLedger, error)

	// EnsureLedger returns the ledger, creating an empty one if needed.
	EnsureLedger(ctx context.Context, orgID uuid.UUID) (*models.CreditLedger, error)

	// Debit takes the amount from the monthly pool first, then purchased. A
	// key already used returns its original row with replayed=true.
	Debit(ctx context.Context, orgID uuid.UUID, d models.CreditDebit) (balance models.Micro, txn *models.CreditTransaction, replayed bool, err error)

	// Grant adds to the monthly pool; GrantPurchased to the purchased pool,
	// also raising the lifetime total. A key makes either one idempotent.
	Grant(ctx context.Context, orgID uuid.UUID, amount models.Micro, reason, idempotencyKey string) (models.Micro, *models.CreditTransaction, error)
	GrantPurchased(ctx context.Context, orgID uuid.UUID, amount models.Micro, reason, idempotencyKey string) (models.Micro, *models.CreditTransaction, error)

	// ResetMonthly sets the monthly pool to the allowance (purchased untouched)
	// and stamps the reset. The key (the Stripe event id) makes it idempotent.
	ResetMonthly(ctx context.Context, orgID uuid.UUID, allowance models.Micro, idempotencyKey string) (*models.CreditLedger, error)

	// RefundSpend gives back what the charge under spendKey took, to the pools
	// it came from, once (refundKey). Monthly money spent before the last
	// reset expired with it, so only the purchased part comes back then.
	RefundSpend(ctx context.Context, orgID uuid.UUID, spendKey, refundKey, reason string) (refunded, balance models.Micro, err error)

	// SpentInWindows sums net debits since each window start.
	SpentInWindows(ctx context.Context, orgID uuid.UUID, dayStart, weekStart, monthStart time.Time) (day, week, month models.Micro, err error)
	// MemberSpentInWindows is SpentInWindows for one member's charges.
	MemberSpentInWindows(ctx context.Context, orgID, userID uuid.UUID, dayStart, weekStart, monthStart time.Time) (day, week, month models.Micro, err error)

	// UsageDaily is spend per UTC day since `since`.
	UsageDaily(ctx context.Context, orgID uuid.UUID, since time.Time) ([]models.CreditUsagePoint, error)
	// UsageBreakdown groups spend since `since` by "reason" or "model".
	UsageBreakdown(ctx context.Context, orgID uuid.UUID, since time.Time, by string) ([]models.CreditUsageBucket, error)

	// AnnualAllowancesDue lists active subscriptions billed over more than a
	// month, with their period start, plan allowance and last reset, so the
	// allowance can refresh every month in between invoices. Oldest reset first.
	AnnualAllowancesDue(ctx context.Context, limit int) ([]AnnualAllowance, error)

	ListTransactions(ctx context.Context, orgID uuid.UUID, limit int) ([]models.CreditTransaction, error)
	// ListTransactionsBefore keyset-paginates rows older than the cursor.
	ListTransactionsBefore(ctx context.Context, orgID uuid.UUID, limit int, beforeCreatedAt time.Time, beforeID uuid.UUID) ([]models.CreditTransaction, error)
}

type creditRepository struct {
	DB *db.DB
}

func NewCreditRepository(database *db.DB) CreditRepository {
	return &creditRepository{DB: database}
}

const creditLedgerCols = `org_id, balance_micro, purchased_micro, month_reset_at, total_purchased_micro, created_at, updated_at`

func scanLedger(row pgx.Row, l *models.CreditLedger) error {
	return row.Scan(&l.OrgID, &l.Monthly, &l.Purchased, &l.MonthResetAt, &l.TotalPurchased, &l.CreatedAt, &l.UpdatedAt)
}

// Rows written before the dollar columns existed carry credits only.
const creditTxnCols = `id, org_id,
	COALESCE(amount_micro, amount::bigint * legacy_credit_micro()),
	reason, model_used,
	CASE WHEN input_tokens + output_tokens = 0 THEN tokens_used ELSE input_tokens END,
	output_tokens, quantity, cost_micro,
	COALESCE(balance_after_micro, balance_after::bigint * legacy_credit_micro()),
	COALESCE(purchased_delta_micro, purchased_delta::bigint * legacy_credit_micro()),
	COALESCE(purchased_balance_after_micro, purchased_balance_after::bigint * legacy_credit_micro()),
	idempotency_key, actor_user_id, context, created_at`

func scanTxn(row pgx.Row, t *models.CreditTransaction) error {
	var rawCtx []byte
	if err := row.Scan(
		&t.ID, &t.OrgID, &t.Amount, &t.Reason, &t.ModelUsed,
		&t.InputTokens, &t.OutputTokens, &t.Quantity, &t.Cost,
		&t.MonthlyAfter, &t.PurchasedDelta, &t.PurchasedAfter,
		&t.IdempotencyKey, &t.ActorUserID, &rawCtx, &t.CreatedAt,
	); err != nil {
		return err
	}
	if len(rawCtx) > 0 {
		_ = json.Unmarshal(rawCtx, &t.Context) // a malformed blob renders empty, never fails the read
	}
	return nil
}

func (r *creditRepository) GetBalance(ctx context.Context, orgID uuid.UUID) (*models.CreditLedger, error) {
	l := &models.CreditLedger{}
	err := scanLedger(r.DB.QueryRow(ctx, `SELECT `+creditLedgerCols+` FROM credit_ledger WHERE org_id = $1`, orgID), l)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return l, nil
}

func (r *creditRepository) EnsureLedger(ctx context.Context, orgID uuid.UUID) (*models.CreditLedger, error) {
	l := &models.CreditLedger{}
	err := scanLedger(r.DB.QueryRow(ctx, `
		INSERT INTO credit_ledger (org_id) VALUES ($1)
		ON CONFLICT (org_id) DO UPDATE SET org_id = EXCLUDED.org_id
		RETURNING `+creditLedgerCols, orgID), l)
	if err != nil {
		return nil, err
	}
	return l, nil
}

// scopeKey namespaces an idempotency key with the org id, so a key value from
// one tenant can never replay against (or return) another tenant's entry.
// Empty stays empty (non-idempotent).
func scopeKey(orgID uuid.UUID, key string) string {
	if key == "" {
		return ""
	}
	return orgID.String() + ":" + key
}

// replayByKey returns the prior transaction for an idempotency key, if any.
// Must run inside the caller's transaction.
func replayByKey(ctx context.Context, tx pgx.Tx, idempotencyKey string) (*models.CreditTransaction, error) {
	existing := &models.CreditTransaction{}
	err := scanTxn(tx.QueryRow(ctx,
		`SELECT `+creditTxnCols+` FROM credit_ledger_transactions WHERE idempotency_key = $1`, idempotencyKey), existing)
	if err == nil {
		return existing, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return nil, err
}

// insertTxn appends one log row inside the caller's transaction. Attribution
// (actor + what-ran context) is read off the request context when the call
// site attached it via models.WithCreditMeta. The credit columns are filled by
// the table's trigger.
func insertTxn(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, t models.CreditTransaction) (*models.CreditTransaction, error) {
	var keyArg *string
	if t.IdempotencyKey != nil && *t.IdempotencyKey != "" {
		keyArg = t.IdempotencyKey
	}
	meta := models.CreditMetaFrom(ctx)
	var actorArg *uuid.UUID
	if meta.ActorID != uuid.Nil {
		actor := meta.ActorID
		actorArg = &actor
	}
	ctxJSON := []byte("{}")
	if !meta.Context.Empty() {
		if raw, err := json.Marshal(meta.Context); err == nil {
			ctxJSON = raw
		}
	}
	out := &models.CreditTransaction{}
	err := scanTxn(tx.QueryRow(ctx, `
		INSERT INTO credit_ledger_transactions
			(org_id, amount, balance_after, reason, model_used, input_tokens, output_tokens, quantity, cost_micro,
			 amount_micro, balance_after_micro, purchased_delta_micro, purchased_balance_after_micro,
			 idempotency_key, actor_user_id, context)
		VALUES ($1, 0, 0, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING `+creditTxnCols,
		orgID, t.Reason, t.ModelUsed, t.InputTokens, t.OutputTokens, t.Quantity, t.Cost,
		t.Amount, t.MonthlyAfter, t.PurchasedDelta, t.PurchasedAfter, keyArg, actorArg, ctxJSON), out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func keyPtr(key string) *string {
	if key == "" {
		return nil
	}
	return &key
}

func (r *creditRepository) Debit(ctx context.Context, orgID uuid.UUID, d models.CreditDebit) (models.Micro, *models.CreditTransaction, bool, error) {
	if d.Amount <= 0 {
		return 0, nil, false, errors.New("debit amount must be positive")
	}
	key := scopeKey(orgID, d.IdempotencyKey)

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return 0, nil, false, err
	}
	defer tx.Rollback(ctx)

	if key != "" {
		existing, err := replayByKey(ctx, tx, key)
		if err != nil {
			return 0, nil, false, err
		}
		if existing != nil {
			if cerr := tx.Commit(ctx); cerr != nil {
				return 0, nil, false, cerr
			}
			return existing.MonthlyAfter + existing.PurchasedAfter, existing, true, nil
		}
	}

	// The row lock serializes concurrent debits; a missing ledger is an empty one.
	var monthly, purchased models.Micro
	err = tx.QueryRow(ctx, `SELECT balance_micro, purchased_micro FROM credit_ledger WHERE org_id = $1 FOR UPDATE`, orgID).
		Scan(&monthly, &purchased)
	if errors.Is(err, pgx.ErrNoRows) {
		if !d.Clamp {
			return 0, nil, false, ErrInsufficientCredits
		}
		return 0, nil, false, tx.Commit(ctx)
	}
	if err != nil {
		return 0, nil, false, err
	}

	take := d.Amount
	if monthly+purchased < take {
		if !d.Clamp {
			return 0, nil, false, ErrInsufficientCredits
		}
		take = monthly + purchased
	}
	if take == 0 {
		return monthly + purchased, nil, false, tx.Commit(ctx)
	}

	fromMonthly := min(take, monthly)
	fromPurchased := take - fromMonthly
	monthly -= fromMonthly
	purchased -= fromPurchased

	if _, err := tx.Exec(ctx, `
		UPDATE credit_ledger SET balance_micro = $2, purchased_micro = $3, updated_at = now() WHERE org_id = $1
	`, orgID, monthly, purchased); err != nil {
		return 0, nil, false, err
	}

	txn, err := insertTxn(ctx, tx, orgID, models.CreditTransaction{
		Amount: -take, Reason: d.Reason, ModelUsed: d.Model,
		InputTokens: d.InputTokens, OutputTokens: d.OutputTokens, Quantity: d.Quantity, Cost: d.Cost,
		MonthlyAfter: monthly, PurchasedDelta: -fromPurchased, PurchasedAfter: purchased,
		IdempotencyKey: keyPtr(key),
	})
	if err != nil {
		return 0, nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, nil, false, err
	}
	return monthly + purchased, txn, false, nil
}

func (r *creditRepository) RefundSpend(ctx context.Context, orgID uuid.UUID, spendKey, refundKey, reason string) (models.Micro, models.Micro, error) {
	spendKey, refundKey = scopeKey(orgID, spendKey), scopeKey(orgID, refundKey)
	if spendKey == "" || refundKey == "" {
		return 0, 0, errors.New("refund needs both keys")
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	if existing, err := replayByKey(ctx, tx, refundKey); err != nil {
		return 0, 0, err
	} else if existing != nil {
		return existing.Amount, existing.MonthlyAfter + existing.PurchasedAfter, tx.Commit(ctx)
	}
	spend, err := replayByKey(ctx, tx, spendKey)
	if err != nil {
		return 0, 0, err
	}
	if spend == nil || spend.OrgID != orgID || spend.Amount >= 0 {
		return 0, 0, tx.Commit(ctx)
	}
	var resetAt time.Time
	if err := tx.QueryRow(ctx, `SELECT month_reset_at FROM credit_ledger WHERE org_id = $1 FOR UPDATE`, orgID).Scan(&resetAt); err != nil {
		return 0, 0, err
	}
	toPurchased := min(-spend.PurchasedDelta, -spend.Amount)
	toMonthly := -spend.Amount - toPurchased
	if resetAt.After(spend.CreatedAt) {
		toMonthly = 0
	}
	total := toMonthly + toPurchased
	if total == 0 {
		return 0, 0, tx.Commit(ctx)
	}

	var monthly, purchased models.Micro
	if err := tx.QueryRow(ctx, `
		UPDATE credit_ledger SET balance_micro = balance_micro + $2, purchased_micro = purchased_micro + $3, updated_at = now()
		WHERE org_id = $1
		RETURNING balance_micro, purchased_micro
	`, orgID, toMonthly, toPurchased).Scan(&monthly, &purchased); err != nil {
		return 0, 0, err
	}
	if _, err := insertTxn(ctx, tx, orgID, models.CreditTransaction{
		Amount: total, Reason: reason,
		MonthlyAfter: monthly, PurchasedDelta: toPurchased, PurchasedAfter: purchased,
		IdempotencyKey: keyPtr(refundKey),
	}); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return total, monthly + purchased, nil
}

// A keyed refund names its charge (RefundSpend writes "<charge key>:refund"),
// so a spend window nets a refund against the charge it returns, in the window
// the charge fell in, however much later the refund came.
const (
	debitMicro = `COALESCE(d.amount_micro, d.amount::bigint * legacy_credit_micro())`
	refundOf   = `LEFT JOIN credit_ledger_transactions rf
		ON d.idempotency_key IS NOT NULL AND rf.idempotency_key = d.idempotency_key || ':refund'`
	netSpend = `GREATEST(-` + debitMicro + ` - COALESCE(rf.amount_micro, rf.amount::bigint * legacy_credit_micro(), 0), 0)`
)

func (r *creditRepository) SpentInWindows(ctx context.Context, orgID uuid.UUID, dayStart, weekStart, monthStart time.Time) (models.Micro, models.Micro, models.Micro, error) {
	var day, week, month models.Micro
	err := r.DB.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(`+netSpend+`) FILTER (WHERE d.created_at >= $2), 0),
			COALESCE(SUM(`+netSpend+`) FILTER (WHERE d.created_at >= $3), 0),
			COALESCE(SUM(`+netSpend+`) FILTER (WHERE d.created_at >= $4), 0)
		FROM credit_ledger_transactions d `+refundOf+`
		WHERE d.org_id = $1 AND `+debitMicro+` < 0 AND d.created_at >= LEAST($2, $3, $4)
	`, orgID, dayStart, weekStart, monthStart).Scan(&day, &week, &month)
	return day, week, month, err
}

func (r *creditRepository) MemberSpentInWindows(ctx context.Context, orgID, userID uuid.UUID, dayStart, weekStart, monthStart time.Time) (models.Micro, models.Micro, models.Micro, error) {
	var day, week, month models.Micro
	err := r.DB.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(`+netSpend+`) FILTER (WHERE d.created_at >= $3), 0),
			COALESCE(SUM(`+netSpend+`) FILTER (WHERE d.created_at >= $4), 0),
			COALESCE(SUM(`+netSpend+`) FILTER (WHERE d.created_at >= $5), 0)
		FROM credit_ledger_transactions d `+refundOf+`
		WHERE d.org_id = $1 AND d.actor_user_id = $2 AND `+debitMicro+` < 0 AND d.created_at >= LEAST($3, $4, $5)
	`, orgID, userID, dayStart, weekStart, monthStart).Scan(&day, &week, &month)
	return day, week, month, err
}

const txnTokens = `CASE WHEN input_tokens + output_tokens = 0 THEN tokens_used ELSE input_tokens + output_tokens END`

func (r *creditRepository) UsageDaily(ctx context.Context, orgID uuid.UUID, since time.Time) ([]models.CreditUsagePoint, error) {
	rows, err := r.DB.Query(ctx, `
		SELECT to_char(date_trunc('day', created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD'),
		       COALESCE(SUM(-COALESCE(amount_micro, amount::bigint * legacy_credit_micro())), 0),
		       COALESCE(SUM(`+txnTokens+`), 0)
		FROM credit_ledger_transactions
		WHERE org_id = $1 AND COALESCE(amount_micro, amount::bigint) < 0 AND created_at >= $2
		GROUP BY 1 ORDER BY 1
	`, orgID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.CreditUsagePoint, 0)
	for rows.Next() {
		var p models.CreditUsagePoint
		if err := rows.Scan(&p.Date, &p.Amount, &p.Tokens); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *creditRepository) UsageBreakdown(ctx context.Context, orgID uuid.UUID, since time.Time, by string) ([]models.CreditUsageBucket, error) {
	col := "reason"
	if by == "model" {
		col = "model_used"
	}
	rows, err := r.DB.Query(ctx, `
		SELECT `+col+`, COALESCE(SUM(-COALESCE(amount_micro, amount::bigint * legacy_credit_micro())), 0),
		       COALESCE(SUM(`+txnTokens+`), 0), COUNT(*)
		FROM credit_ledger_transactions
		WHERE org_id = $1 AND COALESCE(amount_micro, amount::bigint) < 0 AND created_at >= $2
		GROUP BY 1 ORDER BY 2 DESC
	`, orgID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.CreditUsageBucket, 0)
	for rows.Next() {
		var b models.CreditUsageBucket
		if err := rows.Scan(&b.Key, &b.Amount, &b.Tokens, &b.Count); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (r *creditRepository) Grant(ctx context.Context, orgID uuid.UUID, amount models.Micro, reason, idempotencyKey string) (models.Micro, *models.CreditTransaction, error) {
	return r.grant(ctx, orgID, amount, reason, idempotencyKey, false)
}

func (r *creditRepository) GrantPurchased(ctx context.Context, orgID uuid.UUID, amount models.Micro, reason, idempotencyKey string) (models.Micro, *models.CreditTransaction, error) {
	return r.grant(ctx, orgID, amount, reason, idempotencyKey, true)
}

func (r *creditRepository) grant(ctx context.Context, orgID uuid.UUID, amount models.Micro, reason, idempotencyKey string, purchased bool) (models.Micro, *models.CreditTransaction, error) {
	if amount <= 0 {
		return 0, nil, errors.New("grant amount must be positive")
	}
	key := scopeKey(orgID, idempotencyKey)

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback(ctx)

	if key != "" {
		existing, err := replayByKey(ctx, tx, key)
		if err != nil {
			return 0, nil, err
		}
		if existing != nil {
			if cerr := tx.Commit(ctx); cerr != nil {
				return 0, nil, cerr
			}
			return existing.MonthlyAfter + existing.PurchasedAfter, existing, nil
		}
	}

	query := `
		INSERT INTO credit_ledger (org_id, balance_micro) VALUES ($1, $2)
		ON CONFLICT (org_id) DO UPDATE
			SET balance_micro = credit_ledger.balance_micro + EXCLUDED.balance_micro, updated_at = now()
		RETURNING balance_micro, purchased_micro`
	if purchased {
		query = `
		INSERT INTO credit_ledger (org_id, purchased_micro, total_purchased_micro) VALUES ($1, $2, $2)
		ON CONFLICT (org_id) DO UPDATE
			SET purchased_micro       = credit_ledger.purchased_micro + EXCLUDED.purchased_micro,
			    total_purchased_micro = credit_ledger.total_purchased_micro + EXCLUDED.total_purchased_micro,
			    updated_at = now()
		RETURNING balance_micro, purchased_micro`
	}

	var monthly, purch models.Micro
	if err := tx.QueryRow(ctx, query, orgID, amount).Scan(&monthly, &purch); err != nil {
		return 0, nil, err
	}
	var purchasedDelta models.Micro
	if purchased {
		purchasedDelta = amount
	}
	txn, err := insertTxn(ctx, tx, orgID, models.CreditTransaction{
		Amount: amount, Reason: reason,
		MonthlyAfter: monthly, PurchasedDelta: purchasedDelta, PurchasedAfter: purch,
		IdempotencyKey: keyPtr(key),
	})
	if err != nil {
		return 0, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, nil, err
	}
	return monthly + purch, txn, nil
}

func (r *creditRepository) ResetMonthly(ctx context.Context, orgID uuid.UUID, allowance models.Micro, idempotencyKey string) (*models.CreditLedger, error) {
	if allowance < 0 {
		return nil, errors.New("allowance must be non-negative")
	}
	key := scopeKey(orgID, idempotencyKey)

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if key != "" {
		existing, err := replayByKey(ctx, tx, key)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			if cerr := tx.Commit(ctx); cerr != nil {
				return nil, cerr
			}
			return r.GetBalance(ctx, orgID)
		}
	}

	// Create the row first so the FOR UPDATE below locks a real row and no
	// concurrent grant slips in between reading and resetting the pool.
	if _, err := tx.Exec(ctx, `INSERT INTO credit_ledger (org_id) VALUES ($1) ON CONFLICT (org_id) DO NOTHING`, orgID); err != nil {
		return nil, err
	}
	var prev models.Micro
	if err := tx.QueryRow(ctx, `SELECT balance_micro FROM credit_ledger WHERE org_id = $1 FOR UPDATE`, orgID).Scan(&prev); err != nil {
		return nil, err
	}

	l := &models.CreditLedger{}
	err = scanLedger(tx.QueryRow(ctx, `
		UPDATE credit_ledger
			SET balance_micro = $2, month_reset_at = now(), updated_at = now()
		WHERE org_id = $1
		RETURNING `+creditLedgerCols, orgID, allowance), l)
	if err != nil {
		return nil, err
	}
	if _, err := insertTxn(ctx, tx, orgID, models.CreditTransaction{
		Amount: allowance - prev, Reason: "monthly_reset",
		MonthlyAfter: l.Monthly, PurchasedAfter: l.Purchased,
		IdempotencyKey: keyPtr(key),
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return l, nil
}

func (r *creditRepository) ListTransactions(ctx context.Context, orgID uuid.UUID, limit int) ([]models.CreditTransaction, error) {
	return r.ListTransactionsBefore(ctx, orgID, limit, time.Time{}, uuid.Nil)
}

func (r *creditRepository) ListTransactionsBefore(ctx context.Context, orgID uuid.UUID, limit int, beforeCreatedAt time.Time, beforeID uuid.UUID) ([]models.CreditTransaction, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `
		SELECT ` + creditTxnCols + `
		FROM credit_ledger_transactions
		WHERE org_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2`
	args := []any{orgID, limit}
	if !beforeCreatedAt.IsZero() {
		query = `
		SELECT ` + creditTxnCols + `
		FROM credit_ledger_transactions
		WHERE org_id = $1 AND (created_at, id) < ($3, $4)
		ORDER BY created_at DESC, id DESC
		LIMIT $2`
		args = append(args, beforeCreatedAt, beforeID)
	}
	rows, err := r.DB.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.CreditTransaction, 0)
	for rows.Next() {
		var t models.CreditTransaction
		if err := scanTxn(rows, &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// AnnualAllowance is one subscription billed over more than a month.
type AnnualAllowance struct {
	OrgID       uuid.UUID
	PeriodStart time.Time
	Allowance   models.Micro
	LastReset   time.Time
}

func (r *creditRepository) AnnualAllowancesDue(ctx context.Context, limit int) ([]AnnualAllowance, error) {
	rows, err := r.DB.Query(ctx, `
		SELECT s.organization_id, s.current_period_start, p.monthly_allowance_micro,
		       COALESCE(l.month_reset_at, 'epoch'::timestamptz)
		FROM subscriptions s
		JOIN plans p ON p.id = s.plan_id
		LEFT JOIN credit_ledger l ON l.org_id = s.organization_id
		WHERE s.status = 'active'
		  AND s.current_period_start IS NOT NULL
		  AND s.current_period_end > now()
		  AND s.current_period_end - s.current_period_start > interval '35 days'
		  AND p.monthly_allowance_micro > 0
		  AND COALESCE(l.month_reset_at, 'epoch'::timestamptz) < now() - interval '27 days'
		ORDER BY COALESCE(l.month_reset_at, 'epoch'::timestamptz), s.organization_id
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AnnualAllowance
	for rows.Next() {
		var a AnnualAllowance
		if err := rows.Scan(&a.OrgID, &a.PeriodStart, &a.Allowance, &a.LastReset); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
