package jobs

import (
	"context"
	"sync"
	"time"

	"github.com/warmbly/warmbly/internal/observability/errs"

	emailverifyapp "github.com/warmbly/warmbly/internal/app/emailverify"
	"github.com/warmbly/warmbly/internal/jobrun"
)

// EmailVerificationJob verifies a batch of contacts due for a check each run,
// so the platform can drop hard-bouncing addresses before any worker sends
// to them. A run that fills its batch is repeated until the backlog is below
// one batch, so a large import drains at the verifier's speed.
//
// Control-plane only: the in-house verifier dials remote MX hosts on :25 and
// must run from the backend/consumer, never a worker (sending) IP.
type EmailVerificationJob struct {
	svc       emailverifyapp.Service
	batchSize int
}

// NewEmailVerificationJob creates the job. batchSize caps how many contacts are
// verified per pass (defaults to 100 when non-positive).
func NewEmailVerificationJob(svc emailverifyapp.Service, batchSize int) *EmailVerificationJob {
	if batchSize <= 0 {
		batchSize = 100
	}
	return &EmailVerificationJob{svc: svc, batchSize: batchSize}
}

// Run drains the backlog in batches. Safe to call frequently; it no-ops when
// nothing is due.
func (j *EmailVerificationJob) Run(ctx context.Context) error {
	if j.svc == nil {
		return nil
	}
	for {
		n, err := j.svc.VerifyPending(ctx, j.batchSize)
		if err != nil {
			errs.CaptureException(err)
			return err
		}
		if n < j.batchSize || ctx.Err() != nil {
			return nil
		}
	}
}

// EmailVerificationScheduler runs the job on a fixed interval and whenever
// the service is kicked (a re-verify request, an import).
type EmailVerificationScheduler struct {
	job      *EmailVerificationJob
	interval time.Duration
	stopCh   chan struct{}
	mu       sync.Mutex
}

// NewEmailVerificationScheduler creates the scheduler.
func NewEmailVerificationScheduler(job *EmailVerificationJob, interval time.Duration) *EmailVerificationScheduler {
	return &EmailVerificationScheduler{
		job:      job,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start begins scheduled execution.
func (s *EmailVerificationScheduler) Start(ctx context.Context) {
	ctx, cancel := stopContext(ctx, s.stopCh)
	defer cancel()
	// A wake (import, re-verify) runs the same pass outside the tick; the
	// mutex keeps it from overlapping a tick or a "run now".
	run := func(ctx context.Context) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.job.Run(ctx)
	}
	if s.job != nil && s.job.svc != nil {
		wake := s.job.svc.Wake()
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-wake:
					jobrun.Run(ctx, "email_verification", s.interval, run)
				}
			}
		}()
	}
	jobrun.Loop(ctx, "email_verification", s.interval, false, run)
}

// Stop halts the scheduled execution.
func (s *EmailVerificationScheduler) Stop() {
	close(s.stopCh)
}
