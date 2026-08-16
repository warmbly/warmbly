package email

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/worker"
)

// stubAssignment records what releaseDeadWorker asked of the assignment
// service. Embedding the interface keeps the stub to the two methods under
// test; anything else would panic loudly rather than pass silently.
type stubAssignment struct {
	worker.WorkerAssignmentService

	live          bool
	liveErr       error
	unassignErr   error
	unassignCalls int
}

func (s *stubAssignment) IsWorkerLive(ctx context.Context, workerID uuid.UUID) (bool, error) {
	return s.live, s.liveErr
}

func (s *stubAssignment) UnassignWorkerFromEmail(ctx context.Context, emailAccountID uuid.UUID) error {
	s.unassignCalls++
	return s.unassignErr
}

func TestReleaseDeadWorkerReleasesOrphanedMailbox(t *testing.T) {
	dead := uuid.New()
	st := &stubAssignment{live: false}
	s := &emailService{workerAssignment: st}

	got, err := s.releaseDeadWorker(context.Background(), uuid.New(), &dead)
	if err != nil {
		t.Fatalf("releaseDeadWorker: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil so the caller places the mailbox on a live worker", got)
	}
	if st.unassignCalls != 1 {
		t.Errorf("unassign called %d times, want 1", st.unassignCalls)
	}
}

func TestReleaseDeadWorkerKeepsLiveWorker(t *testing.T) {
	current := uuid.New()
	st := &stubAssignment{live: true}
	s := &emailService{workerAssignment: st}

	got, err := s.releaseDeadWorker(context.Background(), uuid.New(), &current)
	if err != nil {
		t.Fatalf("releaseDeadWorker: %v", err)
	}
	if got == nil || *got != current {
		t.Error("a live worker's assignment was not preserved")
	}
	if st.unassignCalls != 0 {
		t.Errorf("a live worker was released (%d unassign calls)", st.unassignCalls)
	}
}

// A database blip must not churn placements: moving a mailbox changes the IP it
// sends from, which is not something to do on a failed lookup.
func TestReleaseDeadWorkerKeepsAssignmentOnLookupFailure(t *testing.T) {
	current := uuid.New()
	st := &stubAssignment{liveErr: errors.New("db down")}
	s := &emailService{workerAssignment: st}

	got, err := s.releaseDeadWorker(context.Background(), uuid.New(), &current)
	if err != nil {
		t.Fatalf("releaseDeadWorker returned an error for a transient lookup failure: %v", err)
	}
	if got == nil || *got != current {
		t.Error("the existing assignment was not preserved through a lookup failure")
	}
	if st.unassignCalls != 0 {
		t.Errorf("mailbox was released on a lookup failure (%d unassign calls)", st.unassignCalls)
	}
}

func TestReleaseDeadWorkerHandlesUnassignedAndUnwired(t *testing.T) {
	// No worker assigned yet: nothing to check.
	s := &emailService{workerAssignment: &stubAssignment{live: false}}
	if got, err := s.releaseDeadWorker(context.Background(), uuid.New(), nil); err != nil || got != nil {
		t.Errorf("got (%v, %v), want (nil, nil)", got, err)
	}

	// No assignment service wired (jobs, tests): leave the mailbox alone.
	current := uuid.New()
	bare := &emailService{}
	got, err := bare.releaseDeadWorker(context.Background(), uuid.New(), &current)
	if err != nil {
		t.Fatalf("releaseDeadWorker: %v", err)
	}
	if got == nil || *got != current {
		t.Error("assignment was dropped with no assignment service wired")
	}
}

// A failed release must surface, not silently fall through to placing the
// mailbox a second time while the old row still counts it.
func TestReleaseDeadWorkerPropagatesUnassignFailure(t *testing.T) {
	dead := uuid.New()
	st := &stubAssignment{live: false, unassignErr: errors.New("write failed")}
	s := &emailService{workerAssignment: st}

	if _, err := s.releaseDeadWorker(context.Background(), uuid.New(), &dead); err == nil {
		t.Fatal("a failed unassign was swallowed")
	}
}
