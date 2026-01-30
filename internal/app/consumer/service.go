package jobs

import (
	"context"

	"github.com/warmbly/warmbly/internal/events"
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

	// Publisher for sending events to workers
	Publisher events.Publisher

	// Pub/Sub for real-time notifications to users
	StreamingPublisher *pubsub.StreamingPublisher

	eventHandlers map[models.JobEventType]func(ctx context.Context, body any) error
}

func (s *JobsService) Start(ctx context.Context) {
	s.Consumer.Consume(ctx, s.Receive)
}
