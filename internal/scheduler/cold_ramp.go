package scheduler

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/warmupramp"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// coldRampStates loads the pool's graduation inputs. Fails open to an empty
// map: a lookup error must not cap a customer's sending.
func (s *schedulerService) coldRampStates(ctx context.Context, accounts []models.Email) map[uuid.UUID]repository.ColdRampState {
	if s.warmupRepo == nil || len(accounts) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(accounts))
	for _, a := range accounts {
		ids = append(ids, a.ID)
	}
	states, err := s.warmupRepo.ColdRampStateForAccounts(ctx, ids, time.Now().Add(-warmupramp.LookbackWindow))
	if err != nil {
		return nil
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
	return warmupramp.ColdCeiling(warmupDays, rampStart, state.Placements, now, mailboxCap)
}
