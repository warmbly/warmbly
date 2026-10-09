package tasksched

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type localTestRepo struct {
	mu       sync.Mutex
	ids      []uuid.UUID
	retries  map[uuid.UUID]bool
	attempts chan uuid.UUID
}

func (r *localTestRepo) ListDuePendingTaskIDs(_ context.Context, limit int) ([]uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var ids []uuid.UUID
	for _, id := range r.ids {
		if !r.retries[id] {
			ids = append(ids, id)
		}
		if len(ids) == limit {
			break
		}
	}
	return ids, nil
}

func (r *localTestRepo) RecordDispatchAttempt(_ context.Context, id uuid.UUID, failed bool, at time.Time) error {
	r.mu.Lock()
	r.retries[id] = failed
	r.mu.Unlock()
	if failed && time.Until(at) < 4*time.Minute {
		return errors.New("retry delay too short")
	}
	r.attempts <- id
	return nil
}

func TestLocalFailedBatchDoesNotStarveLaterTasks(t *testing.T) {
	bad, good := uuid.New(), uuid.New()
	r := &localTestRepo{ids: []uuid.UUID{bad, good}, retries: make(map[uuid.UUID]bool), attempts: make(chan uuid.UUID, 2)}
	l := NewLocal(r, time.Second, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handle := func(id string) error {
		if id == bad.String() {
			return errors.New("temporary internal failure")
		}
		return nil
	}
	l.tick(ctx, handle)
	select {
	case <-r.attempts:
	case <-time.After(time.Second):
		t.Fatal("failed attempt not persisted")
	}
	// Record may finish just before the goroutine releases its in-flight marker.
	l.tick(ctx, handle)
	select {
	case id := <-r.attempts:
		if id != good {
			t.Fatal("failed batch blocked the newer task")
		}
	case <-time.After(time.Second):
		t.Fatal("newer task was starved")
	}
}

func TestLocalInflightIsNotDispatchedTwice(t *testing.T) {
	id := uuid.New()
	r := &localTestRepo{ids: []uuid.UUID{id}, retries: make(map[uuid.UUID]bool), attempts: make(chan uuid.UUID, 1)}
	l := NewLocal(r, time.Second, 1)
	entered, release := make(chan struct{}, 2), make(chan struct{})
	ctx := context.Background()
	handle := func(string) error { entered <- struct{}{}; <-release; return nil }
	l.tick(ctx, handle)
	<-entered
	l.tick(ctx, handle)
	select {
	case <-entered:
		t.Fatal("in-flight task dispatched twice")
	default:
	}
	close(release)
	select {
	case <-r.attempts:
	case <-time.After(time.Second):
		t.Fatal("attempt did not finish")
	}
}
