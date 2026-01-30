package emailsend

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/feature"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/gtasks"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/scheduler"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

type SendEmailRequest struct {
	To        []string `json:"to" binding:"required"`
	CC        []string `json:"cc"`
	BCC       []string `json:"bcc"`
	Subject   string   `json:"subject" binding:"required"`
	BodyHTML  string   `json:"body_html"`
	BodyPlain string   `json:"body_plain"`
	InReplyTo []string `json:"in_reply_to"`
	ThreadID  string   `json:"thread_id"`
	SendMode  string   `json:"send_mode"` // "instant" or "smart", defaults to "instant"
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
	taskRepo    repository.TaskRepository
	emailRepo   repository.EmailRepository
	scheduler   scheduler.SchedulerService
	tasksClient *gtasks.Client
	featureGate feature.FeatureGateService
}

func NewService(
	taskRepo repository.TaskRepository,
	emailRepo repository.EmailRepository,
	scheduler scheduler.SchedulerService,
	tasksClient *gtasks.Client,
	featureGate feature.FeatureGateService,
) EmailSendService {
	return &emailSendService{
		taskRepo:    taskRepo,
		emailRepo:   emailRepo,
		scheduler:   scheduler,
		tasksClient: tasksClient,
		featureGate: featureGate,
	}
}

func (s *emailSendService) SendEmail(ctx context.Context, userID, orgID, accountID uuid.UUID, req *SendEmailRequest) (*SendEmailResponse, *errx.Error) {
	// Validate email account exists and belongs to user/org
	_, xerr := s.emailRepo.GetByID(ctx, accountID)
	if xerr != nil {
		return nil, xerr
	}

	// Check CanUseUnibox feature gate
	if s.featureGate != nil {
		canUse, _ := s.featureGate.CanUseUnibox(ctx, orgID)
		if !canUse {
			return nil, errx.New(errx.Forbidden, "Unibox requires a paid subscription")
		}
	}

	// Determine send mode and schedule time
	sendMode := req.SendMode
	if sendMode == "" {
		sendMode = "instant"
	}

	var scheduledAt time.Time
	if sendMode == "smart" {
		nextTime, err := s.scheduler.CalculateNextEmailTime(ctx, accountID)
		if err != nil {
			// Fall back to instant
			scheduledAt = time.Now()
			sendMode = "instant"
		} else {
			scheduledAt = nextTime
		}
	} else {
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
