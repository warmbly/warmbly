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
		states[a.ID] = repository.ColdRampState{WarmupStartedAt: a.Warmup}
		// Every reader ignores the state of a mailbox that never warmed.
		if a.Warmup != nil {
			ids = append(ids, a.ID)
		}
	}
	if s.warmupRepo == nil || len(ids) == 0 {
		return states
	}
	observed, err := s.warmupRepo.ColdRampStateForAccounts(ctx, ids, time.Now().Add(-warmupramp.LookbackWindow))
	if err != nil {
		return states
	}
	for _, a := range accounts {
		if a.Warmup != nil {
			states[a.ID] = observed[a.ID].WithKnownWarmup(a.Warmup)
		}
	}
	return states
}

// coldCeilingFor is the graduation ceiling for one mailbox, or mailboxCap when
// the gate does not apply.
//
// It applies only to mailboxes that have warmed and whose warmup is not
// stopped: a stopped mailbox is capped by its own campaign limit and min-gap.
func coldCeilingFor(state repository.ColdRampState, mailboxCap int) int {
	if !state.CeilingApplies() {
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
