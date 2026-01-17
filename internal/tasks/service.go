package tasks

import (
	"github.com/warmbly/warmbly/internal/infrastructure/gtasks"
)

type TasksService interface{

}

type tasksService struct {
	tasks *gtasks.Client
}

func NewService(tasks *gtasks.Client) TasksService {
	return &tasksService{
		tasks: tasks,
	}
}
