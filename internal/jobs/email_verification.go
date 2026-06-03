package jobs

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go"

	emailverifyapp "github.com/warmbly/warmbly/internal/app/emailverify"
)

// EmailVerificationJob verifies a capped batch of not-yet-checked contacts each
// run, so the platform can drop hard-bouncing addresses before any worker sends
// to them. It is a thin wrapper around emailverify.Service.VerifyPending; the
// per-tick cap bounds how much outbound SMTP-probe work one pass does.
//
// Control-plane only: the underlying verifier dials remote MX hosts on :25 and
// must run from the backend/consumer, never a worker (sending) IP.
type EmailVerificationJob struct {
	svc       emailverifyapp.Service
	batchSize int
}

// NewEmailVerificationJob creates the job. batchSize caps how many contacts are
// verified per tick (defaults to 100 when non-positive).
func NewEmailVerificationJob(svc emailverifyapp.Service, batchSize int) *EmailVerificationJob {
	if batchSize <= 0 {
		batchSize = 100
	}
	return &EmailVerificationJob{svc: svc, batchSize: batchSize}
}

// Run performs one capped verification pass. Safe to call frequently — it
// no-ops when there are no unverified contacts.
func (j *EmailVerificationJob) Run(ctx context.Context) error {
	if j.svc == nil {
		return nil
	}
	if _, err := j.svc.VerifyPending(ctx, j.batchSize); err != nil {
		sentry.CaptureException(err)
		return err
	}
	return nil
}

// EmailVerificationScheduler runs the job on a fixed interval.
type EmailVerificationScheduler struct {
	job      *EmailVerificationJob
	interval time.Duration
	stopCh   chan struct{}
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
func (s *EmailVerificationScheduler) Stop() {
	close(s.stopCh)
}
