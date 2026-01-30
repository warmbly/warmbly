package scheduler

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CalculateNextEmailTime calculates the optimal time to send a user-initiated email (smart send)
func (s *schedulerService) CalculateNextEmailTime(ctx context.Context, accountID uuid.UUID) (time.Time, error) {
	// STEP 1: Load email account
	account, xerr := s.emailRepo.GetByID(ctx, accountID)
	if xerr != nil {
		return time.Time{}, xerr
	}
	if account == nil {
		return time.Now(), nil
	}

	accountTZ := loadLocation(account.Timezone)

	// STEP 2: Get last email time and apply min_wait_time
	candidateTime := time.Now()

	lastEmailTime, err := s.taskRepo.GetLastEmailTime(ctx, accountID)
	if err != nil {
		return time.Time{}, err
	}

	if lastEmailTime != nil {
		minWait := time.Second * time.Duration(account.MinWaitTime)
		earliestNext := lastEmailTime.Add(minWait)
		if candidateTime.Before(earliestNext) {
			candidateTime = earliestNext
		}
	}

	// STEP 3: Ensure within business hours (8am-8pm account timezone)
	candidateTime = ensureBusinessHours(candidateTime, account.Timezone)

	// STEP 4: Add jitter (±10 minutes)
	jitter := randomJitter(-10, 10)
	candidateTime = candidateTime.Add(time.Minute * time.Duration(jitter))

	// STEP 5: Resolve conflicts with scheduled tasks
	scheduledTasks, err := s.taskRepo.GetScheduledTasksForAccount(ctx, accountID, candidateTime)
	if err != nil {
		return time.Time{}, err
	}
	candidateTime = resolveConflicts(candidateTime, scheduledTasks, account.MinWaitTime)

	// STEP 6: Apply distribution curve
	candidateTime = applyDistributionCurve(candidateTime, accountTZ)

	// STEP 7: Final business hours check
	candidateTime = ensureBusinessHours(candidateTime, account.Timezone)

	return candidateTime, nil
}

