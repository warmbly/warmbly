package tasks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/repository"
)

type reconcileRecoveryRepo struct {
	repository.TaskRepository
	batches []int
	calls   int
	err     error
}

func (r *reconcileRecoveryRepo) RecoverUnstartedWarmupDispatches(_ context.Context, before time.Time, _ int) (int, error) {
	if d := time.Since(before); d < 9*time.Minute || d > 11*time.Minute {
		return 0, errors.New("changed the unstarted-send safety window")
	}
	r.calls++
	if r.err != nil {
		return 0, r.err
	}
	if len(r.batches) == 0 {
		return 0, nil
	}
	n := r.batches[0]
	r.batches = r.batches[1:]
	return n, nil
}

type reconcileEmptyEmailRepo struct{ repository.EmailRepository }

func (*reconcileEmptyEmailRepo) ListWarmupScheduleCandidates(context.Context, int) ([]uuid.UUID, error) {
	return nil, nil
}

func TestWarmupRecoveryDrainsMultipleOldBatchesOnStartupWithinBound(t *testing.T) {
	for _, test := range []struct {
		name    string
		batches []int
		want    int
	}{
		{"existing fleet", []int{500, 500, 50}, 3},
		{"bounded backlog", []int{500, 500, 500, 500, 500, 500, 500, 500, 500, 500, 500}, 10},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := &reconcileRecoveryRepo{batches: test.batches}
			s := &tasksService{taskRepo: r, emailRepo: &reconcileEmptyEmailRepo{}}
			if _, err := s.ReconcileWarmupSchedules(t.Context(), 500); err != nil || r.calls != test.want {
				t.Fatalf("calls=%d want=%d err=%v", r.calls, test.want, err)
			}
		})
	}
}

func TestWarmupRecoveryUnavailableDoesNotProceedAsSuccessfulRecovery(t *testing.T) {
	want := errors.New("database unavailable")
	s := &tasksService{taskRepo: &reconcileRecoveryRepo{err: want}}
	if _, err := s.ReconcileWarmupSchedules(t.Context(), 500); !errors.Is(err, want) {
		t.Fatalf("lost recovery failure: %v", err)
	}
}
