package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/notify"
	"github.com/warmbly/warmbly/internal/repository"
)

// TrialExpirationJob handles expired free trials
type TrialExpirationJob struct {
	subRepo                  repository.SubscriptionRepository
	db                       *pgxpool.Pool
	emailNotificationService notify.EmailNotificationService
}

// NewTrialExpirationJob creates a new trial expiration job
func NewTrialExpirationJob(
	subRepo repository.SubscriptionRepository,
	emailNotificationService notify.EmailNotificationService,
) *TrialExpirationJob {
	return &TrialExpirationJob{
		subRepo:                  subRepo,
		emailNotificationService: emailNotificationService,
	}
}

// NewTrialExpirationJobWithDB creates a new trial expiration job with database pool
// This is used when additional DB operations are needed (pausing campaigns, disabling warmup)
func NewTrialExpirationJobWithDB(
	subRepo repository.SubscriptionRepository,
	db *pgxpool.Pool,
	emailNotificationService notify.EmailNotificationService,
) *TrialExpirationJob {
	return &TrialExpirationJob{
		subRepo:                  subRepo,
		db:                       db,
		emailNotificationService: emailNotificationService,
	}
}

// Run executes the trial expiration job
// This should be run periodically (e.g., every hour via cron or scheduler)
func (j *TrialExpirationJob) Run(ctx context.Context) error {
	// Skip if no DB connection for operations
	if j.db == nil {
		return nil
	}

	// Find expired trials without paid subscription
	expiredSubs, err := repository.GetExpiredTrialsWithoutPayment(ctx, j.db)
	if err != nil {
		sentry.CaptureException(err)
		return fmt.Errorf("failed to get expired trials: %w", err)
	}

	for _, sub := range expiredSubs {
		// Pause all active campaigns for this organization
		if err := repository.PauseCampaignsByOrganizationID(ctx, j.db, sub.OrganizationID, "paused_trial_expired"); err != nil {
			sentry.CaptureException(err)
			// Continue processing other organizations
		}

		// Disable warmup on all email accounts (they're already blocked, but clean up)
		if err := repository.DisableWarmupByOrganizationID(ctx, j.db, sub.OrganizationID); err != nil {
			sentry.CaptureException(err)
			// Continue processing other organizations
		}

		// Mark subscription as expired
		if err := repository.MarkSubscriptionTrialExpired(ctx, j.db, sub.ID); err != nil {
			sentry.CaptureException(err)
			// Continue processing other users
		}

		// Send notification email to user
		userEmail := ""
		if sub.UserEmail != nil {
			userEmail = *sub.UserEmail
		}
		j.notifyTrialExpired(ctx, sub.UserID, userEmail)
	}

	return nil
}

// notifyTrialExpired sends an email notification about trial expiration
func (j *TrialExpirationJob) notifyTrialExpired(ctx context.Context, userID interface{}, userEmail string) {
	if j.emailNotificationService == nil || userEmail == "" {
		return
	}

	subject := "Your Warmbly trial has expired"
	body := `
		<h2>Your trial has ended</h2>
		<p>Thank you for trying Warmbly! Your free trial has now expired.</p>
		<p>During your trial, your campaigns have been paused and warmup has been disabled to prevent any service interruptions.</p>
		<h3>What happens now?</h3>
		<ul>
			<li>Your data is safe and will be preserved</li>
			<li>Campaigns are paused but not deleted</li>
			<li>Email accounts remain connected</li>
		</ul>
		<p>To resume your campaigns and continue using Warmbly, please upgrade to a paid plan:</p>
		<p><a href="https://app.warmbly.com/settings/billing" style="display:inline-block;padding:12px 24px;background:#4F46E5;color:white;text-decoration:none;border-radius:6px;">Choose a Plan</a></p>
		<p>If you have any questions, our support team is here to help.</p>
	`

	if err := j.emailNotificationService.Send(ctx, []string{userEmail}, nil, nil, subject, body); err != nil {
		sentry.CaptureException(err)
	}
}

// TrialExpirationScheduler runs the trial expiration job on a schedule
type TrialExpirationScheduler struct {
	job      *TrialExpirationJob
	interval time.Duration
	stopCh   chan struct{}
}

// NewTrialExpirationScheduler creates a new scheduler
func NewTrialExpirationScheduler(job *TrialExpirationJob, interval time.Duration) *TrialExpirationScheduler {
	return &TrialExpirationScheduler{
		job:      job,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start begins the scheduled execution
func (s *TrialExpirationScheduler) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Run immediately on start
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

// Stop halts the scheduled execution
func (s *TrialExpirationScheduler) Stop() {
	close(s.stopCh)
}
