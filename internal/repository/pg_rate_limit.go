package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

type RateLimitRepository interface {
	GetUserLimits(ctx context.Context, userID uuid.UUID) (*models.UserRateLimits, *errx.Error)
	GetPlanLimits(ctx context.Context, planID uuid.UUID) (*models.PlanRateLimits, *errx.Error)
	CreateUserLimits(ctx context.Context, userID uuid.UUID) (*models.UserRateLimits, *errx.Error)
	UpdateUserLimits(ctx context.Context, userID, adminID uuid.UUID, data *models.UpdateUserRateLimits) (*models.UserRateLimits, *errx.Error)
}

type rateLimitRepository struct {
	DB *db.DB
}

func NewRateLimitRepository(db *db.DB) RateLimitRepository {
	return &rateLimitRepository{DB: db}
}

const USER_RATE_LIMITS_SELECT = `user_id, limit_read_pm, limit_write_pm, limit_bulk_pm,
	limit_unibox_pm, limit_analytics_pm, limit_api_calls_daily, limit_bulk_ops_daily,
	notes, updated_by, created_at, updated_at`

func scanUserRateLimits(row db.Scannable, limits *models.UserRateLimits) error {
	return row.Scan(
		&limits.UserID, &limits.LimitReadPM, &limits.LimitWritePM, &limits.LimitBulkPM,
		&limits.LimitUniboxPM, &limits.LimitAnalyticsPM, &limits.LimitAPICallsDaily, &limits.LimitBulkOpsDaily,
		&limits.Notes, &limits.UpdatedBy, &limits.CreatedAt, &limits.UpdatedAt,
	)
}

func (r *rateLimitRepository) GetUserLimits(ctx context.Context, userID uuid.UUID) (*models.UserRateLimits, *errx.Error) {
	query := fmt.Sprintf(`
		SELECT %s FROM user_rate_limits WHERE user_id = $1
	`, USER_RATE_LIMITS_SELECT)

	var limits models.UserRateLimits
	row := r.DB.QueryRow(ctx, query, userID)
	if err := scanUserRateLimits(row, &limits); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Return defaults if no custom limits exist
			defaults := models.DefaultRateLimits()
			defaults.UserID = userID
			return defaults, nil
		}
		db.CaptureError(err, query, []any{userID}, "queryrow")
		return nil, errx.InternalError()
	}

	return &limits, nil
}

func (r *rateLimitRepository) GetPlanLimits(ctx context.Context, planID uuid.UUID) (*models.PlanRateLimits, *errx.Error) {
	query := `
		SELECT plan_id, limit_read_pm, limit_write_pm, limit_bulk_pm,
			limit_unibox_pm, limit_analytics_pm, limit_api_calls_daily, limit_bulk_ops_daily,
			created_at, updated_at
		FROM plan_rate_limits WHERE plan_id = $1
	`

	var limits models.PlanRateLimits
	row := r.DB.QueryRow(ctx, query, planID)
	err := row.Scan(
		&limits.PlanID, &limits.LimitReadPM, &limits.LimitWritePM, &limits.LimitBulkPM,
		&limits.LimitUniboxPM, &limits.LimitAnalyticsPM, &limits.LimitAPICallsDaily, &limits.LimitBulkOpsDaily,
		&limits.CreatedAt, &limits.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		db.CaptureError(err, query, []any{planID}, "queryrow")
		return nil, errx.InternalError()
	}

	return &limits, nil
}

func (r *rateLimitRepository) CreateUserLimits(ctx context.Context, userID uuid.UUID) (*models.UserRateLimits, *errx.Error) {
	query := fmt.Sprintf(`
		INSERT INTO user_rate_limits (user_id)
		VALUES ($1)
		ON CONFLICT (user_id) DO UPDATE SET updated_at = now()
		RETURNING %s
	`, USER_RATE_LIMITS_SELECT)

	var limits models.UserRateLimits
	row := r.DB.QueryRow(ctx, query, userID)
	if err := scanUserRateLimits(row, &limits); err != nil {
		db.CaptureError(err, query, []any{userID}, "queryrow")
		return nil, errx.InternalError()
	}

	return &limits, nil
}

func (r *rateLimitRepository) UpdateUserLimits(ctx context.Context, userID, adminID uuid.UUID, data *models.UpdateUserRateLimits) (*models.UserRateLimits, *errx.Error) {
	// First ensure the record exists
	_, xerr := r.CreateUserLimits(ctx, userID)
	if xerr != nil {
		return nil, xerr
	}

	setClauses := []string{}
	args := []any{userID}
	argPos := 2

	if data.LimitReadPM != nil {
		setClauses = append(setClauses, fmt.Sprintf("limit_read_pm = $%d", argPos))
		args = append(args, *data.LimitReadPM)
		argPos++
	}
	if data.LimitWritePM != nil {
		setClauses = append(setClauses, fmt.Sprintf("limit_write_pm = $%d", argPos))
		args = append(args, *data.LimitWritePM)
		argPos++
	}
	if data.LimitBulkPM != nil {
		setClauses = append(setClauses, fmt.Sprintf("limit_bulk_pm = $%d", argPos))
		args = append(args, *data.LimitBulkPM)
		argPos++
	}
	if data.LimitUniboxPM != nil {
		setClauses = append(setClauses, fmt.Sprintf("limit_unibox_pm = $%d", argPos))
		args = append(args, *data.LimitUniboxPM)
		argPos++
	}
	if data.LimitAnalyticsPM != nil {
		setClauses = append(setClauses, fmt.Sprintf("limit_analytics_pm = $%d", argPos))
		args = append(args, *data.LimitAnalyticsPM)
		argPos++
	}
	if data.LimitAPICallsDaily != nil {
		setClauses = append(setClauses, fmt.Sprintf("limit_api_calls_daily = $%d", argPos))
		args = append(args, *data.LimitAPICallsDaily)
		argPos++
	}
	if data.LimitBulkOpsDaily != nil {
		setClauses = append(setClauses, fmt.Sprintf("limit_bulk_ops_daily = $%d", argPos))
		args = append(args, *data.LimitBulkOpsDaily)
		argPos++
	}
	if data.Notes != nil {
		setClauses = append(setClauses, fmt.Sprintf("notes = $%d", argPos))
		args = append(args, *data.Notes)
		argPos++
	}

	if len(setClauses) == 0 {
		return nil, errx.ErrNotEnough
	}

	// Add admin who made the change
	setClauses = append(setClauses, fmt.Sprintf("updated_by = $%d", argPos))
	args = append(args, adminID)
	argPos++

	setClauses = append(setClauses, "updated_at = now()")

	query := fmt.Sprintf(`
		UPDATE user_rate_limits SET %s
		WHERE user_id = $1
		RETURNING %s
	`, strings.Join(setClauses, ", "), USER_RATE_LIMITS_SELECT)

	var limits models.UserRateLimits
	row := r.DB.QueryRow(ctx, query, args...)
	if err := scanUserRateLimits(row, &limits); err != nil {
		db.CaptureError(err, query, args, "queryrow")
		return nil, errx.InternalError()
	}

	return &limits, nil
}
