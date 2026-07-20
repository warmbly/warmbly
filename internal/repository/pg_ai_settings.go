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

// AISettingsRepository persists per-org AI spend controls (spend limits,
// low-balance alerting, auto top-up). A missing row means "all defaults".
type AISettingsRepository interface {
	// Get returns the org's settings row, or nil when none has been saved yet.
	Get(ctx context.Context, orgID uuid.UUID) (*models.AISpendSettings, error)

	// Upsert writes the full settings row (limits, threshold, auto top-up
	// config) and returns the stored state. The low-balance stamp is preserved.
	Upsert(ctx context.Context, s *models.AISpendSettings) (*models.AISpendSettings, error)

	// StampLowBalanceNotified sets low_balance_notified_at = now() only when the
	// alert has not fired within `cooldown`, creating the row if needed. Returns
	// true when this call won the stamp (the caller should send the alert).
	StampLowBalanceNotified(ctx context.Context, orgID uuid.UUID, cooldown time.Duration) (bool, error)
}

type aiSettingsRepository struct {
	DB *db.DB
}

func NewAISettingsRepository(database *db.DB) AISettingsRepository {
	return &aiSettingsRepository{DB: database}
}

const aiSettingsCols = `org_id, spend_limit_daily, spend_limit_weekly, spend_limit_monthly,
	member_limit_daily, member_limit_weekly, member_limit_monthly,
	low_balance_threshold, low_balance_notified_at,
	auto_topup_enabled, auto_topup_pack, auto_topup_threshold, auto_topup_max_per_month,
	created_at, updated_at`

func scanAISettings(row pgx.Row, s *models.AISpendSettings) error {
	return row.Scan(
		&s.OrgID, &s.SpendLimitDaily, &s.SpendLimitWeekly, &s.SpendLimitMonthly,
		&s.MemberLimitDaily, &s.MemberLimitWeekly, &s.MemberLimitMonthly,
		&s.LowBalanceThreshold, &s.LowBalanceNotifiedAt,
		&s.AutoTopupEnabled, &s.AutoTopupPack, &s.AutoTopupThreshold, &s.AutoTopupMaxPerMonth,
		&s.CreatedAt, &s.UpdatedAt,
	)
}

func (r *aiSettingsRepository) Get(ctx context.Context, orgID uuid.UUID) (*models.AISpendSettings, error) {
	s := &models.AISpendSettings{}
	err := scanAISettings(r.DB.QueryRow(ctx, `SELECT `+aiSettingsCols+` FROM org_ai_settings WHERE org_id = $1`, orgID), s)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return s, nil
}

func (r *aiSettingsRepository) Upsert(ctx context.Context, in *models.AISpendSettings) (*models.AISpendSettings, error) {
	s := &models.AISpendSettings{}
	err := scanAISettings(r.DB.QueryRow(ctx, `
		INSERT INTO org_ai_settings
			(org_id, spend_limit_daily, spend_limit_weekly, spend_limit_monthly,
			 member_limit_daily, member_limit_weekly, member_limit_monthly,
			 low_balance_threshold, auto_topup_enabled, auto_topup_pack,
			 auto_topup_threshold, auto_topup_max_per_month)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (org_id) DO UPDATE SET
			spend_limit_daily = EXCLUDED.spend_limit_daily,
			spend_limit_weekly = EXCLUDED.spend_limit_weekly,
			spend_limit_monthly = EXCLUDED.spend_limit_monthly,
			member_limit_daily = EXCLUDED.member_limit_daily,
			member_limit_weekly = EXCLUDED.member_limit_weekly,
			member_limit_monthly = EXCLUDED.member_limit_monthly,
			low_balance_threshold = EXCLUDED.low_balance_threshold,
			auto_topup_enabled = EXCLUDED.auto_topup_enabled,
			auto_topup_pack = EXCLUDED.auto_topup_pack,
			auto_topup_threshold = EXCLUDED.auto_topup_threshold,
			auto_topup_max_per_month = EXCLUDED.auto_topup_max_per_month,
			updated_at = now()
		RETURNING `+aiSettingsCols,
		in.OrgID, in.SpendLimitDaily, in.SpendLimitWeekly, in.SpendLimitMonthly,
		in.MemberLimitDaily, in.MemberLimitWeekly, in.MemberLimitMonthly,
		in.LowBalanceThreshold, in.AutoTopupEnabled, in.AutoTopupPack,
		in.AutoTopupThreshold, in.AutoTopupMaxPerMonth), s)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (r *aiSettingsRepository) StampLowBalanceNotified(ctx context.Context, orgID uuid.UUID, cooldown time.Duration) (bool, error) {
	// Insert-or-stamp in one statement; the WHERE keeps the stamp atomic so
	// concurrent consumes can't both win and double-alert.
	tag, err := r.DB.Exec(ctx, `
		INSERT INTO org_ai_settings (org_id, low_balance_notified_at) VALUES ($1, now())
		ON CONFLICT (org_id) DO UPDATE SET low_balance_notified_at = now(), updated_at = now()
		WHERE org_ai_settings.low_balance_notified_at IS NULL
		   OR org_ai_settings.low_balance_notified_at < now() - $2::interval
	`, orgID, cooldown)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
