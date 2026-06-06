package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// ErrInsufficientCredits is returned by Consume when the org's balance is lower
// than the requested amount. Callers map this to a 402.
var ErrInsufficientCredits = errors.New("insufficient credits")

// CreditRepository persists the AI-credit ledger and its append-only
// transaction log. All balance mutations are performed with atomic, conditional
// SQL so the balance can never go negative under concurrency — this is the
// billing-correctness boundary and must not be reimplemented as an
// application-level read-modify-write.
type CreditRepository interface {
	// GetBalance returns the ledger for an org, or nil if it has none yet.
	GetBalance(ctx context.Context, orgID uuid.UUID) (*models.CreditLedger, error)

	// EnsureLedger creates the org's ledger row if absent (idempotent), then
	// returns it.
	EnsureLedger(ctx context.Context, orgID uuid.UUID) (*models.CreditLedger, error)

	// Consume atomically debits `amount` credits from the org, writing a
	// negative transaction row in the same transaction. Returns the resulting
	// balance, the recorded transaction, and whether this was an idempotent
	// replay (true = no new debit happened, the prior transaction was returned).
	//
	// If idempotencyKey is non-empty and a transaction already exists for it,
	// Consume is a no-op debit and returns that prior transaction with
	// replayed=true (so retries are safe). If the balance is too low, it returns
	// ErrInsufficientCredits and debits nothing.
	Consume(ctx context.Context, orgID uuid.UUID, amount int, reason, model string, tokens int, idempotencyKey string) (balance int, txn *models.CreditTransaction, replayed bool, err error)

	// Grant atomically credits `amount` to the org (creating the ledger if
	// needed) and writes a positive transaction row. Used for monthly plan
	// grants and credit purchases.
	Grant(ctx context.Context, orgID uuid.UUID, amount int, reason string) (int, *models.CreditTransaction, error)

	// ListTransactions returns the org's transaction history, newest first.
	ListTransactions(ctx context.Context, orgID uuid.UUID, limit int) ([]models.CreditTransaction, error)
}

type creditRepository struct {
	DB *db.DB
}

func NewCreditRepository(database *db.DB) CreditRepository {
	return &creditRepository{DB: database}
}

const creditLedgerCols = `org_id, balance, month_reset_at, total_purchased, created_at, updated_at`

func scanLedger(row pgx.Row, l *models.CreditLedger) error {
	return row.Scan(&l.OrgID, &l.Balance, &l.MonthResetAt, &l.TotalPurchased, &l.CreatedAt, &l.UpdatedAt)
}

const creditTxnCols = `id, org_id, amount, reason, model_used, tokens_used, balance_after, idempotency_key, created_at`

func scanTxn(row pgx.Row, t *models.CreditTransaction) error {
	return row.Scan(
		&t.ID, &t.OrgID, &t.Amount, &t.Reason, &t.ModelUsed,
		&t.TokensUsed, &t.BalanceAfter, &t.IdempotencyKey, &t.CreatedAt,
	)
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

func (r *creditRepository) Consume(ctx context.Context, orgID uuid.UUID, amount int, reason, model string, tokens int, idempotencyKey string) (int, *models.CreditTransaction, bool, error) {
	if amount <= 0 {
		return 0, nil, false, errors.New("consume amount must be positive")
	}

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return 0, nil, false, err
	}
	defer tx.Rollback(ctx)

	// Idempotency short-circuit: if this key already produced a transaction,
	// return it unchanged rather than debiting again.
	if idempotencyKey != "" {
		existing := &models.CreditTransaction{}
		err := scanTxn(tx.QueryRow(ctx,
			`SELECT `+creditTxnCols+` FROM credit_ledger_transactions WHERE idempotency_key = $1`, idempotencyKey), existing)
		if err == nil {
			if cerr := tx.Commit(ctx); cerr != nil {
				return 0, nil, false, cerr
			}
			return existing.BalanceAfter, existing, true, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return 0, nil, false, err
		}
	}

	// Atomic conditional debit. The WHERE balance >= amount guard means a
	// concurrent request can never drive the balance below zero, and a missing
	// ledger row yields no rows (treated as insufficient).
	var newBalance int
	err = tx.QueryRow(ctx, `
		UPDATE credit_ledger
		SET balance = balance - $2, updated_at = now()
		WHERE org_id = $1 AND balance >= $2
		RETURNING balance
	`, orgID, amount).Scan(&newBalance)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil, false, ErrInsufficientCredits
	}
	if err != nil {
		return 0, nil, false, err
	}

	var keyArg *string
	if idempotencyKey != "" {
		keyArg = &idempotencyKey
	}
	txn := &models.CreditTransaction{}
	err = scanTxn(tx.QueryRow(ctx, `
		INSERT INTO credit_ledger_transactions
			(org_id, amount, reason, model_used, tokens_used, balance_after, idempotency_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+creditTxnCols,
		orgID, -amount, reason, model, tokens, newBalance, keyArg), txn)
	if err != nil {
		return 0, nil, false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, nil, false, err
	}
	return newBalance, txn, false, nil
}

func (r *creditRepository) Grant(ctx context.Context, orgID uuid.UUID, amount int, reason string) (int, *models.CreditTransaction, error) {
	if amount <= 0 {
		return 0, nil, errors.New("grant amount must be positive")
	}

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback(ctx)

	var newBalance int
	err = tx.QueryRow(ctx, `
		INSERT INTO credit_ledger (org_id, balance) VALUES ($1, $2)
		ON CONFLICT (org_id) DO UPDATE
			SET balance = credit_ledger.balance + EXCLUDED.balance, updated_at = now()
		RETURNING balance
	`, orgID, amount).Scan(&newBalance)
	if err != nil {
		return 0, nil, err
	}

	txn := &models.CreditTransaction{}
	err = scanTxn(tx.QueryRow(ctx, `
		INSERT INTO credit_ledger_transactions
			(org_id, amount, reason, model_used, tokens_used, balance_after, idempotency_key)
		VALUES ($1, $2, $3, '', 0, $4, NULL)
		RETURNING `+creditTxnCols,
		orgID, amount, reason, newBalance), txn)
	if err != nil {
		return 0, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, nil, err
	}
	return newBalance, txn, nil
}

func (r *creditRepository) ListTransactions(ctx context.Context, orgID uuid.UUID, limit int) ([]models.CreditTransaction, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.DB.Query(ctx, `
		SELECT `+creditTxnCols+`
		FROM credit_ledger_transactions
		WHERE org_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, orgID, limit)
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
