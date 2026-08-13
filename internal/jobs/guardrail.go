package jobs

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/guardrail"
	"github.com/warmbly/warmbly/internal/repository"
)

// GuardrailJob evaluates every active campaign that has auto-pause guardrails
// switched on and pauses the ones outside their band.
//
// It also sweeps up expired daily sending plans, which is a cheap piece of
// housekeeping on the same cadence: plans are only useful for the day they
// describe plus a short tail for the dashboard's "yesterday" view.
type GuardrailJob struct {
	svc          guardrail.Service
	behaviorRepo repository.BehaviorRepository
}

func NewGuardrailJob(svc guardrail.Service, behaviorRepo repository.BehaviorRepository) *GuardrailJob {
	return &GuardrailJob{svc: svc, behaviorRepo: behaviorRepo}
}

// planRetention is how long a rolled workday is kept after its date. Long
// enough to explain a send someone is looking at this week, short enough that
// the table stays small at any fleet size.
const planRetention = 14 * 24 * time.Hour

func (j *GuardrailJob) Run(ctx context.Context) {
	if j.svc != nil {
		paused, err := j.svc.Sweep(ctx)
		if err != nil {
			sentry.CaptureException(err)
		} else if paused > 0 {
			log.Info().Int("paused", paused).Msg("guardrail sweep paused campaigns")
		}
	}

	if j.behaviorRepo != nil {
		if _, err := j.behaviorRepo.PurgePlansBefore(ctx, time.Now().Add(-planRetention)); err != nil {
			sentry.CaptureException(err)
		}
	}
}

// GuardrailScheduler runs the job on a fixed interval.
type GuardrailScheduler struct {
	job      *GuardrailJob
	interval time.Duration
	stopCh   chan struct{}
}

// NewGuardrailScheduler builds a scheduler. Fifteen minutes is the balance
// point: a campaign sending at the platform's per-mailbox defaults cannot do
// much damage inside one window, and evaluating more often would mostly re-read
// the same counts.
func NewGuardrailScheduler(job *GuardrailJob, interval time.Duration) *GuardrailScheduler {
	return &GuardrailScheduler{
		job:      job,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start runs Run() on every tick until ctx is cancelled or Stop() is called.
func (s *GuardrailScheduler) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.job.Run(ctx)

	for {
		select {
		case <-ticker.C:
			s.job.Run(ctx)
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// Stop halts the scheduler.
func (s *GuardrailScheduler) Stop() {
	close(s.stopCh)
}
