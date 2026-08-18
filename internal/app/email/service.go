package email

import (
	"context"
	"github.com/warmbly/warmbly/internal/app/instancesettings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/dailythrottle"
	"github.com/warmbly/warmbly/internal/app/feature"
	warmupapp "github.com/warmbly/warmbly/internal/app/warmup"
	"github.com/warmbly/warmbly/internal/app/webhook"
	"github.com/warmbly/warmbly/internal/app/worker"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type EmailService interface {
	Search(ctx context.Context, userID, search, cursor, tag, limit string, allowedAccountIDs []uuid.UUID) (*models.EmailsResult, *errx.Error)
	Get(ctx context.Context, userID, emailAccountID string) (*models.Email, *errx.Error)
	Update(ctx context.Context, userID, emailAccountID string, udata *models.UpdateEmail) (*models.Email, *errx.Error)
	// BulkUpdateTags adds/removes tags across many of the user's mailboxes
	// in one call; returns how many of the requested mailboxes were owned.
	BulkUpdateTags(ctx context.Context, userID string, emailIDs, addTags, removeTags []uuid.UUID) (int, *errx.Error)
	// SetWarmupLifecycle starts, pauses, resumes, or disables warmup for a
	// mailbox. start/resume preserve ramp progress; disable turns warmup off.
	SetWarmupLifecycle(ctx context.Context, userID, emailAccountID, action string) (*models.Email, *errx.Error)
	UpdateTrackingDomain(ctx context.Context, userID, emailAccountID, domain string) (*models.TrackingDomainStatus, *errx.Error)
	Delete(ctx context.Context, userID, emailAccountID string) *errx.Error

	// Onboarding flow
	OAuthStart(ctx context.Context, userID string, orgID *uuid.UUID, provider models.InboxProvider) (*models.EmailOnboardingStartResponse, *errx.Error)
	OAuthFinish(ctx context.Context, userID, code, state string) (*models.Email, *errx.Error)
	OnboardSMTPIMAP(ctx context.Context, userID string, orgID *uuid.UUID, data *models.NewSMTPIMAPAccount) (*models.Email, *errx.Error)

	// Optional: wire in the webhook dispatcher after construction. Once
	// set, account-lifecycle events fan out to customer webhook endpoints.
	WireWebhooks(w webhook.Service)
	WireThrottle(t dailythrottle.Service)
	// WireGraphDelta attaches the Graph delta-cursor repository so the worker
	// reconciler can seed a mailbox's saved cursors when loading it.
	WireGraphDelta(repo repository.EmailGraphDeltaRepository)
	// WireEmailHistoryID attaches the Gmail history-cursor repository, the
	// Google counterpart of WireGraphDelta, so a reloaded mailbox resumes from
	// its saved checkpoint instead of re-bootstrapping.
	WireEmailHistoryID(repo repository.EmailHistoryIDRepository)
	// WireSyncState, WireMailboxes and WireSyncBudget feed the sync fair-use
	// payload: resumable backfill state, saved IMAP folder cursors, and the
	// operator-editable budget the mailbox syncs under.
	WireSyncState(repo repository.EmailSyncStateRepository)
	WireMailboxes(repo repository.MailboxRepository)
	WireSyncBudget(src SyncBudgetSource)
	// GetSyncState is the dashboard's view of a mailbox's sync: nil state when
	// the worker has not reported yet.
	GetSyncState(ctx context.Context, userID, emailID string) (*models.SyncState, models.SyncPolicy, *errx.Error)
	// StartWorkerReconciler periodically ensures every active mailbox is
	// assigned to a worker and loaded onto it (blocks until ctx is cancelled).
	StartWorkerReconciler(ctx context.Context, interval time.Duration)
}

type emailService struct {
	emailRepository    repository.EmailRepository
	cipherService      cipher.CipherService
	featureGate        feature.FeatureGateService
	warmupService      warmupapp.Service
	publisher          events.Publisher
	streamingPublisher *pubsub.StreamingPublisher
	r                  *cache.Cache
	oauthInbox         *config.Oauth2Inbox
	workerAssignment   worker.WorkerAssignmentService
	throttle           dailythrottle.Service
	graphDelta         repository.EmailGraphDeltaRepository
	historyID          repository.EmailHistoryIDRepository
	syncState          repository.EmailSyncStateRepository
	mailboxes          repository.MailboxRepository
	syncBudget         SyncBudgetSource
	// webhookService is optional. When non-nil, account lifecycle events
	// (email_account.connected, email_account.removed) are dispatched to
	// subscribed customer webhooks.
	webhookService webhook.Service
}

// SyncBudgetSource is the operator-editable sync fair-use section, satisfied
// by instancesettings.Service. Injected post-construction; when unset the
// loader ships compiled defaults.
type SyncBudgetSource interface {
	SyncBudget(ctx context.Context) instancesettings.Sync
}

// WireSyncState attaches the mailbox sync-state repository so a (re)loaded
// mailbox resumes its backfill and the API can report progress.
func (s *emailService) WireSyncState(repo repository.EmailSyncStateRepository) {
	s.syncState = repo
}

// WireMailboxes attaches the IMAP folder-state repository so a reloaded IMAP
// mailbox resumes incrementally from its saved HIGHESTMODSEQ per folder
// instead of re-walking every folder from scratch.
func (s *emailService) WireMailboxes(repo repository.MailboxRepository) {
	s.mailboxes = repo
}

// WireSyncBudget attaches the instance settings the sync policy is read from.
func (s *emailService) WireSyncBudget(src SyncBudgetSource) {
	s.syncBudget = src
}

// WireThrottle attaches the daily-creation throttle after construction
// so callers without a Redis cache (jobs, tests) need not provide one.
// When unset, guardMailboxThrottle is a no-op.
func (s *emailService) WireThrottle(t dailythrottle.Service) {
	s.throttle = t
}

// WireWebhooks attaches the webhook dispatcher after construction. Done
// post-construction so callers without a webhook stack (tests, jobs) need
// not provide one.
func (s *emailService) WireWebhooks(w webhook.Service) {
	s.webhookService = w
}

func NewService(
	emailRepository repository.EmailRepository,
	cipherService cipher.CipherService,
	featureGate feature.FeatureGateService,
	warmupService warmupapp.Service,
	publisher events.Publisher,
	streamingPublisher ...*pubsub.StreamingPublisher,
) EmailService {
	var realtime *pubsub.StreamingPublisher
	if len(streamingPublisher) > 0 {
		realtime = streamingPublisher[0]
	}

	return &emailService{
		emailRepository:    emailRepository,
		cipherService:      cipherService,
		featureGate:        featureGate,
		warmupService:      warmupService,
		publisher:          publisher,
		streamingPublisher: realtime,
	}
}

// NewServiceWithWorker builds the email service with the deps needed for
// worker-facing flows (credential validation over the event bus, mailbox OAuth,
// worker assignment). Publishing goes through the events.Publisher, so this is
// transport-agnostic (Kafka or NATS).
func NewServiceWithWorker(
	emailRepository repository.EmailRepository,
	cipherService cipher.CipherService,
	featureGate feature.FeatureGateService,
	warmupService warmupapp.Service,
	publisher events.Publisher,
	r *cache.Cache,
	oauthInbox *config.Oauth2Inbox,
	workerAssignment worker.WorkerAssignmentService,
	streamingPublisher ...*pubsub.StreamingPublisher,
) EmailService {
	var realtime *pubsub.StreamingPublisher
	if len(streamingPublisher) > 0 {
		realtime = streamingPublisher[0]
	}

	return &emailService{
		emailRepository:    emailRepository,
		cipherService:      cipherService,
		featureGate:        featureGate,
		warmupService:      warmupService,
		publisher:          publisher,
		streamingPublisher: realtime,
		r:                  r,
		oauthInbox:         oauthInbox,
		workerAssignment:   workerAssignment,
	}
}

func (s *emailService) publishAccountEvent(ctx context.Context, eventType pubsub.EventType, account *models.Email) {
	if s.streamingPublisher == nil || account == nil {
		return
	}

	var orgID string
	if account.OrganizationID != nil {
		orgID = account.OrganizationID.String()
	}
	s.streamingPublisher.PublishAccountEvent(ctx, &pubsub.AccountEvent{
		BaseEvent: pubsub.BaseEvent{
			EventType: eventType,
			UserID:    account.UserID,
		},
		OrgID:          orgID,
		EmailAccountID: account.ID.String(),
		Email:          account.Email,
		Provider:       account.Provider,
		Status:         account.Status,
	})
}

// GetSyncState returns the persisted sync state and the policy currently in
// force. It goes through Get so ownership is checked the same way as every
// other per-mailbox read.
func (s *emailService) GetSyncState(ctx context.Context, userID, emailID string) (*models.SyncState, models.SyncPolicy, *errx.Error) {
	acc, xerr := s.Get(ctx, userID, emailID)
	if xerr != nil {
		return nil, models.SyncPolicy{}, xerr
	}
	data := s.syncDataFor(ctx, acc.ID)
	return data.State, data.Policy, nil
}
