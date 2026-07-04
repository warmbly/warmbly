package email

import (
	"context"
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
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type EmailService interface {
	Search(ctx context.Context, userID, search, cursor, tag, limit string, allowedAccountIDs []uuid.UUID) (*models.EmailsResult, *errx.Error)
	Get(ctx context.Context, userID, emailAccountID string) (*models.Email, *errx.Error)
	Update(ctx context.Context, userID, emailAccountID string, udata *models.UpdateEmail) (*models.Email, *errx.Error)
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
	producer           *kafka.Producer
	r                  *cache.Cache
	oauthInbox         *config.Oauth2Inbox
	workerAssignment   worker.WorkerAssignmentService
	throttle           dailythrottle.Service
	graphDelta         repository.EmailGraphDeltaRepository
	// webhookService is optional. When non-nil, account lifecycle events
	// (email_account.connected, email_account.removed) are dispatched to
	// subscribed customer webhooks.
	webhookService webhook.Service
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

func NewServiceWithKafka(
	emailRepository repository.EmailRepository,
	cipherService cipher.CipherService,
	featureGate feature.FeatureGateService,
	warmupService warmupapp.Service,
	publisher events.Publisher,
	producer *kafka.Producer,
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
		producer:           producer,
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
