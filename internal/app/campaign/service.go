package campaign

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/dailythrottle"
	"github.com/warmbly/warmbly/internal/app/feature"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/scheduler"
	"github.com/warmbly/warmbly/internal/tasksched"
)

const campaignCooldownSeconds = 60

type CampaignService interface {
	Create(ctx context.Context, userID string, orgID *uuid.UUID, data *models.CreateCampaign) (*models.Campaign, *errx.Error)
	Get(ctx context.Context, userID, id string) (*models.Campaign, *errx.Error)
	Search(ctx context.Context, userID, query, cursor, folder, status, limit string) (*models.CampaignsResult, *errx.Error)
	Overview(ctx context.Context, orgID string) (*models.CampaignsOverview, *errx.Error)
	Update(ctx context.Context, userID, id string, data *models.UpdateCampaign) (*models.Campaign, *errx.Error)
	// Delete removes an organization's campaign outright. A running campaign
	// is stopped as part of it: its pending tasks are cancelled in the same
	// transaction, so nothing keeps sending for a campaign that is gone.
	// Returns the deleted campaign so callers can name it after the fact.
	Delete(ctx context.Context, orgID uuid.UUID, campaignID string) (*models.Campaign, *errx.Error)
	// Duplicate creates a draft copy of a campaign's configuration owned by
	// userID. An empty name derives "<source name> (copy)".
	Duplicate(ctx context.Context, orgID, userID uuid.UUID, campaignID, name string) (*models.Campaign, *errx.Error)

	// Start/Stop
	StartCampaign(ctx context.Context, orgID uuid.UUID, campaignID string) *errx.Error
	StopCampaign(ctx context.Context, orgID uuid.UUID, campaignID string) *errx.Error

	// Logs
	GetLogs(ctx context.Context, userID, campaignID string, limit int, cursor *string) (*models.CampaignLogsResult, *errx.Error)

	// WakeCampaigns pulls the parked wakeup of each listed active campaign
	// forward when its next slot is sooner than where it is parked. Called after
	// leads are attached to a campaign: the chain is one self-perpetuating task,
	// so without this a campaign that had nothing to do keeps sleeping and the
	// new leads sit at "Queued / Not started". Best effort and never an error to
	// the caller — the lead was still added.
	WakeCampaigns(ctx context.Context, orgID uuid.UUID, campaignIDs []string)

	// Explicit sender pool (feature 1).
	ListCampaignSenders(ctx context.Context, orgID uuid.UUID, campaignID string) ([]models.CampaignSender, *errx.Error)
	ReplaceCampaignSenders(ctx context.Context, orgID uuid.UUID, campaignID string, in []models.CampaignSenderInput) ([]models.CampaignSender, *errx.Error)

	// Campaign-scoped tracking domain (feature 5). Resolves the override's CNAME
	// and flips verified on success; an unresolved record stays "pending".
	VerifyCampaignTrackingDomain(ctx context.Context, orgID uuid.UUID, campaignID string) (*models.TrackingDomainStatus, *errx.Error)
}

type campaignService struct {
	campaignRepository repository.CampaignRepository
	taskRepo           repository.TaskRepository
	emailRepo          repository.EmailRepository
	campaignLogRepo    repository.CampaignLogRepository
	featureGate        feature.FeatureGateService
	throttle           dailythrottle.Service
	scheduler          scheduler.SchedulerService
	tasksClient        tasksched.Scheduler
	streamingPublisher *pubsub.StreamingPublisher
	attachmentRepo     repository.AttachmentRepository
	storage            storage.Store
	// audienceRepo measures the campaign's list at launch. Optional/nil-safe:
	// without it no launch is ever refused on list quality.
	audienceRepo repository.CampaignAudienceRepository
}

// WireAudience attaches the launch-time list gate.
func (s *campaignService) WireAudience(r repository.CampaignAudienceRepository) {
	s.audienceRepo = r
}

// AudienceAware is the optional capability the caller uses to attach it.
type AudienceAware interface {
	WireAudience(r repository.CampaignAudienceRepository)
}

// AttachmentAware is implemented by the campaign service so main can hand it
// the attachment repository and object store once those exist. Without them
// delete leaves attachment objects behind and duplicate skips attachments.
type AttachmentAware interface {
	WireAttachments(repo repository.AttachmentRepository, store storage.Store)
}

func (s *campaignService) WireAttachments(repo repository.AttachmentRepository, store storage.Store) {
	s.attachmentRepo = repo
	s.storage = store
}

func NewService(
	campaignRepository repository.CampaignRepository,
	taskRepo repository.TaskRepository,
	emailRepo repository.EmailRepository,
	campaignLogRepo repository.CampaignLogRepository,
	featureGate feature.FeatureGateService,
	throttle dailythrottle.Service,
	scheduler scheduler.SchedulerService,
	tasksClient tasksched.Scheduler,
	streamingPublisher *pubsub.StreamingPublisher,
) CampaignService {
	return &campaignService{
		campaignRepository: campaignRepository,
		taskRepo:           taskRepo,
		emailRepo:          emailRepo,
		campaignLogRepo:    campaignLogRepo,
		featureGate:        featureGate,
		throttle:           throttle,
		scheduler:          scheduler,
		tasksClient:        tasksClient,
		streamingPublisher: streamingPublisher,
	}
}
