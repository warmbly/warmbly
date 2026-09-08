package jobs

import (
	"context"
	"time"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/repository"
)

// AuditRetentionJob deletes audit-log entries older than the retention window.
// Bounding the trail's age also bounds how long PII (IP addresses, user agents,
// change payloads) is retained, which is a privacy-positive property. The
// window is an instance setting, read on every pass.
type AuditRetentionJob struct {
	repo      repository.AuditRepository
	retention RetentionSource
}

// NewAuditRetentionJob creates a retention job for the audit trail. Without a
// wired settings source it prunes at the compiled default window.
func NewAuditRetentionJob(repo repository.AuditRepository) *AuditRetentionJob {
	return &AuditRetentionJob{repo: repo}
}

// WireRetention attaches the instance settings the window is read from.
func (j *AuditRetentionJob) WireRetention(src RetentionSource) *AuditRetentionJob {
	j.retention = src
	return j
}

// Run executes one pruning pass.
func (j *AuditRetentionJob) Run(ctx context.Context) error {
	if j.repo == nil {
		return nil
	}
	days := config.AuditLogRetentionDaysDefault
	if j.retention != nil {
		days = j.retention.RetentionWindows(ctx).AuditLogDays
	}

	cutoff := time.Now().AddDate(0, 0, -days)
	if _, err := j.repo.PruneOlderThan(ctx, cutoff); err != nil {
		errs.CaptureException(err)
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
	ctx, cancel := stopContext(ctx, s.stopCh)
	defer cancel()
	jobrun.Loop(ctx, "audit_retention", s.interval, true, s.job.Run)
}

// Stop halts the scheduled execution.
func (s *AuditRetentionScheduler) Stop() {
	close(s.stopCh)
}
