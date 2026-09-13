package warmup

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// The bands read windows far shorter than the blocks they hand out: spam
// placement blocks for 30 days, and the band that decided it reads seven. Without
// a floor the first sweep after the placements aged out cleared the block, so the
// 30 days in the docs lasted about a week, and a re-added mailbox with no history
// at all was cleared on the next sweep.
func TestHoldActiveBlock(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	future := now.Add(20 * 24 * time.Hour)
	past := now.Add(-time.Minute)
	reason := "invalid warmup token attempts exceeded threshold: 3 in 24h"
	blocked := &models.WarmupParticipantHealth{HealthState: models.WarmupHealthBlocked, BlockedUntil: &future, BlockedReason: &reason}
	served := &models.WarmupParticipantHealth{HealthState: models.WarmupHealthBlocked, BlockedUntil: &past, BlockedReason: &reason}
	quarantined := &models.WarmupParticipantHealth{HealthState: models.WarmupHealthQuarantined, BlockedUntil: &future}
	healthy := evaluationDecision{State: models.WarmupHealthHealthy, Score: 0.5}
	worse := evaluationDecision{State: models.WarmupHealthBlocked, BlockedUntil: &future, Reason: "complaints", Score: 9}

	tests := []struct {
		name     string
		current  *models.WarmupParticipantHealth
		decision evaluationDecision
		want     models.WarmupHealthState
		wantWhen *time.Time
	}{
		{"a clean reading does not release a block still in force", blocked, healthy, models.WarmupHealthBlocked, &future},
		{"a reading at least as severe replaces the block", blocked, worse, models.WarmupHealthBlocked, &future},
		{"a served block is released by the reading", served, healthy, models.WarmupHealthHealthy, nil},
		{"a quarantine in force is a floor too", quarantined, healthy, models.WarmupHealthQuarantined, &future},
		{"no participant, nothing to hold", nil, healthy, models.WarmupHealthHealthy, nil},
		{"no block, nothing to hold", &models.WarmupParticipantHealth{HealthState: models.WarmupHealthWatch}, healthy, models.WarmupHealthHealthy, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := holdActiveBlock(tc.current, tc.decision, now)
			if got.State != tc.want {
				t.Fatalf("state = %s, want %s", got.State, tc.want)
			}
			if (got.BlockedUntil == nil) != (tc.wantWhen == nil) || (tc.wantWhen != nil && !got.BlockedUntil.Equal(*tc.wantWhen)) {
				t.Fatalf("blocked_until = %v, want %v", got.BlockedUntil, tc.wantWhen)
			}
			if got.Score != tc.decision.Score {
				t.Fatalf("the fresh score is a reading and must be kept: got %v, want %v", got.Score, tc.decision.Score)
			}
		})
	}

	t.Run("a held block keeps its reason", func(t *testing.T) {
		if got := holdActiveBlock(blocked, healthy, now); got.Reason != reason {
			t.Fatalf("reason = %q, want %q", got.Reason, reason)
		}
	})
}

func TestWarmupHealthStateRankIsAStrictOrder(t *testing.T) {
	order := []models.WarmupHealthState{
		models.WarmupHealthHealthy, models.WarmupHealthWatch, models.WarmupHealthThrottled,
		models.WarmupHealthQuarantined, models.WarmupHealthBlocked,
	}
	for i := 1; i < len(order); i++ {
		if order[i].Rank() <= order[i-1].Rank() {
			t.Fatalf("%s must rank above %s", order[i], order[i-1])
		}
	}
}

// holdRepo drives evaluateAndPersist end to end: a blocked participant, no
// signals at all, and the one thing that matters is what gets written.
type holdRepo struct {
	repository.WarmupRepository

	health  *models.WarmupParticipantHealth
	written []models.WarmupHealthState
	until   []*time.Time
}

func (r *holdRepo) GetParticipantHealth(context.Context, uuid.UUID, string) (*models.WarmupParticipantHealth, error) {
	return r.health, nil
}
func (r *holdRepo) UpdateParticipantHealth(_ context.Context, _ uuid.UUID, state models.WarmupHealthState, until *time.Time, _ string, _ float64) error {
	r.written = append(r.written, state)
	r.until = append(r.until, until)
	return nil
}
func (r *holdRepo) SumWarmupSentSince(context.Context, uuid.UUID, time.Time) (int, error) {
	return 0, nil
}
func (r *holdRepo) CountSpamPlacementsSince(context.Context, uuid.UUID, time.Time) (int, error) {
	return 0, nil
}
func (r *holdRepo) CountUserComplaintsSince(context.Context, uuid.UUID, time.Time) (int, error) {
	return 0, nil
}
func (r *holdRepo) CountRecentInvalidAttempts(context.Context, uuid.UUID, time.Time) (int, error) {
	return 0, nil
}
func (r *holdRepo) GetSpamScore(context.Context, uuid.UUID) (int, error) { return 0, nil }
func (r *holdRepo) CountDeliverabilityEventsByAccount(context.Context, uuid.UUID, string, time.Time) (int, error) {
	return 0, nil
}
func (r *holdRepo) CountDeliveredByAccount(context.Context, uuid.UUID, time.Time) (int, error) {
	return 0, nil
}

// A mailbox re-added under its old standing is exactly this: blocked, with
// every signal behind the block gone. The sweep must write the block back.
func TestEvaluateAndPersistHoldsABlockWithNoHistory(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	until := now.Add(25 * 24 * time.Hour)
	reason := "carried over"
	repo := &holdRepo{health: &models.WarmupParticipantHealth{
		PoolType: "premium", HealthState: models.WarmupHealthBlocked, BlockedUntil: &until, BlockedReason: &reason,
	}}
	s := &service{repo: repo, now: func() time.Time { return now }}

	if _, xerr := s.evaluateAndPersist(context.Background(), uuid.New(), "premium", repo.health); xerr != nil {
		t.Fatalf("evaluateAndPersist: %v", xerr)
	}
	if len(repo.written) != 1 || repo.written[0] != models.WarmupHealthBlocked {
		t.Fatalf("wrote %v, want the block held", repo.written)
	}
	if repo.until[0] == nil || !repo.until[0].Equal(until) {
		t.Fatalf("blocked_until = %v, want %v", repo.until[0], until)
	}
}
