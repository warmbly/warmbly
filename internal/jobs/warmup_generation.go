package jobs

import (
	"context"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/warmbly/warmbly/internal/app/warmupcontent"
	"github.com/warmbly/warmbly/internal/repository"
)

// WarmupGenerationJob periodically tops up the warmup content bank toward the
// per-pool/segment targets in the admin settings. It is a thin scheduler around
// warmupcontent.Service.RunScheduled; all policy (enabled, cadence, caps,
// targets) lives in the settings so admins keep full control.
type WarmupGenerationJob struct {
	svc  warmupcontent.Service
	repo repository.WarmupContentRepository

	mu      sync.Mutex
	lastRun time.Time
}

// NewWarmupGenerationJob creates the job.
func NewWarmupGenerationJob(svc warmupcontent.Service, repo repository.WarmupContentRepository) *WarmupGenerationJob {
	return &WarmupGenerationJob{svc: svc, repo: repo}
}

// Run performs one top-up pass when scheduling is enabled and the configured
// cadence has elapsed. Safe to call frequently — it no-ops when there's nothing
// to do.
func (j *WarmupGenerationJob) Run(ctx context.Context) error {
	if j.svc == nil || j.repo == nil || !j.svc.Enabled() {
		return nil
	}
	settings, err := j.repo.GetGenerationSettings(ctx)
	if err != nil {
		sentry.CaptureException(err)
		return err
	}
	if settings == nil || !settings.ScheduleEnabled {
		return nil
	}

	cadence := time.Duration(settings.CadenceHours) * time.Hour
	j.mu.Lock()
	if !j.lastRun.IsZero() && cadence > 0 && time.Since(j.lastRun) < cadence {
		j.mu.Unlock()
		return nil
	}
	j.lastRun = time.Now()
	j.mu.Unlock()

	if err := j.svc.RunScheduled(ctx); err != nil {
		sentry.CaptureException(err)
		return err
	}
	return nil
}

// WarmupGenerationScheduler runs the job on a fixed interval (the per-run
// cadence gate inside Run honours the admin's configured cadence_hours).
type WarmupGenerationScheduler struct {
	job      *WarmupGenerationJob
	interval time.Duration
	stopCh   chan struct{}
}

// NewWarmupGenerationScheduler creates the scheduler.
func NewWarmupGenerationScheduler(job *WarmupGenerationJob, interval time.Duration) *WarmupGenerationScheduler {
	return &WarmupGenerationScheduler{
		job:      job,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start begins scheduled execution.
func (s *WarmupGenerationScheduler) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := s.job.Run(ctx); err != nil {
				sentry.CaptureException(err)
			}
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// Stop halts the scheduled execution.
func (s *WarmupGenerationScheduler) Stop() {
	close(s.stopCh)
}
