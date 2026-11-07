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

// AISettingsRepository persists per-org spend controls: spend limits, the
// low-balance notice, and automatic reload. A missing row means all defaults.
// The credit columns are kept in step by the table's trigger.
type AISettingsRepository interface {
	// Get returns the org's settings row, or nil when none has been saved yet.
	Get(ctx context.Context, orgID uuid.UUID) (*models.AISpendSettings, error)

	// UpsertLimits writes the spend limits and the low-balance threshold,
	// leaving automatic reload and the notice stamps as they are.
	UpsertLimits(ctx context.Context, s *models.AISpendSettings) (*models.AISpendSettings, error)

	// UpsertAutoReload writes automatic reload. Turning it on clears a
	// recorded failure.
	UpsertAutoReload(ctx context.Context, orgID uuid.UUID, enabled bool, threshold, amount, monthlyLimit models.Micro) (*models.AISpendSettings, error)

	// DisableAutoReload turns reload off and records why, only while it is
	// on. Returns true when this call turned it off.
	DisableAutoReload(ctx context.Context, orgID uuid.UUID, failureCode string) (bool, error)

	// StampNotice records that the low ("low") or empty ("out") balance notice
	// went out, once per period: true when this call won the stamp.
	StampNotice(ctx context.Context, orgID uuid.UUID, kind string, periodStart time.Time) (bool, error)

	// RearmNotices clears both notice stamps, after money is added.
	RearmNotices(ctx context.Context, orgID uuid.UUID) error

	// AutoReloadDue lists orgs with reload on whose balance is under their
	// threshold and that have no charge in flight.
	AutoReloadDue(ctx context.Context, limit int) ([]uuid.UUID, error)
}

type aiSettingsRepository struct {
	DB *db.DB
}

func NewAISettingsRepository(database *db.DB) AISettingsRepository {
	return &aiSettingsRepository{DB: database}
}

const aiSettingsCols = `org_id, spend_limit_daily_micro, spend_limit_weekly_micro, spend_limit_monthly_micro,
	member_limit_daily_micro, member_limit_weekly_micro, member_limit_monthly_micro,
	low_balance_threshold_micro, low_balance_notified_at, balance_out_notified_at,
	auto_reload_enabled, auto_reload_threshold_micro, auto_reload_amount_micro, auto_reload_monthly_limit_micro,
	COALESCE(auto_reload_failure_code, ''), auto_reload_failed_at, created_at, updated_at`

func scanAISettings(row pgx.Row, s *models.AISpendSettings) error {
	return row.Scan(
		&s.OrgID, &s.SpendLimitDaily, &s.SpendLimitWeekly, &s.SpendLimitMonthly,
		&s.MemberLimitDaily, &s.MemberLimitWeekly, &s.MemberLimitMonthly,
		&s.LowBalanceThreshold, &s.LowBalanceNotifiedAt, &s.BalanceOutNotifiedAt,
		&s.AutoReloadEnabled, &s.AutoReloadThreshold, &s.AutoReloadAmount, &s.AutoReloadMonthlyMax,
		&s.AutoReloadFailureCode, &s.AutoReloadFailedAt, &s.CreatedAt, &s.UpdatedAt,
	)
}

func (r *aiSettingsRepository) one(ctx context.Context, query string, args ...any) (*models.AISpendSettings, error) {
	s := &models.AISpendSettings{}
	if err := scanAISettings(r.DB.QueryRow(ctx, query, args...), s); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return s, nil
}

func (r *aiSettingsRepository) Get(ctx context.Context, orgID uuid.UUID) (*models.AISpendSettings, error) {
	return r.one(ctx, `SELECT `+aiSettingsCols+` FROM org_ai_settings WHERE org_id = $1`, orgID)
}

func (r *aiSettingsRepository) UpsertLimits(ctx context.Context, in *models.AISpendSettings) (*models.AISpendSettings, error) {
	return r.one(ctx, `
		INSERT INTO org_ai_settings
			(org_id, spend_limit_daily_micro, spend_limit_weekly_micro, spend_limit_monthly_micro,
			 member_limit_daily_micro, member_limit_weekly_micro, member_limit_monthly_micro, low_balance_threshold_micro)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (org_id) DO UPDATE SET
			spend_limit_daily_micro = EXCLUDED.spend_limit_daily_micro,
			spend_limit_weekly_micro = EXCLUDED.spend_limit_weekly_micro,
			spend_limit_monthly_micro = EXCLUDED.spend_limit_monthly_micro,
			member_limit_daily_micro = EXCLUDED.member_limit_daily_micro,
			member_limit_weekly_micro = EXCLUDED.member_limit_weekly_micro,
			member_limit_monthly_micro = EXCLUDED.member_limit_monthly_micro,
			low_balance_threshold_micro = EXCLUDED.low_balance_threshold_micro,
			updated_at = now()
		RETURNING `+aiSettingsCols,
		in.OrgID, in.SpendLimitDaily, in.SpendLimitWeekly, in.SpendLimitMonthly,
		in.MemberLimitDaily, in.MemberLimitWeekly, in.MemberLimitMonthly, in.LowBalanceThreshold)
}

func (r *aiSettingsRepository) UpsertAutoReload(ctx context.Context, orgID uuid.UUID, enabled bool, threshold, amount, monthlyLimit models.Micro) (*models.AISpendSettings, error) {
	return r.one(ctx, `
		INSERT INTO org_ai_settings
			(org_id, auto_reload_enabled, auto_reload_threshold_micro, auto_reload_amount_micro, auto_reload_monthly_limit_micro)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (org_id) DO UPDATE SET
			auto_reload_enabled = EXCLUDED.auto_reload_enabled,
			auto_reload_threshold_micro = EXCLUDED.auto_reload_threshold_micro,
			auto_reload_amount_micro = EXCLUDED.auto_reload_amount_micro,
			auto_reload_monthly_limit_micro = EXCLUDED.auto_reload_monthly_limit_micro,
			auto_reload_failure_code = CASE WHEN EXCLUDED.auto_reload_enabled THEN NULL ELSE org_ai_settings.auto_reload_failure_code END,
			auto_reload_failed_at = CASE WHEN EXCLUDED.auto_reload_enabled THEN NULL ELSE org_ai_settings.auto_reload_failed_at END,
			updated_at = now()
		RETURNING `+aiSettingsCols,
		orgID, enabled, threshold, amount, monthlyLimit)
}

func (r *aiSettingsRepository) DisableAutoReload(ctx context.Context, orgID uuid.UUID, failureCode string) (bool, error) {
	tag, err := r.DB.Exec(ctx, `
		UPDATE org_ai_settings
		SET auto_reload_enabled = false, auto_reload_failure_code = $2, auto_reload_failed_at = now(), updated_at = now()
		WHERE org_id = $1 AND auto_reload_enabled`, orgID, failureCode)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *aiSettingsRepository) StampNotice(ctx context.Context, orgID uuid.UUID, kind string, periodStart time.Time) (bool, error) {
	col := "low_balance_notified_at"
	if kind == "out" {
		col = "balance_out_notified_at"
	}
	// One statement, so concurrent charges cannot both win and double-notify.
	tag, err := r.DB.Exec(ctx, `
		INSERT INTO org_ai_settings (org_id, `+col+`) VALUES ($1, now())
		ON CONFLICT (org_id) DO UPDATE SET `+col+` = now(), updated_at = now()
		WHERE org_ai_settings.`+col+` IS NULL OR org_ai_settings.`+col+` < $2`, orgID, periodStart)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *aiSettingsRepository) RearmNotices(ctx context.Context, orgID uuid.UUID) error {
	_, err := r.DB.Exec(ctx, `
		UPDATE org_ai_settings SET low_balance_notified_at = NULL, balance_out_notified_at = NULL
		WHERE org_id = $1 AND (low_balance_notified_at IS NOT NULL OR balance_out_notified_at IS NOT NULL)`, orgID)
	return err
}

func (r *aiSettingsRepository) AutoReloadDue(ctx context.Context, limit int) ([]uuid.UUID, error) {
	rows, err := r.DB.Query(ctx, `
		SELECT s.org_id
		FROM org_ai_settings s
		JOIN credit_ledger l ON l.org_id = s.org_id
		WHERE s.auto_reload_enabled
		  AND l.balance_micro + l.purchased_micro < s.auto_reload_threshold_micro
		  AND NOT EXISTS (SELECT 1 FROM credit_auto_topup_attempts a
		                  WHERE a.organization_id = s.org_id AND a.status = 'pending')
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
