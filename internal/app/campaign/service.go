package campaign

import (
	"context"
	"time"

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
	// Get loads one of orgID's campaigns; any other id is not found.
	Get(ctx context.Context, orgID, id string) (*models.Campaign, *errx.Error)
	Search(ctx context.Context, userID, query, cursor, folder, status, kind, limit string) (*models.CampaignsResult, *errx.Error)
	Overview(ctx context.Context, orgID string) (*models.CampaignsOverview, *errx.Error)
	// Estimate projects how many contacts a set of segments reaches and how
	// many sending days a mailbox pool needs under the per-mailbox caps.
	// Read-only; the wizard shows it before a one-time email is created.
	Estimate(ctx context.Context, orgID uuid.UUID, in *models.CampaignEstimate) (*models.CampaignEstimateResult, *errx.Error)
	Update(ctx context.Context, orgID, id string, data *models.UpdateCampaign) (*models.Campaign, *errx.Error)
	// Delete removes an organization's campaign outright. A running campaign
	// is stopped as part of it: its pending tasks are cancelled in the same
	// transaction, so nothing keeps sending for a campaign that is gone.
	// Returns the deleted campaign so callers can name it after the fact.
	Delete(ctx context.Context, orgID uuid.UUID, campaignID string) (*models.Campaign, *errx.Error)
	// Duplicate creates a draft copy of a campaign's configuration owned by
	// userID. An empty name derives "<source name> (copy)".
	Duplicate(ctx context.Context, orgID, userID uuid.UUID, campaignID, name string) (*models.Campaign, *errx.Error)

	// Start/Stop
	StartCampaign(ctx context.Context, orgID uuid.UUID, campaignID string, opts models.StartCampaignOptions) *errx.Error
	// ResumeVerificationPaused restarts the org's campaigns parked at
	// paused_undeliverable. Called after verification verdicts change; a
	// campaign that still has nothing to send parks itself again.
	ResumeVerificationPaused(ctx context.Context, orgID uuid.UUID)
	StopCampaign(ctx context.Context, orgID uuid.UUID, campaignID string) *errx.Error

	// Logs
	GetLogs(ctx context.Context, orgID, campaignID string, limit int, cursor *string) (*models.CampaignLogsResult, *errx.Error)

	// WakeCampaigns pulls the parked wakeup of each listed active campaign
	// forward when its next slot is sooner than where it is parked. Called after
	// leads are attached to a campaign: the chain is one self-perpetuating task,
	// so without this a campaign that had nothing to do keeps sleeping and the
	// new leads sit at "Queued / Not started". Best effort and never an error to
	// the caller — the lead was still added.
	WakeCampaigns(ctx context.Context, orgID uuid.UUID, campaignIDs []string)

	// KeepRunning turns on the campaign's "Keep running for new leads" setting
	// because a live lead source (a form, an automation) now feeds it, the
	// same way linking a segment does. reason is written to the campaign's
	// activity log on the transition; a campaign already continuous is left
	// alone. Returns ErrNotFound when the campaign is not the organization's.
	KeepRunning(ctx context.Context, orgID, campaignID uuid.UUID, reason string) *errx.Error

	// Explicit sender pool (feature 1).
	ListCampaignSenders(ctx context.Context, orgID uuid.UUID, campaignID string) ([]models.CampaignSender, *errx.Error)
	ReplaceCampaignSenders(ctx context.Context, orgID uuid.UUID, campaignID string, in []models.CampaignSenderInput) ([]models.CampaignSender, *errx.Error)

	// Campaign-scoped tracking domain (feature 5). Resolves the override's CNAME
	// and flips verified on success; an unresolved record stays "pending".
	VerifyCampaignTrackingDomain(ctx context.Context, orgID uuid.UUID, campaignID string) (*models.TrackingDomainStatus, *errx.Error)

	// PauseLead parks ONE contact's flow inside ONE campaign until `until`,
	// or with no end when it is nil. The contact stays subscribed and stays a
	// lead: this is the lever for "they are away for a week", which
	// unsubscribing and suppressing both answer far too permanently.
	PauseLead(ctx context.Context, orgID, campaignID, contactID uuid.UUID, until *time.Time, reason string) (*models.LeadHold, *errx.Error)
	// ResumeLead lifts the hold now and wakes the campaign so the step goes
	// out on the next pass rather than when the chain happens to be parked.
	ResumeLead(ctx context.Context, orgID, campaignID, contactID uuid.UUID) *errx.Error
	// GetLeadHold reads the live hold on one lead (nil when it is not held).
	GetLeadHold(ctx context.Context, orgID, campaignID, contactID uuid.UUID) (*models.LeadHold, *errx.Error)
}

// Bounds on a manual lead hold. A hold in the past would lift the moment it
// was written; one years out is indistinguishable from removing the lead, and
// the member who wants that has "remove from campaign".
const (
	leadHoldMaxDays      = 365
	leadHoldReasonMaxLen = 200
)

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
	// campaignProgressRepo counts the leads verification refused, so a start
	// with nothing left to send can say why. Optional/nil-safe.
	campaignProgressRepo repository.CampaignProgressRepository
	// segments counts an audience for Estimate. Optional: without it an
	// estimate reports zero recipients.
	segments SegmentCounter
}

// SegmentCounter is the slice of the segment service Estimate needs.
// Satisfied structurally by segment.Service.
type SegmentCounter interface {
	Preview(ctx context.Context, orgID uuid.UUID, in *models.SegmentPreview) (int, *errx.Error)
}

// SegmentAware lets main hand the campaign service the segment counter.
type SegmentAware interface {
	WireSegments(c SegmentCounter)
}

func (s *campaignService) WireSegments(c SegmentCounter) {
	s.segments = c
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

// ProgressAware lets main hand the campaign service the progress repository.
type ProgressAware interface {
	WireProgress(r repository.CampaignProgressRepository)
}

func (s *campaignService) WireProgress(r repository.CampaignProgressRepository) {
	s.campaignProgressRepo = r
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
