package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type validationRepo struct {
	repository.WorkerRepository
	live   []models.Worker
	placed *uuid.UUID
}

func (r *validationRepo) ListPlaceableWorkers(context.Context) ([]models.Worker, error) {
	return append([]models.Worker(nil), r.live...), nil
}
func (r *validationRepo) GetEmailAccountPlacementHint(context.Context, uuid.UUID) (*repository.EmailAccountPlacementHint, error) {
	return nil, errors.New("no mailbox yet")
}
func (r *validationRepo) CountOrgMailboxes(context.Context, uuid.UUID) (int, error) { return 0, nil }
func (r *validationRepo) ListPlacementCandidates(context.Context, uuid.UUID, string, []models.WorkerHealthState) ([]repository.PlacementCandidateRow, error) {
	if r.placed == nil {
		return nil, nil
	}
	return []repository.PlacementCandidateRow{{WorkerCapacityRowDB: repository.WorkerCapacityRowDB{
		WorkerID: *r.placed, HealthState: models.WorkerHealthHealthy, BaseCapacity: 16, HealthMultiplier: 1, AgeMultiplier: 1,
	}}}, nil
}
func (r *validationRepo) GetByID(_ context.Context, id uuid.UUID) (*models.Worker, error) {
	for _, w := range r.live {
		if w.ID == id {
			return &w, nil
		}
	}
	return nil, nil
}

func ids(ws []models.Worker) []uuid.UUID {
	out := make([]uuid.UUID, len(ws))
	for i, w := range ws {
		out[i] = w.ID
	}
	return out
}

func TestValidationWorkersOrderAndHealth(t *testing.T) {
	a, b, c, blocked := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	repo := &validationRepo{live: []models.Worker{
		{ID: a, HealthState: models.WorkerHealthHealthy},
		{ID: blocked, HealthState: models.WorkerHealthBlocked},
		{ID: b, HealthState: models.WorkerHealthWatch},
		{ID: c, HealthState: models.WorkerHealthHealthy},
	}, placed: &c}
	svc := &workerAssignmentService{workerRepo: repo}
	org := uuid.New()

	got, err := svc.ValidationWorkers(context.Background(), org, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []uuid.UUID{c, a, b}; !equalIDs(ids(got), want) {
		t.Fatalf("new mailbox order = %v, want placement first then by load %v", ids(got), want)
	}

	got, _ = svc.ValidationWorkers(context.Background(), org, &b)
	if want := []uuid.UUID{b, c, a}; !equalIDs(ids(got), want) {
		t.Fatalf("reconnect order = %v, want the mailbox's own worker first %v", ids(got), want)
	}

	got, _ = svc.ValidationWorkers(context.Background(), org, &blocked)
	if want := []uuid.UUID{c, a, b}; !equalIDs(ids(got), want) {
		t.Fatalf("an unhealthy own worker was offered: %v", ids(got))
	}

	repo.live = []models.Worker{{ID: blocked, HealthState: models.WorkerHealthBlocked}}
	if _, err := svc.ValidationWorkers(context.Background(), org, nil); !errors.Is(err, ErrNoAvailableWorkers) {
		t.Fatalf("no healthy worker: err = %v", err)
	}
}

func equalIDs(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
