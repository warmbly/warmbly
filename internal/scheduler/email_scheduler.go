package scheduler

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/behavior"
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

	// STEP 1.5: Resolve the mailbox's sending-behaviour profile. Smart send is
	// a user-initiated email, so it follows the persona's WORKDAY (hours,
	// lunch, working weekdays) but is never charged against the cold daily or
	// hourly budgets — those exist to pace outreach, not someone's own replies.
	bhv := s.behaviorFor(ctx, account)
	gapSeconds := s.behaviorGap(bhv, time.Now(), account.MinWaitTime)

	// STEP 2: Get last email time and apply min_wait_time
	candidateTime := time.Now()

	lastEmailTime, err := s.taskRepo.GetLastEmailTime(ctx, accountID)
	if err != nil {
		return time.Time{}, err
	}

	if lastEmailTime != nil {
		minWait := time.Second * time.Duration(gapSeconds)
		earliestNext := lastEmailTime.Add(minWait)
		if candidateTime.Before(earliestNext) {
			candidateTime = earliestNext
		}
	}

	// STEP 3: Ensure within the mailbox's sending window — its own rolled
	// workday when a profile is enabled, otherwise the historical 8am-8pm band.
	candidateTime = s.snapEmailToWindow(bhv, candidateTime, account.Timezone)

	// STEP 4: Add jitter (±10 minutes)
	jitter := randomJitter(-10, 10)
	candidateTime = notBefore(candidateTime.Add(time.Minute * time.Duration(jitter)))

	// STEP 5: Resolve conflicts with scheduled tasks
	scheduledTasks, err := s.taskRepo.GetScheduledTasksForAccount(ctx, accountID, candidateTime)
	if err != nil {
		return time.Time{}, err
	}
	candidateTime = resolveConflicts(candidateTime, scheduledTasks, gapSeconds)

	// STEP 6: Apply distribution curve. Skipped when a profile owns the day.
	if !bhv.Enabled {
		candidateTime = applyDistributionCurve(candidateTime, accountTZ)
	}

	// STEP 7: Final window check after jitter and conflict resolution
	return notBefore(s.snapEmailToWindow(bhv, candidateTime, account.Timezone)), nil
}

// snapEmailToWindow places a smart-send candidate inside the mailbox's rolled
// workday, falling back to the legacy 8am-8pm business-hours band for mailboxes
// with no behaviour profile.
func (s *schedulerService) snapEmailToWindow(r behavior.Resolved, candidate time.Time, timezone string) time.Time {
	if !r.Enabled {
		return ensureBusinessHours(candidate, timezone)
	}
	if snapped, ok := behaviorWindow(r, candidate); ok {
		return snapped
	}
	return candidate
}
