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

// BehaviorRepository stores each mailbox's sending-behaviour ranges and the
// workday it rolled for a given local date, and answers the volume questions
// those plans are enforced against.
type BehaviorRepository interface {
	// GetBehavior returns the mailbox's profile, or nil when it has never been
	// configured. Callers substitute models.DefaultSendingBehavior.
	GetBehavior(ctx context.Context, accountID uuid.UUID) (*models.SendingBehavior, error)
	// GetBehaviors is the batch read the campaign scheduler uses, so resolving
	// a campaign's whole sender pool costs one query rather than one per
	// mailbox. Mailboxes with no row are simply absent from the map.
	GetBehaviors(ctx context.Context, accountIDs []uuid.UUID) (map[uuid.UUID]models.SendingBehavior, error)
	UpsertBehavior(ctx context.Context, b *models.SendingBehavior) (*models.SendingBehavior, error)
	DeleteBehavior(ctx context.Context, accountID uuid.UUID) error

	// GetPlan returns the stored plan for a local date (YYYY-MM-DD), or nil.
	GetPlan(ctx context.Context, accountID uuid.UUID, planDate string) (*models.DailyPlan, error)
	// EnsurePlan writes the rolled plan if this mailbox has none for that date
	// and returns whatever is stored afterwards. Concurrent writers are safe:
	// the roll is deterministic, so the loser of the race reads back the same
	// numbers it computed.
	EnsurePlan(ctx context.Context, plan models.DailyPlan) (*models.DailyPlan, error)
	// PurgePlansBefore drops plans older than a cutoff date.
	PurgePlansBefore(ctx context.Context, before time.Time) (int64, error)

	// CountSendsBetween counts a mailbox's sends in a half-open time range.
	// Pending and active tasks count alongside completed ones: a slot that is
	// already booked has consumed the budget even though the mail has not gone
	// out yet, and without that the scheduler would happily stack a day's worth
	// of tasks into one hour.
	CountSendsBetween(ctx context.Context, accountID uuid.UUID, taskType string, from, to time.Time) (int, error)
}

type behaviorRepository struct {
	db *pgxpool.Pool
}

func NewBehaviorRepository(db *pgxpool.Pool) BehaviorRepository {
	return &behaviorRepository{db: db}
}

const behaviorColumns = `
	email_account_id, enabled,
	daily_limit_min, daily_limit_max,
	hourly_limit_min, hourly_limit_max,
	gap_min_seconds, gap_max_seconds,
	work_start_min, work_start_max, work_end_min, work_end_max,
	lunch_enabled, lunch_earliest, lunch_latest, lunch_min_minutes, lunch_max_minutes,
	weekdays, created_at, updated_at`

func scanBehavior(row pgx.Row) (*models.SendingBehavior, error) {
	var b models.SendingBehavior
	err := row.Scan(
		&b.EmailAccountID, &b.Enabled,
		&b.DailyLimitMin, &b.DailyLimitMax,
		&b.HourlyLimitMin, &b.HourlyLimitMax,
		&b.GapMinSeconds, &b.GapMaxSeconds,
		&b.WorkStartMin, &b.WorkStartMax, &b.WorkEndMin, &b.WorkEndMax,
		&b.LunchEnabled, &b.LunchEarliest, &b.LunchLatest, &b.LunchMinMinutes, &b.LunchMaxMinutes,
		&b.Weekdays, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &b, nil
}

func (r *behaviorRepository) GetBehavior(ctx context.Context, accountID uuid.UUID) (*models.SendingBehavior, error) {
	return scanBehavior(r.db.QueryRow(ctx,
		`SELECT `+behaviorColumns+` FROM email_account_behavior WHERE email_account_id = $1`, accountID))
}

func (r *behaviorRepository) GetBehaviors(ctx context.Context, accountIDs []uuid.UUID) (map[uuid.UUID]models.SendingBehavior, error) {
	out := map[uuid.UUID]models.SendingBehavior{}
	if len(accountIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx,
		`SELECT `+behaviorColumns+` FROM email_account_behavior WHERE email_account_id = ANY($1)`, accountIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		b, serr := scanBehavior(rows)
		if serr != nil {
			return nil, serr
		}
		if b != nil {
			out[b.EmailAccountID] = *b
		}
	}
	return out, rows.Err()
}

func (r *behaviorRepository) UpsertBehavior(ctx context.Context, b *models.SendingBehavior) (*models.SendingBehavior, error) {
	query := `
		INSERT INTO email_account_behavior (
			email_account_id, enabled,
			daily_limit_min, daily_limit_max,
			hourly_limit_min, hourly_limit_max,
			gap_min_seconds, gap_max_seconds,
			work_start_min, work_start_max, work_end_min, work_end_max,
			lunch_enabled, lunch_earliest, lunch_latest, lunch_min_minutes, lunch_max_minutes,
			weekdays
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
		ON CONFLICT (email_account_id) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			daily_limit_min = EXCLUDED.daily_limit_min,
			daily_limit_max = EXCLUDED.daily_limit_max,
			hourly_limit_min = EXCLUDED.hourly_limit_min,
			hourly_limit_max = EXCLUDED.hourly_limit_max,
			gap_min_seconds = EXCLUDED.gap_min_seconds,
			gap_max_seconds = EXCLUDED.gap_max_seconds,
			work_start_min = EXCLUDED.work_start_min,
			work_start_max = EXCLUDED.work_start_max,
			work_end_min = EXCLUDED.work_end_min,
			work_end_max = EXCLUDED.work_end_max,
			lunch_enabled = EXCLUDED.lunch_enabled,
			lunch_earliest = EXCLUDED.lunch_earliest,
			lunch_latest = EXCLUDED.lunch_latest,
			lunch_min_minutes = EXCLUDED.lunch_min_minutes,
			lunch_max_minutes = EXCLUDED.lunch_max_minutes,
			weekdays = EXCLUDED.weekdays,
			updated_at = now()
		RETURNING ` + behaviorColumns

	return scanBehavior(r.db.QueryRow(ctx, query,
		b.EmailAccountID, b.Enabled,
		b.DailyLimitMin, b.DailyLimitMax,
		b.HourlyLimitMin, b.HourlyLimitMax,
		b.GapMinSeconds, b.GapMaxSeconds,
		b.WorkStartMin, b.WorkStartMax, b.WorkEndMin, b.WorkEndMax,
		b.LunchEnabled, b.LunchEarliest, b.LunchLatest, b.LunchMinMinutes, b.LunchMaxMinutes,
		b.Weekdays,
	))
}

func (r *behaviorRepository) DeleteBehavior(ctx context.Context, accountID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM email_account_behavior WHERE email_account_id = $1`, accountID)
	return err
}

const planColumns = `
	email_account_id, plan_date::text, timezone, is_working_day,
	daily_limit, hourly_limit, work_start_minute, work_end_minute,
	lunch_start_minute, lunch_end_minute, gap_min_seconds, gap_max_seconds, created_at`

func scanPlan(row pgx.Row) (*models.DailyPlan, error) {
	var p models.DailyPlan
	err := row.Scan(
		&p.EmailAccountID, &p.PlanDate, &p.Timezone, &p.IsWorkingDay,
		&p.DailyLimit, &p.HourlyLimit, &p.WorkStartMinute, &p.WorkEndMinute,
		&p.LunchStartMinute, &p.LunchEndMinute, &p.GapMinSeconds, &p.GapMaxSeconds, &p.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

func (r *behaviorRepository) GetPlan(ctx context.Context, accountID uuid.UUID, planDate string) (*models.DailyPlan, error) {
	return scanPlan(r.db.QueryRow(ctx,
		`SELECT `+planColumns+` FROM email_account_daily_plan WHERE email_account_id = $1 AND plan_date = $2::date`,
		accountID, planDate))
}

func (r *behaviorRepository) EnsurePlan(ctx context.Context, plan models.DailyPlan) (*models.DailyPlan, error) {
	// DO NOTHING rather than DO UPDATE: a day's shape is decided once. If a
	// customer widens their ranges at noon, today keeps the workday it already
	// started and the new ranges take effect tomorrow.
	_, err := r.db.Exec(ctx, `
		INSERT INTO email_account_daily_plan (
			email_account_id, plan_date, timezone, is_working_day,
			daily_limit, hourly_limit, work_start_minute, work_end_minute,
			lunch_start_minute, lunch_end_minute, gap_min_seconds, gap_max_seconds
		) VALUES ($1,$2::date,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (email_account_id, plan_date) DO NOTHING`,
		plan.EmailAccountID, plan.PlanDate, plan.Timezone, plan.IsWorkingDay,
		plan.DailyLimit, plan.HourlyLimit, plan.WorkStartMinute, plan.WorkEndMinute,
		plan.LunchStartMinute, plan.LunchEndMinute, plan.GapMinSeconds, plan.GapMaxSeconds,
	)
	if err != nil {
		return nil, err
	}
	return r.GetPlan(ctx, plan.EmailAccountID, plan.PlanDate)
}

func (r *behaviorRepository) PurgePlansBefore(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.db.Exec(ctx,
		`DELETE FROM email_account_daily_plan WHERE plan_date < $1::date`, before.Format("2006-01-02"))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *behaviorRepository) CountSendsBetween(ctx context.Context, accountID uuid.UUID, taskType string, from, to time.Time) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM tasks
		WHERE email_account_id = $1
		  AND task_type = $2::task_type
		  AND (
		        (status = 'completed' AND completed_at >= $3 AND completed_at < $4)
		     OR (status IN ('pending', 'active') AND scheduled_at >= $3 AND scheduled_at < $4)
		  )`

	var count int
	err := r.db.QueryRow(ctx, query, accountID, taskType, from, to).Scan(&count)
	return count, err
}
