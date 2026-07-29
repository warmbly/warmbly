package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/notify"
	"github.com/warmbly/warmbly/internal/notify/templates"
	"github.com/warmbly/warmbly/internal/repository"
)

// OrgNotifier raises a permission-targeted org notification (in-app feed +
// each member's enabled channels, email digest-coalesced). Satisfied by
// *notification.Service; local interface to avoid an import cycle.
type OrgNotifier interface {
	NotifyOrg(ctx context.Context, orgID uuid.UUID, perm models.OrganizationPermission, exclude uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any, groupKey string)
}

// TrialExpirationJob handles expired free trials
type TrialExpirationJob struct {
	subRepo                  repository.SubscriptionRepository
	db                       *pgxpool.Pool
	emailNotificationService notify.EmailNotificationService
	notifier                 OrgNotifier
}

// WireNotifier routes trial-expired alerts through the notification system
// (billing members, preference-gated, one coalesced email) instead of the
// legacy direct email to the subscription owner.
func (j *TrialExpirationJob) WireNotifier(n OrgNotifier) {
	j.notifier = n
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

		// Tell whoever can fix it: members with manage_billing, through the
		// notification system (their prefs gate channels; the shared group
		// key coalesces several admins into one email). Falls back to the
		// legacy direct owner email when the notifier isn't wired.
		if j.notifier != nil && sub.OrganizationID != uuid.Nil {
			j.notifier.NotifyOrg(ctx, sub.OrganizationID, models.PermManageBilling, uuid.Nil,
				models.NotifBillingAlert,
				"Your Warmbly trial has expired",
				"Campaigns are paused and warmup is disabled until you upgrade.",
				"/app/settings/billing", nil,
				"trial_expired:"+sub.OrganizationID.String())
			continue
		}
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
	body, err := templates.GenerateTrialExpiredHTML()
	if err != nil {
		// GenerateTrialExpiredHTML already reported to Sentry.
		return
	}

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
