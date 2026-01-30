package campaign

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
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

func (s *campaignService) StartCampaign(ctx context.Context, orgID uuid.UUID, campaignID string) *errx.Error {
	cID, parseErr := uuid.Parse(campaignID)
	if parseErr != nil {
		return errx.ErrUuid
	}

	// Get campaign
	campaign, err := s.campaignRepository.GetByID(ctx, cID)
	if err != nil {
		return errx.InternalError()
	}
	if campaign == nil {
		return errx.ErrNotFound
	}

	// Verify it belongs to the org
	if campaign.OrganizationID == nil || *campaign.OrganizationID != orgID {
		return errx.ErrNotFound
	}

	// Verify status allows starting
	if campaign.Status != "draft" && campaign.Status != "paused" && campaign.Status != "paused_no_accounts" {
		return errx.New(errx.BadRequest, "campaign must be in draft, paused, or paused_no_accounts status to start")
	}

	// Check cooldown
	if campaign.LastStatusChangeAt != nil {
		elapsed := time.Since(*campaign.LastStatusChangeAt)
		if elapsed.Seconds() < campaignCooldownSeconds {
			return errx.New(errx.BadRequest, "please wait before changing campaign status")
		}
	}

	// Check feature gate
	if s.featureGate != nil {
		canSend, xerr := s.featureGate.CanSendCampaignEmail(ctx, orgID)
		if xerr != nil {
			return xerr
		}
		if !canSend {
			return errx.New(errx.Forbidden, "your plan does not allow sending campaign emails")
		}
	}

	// Validate campaign readiness
	if err := s.campaignRepository.ValidateCampaignReady(ctx, cID); err != nil {
		var bizErr *errx.Error
		if errors.As(err, &bizErr) {
			return bizErr
		}
		return errx.InternalError()
	}

	// Validate active email accounts exist for the campaign's email tags
	accounts, xerr := s.emailRepo.GetByTags(ctx, campaign.UserID, campaign.EmailTags)
	if xerr != nil {
		return xerr
	}
	if len(accounts) == 0 {
		return errx.New(errx.BadRequest, "no active email accounts found for campaign's email tags")
	}

	// Set status to active
	if err := s.campaignRepository.StartCampaign(ctx, cID); err != nil {
		return errx.InternalError()
	}

	// Log campaign started
	if s.campaignLogRepo != nil {
		s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
			CampaignID: cID,
			EventType:  "started",
			Message:    "Campaign started",
		})
	}

	// Publish realtime event
	if s.streamingPublisher != nil {
		s.streamingPublisher.PublishCampaignEvent(ctx, &pubsub.CampaignEvent{
			BaseEvent: pubsub.BaseEvent{
				EventType: pubsub.EventCampaignStarted,
				UserID:    campaign.UserID,
			},
			CampaignID: cID.String(),
			Name:       campaign.Name,
			Status:     "active",
		})
	}

	return nil
}

func (s *campaignService) StopCampaign(ctx context.Context, orgID uuid.UUID, campaignID string) *errx.Error {
	cID, parseErr := uuid.Parse(campaignID)
	if parseErr != nil {
		return errx.ErrUuid
	}

	// Get campaign
	campaign, err := s.campaignRepository.GetByID(ctx, cID)
	if err != nil {
		return errx.InternalError()
	}
	if campaign == nil {
		return errx.ErrNotFound
	}

	// Verify it belongs to the org
	if campaign.OrganizationID == nil || *campaign.OrganizationID != orgID {
		return errx.ErrNotFound
	}

	// Verify status
	if campaign.Status != "active" {
		return errx.New(errx.BadRequest, "campaign must be active to stop")
	}

	// Check cooldown
	if campaign.LastStatusChangeAt != nil {
		elapsed := time.Since(*campaign.LastStatusChangeAt)
		if elapsed.Seconds() < campaignCooldownSeconds {
			return errx.New(errx.BadRequest, "please wait before changing campaign status")
		}
	}

	// Set status to paused
	if err := s.campaignRepository.StopCampaign(ctx, cID); err != nil {
		return errx.InternalError()
	}

	// Log campaign stopped
	if s.campaignLogRepo != nil {
		s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
			CampaignID: cID,
			EventType:  "stopped",
			Message:    "Campaign stopped by user",
		})
	}

	// Publish realtime event
	if s.streamingPublisher != nil {
		s.streamingPublisher.PublishCampaignEvent(ctx, &pubsub.CampaignEvent{
			BaseEvent: pubsub.BaseEvent{
				EventType: pubsub.EventCampaignPaused,
				UserID:    campaign.UserID,
			},
			CampaignID: cID.String(),
			Name:       campaign.Name,
			Status:     "paused",
		})
	}

	// Get and delete all pending tasks
	pendingTasks, err := s.campaignRepository.GetPendingCampaignTasks(ctx, cID)
	if err != nil {
		// Log but don't fail the stop
		return nil
	}

	for _, task := range pendingTasks {
		// Delete from DB (GCP Cloud Tasks will fail gracefully when triggered)
		if s.taskRepo != nil {
			_ = s.taskRepo.DeleteTask(ctx, task.ID)
		}
	}

	return nil
}

func (s *campaignService) GetLogs(ctx context.Context, userID, campaignID string, limit int, cursor *string) (*models.CampaignLogsResult, *errx.Error) {
	cID, parseErr := uuid.Parse(campaignID)
	if parseErr != nil {
		return nil, errx.ErrUuid
	}

	// Verify user owns this campaign
	_, err := s.campaignRepository.Get(ctx, userID, campaignID)
	if err != nil {
		if errors.Is(err, errx.ErrResourceNotFound) {
			return nil, errx.ErrNotFound
		}
		return nil, errx.InternalError()
	}

	result, err := s.campaignLogRepo.GetLogs(ctx, cID, limit, cursor)
	if err != nil {
		return nil, errx.InternalError()
	}

	return result, nil
}
