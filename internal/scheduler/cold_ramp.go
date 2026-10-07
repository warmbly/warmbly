package scheduler

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/warmupramp"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Unknown feedback cannot erase a mailbox's known warmup history.
func (s *schedulerService) coldRampStates(ctx context.Context, accounts []models.Email) map[uuid.UUID]repository.ColdRampState {
	states := make(map[uuid.UUID]repository.ColdRampState, len(accounts))
	ids := make([]uuid.UUID, 0, len(accounts))
	for _, a := range accounts {
		ids = append(ids, a.ID)
		states[a.ID] = repository.ColdRampState{WarmupStartedAt: a.Warmup}
	}
	if s.warmupRepo == nil || len(accounts) == 0 {
		return states
	}
	observed, err := s.warmupRepo.ColdRampStateForAccounts(ctx, ids, time.Now().Add(-warmupramp.LookbackWindow))
	if err != nil {
		return states
	}
	for _, a := range accounts {
		states[a.ID] = observed[a.ID].WithKnownWarmup(a.Warmup)
	}
	return states
}

// coldCeilingFor is the graduation ceiling for one mailbox, or mailboxCap when
// the gate does not apply.
//
// It applies only to mailboxes that have warmed. Gating one that never used
// warmup would cap customers who never opted into it, which is a different
// decision from stopping the warmup-to-cold spike this exists to stop.
func coldCeilingFor(state repository.ColdRampState, mailboxCap int) int {
	if state.WarmupStartedAt == nil {
		return mailboxCap
	}
	now := time.Now()
	warmupDays := int(now.Sub(*state.WarmupStartedAt).Hours() / 24)
	if warmupDays < 0 {
		warmupDays = 0
	}
	var rampStart time.Time
	if state.ColdRampStartedAt != nil {
		rampStart = *state.ColdRampStartedAt
	}
	return warmupramp.ColdCeiling(warmupDays, rampStart, state.Placements, now, mailboxCap, state.ConfirmedReplies)
}
