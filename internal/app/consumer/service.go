package jobs

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/advanced"
	"github.com/warmbly/warmbly/internal/app/inboxtag"
	warmupapp "github.com/warmbly/warmbly/internal/app/warmup"
	workerapp "github.com/warmbly/warmbly/internal/app/worker"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/codec"
	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// CloudLinkVerifier is the self-hosted pool link as the consumer sees it.
type CloudLinkVerifier interface {
	CheckEnrollment(ctx context.Context, accountID uuid.UUID) (bool, error)
	VerifyWarmupToken(ctx context.Context, accountID uuid.UUID, token string) (bool, error)
	IsCloudWarmupDelivery(ctx context.Context, accountID uuid.UUID, sender, messageID, subject string) (bool, error)
	IsCloudWarmupThreadReply(ctx context.Context, accountID uuid.UUID, messageID string, inReplyTo []string) (bool, error)
}

type JobsService struct {
	// Bus delivers the jobs.worker-events stream (Kafka or NATS).
	Bus eventbus.EventBus
	// Codec decodes bus payloads (jobs.worker-events); it must match the
	// CODEC_PROVIDER the producing services run with.
	Codec                       codec.Codec
	UniboxRepository            repository.UniboxRepository
	MailboxRepository           repository.MailboxRepository
	EmailRepository             repository.EmailRepository
	EmailHistoryIDRepository    repository.EmailHistoryIDRepository
	EmailGraphDeltaRepository   repository.EmailGraphDeltaRepository
	EmailSyncStateRepository    repository.EmailSyncStateRepository
	EmailAccountErrorRepository repository.EmailAccountErrorRepository
	WarmupRepo                  repository.WarmupRepository
	// PoolLinkRepo marks warmup-only mailboxes of linked instances; nil when unused.
	PoolLinkRepo repository.PoolLinkRepository
	// CloudLink (self-hosted) verifies cloud warmup mail in mailboxes the cloud warms; nil when unused.
	CloudLink            CloudLinkVerifier
	WarmupContentRepo    repository.WarmupContentRepository
	WarmupEngagementRepo repository.WarmupEngagementRepository
	// WarmupPlacementRepo keeps each sender's daily placement history. Optional.
	WarmupPlacementRepo repository.WarmupPlacementRepository
	// PlacementRepo resolves placement test probes from worker send results.
	// Optional.
	PlacementRepo repository.PlacementRepository
	WarmupService warmupapp.Service
	WorkerRepo    repository.WorkerRepository
	FleetNodeRepo repository.FleetNodeRepository
	// LifecycleRepo moves mailboxes in and out of cold rotation. Nil disables
	// the lifecycle rebalancer entirely.
	LifecycleRepo repository.SendLifecycleRepository

	// Publisher for sending events to workers
	Publisher events.Publisher

	// Pub/Sub for real-time notifications to users
	StreamingPublisher *pubsub.StreamingPublisher
	AdvancedService    advanced.Service

	// InboxTagger classifies inbound mail into labels and a relevance score.
	// Optional and nil by default: an instance with no TypeSafe key configured
	// never constructs it, and this handler's tagging step is skipped entirely.
	InboxTagger *inboxtag.Service

	// Cache for dead worker detection
	Cache *cache.Cache

	// Retention is the operator-editable retention section, read by the
	// warmup mail retention sweep on every pass. Nil keeps the compiled
	// defaults.
	Retention RetentionSource

	// AdminRepo for writing audit-log rows when the dead-worker job
	// auto-reassigns email accounts (optional — heartbeat sync also writes
	// here so admins can see why their fleet moved). Nil disables logging.
	AdminRepo repository.AdminRepository

	// AssignmentService is used by dead-worker recovery and placement-aware
	// control loops. Nil keeps the legacy least-loaded fallback.
	AssignmentService workerapp.WorkerAssignmentService

	// Notifier tells affected orgs (members with manage_emails) when the
	// dead-worker job moves or strands their mailboxes. Nil disables it.
	Notifier OrgNotifier

	// OpsNotifier raises instance-wide operator alerts, which are a different
	// audience from Notifier: the operator hears about the fleet, the tenant
	// hears about their mailboxes. Nil disables it.
	OpsNotifier OperatorNotifier

	// Send-outcome handling (EMAIL_SENT / EMAIL_FAILED from workers). The task
	// and campaign progress the control plane stamped at hand-off are walked
	// back here when a worker reports it could not send. Nil TaskRepo disables
	// the handlers.
	TaskRepo             repository.TaskRepository
	CampaignRepo         repository.CampaignRepository
	CampaignProgressRepo repository.CampaignProgressRepository
	CampaignLogRepo      repository.CampaignLogRepository
	ContactRepo          repository.ContactRepository
	// Evidence teaches verification what send results showed.
	Evidence advanced.EvidenceRecorder

	eventHandlers map[models.JobEventType]func(ctx context.Context, body any) error
}

func (s *JobsService) Start(ctx context.Context) {
	// Follow-up labels move because the calendar moved, not because anything
	// happened, so there is no event to hang them off and they are recomputed
	// on a schedule instead. Free: no model call, just arithmetic over stored
	// timestamps.
	go s.sweepFollowUps(ctx)

	if err := s.Bus.Subscribe(ctx, []string{kafka.TopicWorkerEvents}, "consumer-group", s.receive); err != nil {
		log.Error().Err(err).Msg("consumer: worker-events subscription ended")
	}
}

// followUpSweepInterval is hourly rather than daily: the states are measured in
// days, so an hour is far finer than the thing being measured, and it means a
// reply clears "follow-up-due" within the hour instead of the next morning.
const followUpSweepInterval = time.Hour

// followUpSweepWindow bounds how far back a sweep looks. A thread nobody has
// touched in three months is not one anybody is about to chase.
const followUpSweepWindow = 90

func (s *JobsService) sweepFollowUps(ctx context.Context) {
	if s.InboxTagger == nil || s.EmailRepository == nil {
		return
	}
	ticker := time.NewTicker(followUpSweepInterval)
	defer ticker.Stop()

	for {
		orgs, err := s.EmailRepository.ListOrganizationIDs(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("follow-up sweep: could not list workspaces")
		}
		for _, orgID := range orgs {
			since := time.Now().AddDate(0, 0, -followUpSweepWindow)
			if p, serr := s.InboxTagger.SweepFollowUps(ctx, orgID, since, 0); serr != nil {
				if ctx.Err() != nil {
					return
				}
				log.Warn().Err(serr).Str("org_id", orgID.String()).Msg("follow-up sweep failed")
			} else if p.Threads > 0 {
				log.Debug().Str("org_id", orgID.String()).Int("threads", p.Threads).Msg("follow-up sweep")
				if s.StreamingPublisher != nil {
					s.StreamingPublisher.PublishEmailUpdated(ctx, &pubsub.EmailInboxEvent{OrgID: orgID.String()})
				}
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
