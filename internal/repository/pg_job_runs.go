package repository

import (
	"context"
	"time"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// JobRunRepository persists scheduled_job_runs, one row per background loop.
// It is the jobrun.Store the backend and consumer record to.
type JobRunRepository interface {
	Register(ctx context.Context, name, service string, interval time.Duration, nextRunAt time.Time) error
	MarkStarted(ctx context.Context, name string, at time.Time) error
	MarkFinished(ctx context.Context, name string, startedAt, finishedAt time.Time, runErr error, nextRunAt time.Time) error
	RequestRun(ctx context.Context, name string) (bool, error)
	TakeRunRequest(ctx context.Context, name string) (bool, error)
	List(ctx context.Context) ([]models.ScheduledJobRun, error)
}

type jobRunRepository struct {
	db *db.DB
}

func NewJobRunRepository(d *db.DB) JobRunRepository {
	return &jobRunRepository{db: d}
}

func (r *jobRunRepository) Register(ctx context.Context, name, service string, interval time.Duration, nextRunAt time.Time) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO scheduled_job_runs (name, service, interval_seconds, next_run_at, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (name) DO UPDATE SET
			service = EXCLUDED.service,
			interval_seconds = EXCLUDED.interval_seconds,
			next_run_at = EXCLUDED.next_run_at,
			last_status = CASE WHEN scheduled_job_runs.last_status = 'running' THEN 'idle' ELSE scheduled_job_runs.last_status END,
			updated_at = now()
	`, name, service, int(interval.Seconds()), nextRunAt)
	return err
}

func (r *jobRunRepository) MarkStarted(ctx context.Context, name string, at time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE scheduled_job_runs
		SET last_started_at = $2, last_status = 'running', updated_at = now()
		WHERE name = $1
	`, name, at)
	return err
}

func (r *jobRunRepository) MarkFinished(ctx context.Context, name string, startedAt, finishedAt time.Time, runErr error, nextRunAt time.Time) error {
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
		    next_run_at = $6,
		    updated_at = now()
		WHERE name = $1
	`, name, finishedAt, finishedAt.Sub(startedAt).Milliseconds(), status, msg, nextRunAt)
	return err
}

func (r *jobRunRepository) RequestRun(ctx context.Context, name string) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE scheduled_job_runs SET run_requested_at = now(), updated_at = now() WHERE name = $1
	`, name)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *jobRunRepository) TakeRunRequest(ctx context.Context, name string) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE scheduled_job_runs SET run_requested_at = NULL, updated_at = now()
		WHERE name = $1 AND run_requested_at IS NOT NULL
	`, name)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *jobRunRepository) List(ctx context.Context) ([]models.ScheduledJobRun, error) {
	rows, err := r.db.Query(ctx, `
		SELECT name, service, interval_seconds, last_started_at, last_finished_at, last_duration_ms,
		       last_status, last_error, run_count, error_count, run_requested_at, next_run_at, updated_at
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
			&j.LastStatus, &j.LastError, &j.RunCount, &j.ErrorCount, &j.RunRequestedAt, &j.NextRunAt, &j.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
