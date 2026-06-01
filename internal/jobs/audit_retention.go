package jobs

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/warmbly/warmbly/internal/repository"
)

// AuditRetentionJob deletes audit-log entries older than the retention window.
// Bounding the trail's age also bounds how long PII (IP addresses, user agents,
// change payloads) is retained, which is a privacy-positive property.
type AuditRetentionJob struct {
	repo      repository.AuditRepository
	retention time.Duration
}

// NewAuditRetentionJob creates a retention job that prunes entries older than
// the given retention window.
func NewAuditRetentionJob(repo repository.AuditRepository, retention time.Duration) *AuditRetentionJob {
	return &AuditRetentionJob{
		repo:      repo,
		retention: retention,
	}
}

// Run executes one pruning pass.
func (j *AuditRetentionJob) Run(ctx context.Context) error {
	if j.repo == nil || j.retention <= 0 {
		return nil
	}

	cutoff := time.Now().Add(-j.retention)
	if _, err := j.repo.PruneOlderThan(ctx, cutoff); err != nil {
		sentry.CaptureException(err)
		return err
	}
	return nil
}

// AuditRetentionScheduler runs the audit retention job on a fixed interval.
type AuditRetentionScheduler struct {
	job      *AuditRetentionJob
	interval time.Duration
	stopCh   chan struct{}
}

// NewAuditRetentionScheduler creates a scheduler for the retention job.
func NewAuditRetentionScheduler(job *AuditRetentionJob, interval time.Duration) *AuditRetentionScheduler {
	return &AuditRetentionScheduler{
		job:      job,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start begins scheduled execution, running once immediately on boot.
func (s *AuditRetentionScheduler) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	if err := s.job.Run(ctx); err != nil {
		sentry.CaptureException(err)
	}

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
func (s *AuditRetentionScheduler) Stop() {
	close(s.stopCh)
}
