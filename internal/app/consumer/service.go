package jobs

import (
	"context"

	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type JobsService struct {
	Consumer                 *kafka.Consumer
	UniboxRepository         repository.UniboxRepository
	MailboxRepository        repository.MailboxRepository
	EmailRepository          repository.EmailRepository
	EmailHistoryIDRepository repository.EmailHistoryIDRepository

	eventHandlers map[models.JobEventType]func(ctx context.Context, body any) error
}

func (s *JobsService) Start(ctx context.Context) {
}
