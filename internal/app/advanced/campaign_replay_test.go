package advanced

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

type replayRepo struct {
	repository.AdvancedOutreachRepository
	dlq      *models.TaskDeadLetter
	replayed int
}

func (r *replayRepo) GetTaskDeadLetter(context.Context, uuid.UUID, uuid.UUID) (*models.TaskDeadLetter, error) {
	return r.dlq, nil
}

func (r *replayRepo) MarkTaskDeadLetterReplayed(context.Context, uuid.UUID) error {
	r.replayed++
	return nil
}

type replayTasks struct {
	repository.TaskRepository
	task       *repository.Task
	campaignID *uuid.UUID
	ctErr      error
	created    bool
	inserted   int
	repended   int
	deleted    int
}

func (t *replayTasks) DeleteTask(context.Context, uuid.UUID) error {
	t.deleted++
	return nil
}

func (t *replayTasks) GetTask(context.Context, uuid.UUID) (*repository.Task, error) {
	return t.task, nil
}

func (t *replayTasks) GetCampaignTask(context.Context, uuid.UUID) (*repository.CampaignTask, error) {
	if t.ctErr != nil {
		return nil, t.ctErr
	}
	return &repository.CampaignTask{TaskID: t.task.ID, CampaignID: t.campaignID}, nil
}

func (t *replayTasks) CreateTaskWithLock(context.Context, *repository.Task, *repository.CampaignTask) (bool, error) {
	if t.created {
		t.inserted++
	}
	return t.created, nil
}

func (t *replayTasks) UpdateTaskScheduledAt(context.Context, uuid.UUID, time.Time, string) error {
	return nil
}

func (t *replayTasks) UpdateTaskStatus(context.Context, uuid.UUID, string) error {
	t.repended++
	return nil
}

type replayClient struct{ fail bool }

func (c replayClient) CreateTask(context.Context, *proto.ProcessTask, time.Time) (string, error) {
	if c.fail {
		return "", errors.New("queue unavailable")
	}
	return "local", nil
}
func (replayClient) DeleteTask(context.Context, string) error { return nil }

// A dead-lettered campaign pass is replayed as a fresh pass through the
// campaign's lock, never by putting the old pass back to pending, and a
// replay that could not be queued keeps its dead letter.
func TestReplayDeadLetterForACampaignPass(t *testing.T) {
	campaignID := uuid.New()
	for _, tc := range []struct {
		name         string
		ctErr        error
		created      bool
		queueFails   bool
		wantErr      bool
		wantInserted int
		wantReplayed int
		wantDeleted  int
	}{
		{name: "chain has no pass", created: true, wantInserted: 1, wantReplayed: 1},
		{name: "chain already has its next pass", created: false, wantReplayed: 1},
		{name: "campaign task unreadable", ctErr: errors.New("connection reset"), wantErr: true},
		{name: "queue refuses the new pass", created: true, queueFails: true, wantErr: true, wantInserted: 1, wantDeleted: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := &repository.Task{ID: uuid.New(), TaskType: "campaign", EmailAccountID: uuid.New()}
			repo := &replayRepo{dlq: &models.TaskDeadLetter{ID: uuid.New(), TaskID: task.ID}}
			tasks := &replayTasks{task: task, campaignID: &campaignID, ctErr: tc.ctErr, created: tc.created}
			svc := &service{repo: repo, taskRepo: tasks, tasksClient: replayClient{fail: tc.queueFails}}

			xerr := svc.ReplayDeadLetter(context.Background(), uuid.New(), repo.dlq.ID)
			if (xerr != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error %v", xerr, tc.wantErr)
			}
			if tasks.repended != 0 {
				t.Fatal("the old pass was put back to pending")
			}
			if tasks.inserted != tc.wantInserted || repo.replayed != tc.wantReplayed || tasks.deleted != tc.wantDeleted {
				t.Fatalf("inserted %d replayed %d deleted %d, want %d, %d and %d",
					tasks.inserted, repo.replayed, tasks.deleted, tc.wantInserted, tc.wantReplayed, tc.wantDeleted)
			}
		})
	}
}
