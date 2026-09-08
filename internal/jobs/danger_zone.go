package jobs

import (
	"context"
	"time"

	"github.com/warmbly/warmbly/internal/observability/errs"

	"github.com/warmbly/warmbly/internal/app/dangerzone"
	"github.com/warmbly/warmbly/internal/jobrun"
)

// DangerZoneJob ticks the dangerzone subsystem: it executes any pending
// deletions that have passed their grace period and dispatches the
// 7-day / 24-hour reminder emails for the ones still pending.
type DangerZoneJob struct {
	svc dangerzone.Service
}

// NewDangerZoneJob constructs the job.
func NewDangerZoneJob(svc dangerzone.Service) *DangerZoneJob {
	return &DangerZoneJob{svc: svc}
}

// Run performs one tick. Errors are logged to Sentry and swallowed so
// the scheduler keeps ticking; the next tick will retry anything that
// got marked failed.
func (j *DangerZoneJob) Run(ctx context.Context) {
	if _, _, err := j.svc.ExecuteDuePendingDeletions(ctx); err != nil {
		errs.CaptureException(err)
	}
	if err := j.svc.DispatchReminders(ctx); err != nil {
		errs.CaptureException(err)
	}
}

// DangerZoneScheduler runs the job on a fixed interval.
type DangerZoneScheduler struct {
	job      *DangerZoneJob
	interval time.Duration
	stopCh   chan struct{}
}

// NewDangerZoneScheduler builds a scheduler. A 1 hour interval is the
// sweet spot: the grace windows are in days, so we don't need to tick
// often, and reminders use absolute time windows so a small skew is fine.
func NewDangerZoneScheduler(job *DangerZoneJob, interval time.Duration) *DangerZoneScheduler {
	return &DangerZoneScheduler{
		job:      job,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start runs Run() on every tick until ctx is cancelled or Stop() is called.
func (s *DangerZoneScheduler) Start(ctx context.Context) {
	ctx, cancel := stopContext(ctx, s.stopCh)
	defer cancel()
	jobrun.Loop(ctx, "danger_zone", s.interval, true, func(ctx context.Context) error {
		s.job.Run(ctx)
		return nil
	})
}

// Stop halts the scheduler.
func (s *DangerZoneScheduler) Stop() {
	close(s.stopCh)
}
