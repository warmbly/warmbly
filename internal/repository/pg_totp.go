package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

type TOTPRepository interface {
	Get(ctx context.Context, userID uuid.UUID) (*models.UserTOTP, error) // nil if no row
	IsEnabled(ctx context.Context, userID uuid.UUID) (bool, error)
	UpsertPending(ctx context.Context, userID uuid.UUID, sealedSecret string) error // enabled=false
	Enable(ctx context.Context, userID uuid.UUID) error
	Delete(ctx context.Context, userID uuid.UUID) error
	InsertRecoveryCodes(ctx context.Context, userID uuid.UUID, hashes []string) error
	ListUnusedRecoveryCodes(ctx context.Context, userID uuid.UUID) ([]models.RecoveryCode, error)
	ConsumeRecoveryCode(ctx context.Context, codeID uuid.UUID) error
	// ConsumeTOTPStep records that this time step has been spent, and reports
	// whether it was still available. It is compare-and-swap so two requests
	// racing with the same code cannot both win.
	ConsumeTOTPStep(ctx context.Context, userID uuid.UUID, step uint64) (bool, error)
	// CountRecoveryCodes reports how many recovery codes the user has left
	// unused, and how many were issued with the current set.
	CountRecoveryCodes(ctx context.Context, userID uuid.UUID) (unused, total int, err error)
}

var ErrRecoveryCodeUsed = errors.New("recovery code already used")

type totpRepository struct {
	db *pgxpool.Pool
}

func NewTOTPRepository(db *pgxpool.Pool) TOTPRepository {
	return &totpRepository{db: db}
}

func (r *totpRepository) Get(ctx context.Context, userID uuid.UUID) (*models.UserTOTP, error) {
	var t models.UserTOTP
	err := r.db.QueryRow(ctx,
		`SELECT user_id, totp_secret_sealed, totp_enabled, confirmed_at FROM user_totp_settings WHERE user_id = $1`, userID).
		Scan(&t.UserID, &t.SecretSealed, &t.Enabled, &t.ConfirmedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

func (r *totpRepository) IsEnabled(ctx context.Context, userID uuid.UUID) (bool, error) {
	var enabled bool
	err := r.db.QueryRow(ctx, `SELECT totp_enabled FROM user_totp_settings WHERE user_id = $1`, userID).Scan(&enabled)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return enabled, nil
}

func (r *totpRepository) UpsertPending(ctx context.Context, userID uuid.UUID, sealedSecret string) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO user_totp_settings (user_id, totp_secret_sealed, totp_enabled, updated_at)
		VALUES ($1, $2, false, now())
		ON CONFLICT (user_id) DO UPDATE SET
			totp_secret_sealed = EXCLUDED.totp_secret_sealed,
			totp_enabled = false,
			confirmed_at = NULL,
			updated_at = now()`, userID, sealedSecret)
	return err
}

func (r *totpRepository) Enable(ctx context.Context, userID uuid.UUID) error {
	_, err := r.db.Exec(ctx,
		`UPDATE user_totp_settings SET totp_enabled = true, confirmed_at = now(), updated_at = now() WHERE user_id = $1`, userID)
	return err
}

func (r *totpRepository) Delete(ctx context.Context, userID uuid.UUID) error {
	if _, err := r.db.Exec(ctx, `DELETE FROM user_totp_recovery_codes WHERE user_id = $1`, userID); err != nil {
		return err
	}
	_, err := r.db.Exec(ctx, `DELETE FROM user_totp_settings WHERE user_id = $1`, userID)
	return err
}

func (r *totpRepository) InsertRecoveryCodes(ctx context.Context, userID uuid.UUID, hashes []string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM user_totp_recovery_codes WHERE user_id = $1`, userID); err != nil {
		return err
	}
	for _, h := range hashes {
		if _, err := tx.Exec(ctx, `INSERT INTO user_totp_recovery_codes (user_id, code_hash) VALUES ($1, $2)`, userID, h); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *totpRepository) ListUnusedRecoveryCodes(ctx context.Context, userID uuid.UUID) ([]models.RecoveryCode, error) {
	rows, err := r.db.Query(ctx, `SELECT id, code_hash FROM user_totp_recovery_codes WHERE user_id = $1 AND used_at IS NULL`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.RecoveryCode{}
	for rows.Next() {
		var rc models.RecoveryCode
		if err := rows.Scan(&rc.ID, &rc.CodeHash); err != nil {
			return nil, err
		}
		out = append(out, rc)
	}
	return out, rows.Err()
}

func (r *totpRepository) CountRecoveryCodes(ctx context.Context, userID uuid.UUID) (int, int, error) {
	var unused, total int
	err := r.db.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE used_at IS NULL), count(*) FROM user_totp_recovery_codes WHERE user_id = $1`,
		userID).Scan(&unused, &total)
	return unused, total, err
}

func (r *totpRepository) ConsumeRecoveryCode(ctx context.Context, codeID uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `UPDATE user_totp_recovery_codes SET used_at = now() WHERE id = $1 AND used_at IS NULL`, codeID)
	if err != nil {
		return err
	}
	// 0 rows = the code was already consumed by a concurrent request. Returning
	// an error makes the caller treat the attempt as a miss (no double-spend).
	if tag.RowsAffected() == 0 {
		return ErrRecoveryCodeUsed
	}
	return nil
}

// ConsumeTOTPStep retires a TOTP time step for a user.
//
// The guard is in the WHERE clause rather than a read-then-write, so two
// sign-ins presenting the same code at the same moment cannot both pass: only
// the statement that actually updates a row returns true.
func (r *totpRepository) ConsumeTOTPStep(ctx context.Context, userID uuid.UUID, step uint64) (bool, error) {
	tag, err := r.db.Exec(ctx,
		`UPDATE user_totp_settings SET last_used_step = $2 WHERE user_id = $1 AND totp_enabled AND last_used_step < $2`,
		userID, int64(step))
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 1 {
		return true, nil
	}
	var used bool
	if err := r.db.QueryRow(ctx, `SELECT last_used_step >= $2 FROM user_totp_settings WHERE user_id = $1 AND totp_enabled`, userID, int64(step)).Scan(&used); err != nil {
		return false, err
	}
	if !used {
		return false, errors.New("authenticator step unavailable")
	}
	return false, nil
}
