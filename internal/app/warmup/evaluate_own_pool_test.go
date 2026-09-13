package warmup

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// ownPoolRepo answers every metric read with zero and records which pool the
// decision was read back from. Embedding the interface makes any other call
// panic loudly rather than silently pass.
type ownPoolRepo struct {
	repository.WarmupRepository

	health       *models.WarmupParticipantHealth
	updated      bool
	readBackPool string
}

func (r *ownPoolRepo) GetParticipantHealthForAccount(context.Context, uuid.UUID) (*models.WarmupParticipantHealth, error) {
	return r.health, nil
}
func (r *ownPoolRepo) GetParticipantHealth(_ context.Context, _ uuid.UUID, poolType string) (*models.WarmupParticipantHealth, error) {
	r.readBackPool = poolType
	if poolType != "free" {
		return nil, nil // absent, not broken
	}
	return r.health, nil
}
func (r *ownPoolRepo) UpdateParticipantHealth(context.Context, uuid.UUID, models.WarmupHealthState, *time.Time, string, float64) error {
	r.updated = true
	return nil
}
func (r *ownPoolRepo) SumWarmupSentSince(context.Context, uuid.UUID, time.Time) (int, error) {
	return 0, nil
}
func (r *ownPoolRepo) CountSpamPlacementsSince(context.Context, uuid.UUID, time.Time) (int, error) {
	return 0, nil
}
func (r *ownPoolRepo) CountUserComplaintsSince(context.Context, uuid.UUID, time.Time) (int, error) {
	return 0, nil
}
func (r *ownPoolRepo) GetSpamScore(context.Context, uuid.UUID) (int, error) { return 0, nil }
func (r *ownPoolRepo) CountDeliverabilityEventsByAccount(context.Context, uuid.UUID, string, time.Time) (int, error) {
	return 0, nil
}
func (r *ownPoolRepo) CountDeliveredByAccount(context.Context, uuid.UUID, time.Time) (int, error) {
	return 0, nil
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
	if repo.readBackPool != "free" {
		t.Fatalf("read the decision back from pool %q, want the pool the mailbox is actually in", repo.readBackPool)
	}
}
