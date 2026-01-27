package tasks

import (
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/feature"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/gtasks"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
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
}

type tasksService struct {
	// Infrastructure
	tasksClient      *gtasks.Client
	producerClient   *kafka.Producer
	generationClient *generation.GenerationClient

	// Services
	scheduler     scheduler.SchedulerService
	cipherService cipher.CipherService
	emailSender   EmailSender
	featureGate   feature.FeatureGateService

	// Repositories
	taskRepo             repository.TaskRepository
	warmupRepo           repository.WarmupRepository
	campaignProgressRepo repository.CampaignProgressRepository
	emailRepo            repository.EmailRepository
	campaignRepo         repository.CampaignRepository
	contactRepo          repository.ContactRepository
}

func NewService(
	tasksClient *gtasks.Client,
	producerClient *kafka.Producer,
	generationClient *generation.GenerationClient,
	scheduler scheduler.SchedulerService,
	cipherService cipher.CipherService,
	emailSender EmailSender,
	featureGate feature.FeatureGateService,
	taskRepo repository.TaskRepository,
	warmupRepo repository.WarmupRepository,
	campaignProgressRepo repository.CampaignProgressRepository,
	emailRepo repository.EmailRepository,
	campaignRepo repository.CampaignRepository,
	contactRepo repository.ContactRepository,
) TasksService {
	return &tasksService{
		tasksClient:          tasksClient,
		producerClient:       producerClient,
		generationClient:     generationClient,
		scheduler:            scheduler,
		cipherService:        cipherService,
		emailSender:          emailSender,
		featureGate:          featureGate,
		taskRepo:             taskRepo,
		warmupRepo:           warmupRepo,
		campaignProgressRepo: campaignProgressRepo,
		emailRepo:            emailRepo,
		campaignRepo:         campaignRepo,
		contactRepo:          contactRepo,
	}
}
