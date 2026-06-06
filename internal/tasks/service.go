package tasks

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/advanced"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/feature"
	warmupapp "github.com/warmbly/warmbly/internal/app/warmup"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/infrastructure/gtasks"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/scheduler"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

// Type aliases for repository types
type (
	Task                = repository.Task
	CampaignTask        = repository.CampaignTask
	WarmupTask          = repository.WarmupTask
	ContactSequencePair = repository.ContactSequencePair
)

// Type aliases for model types
type (
	Email    = models.Email
	Contact  = models.Contact
	Campaign = models.Campaign
	Sequence = models.Sequence
)

type TasksService interface {
	HandleCampaignTask(task *proto.ProcessTask) *errx.Error
	HandleEmailTask(task *proto.ProcessTask) *errx.Error
	HandleUserEmailTask(task *proto.ProcessTask) *errx.Error

	// Test email support
	SendTestEmail(ctx context.Context, userID string, accountID uuid.UUID, recipient string, campaign *models.Campaign, sequence *models.Sequence) *errx.Error
	GetCampaignSequences(ctx context.Context, campaignID uuid.UUID) ([]models.Sequence, error)

	// Warmup scheduling lifecycle
	EnsureWarmupScheduled(ctx context.Context, accountID uuid.UUID) error
	StartWarmupReconciler(ctx context.Context, interval time.Duration)
}

type tasksService struct {
	// Infrastructure
	tasksClient        *gtasks.Client
	producerClient     *kafka.Producer
	generationClient   *generation.GenerationClient
	streamingPublisher *pubsub.StreamingPublisher
	eventsPublisher    events.Publisher

	// Services
	scheduler     scheduler.SchedulerService
	cipherService cipher.CipherService
	emailSender   EmailSender
	featureGate   feature.FeatureGateService
	advanced      advanced.Service
	warmupHealth  warmupapp.Service

	// Repositories
	taskRepo             repository.TaskRepository
	warmupRepo           repository.WarmupRepository
	warmupRoutingRepo    repository.WarmupRoutingRepository
	warmupContentRepo    repository.WarmupContentRepository
	campaignProgressRepo repository.CampaignProgressRepository
	emailRepo            repository.EmailRepository
	campaignRepo         repository.CampaignRepository
	contactRepo          repository.ContactRepository
	campaignLogRepo      repository.CampaignLogRepository
	attachmentRepo       repository.AttachmentRepository

	// warmupSettings caches the warmup generation settings in-process so the
	// per-send AI-vs-static decision doesn't hit Postgres on every warmup.
	warmupSettings *warmupSettingsCache
}

// warmupSettingsCache is a tiny TTL cache over the generation settings.
type warmupSettingsCache struct {
	mu      sync.RWMutex
	val     models.WarmupGenerationSettings
	fetched time.Time
}

func NewService(
	tasksClient *gtasks.Client,
	producerClient *kafka.Producer,
	generationClient *generation.GenerationClient,
	streamingPublisher *pubsub.StreamingPublisher,
	eventsPublisher events.Publisher,
	scheduler scheduler.SchedulerService,
	cipherService cipher.CipherService,
	emailSender EmailSender,
	featureGate feature.FeatureGateService,
	warmupHealth warmupapp.Service,
	taskRepo repository.TaskRepository,
	warmupRepo repository.WarmupRepository,
	warmupRoutingRepo repository.WarmupRoutingRepository,
	warmupContentRepo repository.WarmupContentRepository,
	campaignProgressRepo repository.CampaignProgressRepository,
	emailRepo repository.EmailRepository,
	campaignRepo repository.CampaignRepository,
	contactRepo repository.ContactRepository,
	campaignLogRepo repository.CampaignLogRepository,
	advanced advanced.Service,
	attachmentRepo repository.AttachmentRepository,
) TasksService {
	return &tasksService{
		tasksClient:          tasksClient,
		producerClient:       producerClient,
		generationClient:     generationClient,
		streamingPublisher:   streamingPublisher,
		eventsPublisher:      eventsPublisher,
		scheduler:            scheduler,
		cipherService:        cipherService,
		emailSender:          emailSender,
		featureGate:          featureGate,
		advanced:             advanced,
		warmupHealth:         warmupHealth,
		taskRepo:             taskRepo,
		warmupRepo:           warmupRepo,
		warmupRoutingRepo:    warmupRoutingRepo,
		warmupContentRepo:    warmupContentRepo,
		campaignProgressRepo: campaignProgressRepo,
		emailRepo:            emailRepo,
		campaignRepo:         campaignRepo,
		contactRepo:          contactRepo,
		campaignLogRepo:      campaignLogRepo,
		attachmentRepo:       attachmentRepo,
		warmupSettings:       &warmupSettingsCache{},
	}
}
