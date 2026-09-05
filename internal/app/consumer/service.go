package jobs

import (
	"context"

	"github.com/google/uuid"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/advanced"
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
	IsEnrolled(ctx context.Context, accountID uuid.UUID) bool
	VerifyWarmupToken(ctx context.Context, accountID uuid.UUID, token string) (bool, error)
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
	WarmupService        warmupapp.Service
	WorkerRepo           repository.WorkerRepository
	// LifecycleRepo moves mailboxes in and out of cold rotation. Nil disables
	// the lifecycle rebalancer entirely.
	LifecycleRepo repository.SendLifecycleRepository

	// Publisher for sending events to workers
	Publisher events.Publisher

	// Pub/Sub for real-time notifications to users
	StreamingPublisher *pubsub.StreamingPublisher
	AdvancedService    advanced.Service

	// Cache for dead worker detection
	Cache *cache.Cache

	// AdminRepo for writing audit-log rows when the dead-worker job
	// auto-reassigns email accounts (optional — heartbeat sync also writes
	// here so admins can see why their fleet moved). Nil disables logging.
	AdminRepo repository.AdminRepository

	// AssignmentService is used by the risk rebalancer to pick replacement
	// workers when a mailbox's risk band changes. Nil disables the job.
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
	if err := s.Bus.Subscribe(ctx, []string{kafka.TopicWorkerEvents}, "consumer-group", s.receive); err != nil {
		log.Error().Err(err).Msg("consumer: worker-events subscription ended")
	}
}
