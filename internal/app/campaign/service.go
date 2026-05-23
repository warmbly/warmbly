package campaign

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/feature"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const campaignCooldownSeconds = 60

type CampaignService interface {
	Create(ctx context.Context, userID string, data *models.CreateCampaign) (*models.Campaign, *errx.Error)
	Get(ctx context.Context, userID, id string) (*models.Campaign, *errx.Error)
	Search(ctx context.Context, userID, query, cursor, folder, limit string) (*models.CampaignsResult, *errx.Error)
	Update(ctx context.Context, userID, id string, data *models.UpdateCampaign) (*models.Campaign, *errx.Error)
	Delete(ctx context.Context, userID, id string) *errx.Error

	// Start/Stop
	StartCampaign(ctx context.Context, orgID uuid.UUID, campaignID string) *errx.Error
	StopCampaign(ctx context.Context, orgID uuid.UUID, campaignID string) *errx.Error

	// Logs
	GetLogs(ctx context.Context, userID, campaignID string, limit int, cursor *string) (*models.CampaignLogsResult, *errx.Error)
}

type campaignService struct {
	campaignRepository repository.CampaignRepository
	taskRepo           repository.TaskRepository
	emailRepo          repository.EmailRepository
	campaignLogRepo    repository.CampaignLogRepository
	featureGate        feature.FeatureGateService
	streamingPublisher *pubsub.StreamingPublisher
}

func NewService(
	campaignRepository repository.CampaignRepository,
	taskRepo repository.TaskRepository,
	emailRepo repository.EmailRepository,
	campaignLogRepo repository.CampaignLogRepository,
	featureGate feature.FeatureGateService,
	streamingPublisher *pubsub.StreamingPublisher,
) CampaignService {
	return &campaignService{
		campaignRepository: campaignRepository,
		taskRepo:           taskRepo,
		emailRepo:          emailRepo,
		campaignLogRepo:    campaignLogRepo,
		featureGate:        featureGate,
		streamingPublisher: streamingPublisher,
	}
}
