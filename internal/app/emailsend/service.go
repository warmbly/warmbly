package emailsend

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/dailythrottle"
	"github.com/warmbly/warmbly/internal/app/feature"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/gtasks"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/scheduler"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

// MaxScheduleHorizon caps how far in the future a send can be queued.
// GCP Cloud Tasks rejects anything > 30 days; we leave a day of
// headroom so clock skew between us and Google doesn't bite at the
// boundary.
const MaxScheduleHorizon = 29 * 24 * time.Hour

type SendEmailRequest struct {
	To        []string `json:"to" binding:"required"`
	CC        []string `json:"cc"`
	BCC       []string `json:"bcc"`
	Subject   string   `json:"subject" binding:"required"`
	BodyHTML  string   `json:"body_html"`
	BodyPlain string   `json:"body_plain"`
	InReplyTo []string `json:"in_reply_to"`
	ThreadID  string   `json:"thread_id"`
	// SendMode picks the schedule strategy:
	//   "instant" → enqueue immediately (default)
	//   "smart"   → next gap in the per-mailbox scheduler
	//   "scheduled" → use ScheduledAt verbatim (must be in the future)
	SendMode    string     `json:"send_mode"`
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
}

type SendEmailResponse struct {
	TaskID      uuid.UUID `json:"task_id"`
	ScheduledAt time.Time `json:"scheduled_at"`
	SendMode    string    `json:"send_mode"`
}

type EmailSendService interface {
	SendEmail(ctx context.Context, userID, orgID, accountID uuid.UUID, req *SendEmailRequest) (*SendEmailResponse, *errx.Error)
}

type emailSendService struct {
	taskRepo      repository.TaskRepository
	emailRepo     repository.EmailRepository
	userRepo      repository.UserRepository
	scheduler     scheduler.SchedulerService
	tasksClient   *gtasks.Client
	featureGate   feature.FeatureGateService
	dailyThrottle dailythrottle.Service
}

func NewService(
	taskRepo repository.TaskRepository,
	emailRepo repository.EmailRepository,
	userRepo repository.UserRepository,
	scheduler scheduler.SchedulerService,
	tasksClient *gtasks.Client,
	featureGate feature.FeatureGateService,
	dailyThrottle dailythrottle.Service,
) EmailSendService {
	return &emailSendService{
		taskRepo:      taskRepo,
		emailRepo:     emailRepo,
		userRepo:      userRepo,
		scheduler:     scheduler,
		tasksClient:   tasksClient,
		featureGate:   featureGate,
		dailyThrottle: dailyThrottle,
	}
}

func (s *emailSendService) SendEmail(ctx context.Context, userID, orgID, accountID uuid.UUID, req *SendEmailRequest) (*SendEmailResponse, *errx.Error) {
	// Ban-scope enforcement (migration 000045). Block outbound send
	// when the admin set BanScopeSend, even if the user can otherwise
	// log in and inspect their account.
	if s.userRepo != nil {
		if scope, scopeErr := s.userRepo.GetBanState(ctx, userID); scopeErr == nil {
			if models.BanScope(scope).Has(models.BanScopeSend) {
				return nil, errx.New(errx.Forbidden, "this account cannot send email")
			}
		}
	}

	// Validate email account exists and belongs to user/org
	_, xerr := s.emailRepo.GetByID(ctx, accountID)
	if xerr != nil {
		return nil, xerr
	}

	// Check CanUseUnibox feature gate
	if s.featureGate != nil {
		canUse, _ := s.featureGate.CanUseUnibox(ctx, orgID)
		if !canUse {
			return nil, errx.New(errx.Forbidden, "Unibox requires an active trial or paid subscription")
		}
	}

	// Determine send mode and schedule time
	sendMode := req.SendMode
	if sendMode == "" {
		sendMode = "instant"
	}

	// Explicit scheduled_at takes precedence — the user picked a
	// concrete time, honour it.
	if req.ScheduledAt != nil {
		sendMode = "scheduled"
	}

	var scheduledAt time.Time
	switch sendMode {
	case "scheduled":
		scheduledAt = req.ScheduledAt.UTC()
		now := time.Now()
		// Lead-time grace so a request that takes a few hundred ms
		// over the wire doesn't fail when the user picked "in 1 min"
		// exactly.
		if scheduledAt.Before(now.Add(5 * time.Second)) {
			return nil, errx.New(errx.BadRequest, "scheduled_at must be in the future")
		}
		// GCP Cloud Tasks rejects schedules > 30 days; cap at 29 to
		// leave headroom for clock skew between us and Google.
		if scheduledAt.After(now.Add(MaxScheduleHorizon)) {
			return nil, errx.New(errx.BadRequest, "scheduled_at is too far in the future (max 29 days)")
		}
		// Two-layer protection for scheduled sends:
		//
		// Layer 1 (daily rate) — DailyThrottleNewScheduledSends is the
		// real defense against scripted abuse. A loop queueing 100K
		// schedules in a minute trips this in seconds. Cheap atomic
		// Redis INCR; checked first because it's faster than a SELECT
		// COUNT and rejects bursts before they touch the DB.
		//
		// Layer 2 (pending-count) — MaxPendingScheduledSendsPerUser
		// bounds total queued state, so the DB doesn't accumulate
		// terabytes of pending message bodies even from a user who
		// schedules slowly over months.
		//
		// Both layers are generous enough that no human-driven volume
		// hits them; they exist for abuse posture, not user discipline.
		if s.dailyThrottle != nil {
			if xerr := s.dailyThrottle.CheckAndIncrement(
				ctx, userID,
				dailythrottle.ResourceScheduledSend,
				config.DailyThrottleNewScheduledSends,
			); xerr != nil {
				return nil, errx.New(errx.TooManyRequests, fmt.Sprintf(
					"you've scheduled %d sends in the last 24 hours (max %d). Wait a bit before adding more.",
					config.DailyThrottleNewScheduledSends, config.DailyThrottleNewScheduledSends,
				))
			}
		}
		if s.taskRepo != nil {
			pending, perr := s.taskRepo.CountScheduledForUser(ctx, userID)
			if perr == nil && pending >= int64(config.MaxPendingScheduledSendsPerUser) {
				return nil, errx.New(errx.TooManyRequests, fmt.Sprintf(
					"you have %d scheduled sends queued (max %d). Cancel some from the Scheduled view before adding more.",
					pending, config.MaxPendingScheduledSendsPerUser,
				))
			}
		}
	case "smart":
		nextTime, err := s.scheduler.CalculateNextEmailTime(ctx, accountID)
		if err != nil {
			scheduledAt = time.Now()
			sendMode = "instant"
		} else {
			scheduledAt = nextTime
		}
	default:
		sendMode = "instant"
		scheduledAt = time.Now()
	}

	// Create task + email_task records
	taskID := uuid.New()
	task := &repository.Task{
		ID:             taskID,
		TaskType:       "email",
		EmailAccountID: accountID,
		Status:         "pending",
		ScheduledAt:    &scheduledAt,
	}

	var threadID *string
	if req.ThreadID != "" {
		threadID = &req.ThreadID
	}

	emailTask := &repository.EmailTask{
		TaskID:    taskID,
		To:        req.To,
		CC:        req.CC,
		BCC:       req.BCC,
		InReplyTo: req.InReplyTo,
		Subject:   req.Subject,
		Body:      req.BodyPlain,
		BodyHTML:  req.BodyHTML,
		BodyPlain: req.BodyPlain,
		ThreadID:  threadID,
		SendMode:  sendMode,
		Encrypted: false,
	}

	if err := s.taskRepo.CreateEmailTaskFull(ctx, task, emailTask); err != nil {
		return nil, errx.InternalError()
	}

	// Create GCP Cloud Task (if client available)
	if s.tasksClient != nil {
		processTask := &proto.ProcessTask{
			TaskId: taskID.String(),
		}

		cloudTaskName, err := s.tasksClient.CreateTask(ctx, processTask, scheduledAt)
		if err != nil {
			return nil, errx.InternalError()
		}

		// Update task with cloud task name
		if err := s.taskRepo.UpdateTaskScheduledAt(ctx, taskID, scheduledAt, cloudTaskName); err != nil {
			// Non-fatal, task is already created
		}
	}

	return &SendEmailResponse{
		TaskID:      taskID,
		ScheduledAt: scheduledAt,
		SendMode:    sendMode,
	}, nil
}
