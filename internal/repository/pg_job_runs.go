package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// JobRunRepository persists scheduled_job_runs, one row per background loop.
// It is the jobrun.Store the backend and consumer record to.
type JobRunRepository interface {
	Register(ctx context.Context, name, service string, interval time.Duration, nextRunAt, earliest time.Time) (time.Time, error)
	Claim(ctx context.Context, name string, due, next time.Time) (bool, time.Time, error)
	MarkStarted(ctx context.Context, name string, at time.Time) error
	MarkFinished(ctx context.Context, name string, startedAt, finishedAt time.Time, runErr error) error
	RequestRun(ctx context.Context, name string) (bool, error)
	TakeRunRequests(ctx context.Context, names []string) ([]string, error)
	List(ctx context.Context) ([]models.ScheduledJobRun, error)
}

type jobRunRepository struct {
	db *db.DB
}

func NewJobRunRepository(d *db.DB) JobRunRepository {
	return &jobRunRepository{db: d}
}

// Register keeps a stored due time across restarts, clamped to [earliest,
// nextRunAt], and returns the one the loop should wait for.
func (r *jobRunRepository) Register(ctx context.Context, name, service string, interval time.Duration, nextRunAt, earliest time.Time) (time.Time, error) {
	var due time.Time
	err := r.db.QueryRow(ctx, `
		INSERT INTO scheduled_job_runs (name, service, interval_seconds, next_run_at, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (name) DO UPDATE SET
			service = EXCLUDED.service,
			interval_seconds = EXCLUDED.interval_seconds,
			next_run_at = CASE
				WHEN scheduled_job_runs.next_run_at IS NULL THEN EXCLUDED.next_run_at
				ELSE GREATEST($5::timestamptz, LEAST(scheduled_job_runs.next_run_at, EXCLUDED.next_run_at))
			END,
			last_status = CASE WHEN scheduled_job_runs.last_status = 'running' THEN 'idle' ELSE scheduled_job_runs.last_status END,
			run_requested_at = CASE WHEN scheduled_job_runs.retired_at IS NOT NULL THEN NULL ELSE scheduled_job_runs.run_requested_at END,
			retired_at = NULL,
			updated_at = now()
		RETURNING next_run_at
	`, name, service, int(interval.Seconds()), nextRunAt, earliest).Scan(&due)
	return due, err
}

// Claim moves the job from due to next when no other instance has, so each
// slot runs once however many processes host the job. On a lost claim it
// returns the due time the winner stored.
func (r *jobRunRepository) Claim(ctx context.Context, name string, due, next time.Time) (bool, time.Time, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE scheduled_job_runs SET next_run_at = $3, updated_at = now()
		WHERE name = $1 AND next_run_at = $2 AND retired_at IS NULL
	`, name, due, next)
	if err != nil {
		return false, time.Time{}, err
	}
	if tag.RowsAffected() > 0 {
		return true, next, nil
	}
	var current, retiredAt *time.Time
	err = r.db.QueryRow(ctx, `SELECT next_run_at, retired_at FROM scheduled_job_runs WHERE name = $1`, name).Scan(&current, &retiredAt)
	if err == nil && retiredAt != nil {
		return false, next, nil
	}
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && current == nil) {
		// Nothing to compete over; take the slot and leave the row alone.
		return true, next, nil
	}
	if err != nil {
		return false, time.Time{}, err
	}
	return false, *current, nil
}

func (r *jobRunRepository) MarkStarted(ctx context.Context, name string, at time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE scheduled_job_runs
		SET last_started_at = $2, last_status = 'running', updated_at = now()
		WHERE name = $1 AND retired_at IS NULL
	`, name, at)
	return err
}

// MarkFinished records the outcome; the schedule is owned by Register and Claim.
func (r *jobRunRepository) MarkFinished(ctx context.Context, name string, startedAt, finishedAt time.Time, runErr error) error {
	status, msg := "ok", ""
	if runErr != nil {
		status, msg = "error", runErr.Error()
		if len(msg) > 2000 {
			msg = msg[:2000]
		}
	}
	_, err := r.db.Exec(ctx, `
		UPDATE scheduled_job_runs
		SET last_finished_at = $2,
		    last_duration_ms = $3,
		    last_status = $4,
		    last_error = $5,
		    run_count = run_count + 1,
		    error_count = error_count + CASE WHEN $4 = 'error' THEN 1 ELSE 0 END,
		    updated_at = now()
		WHERE name = $1
	`, name, finishedAt, finishedAt.Sub(startedAt).Milliseconds(), status, msg)
	return err
}

func (r *jobRunRepository) RequestRun(ctx context.Context, name string) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE scheduled_job_runs SET run_requested_at = now(), updated_at = now()
		WHERE name = $1 AND retired_at IS NULL
	`, name)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *jobRunRepository) TakeRunRequests(ctx context.Context, names []string) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		UPDATE scheduled_job_runs SET run_requested_at = NULL, updated_at = now()
		WHERE name = ANY($1) AND run_requested_at IS NOT NULL AND retired_at IS NULL
		RETURNING name
	`, names)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func (r *jobRunRepository) List(ctx context.Context) ([]models.ScheduledJobRun, error) {
	rows, err := r.db.Query(ctx, `
		SELECT name, service, interval_seconds, last_started_at, last_finished_at, last_duration_ms,
		       last_status, last_error, run_count, error_count, run_requested_at, next_run_at, retired_at, updated_at
		FROM scheduled_job_runs
		ORDER BY service, name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.ScheduledJobRun{}
	for rows.Next() {
		var j models.ScheduledJobRun
		if err := rows.Scan(
			&j.Name, &j.Service, &j.IntervalSeconds, &j.LastStartedAt, &j.LastFinishedAt, &j.LastDurationMs,
			&j.LastStatus, &j.LastError, &j.RunCount, &j.ErrorCount, &j.RunRequestedAt, &j.NextRunAt, &j.RetiredAt, &j.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
