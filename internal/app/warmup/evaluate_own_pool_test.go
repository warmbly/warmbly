package warmup

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// zeroMetricsRepo answers the metric read with zeros and writes nothing.
// Embedding the interface makes any other call panic loudly rather than pass.
type zeroMetricsRepo struct {
	repository.WarmupRepository
}

func (zeroMetricsRepo) HealthMetricCounts(context.Context, uuid.UUID, time.Time, time.Time) (models.WarmupHealthCounts, error) {
	return models.WarmupHealthCounts{}, nil
}

// ownPoolRepo serves one row through the account-scoped read only; a
// pool-pinned read panics, which is the point of the test below.
type ownPoolRepo struct {
	zeroMetricsRepo

	health  *models.WarmupParticipantHealth
	updated bool
}

func (r *ownPoolRepo) GetParticipantHealthForAccount(context.Context, uuid.UUID) (*models.WarmupParticipantHealth, error) {
	return r.health, nil
}
func (r *ownPoolRepo) UpdateParticipantHealth(_ context.Context, _ uuid.UUID, state models.WarmupHealthState, _ *time.Time, _ string, _ float64) (*models.WarmupParticipantHealth, error) {
	r.updated = true
	written := *r.health
	written.PoolType = "" // the write does not know the pool; the caller does
	written.HealthState = state
	return &written, nil
}

// The evaluation runs on the mailbox's own pool row. Probing "premium" first
// and treating "not in that pool" as a hard failure is what stopped health
// evaluation ever running for a free-pool account (#195); membership is read
// per account, which is exact because a mailbox is in exactly one pool (#211).
// The live tests cover the same path against Postgres but never run in CI.
func TestEvaluateAnyPoolEvaluatesTheMailboxesOwnPool(t *testing.T) {
	repo := &ownPoolRepo{health: &models.WarmupParticipantHealth{PoolType: "free", HealthState: models.WarmupHealthHealthy}}

	health, err := NewService(repo).(*service).evaluateAndPersistAnyPool(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if health == nil {
		t.Fatal("expected the free-pool participant")
	}
	if !repo.updated {
		t.Fatal("the evaluation never persisted a decision")
	}
	if health.PoolType != "free" {
		t.Fatalf("read the decision back from pool %q, want the pool the mailbox is actually in", health.PoolType)
	}
}
