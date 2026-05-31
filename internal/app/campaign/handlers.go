package campaign

import (
	"context"
	"errors"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/dailythrottle"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/scheduler"
	"github.com/warmbly/warmbly/internal/tasks/proto"
	"github.com/warmbly/warmbly/internal/utils/validate"
)

func (s *campaignService) Create(ctx context.Context, userID string, orgID *uuid.UUID, data *models.CreateCampaign) (*models.Campaign, *errx.Error) {
	if err := validate.CampaignName(data.Name); err != nil {
		return nil, err
	}
	if err := validate.CampaignDescription(data.Description); err != nil {
		return nil, err
	}

	// Daily creation throttle (config.DailyThrottleNewCampaigns). Caps
	// per-day new-campaign rate per org so an unlimited plan can't be
	// abused to ramp instantly. Scoped on org when present; otherwise
	// best-effort skipped (the older campaign API allows orgless rows).
	if orgID != nil && s.throttle != nil {
		if xerr := s.throttle.CheckAndIncrement(ctx, *orgID, dailythrottle.ResourceCampaign, config.DailyThrottleNewCampaigns); xerr != nil {
			return nil, xerr
		}
	}

	resp, xerr := s.campaignRepository.Create(ctx, userID, orgID, data)
	if xerr != nil {
		return nil, xerr
	}

	if s.streamingPublisher != nil {
		s.streamingPublisher.PublishCampaignEvent(ctx, &pubsub.CampaignEvent{
			BaseEvent: pubsub.BaseEvent{
				EventType: pubsub.EventCampaignCreated,
				UserID:    userID,
			},
			CampaignID: resp.ID.String(),
			Name:       resp.Name,
			Status:     resp.Status,
		})
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

	activeCampaigns, err := s.campaignRepository.CountActiveForOrganization(ctx, orgID)
	if err != nil {
		return errx.InternalError()
	}
	if activeCampaigns >= config.HardCapCampaignsActive {
		return errx.New(errx.Forbidden, "You have 50 active campaigns. Contact us if you need to run more.")
	}

	// Set status to active
	if err := s.campaignRepository.StartCampaign(ctx, cID); err != nil {
		return errx.InternalError()
	}

	if xerr := s.enqueueCampaignWakeup(ctx, cID); xerr != nil {
		return xerr
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

func (s *campaignService) enqueueCampaignWakeup(ctx context.Context, campaignID uuid.UUID) *errx.Error {
	if s.scheduler == nil || s.tasksClient == nil || s.taskRepo == nil {
		return nil
	}

	nextTime, _, accountID, err := s.scheduler.CalculateNextCampaignTime(ctx, campaignID)
	if err != nil {
		switch {
		case errors.Is(err, scheduler.ErrNoEmailAccounts):
			_ = s.campaignRepository.UpdateStatusWithLock(ctx, campaignID, "paused_no_accounts")
			return errx.New(errx.BadRequest, "no active email accounts found for campaign's email tags")
		case errors.Is(err, scheduler.ErrCampaignCompleted):
			_ = s.campaignRepository.UpdateStatusWithLock(ctx, campaignID, "completed")
			return errx.New(errx.BadRequest, "campaign has no remaining contacts to send")
		default:
			sentry.CaptureException(err)
			return errx.InternalError()
		}
	}

	taskID := uuid.New()
	task := &repository.Task{
		ID:             taskID,
		TaskType:       "campaign",
		EmailAccountID: accountID,
		Status:         "pending",
		ScheduledAt:    &nextTime,
	}
	campaignTask := &repository.CampaignTask{
		TaskID:     taskID,
		CampaignID: &campaignID,
	}

	created, err := s.taskRepo.CreateTaskWithLock(ctx, task, campaignTask)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}
	if !created {
		return nil
	}

	cloudTaskName, err := s.tasksClient.CreateTask(ctx, &proto.ProcessTask{TaskId: taskID.String()}, nextTime)
	if err != nil {
		_ = s.taskRepo.DeleteTask(ctx, taskID)
		_ = s.campaignRepository.StopCampaign(ctx, campaignID)
		sentry.CaptureException(err)
		return errx.New(errx.ServiceUnavailable, "could not schedule campaign right now")
	}
	if err := s.taskRepo.UpdateTaskScheduledAt(ctx, taskID, nextTime, cloudTaskName); err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
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
