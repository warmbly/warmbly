package emailsend

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/dailythrottle"
	"github.com/warmbly/warmbly/internal/app/feature"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/scheduler"
	"github.com/warmbly/warmbly/internal/tasks"
	"github.com/warmbly/warmbly/internal/tasks/proto"
	"github.com/warmbly/warmbly/internal/tasksched"
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
	// Forward is the stored message this send forwards, carried under the
	// body and signature. The caller has already checked it is the org's.
	Forward *models.ForwardedMessage `json:"-"`
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
	tasksClient   tasksched.Scheduler
	featureGate   feature.FeatureGateService
	dailyThrottle dailythrottle.Service
	// orgRiskRepo stops a suspended workspace sending. Optional/nil-safe: no
	// repository means no organization is ever gated on risk.
	orgRiskRepo repository.OrgRiskRepository
	// trackedLinkRepo stores the click tickets a tracked direct send mints.
	// Optional: without it the pixel still goes on and links ship untouched.
	trackedLinkRepo repository.TrackedLinkRepository
}

// WireTrackedLinks attaches the click-ticket store. Off the constructor for the
// same reason as org risk: the service stays constructible without it.
func (s *emailSendService) WireTrackedLinks(r repository.TrackedLinkRepository) {
	s.trackedLinkRepo = r
}

// TrackedLinksAware is the optional capability the caller uses to attach the
// click-ticket store.
type TrackedLinksAware interface {
	WireTrackedLinks(r repository.TrackedLinkRepository)
}

// WireOrgRisk attaches the organization risk posture. Kept off the constructor
// so the service stays constructible where risk is not wired.
func (s *emailSendService) WireOrgRisk(r repository.OrgRiskRepository) {
	s.orgRiskRepo = r
}

// OrgRiskAware is the optional capability the caller uses to attach org risk.
type OrgRiskAware interface {
	WireOrgRisk(r repository.OrgRiskRepository)
}

func NewService(
	taskRepo repository.TaskRepository,
	emailRepo repository.EmailRepository,
	userRepo repository.UserRepository,
	scheduler scheduler.SchedulerService,
	tasksClient tasksched.Scheduler,
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

	// The organization's own posture, which is a different subject from the
	// user's ban: a suspended workspace stops sending even for members who are
	// not themselves banned.
	if s.orgRiskRepo != nil {
		if states, err := s.orgRiskRepo.GetOrgRiskStates(ctx, []uuid.UUID{orgID}); err == nil {
			if states[orgID].BlocksSending() {
				return nil, errx.New(errx.Forbidden, "this workspace is suspended from sending while it is under review")
			}
		}
	}

	// GetByID is unscoped (the org-scoped Get omits worker_id, which the
	// send needs), so the tenant check lives here: a foreign mailbox id is
	// indistinguishable from a missing one.
	account, xerr := s.emailRepo.GetByID(ctx, accountID)
	if xerr != nil {
		return nil, xerr
	}
	if account == nil || account.OrganizationID == nil || *account.OrganizationID != orgID {
		return nil, errx.New(errx.NotFound, "email account not found")
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

	// Undo send: instant sends are queued a short window into the
	// future so the user can still cancel them through the existing
	// DELETE /unibox/scheduled/:task_id path. Clamped to the config
	// bounds so a bad DB value can never park a send for hours.
	if sendMode == "instant" {
		secs := config.UndoSendSecondsDefault
		if s.userRepo != nil {
			if v, err := s.userRepo.GetUndoSendSeconds(ctx, userID); err == nil {
				secs = v
			}
		}
		secs = min(max(secs, config.UndoSendSecondsMin), config.UndoSendSecondsMax)
		scheduledAt = time.Now().Add(time.Duration(secs) * time.Second)
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

	bodyHTML, bodyPlain := req.BodyHTML, req.BodyPlain
	var forwardedHTML, forwardedPlain string
	if req.Forward != nil {
		forwardedHTML, forwardedPlain = renderForwarded(req.Forward, mailboxLocation(account))
		bodyHTML, bodyPlain = forwardNote(bodyHTML, bodyPlain)
	}

	// Only the note is tracked: the forwarded message's links are someone else's.
	bodyHTML, tracked := s.applyDirectTracking(ctx, account, taskID, bodyHTML, bodyHTML != "" || forwardedHTML != "")

	emailTask := &repository.EmailTask{
		TaskID:         taskID,
		To:             req.To,
		CC:             req.CC,
		BCC:            req.BCC,
		InReplyTo:      req.InReplyTo,
		Subject:        req.Subject,
		Body:           bodyPlain,
		BodyHTML:       bodyHTML,
		BodyPlain:      bodyPlain,
		ThreadID:       threadID,
		SendMode:       sendMode,
		Encrypted:      false,
		Tracked:        tracked,
		ForwardedHTML:  forwardedHTML,
		ForwardedPlain: forwardedPlain,
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

// applyDirectTracking adds the open pixel and click tickets to a hand-written
// send, when the sending mailbox has opted in. Returns the body to send and
// whether anything was actually injected. htmlPart says the send carries an
// HTML part at all; a plain-text send must not gain one just for a pixel.
//
// Unlike a campaign, a direct send has no contact or sequence, so clicks are
// counted on the send itself rather than written to email_link_clicks. The
// tickets still need a row to resolve against, minted with a nil campaign id;
// tracked_links carries no foreign key on that column.
func (s *emailSendService) applyDirectTracking(ctx context.Context, account *models.Email, taskID uuid.UUID, bodyHTML string, htmlPart bool) (string, bool) {
	if account == nil || !account.TrackDirectMail || !htmlPart {
		return bodyHTML, false
	}
	host := tasks.MailboxTrackingHost(account)
	if host == "" {
		// No tracking host on this install: a pixel would point nowhere and
		// every wrapped link would 404. Send it clean.
		return bodyHTML, false
	}

	body := tasks.AddOpenTrackingPixel(bodyHTML, taskID, host)

	if s.trackedLinkRepo != nil {
		wrapped, links := tasks.TrackLinks(body, tasks.LinkTracking{
			TaskID:         taskID,
			TrackingDomain: host,
			Wrap:           true,
		})
		if len(links) == 0 {
			body = wrapped
		} else if err := s.trackedLinkRepo.CreateBatch(ctx, links); err != nil {
			// Same posture as the campaign path: working links beat tickets
			// that would 404. The pixel is unaffected.
			log.Warn().Err(err).Str("task_id", taskID.String()).Msg("failed to store direct-mail link tickets; sending links untracked")
		} else {
			body = wrapped
		}
	}

	return body, true
}
