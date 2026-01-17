package campaign

import (
	"context"
	"errors"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils/validate"
)

func (s *campaignService) Create(ctx context.Context, userID string, data *models.CreateCampaign) (*models.Campaign, *errx.Error) {
	if err := validate.CampaignName(data.Name); err != nil {
		return nil, err
	}
	if err := validate.CampaignDescription(data.Description); err != nil {
		return nil, err
	}

	resp, err := s.campaignRepository.Create(ctx, userID, data)
	if err != nil {
		return nil, errx.InternalError()
	}

	return resp, nil
}

func (s *campaignService) Get(ctx context.Context, userID, id string) (*models.Campaign, *errx.Error) {
	resp, err := s.campaignRepository.Get(ctx, userID, id)
	if err != nil {
		if errors.Is(err, errx.ErrResourceNotFound) {
			return nil, errx.ErrNotFound
		}

		return nil, errx.InternalError()
	}

	return resp, nil
}

func (s *campaignService) Search(ctx context.Context, userID, query, cursor, folder, limit string) (*models.CampaignsResult, *errx.Error) {
	cursorId, err := validate.Uuid(cursor)
	if err != nil {
		return nil, err
	}
	folderId, err := validate.Uuid(folder)
	if err != nil {
		return nil, err
	}
	limitN, err := validate.Limit(limit)
	if err != nil {
		return nil, err
	}

	resp, xerr := s.campaignRepository.Search(ctx, userID, query, cursorId, folderId, limitN)
	if xerr != nil {
		return nil, errx.InternalError()
	}

	return resp, nil
}

func (s *campaignService) Update(ctx context.Context, userID, query string, data *models.UpdateCampaign) (*models.Campaign, *errx.Error) {
	return s.campaignRepository.Update(ctx, userID, query, data)
}

func (s *campaignService) Delete(ctx context.Context, userID, id string) *errx.Error {
	if err := s.campaignRepository.Delete(ctx, userID, id); err != nil {
		if errors.Is(err, errx.ErrResourceNotFound) {
			return errx.ErrNotFound
		}
		return errx.InternalError()
	}

	return nil
}
