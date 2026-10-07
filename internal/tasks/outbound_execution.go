package tasks

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *tasksService) ValidateOutboundExecution(ctx context.Context, taskID uuid.UUID) error {
	task, err := s.taskRepo.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if task == nil {
		return errors.New("send task unavailable")
	}
	a, xerr := s.emailRepo.GetByID(ctx, task.EmailAccountID)
	if xerr != nil {
		return xerr
	}
	if a == nil || a.Status != "active" || a.OrganizationID == nil {
		return errors.New("mailbox unavailable")
	}
	if s.orgRiskRepo != nil {
		states, err := s.orgRiskRepo.GetOrgRiskStates(ctx, []uuid.UUID{*a.OrganizationID})
		if err != nil {
			return err
		}
		if states[*a.OrganizationID] == models.OrgRiskSuspended {
			return errors.New("organization sending suspended")
		}
	}
	if s.domainAuthBlocked(ctx, a) {
		return errors.New("sending domain authentication unavailable")
	}
	if task.TaskType == "warmup" {
		return s.ValidateWarmupExecution(ctx, taskID)
	}
	if task.TaskType == "placement" && !a.TestSendingAllowed() {
		return errors.New("diagnostic sending stopped")
	}
	if task.TaskType == "campaign" {
		ct, err := s.taskRepo.GetCampaignTask(ctx, taskID)
		if err != nil {
			return err
		}
		if ct == nil || ct.CampaignID == nil {
			return errors.New("campaign unavailable")
		}
		gate, ok := s.scheduler.(interface {
			OutboundExecutionNotBefore(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, time.Time) (time.Time, error)
		})
		if !ok {
			return errors.New("campaign execution policy unavailable")
		}
		now := time.Now()
		at, err := gate.OutboundExecutionNotBefore(ctx, a.ID, *ct.CampaignID, taskID, now)
		if err != nil {
			return err
		}
		if at.After(now) {
			return errors.New("send outside current calendar or pacing")
		}
	}
	return nil
}
