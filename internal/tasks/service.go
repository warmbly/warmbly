package tasks

import (
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/gtasks"
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

type TasksService interface {
	HandleCampaignTask(task *proto.CampaignTask) *errx.Error
	HandleEmailTask(task *proto.EmailTask) *errx.Error
}

type tasksService struct {
	tasksClient    *gtasks.Client
	producerClient *kafka.Producer
}

func NewService(
	tasksClient *gtasks.Client,
	producerClient *kafka.Producer,
) TasksService {
	return &tasksService{
		tasksClient:    tasksClient,
		producerClient: producerClient,
	}
}
