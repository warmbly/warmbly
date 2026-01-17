package campaign

import (
	"context"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type CampaignService interface {
	Create(ctx context.Context, userID string, data *models.CreateCampaign) (*models.Campaign, *errx.Error)
	Get(ctx context.Context, userID, id string) (*models.Campaign, *errx.Error)
	Search(ctx context.Context, userID, query, cursor, folder, limit string) (*models.CampaignsResult, *errx.Error)
	Update(ctx context.Context, userID, id string, data *models.UpdateCampaign) (*models.Campaign, *errx.Error)
	Delete(ctx context.Context, userID, id string) *errx.Error
}

type campaignService struct {
	campaignRepository repository.CampaignRepository
}

func NewService(campaignRepository repository.CampaignRepository) CampaignService {
	return &campaignService{
		campaignRepository: campaignRepository,
	}
}
