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

// ErrInsufficientCredits is returned by Consume when the org's combined
// balance (monthly + purchased) is lower than the requested amount. Callers
// map this to a 402.
var ErrInsufficientCredits = errors.New("insufficient credits")

// CreditRepository persists the AI-credit ledger and its append-only
// transaction log. The ledger has two pools: `balance` (monthly allowance,
// reset each billing cycle) and `purchased_balance` (top-ups, never expire).
// All balance mutations run inside a transaction that row-locks the ledger
// (SELECT ... FOR UPDATE) and are backstopped by DB CHECK constraints, so
// neither pool can go negative under concurrency — this is the
// billing-correctness boundary.
type CreditRepository interface {
	// GetBalance returns the ledger for an org, or nil if it has none yet.
	GetBalance(ctx context.Context, orgID uuid.UUID) (*models.CreditLedger, error)

	// EnsureLedger creates the org's ledger row if absent (idempotent), then
	// returns it.
	EnsureLedger(ctx context.Context, orgID uuid.UUID) (*models.CreditLedger, error)

	// Consume atomically debits `amount` credits from the org, draining the
	// monthly pool first, then purchased. Returns the resulting combined
	// balance, the recorded transaction, and whether this was an idempotent
	// replay (true = no new debit happened, the prior transaction was returned).
	//
	// If idempotencyKey is non-empty and a transaction already exists for it,
	// Consume is a no-op debit and returns that prior transaction with
	// replayed=true (so retries are safe). If the combined balance is too low,
	// it returns ErrInsufficientCredits and debits nothing.
	Consume(ctx context.Context, orgID uuid.UUID, amount int, reason, model string, tokens int, idempotencyKey string) (balance int, txn *models.CreditTransaction, replayed bool, err error)

	// Grant atomically credits `amount` to the org's monthly pool (creating
	// the ledger if needed). Used for refunds and the one-time trial grant.
	// idempotencyKey may be empty; when set, a replay does not double-grant.
	Grant(ctx context.Context, orgID uuid.UUID, amount int, reason, idempotencyKey string) (int, *models.CreditTransaction, error)

	// GrantPurchased credits `amount` to the org's purchased pool and bumps
	// total_purchased. Used for top-up fulfillment; idempotencyKey (the Stripe
	// event id) makes webhook retries safe.
	GrantPurchased(ctx context.Context, orgID uuid.UUID, amount int, reason, idempotencyKey string) (int, *models.CreditTransaction, error)

	// ResetMonthly sets the monthly pool to `allowance` (set-to-N semantics,
	// purchased pool untouched) and stamps month_reset_at. The transaction row
	// records the signed delta. idempotencyKey (the Stripe event id) makes
	// webhook retries safe.
	ResetMonthly(ctx context.Context, orgID uuid.UUID, allowance int, idempotencyKey string) (*models.CreditLedger, error)

	// ConsumeAtMost debits up to `amount` credits, draining whatever the org
	// still has (possibly zero) instead of failing on a low balance. Used to
	// settle metered usage AFTER an AI result was already delivered: the
	// overage must never fail the delivered work, so it drains to zero at
	// worst. Returns the credits actually consumed and the resulting balance.
	// Same idempotency semantics as Consume.
	ConsumeAtMost(ctx context.Context, orgID uuid.UUID, amount int, reason, model string, tokens int, idempotencyKey string) (consumed, balance int, replayed bool, err error)

	// SpentInWindows sums debited credits since each of the three window
	// starts (calendar day / ISO week / calendar month, all UTC).
	SpentInWindows(ctx context.Context, orgID uuid.UUID, dayStart, weekStart, monthStart time.Time) (day, week, month int, err error)

	// UsageDaily returns per-UTC-day debit totals since `since`, oldest first.
	UsageDaily(ctx context.Context, orgID uuid.UUID, since time.Time) ([]models.CreditUsagePoint, error)

	// UsageBreakdown groups debits since `since` by "reason" or "model",
	// biggest spender first.
	UsageBreakdown(ctx context.Context, orgID uuid.UUID, since time.Time, by string) ([]models.CreditUsageBucket, error)

	// CountGrantsSince counts grant transactions with the given reason since
	// `since` (bounds auto top-up purchases per month).
	CountGrantsSince(ctx context.Context, orgID uuid.UUID, reason string, since time.Time) (int, error)

	// ListTransactions returns the org's transaction history, newest first.
	ListTransactions(ctx context.Context, orgID uuid.UUID, limit int) ([]models.CreditTransaction, error)

	// ListTransactionsBefore keyset-paginates the history: rows strictly older
	// than (beforeCreatedAt, beforeID), newest first. Pass zero values for the
	// first page.
	ListTransactionsBefore(ctx context.Context, orgID uuid.UUID, limit int, beforeCreatedAt time.Time, beforeID uuid.UUID) ([]models.CreditTransaction, error)
}

type creditRepository struct {
	DB *db.DB
}

func NewCreditRepository(database *db.DB) CreditRepository {
	return &creditRepository{DB: database}
}

const creditLedgerCols = `org_id, balance, purchased_balance, month_reset_at, total_purchased, created_at, updated_at`

func scanLedger(row pgx.Row, l *models.CreditLedger) error {
	return row.Scan(&l.OrgID, &l.Balance, &l.PurchasedBalance, &l.MonthResetAt, &l.TotalPurchased, &l.CreatedAt, &l.UpdatedAt)
}

const creditTxnCols = `id, org_id, amount, reason, model_used, tokens_used, balance_after, purchased_delta, purchased_balance_after, idempotency_key, actor_user_id, context, created_at`

func scanTxn(row pgx.Row, t *models.CreditTransaction) error {
	var rawCtx []byte
	if err := row.Scan(
		&t.ID, &t.OrgID, &t.Amount, &t.Reason, &t.ModelUsed,
		&t.TokensUsed, &t.BalanceAfter, &t.PurchasedDelta, &t.PurchasedBalanceAfter,
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

// scopeKey namespaces a client-supplied idempotency key with the org id, so a
// key value from one tenant can never replay against (or return) another
// tenant's ledger entry. Empty stays empty (non-idempotent). Server-generated
// keys (Stripe event ids, "research:<uuid>") are already unique but are scoped
// too for uniformity.
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
// site attached it via models.WithCreditMeta.
func insertTxn(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, amount int, reason, model string, tokens, balanceAfter, purchasedDelta, purchasedAfter int, idempotencyKey string) (*models.CreditTransaction, error) {
	var keyArg *string
	if idempotencyKey != "" {
		keyArg = &idempotencyKey
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
	txn := &models.CreditTransaction{}
	err := scanTxn(tx.QueryRow(ctx, `
		INSERT INTO credit_ledger_transactions
			(org_id, amount, reason, model_used, tokens_used, balance_after, purchased_delta, purchased_balance_after, idempotency_key, actor_user_id, context)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING `+creditTxnCols,
		orgID, amount, reason, model, tokens, balanceAfter, purchasedDelta, purchasedAfter, keyArg, actorArg, ctxJSON), txn)
	if err != nil {
		return nil, err
	}
	return txn, nil
}

func (r *creditRepository) Consume(ctx context.Context, orgID uuid.UUID, amount int, reason, model string, tokens int, idempotencyKey string) (int, *models.CreditTransaction, bool, error) {
	if amount <= 0 {
		return 0, nil, false, errors.New("consume amount must be positive")
	}
	idempotencyKey = scopeKey(orgID, idempotencyKey)

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return 0, nil, false, err
	}
	defer tx.Rollback(ctx)

	// Idempotency short-circuit: if this key already produced a transaction,
	// return it unchanged rather than debiting again.
	if idempotencyKey != "" {
		existing, err := replayByKey(ctx, tx, idempotencyKey)
		if err != nil {
			return 0, nil, false, err
		}
		if existing != nil {
			if cerr := tx.Commit(ctx); cerr != nil {
				return 0, nil, false, cerr
			}
			return existing.BalanceAfter + existing.PurchasedBalanceAfter, existing, true, nil
		}
	}

	// Row-lock the ledger, then debit monthly first and purchased for the
	// remainder. The lock serializes concurrent debits; the CHECK constraints
	// backstop the invariant. A missing ledger row is treated as insufficient.
	var monthly, purchased int
	err = tx.QueryRow(ctx, `SELECT balance, purchased_balance FROM credit_ledger WHERE org_id = $1 FOR UPDATE`, orgID).
		Scan(&monthly, &purchased)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil, false, ErrInsufficientCredits
	}
	if err != nil {
		return 0, nil, false, err
	}
	if monthly+purchased < amount {
		return 0, nil, false, ErrInsufficientCredits
	}

	monthlyUsed := amount
	if monthlyUsed > monthly {
		monthlyUsed = monthly
	}
	purchasedUsed := amount - monthlyUsed
	newMonthly := monthly - monthlyUsed
	newPurchased := purchased - purchasedUsed

	if _, err := tx.Exec(ctx, `
		UPDATE credit_ledger SET balance = $2, purchased_balance = $3, updated_at = now() WHERE org_id = $1
	`, orgID, newMonthly, newPurchased); err != nil {
		return 0, nil, false, err
	}

	txn, err := insertTxn(ctx, tx, orgID, -amount, reason, model, tokens, newMonthly, -purchasedUsed, newPurchased, idempotencyKey)
	if err != nil {
		return 0, nil, false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, nil, false, err
	}
	return newMonthly + newPurchased, txn, false, nil
}

func (r *creditRepository) ConsumeAtMost(ctx context.Context, orgID uuid.UUID, amount int, reason, model string, tokens int, idempotencyKey string) (int, int, bool, error) {
	if amount <= 0 {
		return 0, 0, false, errors.New("consume amount must be positive")
	}
	idempotencyKey = scopeKey(orgID, idempotencyKey)

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return 0, 0, false, err
	}
	defer tx.Rollback(ctx)

	if idempotencyKey != "" {
		existing, err := replayByKey(ctx, tx, idempotencyKey)
		if err != nil {
			return 0, 0, false, err
		}
		if existing != nil {
			if cerr := tx.Commit(ctx); cerr != nil {
				return 0, 0, false, cerr
			}
			return -existing.Amount, existing.BalanceAfter + existing.PurchasedBalanceAfter, true, nil
		}
	}

	var monthly, purchased int
	err = tx.QueryRow(ctx, `SELECT balance, purchased_balance FROM credit_ledger WHERE org_id = $1 FOR UPDATE`, orgID).
		Scan(&monthly, &purchased)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, false, nil // no ledger: nothing to drain
	}
	if err != nil {
		return 0, 0, false, err
	}

	// Drain up to `amount`: everything the org still has when short.
	take := amount
	if avail := monthly + purchased; take > avail {
		take = avail
	}
	if take == 0 {
		if cerr := tx.Commit(ctx); cerr != nil {
			return 0, 0, false, cerr
		}
		return 0, 0, false, nil
	}

	monthlyUsed := take
	if monthlyUsed > monthly {
		monthlyUsed = monthly
	}
	purchasedUsed := take - monthlyUsed
	newMonthly := monthly - monthlyUsed
	newPurchased := purchased - purchasedUsed

	if _, err := tx.Exec(ctx, `
		UPDATE credit_ledger SET balance = $2, purchased_balance = $3, updated_at = now() WHERE org_id = $1
	`, orgID, newMonthly, newPurchased); err != nil {
		return 0, 0, false, err
	}
	if _, err := insertTxn(ctx, tx, orgID, -take, reason, model, tokens, newMonthly, -purchasedUsed, newPurchased, idempotencyKey); err != nil {
		return 0, 0, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, false, err
	}
	return take, newMonthly + newPurchased, false, nil
}

func (r *creditRepository) SpentInWindows(ctx context.Context, orgID uuid.UUID, dayStart, weekStart, monthStart time.Time) (int, int, int, error) {
	var day, week, month int
	err := r.DB.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(-amount) FILTER (WHERE created_at >= $2), 0),
			COALESCE(SUM(-amount) FILTER (WHERE created_at >= $3), 0),
			COALESCE(SUM(-amount) FILTER (WHERE created_at >= $4), 0)
		FROM credit_ledger_transactions
		WHERE org_id = $1 AND amount < 0 AND created_at >= LEAST($2, $3, $4)
	`, orgID, dayStart, weekStart, monthStart).Scan(&day, &week, &month)
	if err != nil {
		return 0, 0, 0, err
	}
	return day, week, month, nil
}

func (r *creditRepository) UsageDaily(ctx context.Context, orgID uuid.UUID, since time.Time) ([]models.CreditUsagePoint, error) {
	rows, err := r.DB.Query(ctx, `
		SELECT to_char(date_trunc('day', created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD'),
		       COALESCE(SUM(-amount), 0), COALESCE(SUM(tokens_used), 0)
		FROM credit_ledger_transactions
		WHERE org_id = $1 AND amount < 0 AND created_at >= $2
		GROUP BY 1 ORDER BY 1
	`, orgID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.CreditUsagePoint, 0)
	for rows.Next() {
		var p models.CreditUsagePoint
		if err := rows.Scan(&p.Date, &p.Credits, &p.Tokens); err != nil {
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
		SELECT `+col+`, COALESCE(SUM(-amount), 0), COALESCE(SUM(tokens_used), 0), COUNT(*)
		FROM credit_ledger_transactions
		WHERE org_id = $1 AND amount < 0 AND created_at >= $2
		GROUP BY 1 ORDER BY 2 DESC
	`, orgID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.CreditUsageBucket, 0)
	for rows.Next() {
		var b models.CreditUsageBucket
		if err := rows.Scan(&b.Key, &b.Credits, &b.Tokens, &b.Count); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (r *creditRepository) CountGrantsSince(ctx context.Context, orgID uuid.UUID, reason string, since time.Time) (int, error) {
	var n int
	err := r.DB.QueryRow(ctx, `
		SELECT COUNT(*) FROM credit_ledger_transactions
		WHERE org_id = $1 AND reason = $2 AND amount > 0 AND created_at >= $3
	`, orgID, reason, since).Scan(&n)
	return n, err
}

func (r *creditRepository) Grant(ctx context.Context, orgID uuid.UUID, amount int, reason, idempotencyKey string) (int, *models.CreditTransaction, error) {
	return r.grant(ctx, orgID, amount, reason, idempotencyKey, false)
}

func (r *creditRepository) GrantPurchased(ctx context.Context, orgID uuid.UUID, amount int, reason, idempotencyKey string) (int, *models.CreditTransaction, error) {
	return r.grant(ctx, orgID, amount, reason, idempotencyKey, true)
}

// grant credits `amount` to one pool. purchased=true also bumps
// total_purchased (lifetime stat).
func (r *creditRepository) grant(ctx context.Context, orgID uuid.UUID, amount int, reason, idempotencyKey string, purchased bool) (int, *models.CreditTransaction, error) {
	if amount <= 0 {
		return 0, nil, errors.New("grant amount must be positive")
	}
	idempotencyKey = scopeKey(orgID, idempotencyKey)

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback(ctx)

	if idempotencyKey != "" {
		existing, err := replayByKey(ctx, tx, idempotencyKey)
		if err != nil {
			return 0, nil, err
		}
		if existing != nil {
			if cerr := tx.Commit(ctx); cerr != nil {
				return 0, nil, cerr
			}
			return existing.BalanceAfter + existing.PurchasedBalanceAfter, existing, nil
		}
	}

	query := `
		INSERT INTO credit_ledger (org_id, balance) VALUES ($1, $2)
		ON CONFLICT (org_id) DO UPDATE
			SET balance = credit_ledger.balance + EXCLUDED.balance, updated_at = now()
		RETURNING balance, purchased_balance`
	if purchased {
		query = `
		INSERT INTO credit_ledger (org_id, purchased_balance, total_purchased) VALUES ($1, $2, $2)
		ON CONFLICT (org_id) DO UPDATE
			SET purchased_balance = credit_ledger.purchased_balance + EXCLUDED.purchased_balance,
			    total_purchased   = credit_ledger.total_purchased + EXCLUDED.total_purchased,
			    updated_at = now()
		RETURNING balance, purchased_balance`
	}

	var monthly, purch int
	if err := tx.QueryRow(ctx, query, orgID, amount).Scan(&monthly, &purch); err != nil {
		return 0, nil, err
	}

	purchasedDelta := 0
	if purchased {
		purchasedDelta = amount
	}
	txn, err := insertTxn(ctx, tx, orgID, amount, reason, "", 0, monthly, purchasedDelta, purch, idempotencyKey)
	if err != nil {
		return 0, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, nil, err
	}
	return monthly + purch, txn, nil
}

func (r *creditRepository) ResetMonthly(ctx context.Context, orgID uuid.UUID, allowance int, idempotencyKey string) (*models.CreditLedger, error) {
	if allowance < 0 {
		return nil, errors.New("allowance must be non-negative")
	}
	idempotencyKey = scopeKey(orgID, idempotencyKey)

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if idempotencyKey != "" {
		existing, err := replayByKey(ctx, tx, idempotencyKey)
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

	// Ensure the ledger row exists FIRST, so the subsequent FOR UPDATE actually
	// locks a real row. A FOR UPDATE against a non-existent row locks nothing,
	// which would let a concurrent insert/grant slip in between reading the
	// pre-reset balance and applying the reset, making the logged signed delta
	// wrong (the balances stay correct because the reset is set-to-N, but the
	// append-only trail's running sum would drift).
	if _, err := tx.Exec(ctx, `INSERT INTO credit_ledger (org_id) VALUES ($1) ON CONFLICT (org_id) DO NOTHING`, orgID); err != nil {
		return nil, err
	}

	// Lock the row and read the pre-reset balance so the log records the exact
	// signed delta. A delta can be negative (downgrade) or zero; the trail still
	// sums to the pool balances.
	var prevBalance int
	if err := tx.QueryRow(ctx, `SELECT balance FROM credit_ledger WHERE org_id = $1 FOR UPDATE`, orgID).Scan(&prevBalance); err != nil {
		return nil, err
	}

	l := &models.CreditLedger{}
	err = scanLedger(tx.QueryRow(ctx, `
		UPDATE credit_ledger
			SET balance = $2, month_reset_at = now(), updated_at = now()
		WHERE org_id = $1
		RETURNING `+creditLedgerCols, orgID, allowance), l)
	if err != nil {
		return nil, err
	}
	if _, err := insertTxn(ctx, tx, orgID, allowance-prevBalance, "monthly_reset", "", 0, l.Balance, 0, l.PurchasedBalance, idempotencyKey); err != nil {
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
