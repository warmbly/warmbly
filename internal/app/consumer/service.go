package jobs

import (
	"context"

	"github.com/warmbly/warmbly/internal/app/advanced"
	warmupapp "github.com/warmbly/warmbly/internal/app/warmup"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type JobsService struct {
	Consumer                    *kafka.Consumer
	UniboxRepository            repository.UniboxRepository
	MailboxRepository           repository.MailboxRepository
	EmailRepository             repository.EmailRepository
	EmailHistoryIDRepository    repository.EmailHistoryIDRepository
	EmailAccountErrorRepository repository.EmailAccountErrorRepository
	WarmupRepo                  repository.WarmupRepository
	WarmupService               warmupapp.Service
	WorkerRepo                  repository.WorkerRepository

	// Publisher for sending events to workers
	Publisher events.Publisher

	// Pub/Sub for real-time notifications to users
	StreamingPublisher *pubsub.StreamingPublisher
	AdvancedService    advanced.Service

	// Cache for dead worker detection
	Cache *cache.Cache

	eventHandlers map[models.JobEventType]func(ctx context.Context, body any) error
}

func (s *JobsService) Start(ctx context.Context) {
	s.Consumer.Consume(ctx, s.Receive)
}
