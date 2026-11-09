package jobs

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func TestWarmupActionWorkerFollowsTheAssignedWorker(t *testing.T) {
	ctx := context.Background()
	worker := uuid.New()
	s := &JobsService{}
	if got, ok := s.warmupActionWorker(ctx, &models.Email{ID: uuid.New(), WorkerID: &worker}); !ok || got != worker {
		t.Fatalf("assigned mailbox = %v, %v", got, ok)
	}
	if _, ok := s.warmupActionWorker(ctx, &models.Email{ID: uuid.New()}); ok {
		t.Fatal("a mailbox with no worker must not be routable")
	}
	if _, ok := s.warmupActionWorker(ctx, nil); ok {
		t.Fatal("an unknown mailbox must not be routable")
	}
}
