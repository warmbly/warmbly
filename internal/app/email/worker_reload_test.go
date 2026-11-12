package email

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// reloadRepo assigns a fixed set of mailboxes to every worker and records which ones a reload reads.
type reloadRepo struct {
	repository.EmailRepository

	assigned []uuid.UUID
	gate     chan struct{}

	mu     sync.Mutex
	lists  int
	loaded []uuid.UUID
}

func (r *reloadRepo) ListActiveAccountsByWorker(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	r.mu.Lock()
	r.lists++
	first := r.lists == 1
	r.mu.Unlock()
	if first {
		<-r.gate
	}
	return r.assigned, nil
}

func (r *reloadRepo) GetByID(_ context.Context, id uuid.UUID) (*models.Email, *errx.Error) {
	r.mu.Lock()
	r.loaded = append(r.loaded, id)
	r.mu.Unlock()
	return nil, nil
}

func (r *reloadRepo) snapshot() (int, []uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lists, slices.Clone(r.loaded)
}

// A worker reporting missing mailboxes gets only those placed on it, and requests made while a pass runs
// join one next pass instead of starting reloads of the whole inventory alongside it.
func TestRequestWorkerReloadNamesOnlyAssignedMailboxesAndCoalesces(t *testing.T) {
	a, b, c, foreign := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	repo := &reloadRepo{assigned: []uuid.UUID{a, b, c}, gate: make(chan struct{})}
	s := &emailService{emailRepository: repo}
	worker := uuid.New()

	s.RequestWorkerReload(worker, false, []uuid.UUID{b, foreign})
	for start := time.Now(); ; time.Sleep(time.Millisecond) {
		if lists, _ := repo.snapshot(); lists == 1 {
			break
		}
		if time.Since(start) > 5*time.Second {
			t.Fatal("first pass never started")
		}
	}
	for i := 0; i < 3; i++ {
		s.RequestWorkerReload(worker, true, nil)
	}
	s.RequestWorkerReload(worker, false, []uuid.UUID{c})
	close(repo.gate)

	deadline := time.Now().Add(5 * time.Second)
	for {
		s.reloadMu.Lock()
		idle := len(s.reloads) == 0
		s.reloadMu.Unlock()
		if idle {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reload never finished")
		}
		time.Sleep(5 * time.Millisecond)
	}
	lists, loaded := repo.snapshot()
	if lists != 2 {
		t.Errorf("listed the worker's mailboxes %d times, want 2 (the named pass, then one merged full pass)", lists)
	}
	if want := []uuid.UUID{b, a, b, c}; !slices.Equal(loaded, want) {
		t.Errorf("loaded %v, want %v", loaded, want)
	}
	if slices.Contains(loaded, foreign) {
		t.Error("a worker named a mailbox placed elsewhere and it was re-shipped")
	}
}
