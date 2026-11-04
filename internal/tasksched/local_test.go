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
	limits   []int
	released []uuid.UUID
}

func (r *localTestRepo) ClaimDueTaskIDs(_ context.Context, limit int, lease time.Duration) ([]uuid.UUID, time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if lease <= 0 {
		return nil, time.Time{}, errors.New("claim without a lease")
	}
	r.limits = append(r.limits, limit)
	var ids []uuid.UUID
	for _, id := range r.ids {
		if !r.retries[id] {
			ids = append(ids, id)
		}
		if len(ids) == limit {
			break
		}
	}
	return ids, time.Now().Add(lease), nil
}

func (r *localTestRepo) RecordDispatchAttempt(_ context.Context, id uuid.UUID, _ time.Time, failed bool, at time.Time) error {
	r.mu.Lock()
	r.retries[id] = failed
	r.mu.Unlock()
	if failed && time.Until(at) < 4*time.Minute {
		return errors.New("retry delay too short")
	}
	r.attempts <- id
	return nil
}

func (r *localTestRepo) ReleaseDispatchClaims(_ context.Context, ids []uuid.UUID, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.released = append(r.released, ids...)
	return nil
}

func TestLocalClaimsOnlyWhatItsFreeSlotsCanRun(t *testing.T) {
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	r := &localTestRepo{ids: ids, retries: make(map[uuid.UUID]bool), attempts: make(chan uuid.UUID, 3)}
	l := NewLocal(r, time.Second, 200, WithConcurrency(3))
	entered, release := make(chan struct{}, 3), make(chan struct{})
	defer close(release)
	handle := func(string) error { entered <- struct{}{}; <-release; return nil }
	l.slots <- struct{}{}
	l.tick(t.Context(), handle)
	<-entered
	<-entered
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.limits) != 1 || r.limits[0] != 2 {
		t.Fatalf("claimed with limits %v, want one claim of the 2 free slots", r.limits)
	}
}

func TestLocalReleasesClaimsItCannotStart(t *testing.T) {
	id := uuid.New()
	r := &localTestRepo{ids: []uuid.UUID{id}, retries: make(map[uuid.UUID]bool), attempts: make(chan uuid.UUID, 1)}
	l := NewLocal(r, time.Second, 1)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	handle := func(string) error { cancel(); close(done); return errors.New("shutting down") }
	l.tick(ctx, handle)
	<-done
	deadline := time.Now().Add(time.Second)
	for {
		r.mu.Lock()
		released, stamped := len(r.released), len(r.retries)
		r.mu.Unlock()
		if released == 1 && stamped == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("released %d claims and stamped %d, want the claim handed back unstamped", released, stamped)
		}
		time.Sleep(time.Millisecond)
	}
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

func TestLocalConcurrencyUsesEffectivePoolCapacity(t *testing.T) {
	for _, tc := range []struct {
		pool, want int
	}{{25, 8}, {4, 1}, {12, 4}, {100, 8}, {1, 1}} {
		l := NewLocal(nil, 0, 0, WithPoolCapacity(tc.pool))
		if got := cap(l.slots); got != tc.want {
			t.Fatalf("pool %d: concurrency %d, want %d", tc.pool, got, tc.want)
		}
	}
}

func TestLocalExcessWorkStaysQueuedAcrossTicks(t *testing.T) {
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	r := &localTestRepo{ids: ids, retries: make(map[uuid.UUID]bool), attempts: make(chan uuid.UUID, 3)}
	l := NewLocal(r, time.Millisecond, 200, WithConcurrency(2))
	entered, release := make(chan string, 3), make(chan struct{})
	var closeOnce sync.Once
	t.Cleanup(func() { closeOnce.Do(func() { close(release) }) })
	handle := func(id string) error { entered <- id; <-release; return nil }
	for round := 0; round < 3; round++ {
		l.tick(t.Context(), handle)
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("available dispatch slot was not used")
		}
	}
	select {
	case <-entered:
		t.Fatal("excess task dispatched while both slots were occupied")
	case <-r.attempts:
		t.Fatal("queued task stamped before a handler completed")
	default:
	}
	closeOnce.Do(func() { close(release) })
	for range 2 {
		select {
		case <-r.attempts:
		case <-time.After(time.Second):
			t.Fatal("dispatch attempt did not finish")
		}
	}
	r.mu.Lock()
	r.ids = ids[2:]
	_, stamped := r.retries[ids[2]]
	r.mu.Unlock()
	if stamped {
		t.Fatal("task that never started received an attempt stamp")
	}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		l.tick(t.Context(), handle)
		select {
		case id := <-entered:
			if id != ids[2].String() {
				t.Fatalf("wrong queued task started: %s", id)
			}
			return
		case <-deadline.C:
			t.Fatal("queued task did not use a released slot")
		case <-tick.C:
		}
	}
}

func TestLocalCancelledTickDoesNotDispatchOrStamp(t *testing.T) {
	r := &localTestRepo{ids: []uuid.UUID{uuid.New()}, retries: make(map[uuid.UUID]bool), attempts: make(chan uuid.UUID, 1)}
	l := NewLocal(r, 0, 0)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	l.tick(ctx, func(string) error { t.Error("cancelled dispatcher started a task"); return nil })
	if len(l.slots) != 0 || len(r.retries) != 0 {
		t.Fatal("cancelled tick claimed or stamped pending work")
	}
}
