package jobs

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/warmbly/warmbly/internal/app/orgtransfer"
)

// OrgTransferJob keeps the workspace-archive tables honest. It deletes archives
// past their retention window (each one is a full copy of a workspace, so they
// do not accumulate) and closes out any export or import whose process died
// mid-run.
//
// Transfers themselves execute in the process that accepted the request, so
// that the operator's passphrase is never persisted anywhere. That is exactly
// what makes the second half of this job necessary: a restart would otherwise
// leave a job showing "running" forever.
type OrgTransferJob struct {
	svc orgtransfer.Service
}

// NewOrgTransferJob constructs the job.
func NewOrgTransferJob(svc orgtransfer.Service) *OrgTransferJob {
	return &OrgTransferJob{svc: svc}
}

// Run performs one tick. Errors are reported and swallowed so the scheduler
// keeps ticking; the next tick retries whatever did not land.
func (j *OrgTransferJob) Run(ctx context.Context) {
	if j.svc == nil {
		return
	}
	if _, err := j.svc.PurgeExpiredExports(ctx); err != nil {
		sentry.CaptureException(err)
	}
}

// OrgTransferScheduler runs the job on a fixed interval.
type OrgTransferScheduler struct {
	job      *OrgTransferJob
	interval time.Duration
	stopCh   chan struct{}
}

// NewOrgTransferScheduler builds a scheduler. Hourly is ample: the retention
// window is measured in days and the stale-job deadline in hours.
func NewOrgTransferScheduler(job *OrgTransferJob, interval time.Duration) *OrgTransferScheduler {
	return &OrgTransferScheduler{
		job:      job,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start runs Run() on every tick until ctx is cancelled or Stop() is called.
func (s *OrgTransferScheduler) Start(ctx context.Context) {
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
func (s *OrgTransferScheduler) Stop() {
	close(s.stopCh)
}
