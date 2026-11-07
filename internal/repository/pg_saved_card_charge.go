package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// ErrChargeInFlight is returned when a workspace already has a saved-card
// charge pending; it must settle before another starts.
var ErrChargeInFlight = errors.New("a charge is already in progress")

// SavedCardChargeRepository persists every charge on a workspace's saved card
// (automatic reloads and one-click top-ups) until it reaches a terminal state.
// At most one is pending per workspace.
type SavedCardChargeRepository interface {
	Create(ctx context.Context, c *models.SavedCardCharge) (*models.SavedCardCharge, error)
	Get(ctx context.Context, id uuid.UUID) (*models.SavedCardCharge, error)
	// Pending is the workspace's in-flight charge, or nil.
	Pending(ctx context.Context, orgID uuid.UUID) (*models.SavedCardCharge, error)
	SetInvoice(ctx context.Context, id uuid.UUID, invoiceID string) error
	SetPaymentMethod(ctx context.Context, id uuid.UUID, paymentMethodID string) error
	// BumpTries counts a payment attempt and returns the new count.
	BumpTries(ctx context.Context, id uuid.UUID) (int, error)
	// Snooze schedules the next attempt after a soft decline.
	Snooze(ctx context.Context, id uuid.UUID, retryAt time.Time, failureCode string) error
	// MarkSucceeded settles a pending charge once; false when it already was.
	MarkSucceeded(ctx context.Context, id uuid.UUID) (bool, error)
	MarkFailed(ctx context.Context, id uuid.UUID, failureCode string) error
	// ReloadedSince sums pending and succeeded automatic reloads since a time,
	// which is what the monthly reload limit counts.
	ReloadedSince(ctx context.Context, orgID uuid.UUID, since time.Time) (models.Micro, error)
	// LastSucceeded is the newest settled charge of a kind, or nil.
	LastSucceeded(ctx context.Context, orgID uuid.UUID, kind string) (*models.SavedCardCharge, error)
	// Due lists pending charges whose retry time has come or that have sat
	// untouched longer than staleAfter, oldest first.
	Due(ctx context.Context, now time.Time, staleAfter time.Duration, limit int) ([]models.SavedCardCharge, error)
	// CloseLegacy fails credit-pack charges the previous release started that
	// are still pending since before a time, so they stop holding the slot.
	CloseLegacy(ctx context.Context, createdBefore time.Time, failureCode string) (int64, error)
	// SettleLegacyPaymentIntent settles a credit-pack charge the previous
	// release started on a payment intent.
	SettleLegacyPaymentIntent(ctx context.Context, paymentIntentID string) error
}

type savedCardChargeRepository struct {
	DB *db.DB
}

func NewSavedCardChargeRepository(database *db.DB) SavedCardChargeRepository {
	return &savedCardChargeRepository{DB: database}
}

const savedCardChargeCols = `id, organization_id, kind, amount_micro, amount_cents, currency, status, invoice_id,
	customer_id, payment_method_id, tries, retry_at, failure_code, created_at, completed_at, pack_key <> 'usd'`

func scanSavedCardCharge(row pgx.Row, c *models.SavedCardCharge) error {
	return row.Scan(&c.ID, &c.OrgID, &c.Kind, &c.Amount, &c.AmountCents, &c.Currency, &c.Status, &c.InvoiceID,
		&c.CustomerID, &c.PaymentMethodID, &c.Tries, &c.RetryAt, &c.FailureCode, &c.CreatedAt, &c.CompletedAt, &c.Legacy)
}

func (r *savedCardChargeRepository) one(ctx context.Context, query string, args ...any) (*models.SavedCardCharge, error) {
	c := &models.SavedCardCharge{}
	if err := scanSavedCardCharge(r.DB.QueryRow(ctx, query, args...), c); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return c, nil
}

func (r *savedCardChargeRepository) Create(ctx context.Context, in *models.SavedCardCharge) (*models.SavedCardCharge, error) {
	// pack_key and credits are what the previous release reads.
	c, err := r.one(ctx, `
		INSERT INTO credit_auto_topup_attempts
			(organization_id, kind, pack_key, credits, amount_micro, amount_cents, currency, customer_id, payment_method_id)
		VALUES ($1, $2, 'usd', GREATEST($3 / legacy_credit_micro(), 1), $3, $4, $5, $6, $7)
		RETURNING `+savedCardChargeCols,
		in.OrgID, in.Kind, in.Amount, in.AmountCents, in.Currency, in.CustomerID, in.PaymentMethodID)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, ErrChargeInFlight
	}
	return c, err
}

func (r *savedCardChargeRepository) Get(ctx context.Context, id uuid.UUID) (*models.SavedCardCharge, error) {
	return r.one(ctx, `SELECT `+savedCardChargeCols+` FROM credit_auto_topup_attempts WHERE id = $1`, id)
}

func (r *savedCardChargeRepository) Pending(ctx context.Context, orgID uuid.UUID) (*models.SavedCardCharge, error) {
	return r.one(ctx, `SELECT `+savedCardChargeCols+` FROM credit_auto_topup_attempts
		WHERE organization_id = $1 AND status = 'pending'`, orgID)
}

func (r *savedCardChargeRepository) SetInvoice(ctx context.Context, id uuid.UUID, invoiceID string) error {
	_, err := r.DB.Exec(ctx, `UPDATE credit_auto_topup_attempts SET invoice_id = $2, updated_at = now()
		WHERE id = $1 AND invoice_id = ''`, id, invoiceID)
	return err
}

func (r *savedCardChargeRepository) SetPaymentMethod(ctx context.Context, id uuid.UUID, paymentMethodID string) error {
	_, err := r.DB.Exec(ctx, `UPDATE credit_auto_topup_attempts SET payment_method_id = $2, updated_at = now()
		WHERE id = $1 AND status = 'pending'`, id, paymentMethodID)
	return err
}

func (r *savedCardChargeRepository) BumpTries(ctx context.Context, id uuid.UUID) (int, error) {
	var tries int
	err := r.DB.QueryRow(ctx, `UPDATE credit_auto_topup_attempts SET tries = tries + 1, retry_at = NULL, updated_at = now()
		WHERE id = $1 RETURNING tries`, id).Scan(&tries)
	return tries, err
}

func (r *savedCardChargeRepository) Snooze(ctx context.Context, id uuid.UUID, retryAt time.Time, failureCode string) error {
	_, err := r.DB.Exec(ctx, `UPDATE credit_auto_topup_attempts SET retry_at = $2, failure_code = $3, updated_at = now()
		WHERE id = $1 AND status = 'pending'`, id, retryAt, failureCode)
	return err
}

func (r *savedCardChargeRepository) MarkSucceeded(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := r.DB.Exec(ctx, `UPDATE credit_auto_topup_attempts
		SET status = 'succeeded', failure_code = '', failure_message = '', retry_at = NULL, completed_at = now(), updated_at = now()
		WHERE id = $1 AND status = 'pending'`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *savedCardChargeRepository) MarkFailed(ctx context.Context, id uuid.UUID, failureCode string) error {
	_, err := r.DB.Exec(ctx, `UPDATE credit_auto_topup_attempts
		SET status = 'failed', failure_code = $2, failure_message = $2, retry_at = NULL, completed_at = now(), updated_at = now()
		WHERE id = $1 AND status = 'pending'`, id, failureCode)
	return err
}

func (r *savedCardChargeRepository) ReloadedSince(ctx context.Context, orgID uuid.UUID, since time.Time) (models.Micro, error) {
	var total models.Micro
	err := r.DB.QueryRow(ctx, `SELECT COALESCE(SUM(amount_micro), 0) FROM credit_auto_topup_attempts
		WHERE organization_id = $1 AND kind = 'auto_reload' AND status IN ('pending', 'succeeded') AND created_at >= $2`,
		orgID, since).Scan(&total)
	return total, err
}

func (r *savedCardChargeRepository) LastSucceeded(ctx context.Context, orgID uuid.UUID, kind string) (*models.SavedCardCharge, error) {
	return r.one(ctx, `SELECT `+savedCardChargeCols+` FROM credit_auto_topup_attempts
		WHERE organization_id = $1 AND kind = $2 AND status = 'succeeded'
		ORDER BY completed_at DESC LIMIT 1`, orgID, kind)
}

func (r *savedCardChargeRepository) Due(ctx context.Context, now time.Time, staleAfter time.Duration, limit int) ([]models.SavedCardCharge, error) {
	// A pack charge the previous release started settles through its own
	// payment intent and is never resumed here.
	rows, err := r.DB.Query(ctx, `SELECT `+savedCardChargeCols+` FROM credit_auto_topup_attempts
		WHERE status = 'pending' AND pack_key = 'usd'
		  AND (retry_at <= $1 OR (retry_at IS NULL AND updated_at < $2))
		ORDER BY created_at LIMIT $3`, now, now.Add(-staleAfter), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.SavedCardCharge
	for rows.Next() {
		var c models.SavedCardCharge
		if err := scanSavedCardCharge(rows, &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *savedCardChargeRepository) CloseLegacy(ctx context.Context, createdBefore time.Time, failureCode string) (int64, error) {
	tag, err := r.DB.Exec(ctx, `UPDATE credit_auto_topup_attempts
		SET status = 'failed', failure_code = $2, failure_message = $2, retry_at = NULL, completed_at = now(), updated_at = now()
		WHERE status = 'pending' AND pack_key <> 'usd' AND created_at < $1`, createdBefore, failureCode)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *savedCardChargeRepository) SettleLegacyPaymentIntent(ctx context.Context, paymentIntentID string) error {
	_, err := r.DB.Exec(ctx, `
		UPDATE credit_auto_topup_attempts
		SET status = 'succeeded', failure_message = '', completed_at = now(), updated_at = now()
		WHERE payment_intent_id = $1`, paymentIntentID)
	return err
}
