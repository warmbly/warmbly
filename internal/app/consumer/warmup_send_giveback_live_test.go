package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type warmupFailureTaskRepo struct {
	repository.TaskRepository
	task *repository.Task
}

func (r warmupFailureTaskRepo) GetTask(context.Context, uuid.UUID) (*repository.Task, error) {
	return r.task, nil
}

type warmupFailureRepo struct {
	repository.WarmupRepository
	err      error
	attempts int
}

func (r *warmupFailureRepo) FailWarmupSend(context.Context, uuid.UUID, uuid.UUID, time.Time, string, string) error {
	r.attempts++
	return r.err
}

func TestWarmupSendFailureIsRetriedWhenTheRefundFails(t *testing.T) {
	want := errors.New("database unavailable")
	task := &repository.Task{ID: uuid.New(), TaskType: "warmup", EmailAccountID: uuid.New(), Status: "completed"}
	warmup := &warmupFailureRepo{err: want}
	s := &JobsService{TaskRepo: warmupFailureTaskRepo{task: task}, WarmupRepo: warmup}

	err := s.HandleEmailFailed(context.Background(), models.SendEmailResult{TaskID: task.ID, LegacyErrorMsg: "refused"})
	if !errors.Is(err, want) {
		t.Fatalf("HandleEmailFailed error = %v, want %v", err, want)
	}
	if warmup.attempts != 1 {
		t.Fatalf("refund attempts = %d, want 1", warmup.attempts)
	}
}
