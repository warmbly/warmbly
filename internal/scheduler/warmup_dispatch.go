package scheduler

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/behavior"
	"github.com/warmbly/warmbly/internal/app/warmupramp"
	"github.com/warmbly/warmbly/internal/models"
)

// WarmupDispatchNotBefore rechecks current policy, not the queue's old snapshot.
func (s *schedulerService) WarmupDispatchNotBefore(ctx context.Context, id uuid.UUID, now time.Time) (time.Time, error) {
	return s.WarmupExecutionNotBefore(ctx, id, uuid.Nil, now)
}

func (s *schedulerService) WarmupExecutionNotBefore(ctx context.Context, id, taskID uuid.UUID, now time.Time) (time.Time, error) {
	a, xerr := s.emailRepo.GetByID(ctx, id)
	if xerr != nil {
		return time.Time{}, xerr
	}
	if a == nil || a.Status != "active" {
		return time.Time{}, ErrWarmupNotEnabled
	}
	inCampaign := false
	if s.campaignRepo != nil {
		n, err := s.campaignRepo.CountActiveCampaignsForAccount(ctx, id)
		if err != nil {
			return time.Time{}, err
		}
		inCampaign = n > 0
	}
	if !a.IsWarmingActive() && !inCampaign {
		return time.Time{}, ErrWarmupNotEnabled
	}
	var bhv behavior.Resolved
	if s.behaviorSvc != nil {
		profile, err := s.behaviorSvc.Get(ctx, id)
		if err != nil {
			return time.Time{}, err
		}
		bhv = behavior.NewStandalone(profile, loadLocation(a.ClockTimezone()))
	}
	at, err := warmupDispatchWindow(a, bhv, now)
	if err != nil {
		return time.Time{}, err
	}
	if at.After(now) {
		return at, nil
	}
	pool := s.warmupPoolTypeForAccount(ctx, a)
	eligible, err := s.warmupRepo.IsPoolEligible(ctx, id, pool, true)
	if err != nil {
		return time.Time{}, err
	}
	if !eligible {
		return time.Time{}, errors.New("warmup sender authority unavailable")
	}
	health, err := s.warmupRepo.GetParticipantHealth(ctx, id, pool)
	if err != nil {
		return time.Time{}, err
	}
	if health == nil {
		return time.Time{}, errors.New("warmup health unavailable")
	}
	anchor := now
	if a.Warmup != nil {
		anchor = *a.Warmup
	}
	plan := warmupramp.Resolve(ctx, s.warmupRepo, warmupramp.Input{AccountID: id, WarmupStart: anchor, ActivelyWarming: a.IsWarmingActive(), Base: a.WarmupBase, Increase: a.WarmupIncrease, Max: a.WarmupMax, InCampaign: inCampaign, Health: health.HealthState, Now: now})
	target := plan.Target
	if a.IsWarmingActive() && target > 0 {
		varied := max(a.WarmupBase, int(float64(target)*dailyVolumeFactor(id, now.In(loadLocation(a.ClockTimezone())))+0.5))
		target = min(target, varied)
	}
	var candidates []models.WarmupPartnerCandidate
	if taskID == uuid.Nil {
		candidates, err = s.warmupRepo.WarmupPartnerCandidates(ctx, pool, id)
	} else if reader, ok := s.warmupRepo.(interface {
		WarmupExecutionCandidates(context.Context, string, uuid.UUID, uuid.UUID) ([]models.WarmupPartnerCandidate, error)
	}); ok {
		candidates, err = reader.WarmupExecutionCandidates(ctx, pool, id, taskID)
	} else {
		return time.Time{}, errors.New("warmup recipient capacity unavailable")
	}
	if err != nil {
		return time.Time{}, err
	}
	target = min(target, len(candidates))
	counter, ok := s.taskRepo.(interface {
		WarmupDispatchUsage(context.Context, uuid.UUID, uuid.UUID, time.Time, time.Time, time.Time, time.Time) (int, int, int, *time.Time, error)
	})
	if !ok {
		return time.Time{}, errors.New("warmup budget authority unavailable")
	}
	loc := loadLocation(a.ClockTimezone())
	local := now.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	hourStart, hourEnd := behavior.HourWindow(now, loc)
	count, daily, hourly, last, err := counter.WarmupDispatchUsage(ctx, id, taskID, start, start.AddDate(0, 0, 1), hourStart, hourEnd)
	if err != nil {
		return time.Time{}, err
	}
	if count >= target {
		return warmupDispatchWindow(a, bhv, start.AddDate(0, 0, 1))
	}
	if bhv.Enabled {
		dayPlan := bhv.PlanOn(behavior.PlanDateFor(now, bhv.Loc))
		if daily >= dayPlan.DailyLimit {
			return warmupDispatchWindow(a, bhv, start.AddDate(0, 0, 1))
		}
		if hourly >= dayPlan.HourlyLimit {
			return warmupDispatchWindow(a, bhv, hourEnd)
		}
	}
	if last != nil {
		gap := s.behaviorGapFloor(bhv, now, a.MinWaitTime)
		gap = int(float64(gap) * adjustmentFor(health.HealthState).minWaitMultiplier)
		if floor := last.Add(time.Duration(gap) * time.Second); floor.After(now) {
			return warmupDispatchWindow(a, bhv, floor)
		}
	}
	return now, nil
}

func warmupDispatchWindow(a *models.Email, bhv behavior.Resolved, desired time.Time) (time.Time, error) {
	loc := loadLocation(a.ClockTimezone())
	at := desired
	for i := 0; i < 32; i++ {
		day := findNextValidDay(at, uint8(a.WarmupDays), loc)
		if !sameLocalDay(at, day, loc) {
			at = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
		}
		if bhv.Enabled {
			open, _, ok := bhv.NextOpen(at)
			if !ok {
				return time.Time{}, errors.New("warmup behavior has no working days")
			}
			at = open
		}
		{
			start, end := models.ClockMinutes(a.WarmupStartTime, 8*60), models.ClockMinutes(a.WarmupEndTime, 20*60)
			if end <= start {
				return time.Time{}, errors.New("invalid warmup time window")
			}
			local := at.In(loc)
			minutes := local.Hour()*60 + local.Minute()
			if minutes >= end {
				local = local.AddDate(0, 0, 1)
				at = time.Date(local.Year(), local.Month(), local.Day(), start/60, start%60, 0, 0, loc)
			} else if minutes < start {
				at = time.Date(local.Year(), local.Month(), local.Day(), start/60, start%60, 0, 0, loc)
			}
		}
		valid := findNextValidDay(at, uint8(a.WarmupDays), loc)
		if bhv.Enabled {
			open, _, ok := bhv.NextOpen(at)
			if !ok {
				return time.Time{}, errors.New("warmup behavior has no working days")
			}
			if open.After(at) {
				at = open
				continue
			}
		}
		if sameLocalDay(at, valid, loc) {
			return at, nil
		}
		at = valid
	}
	return time.Time{}, errors.New("warmup calendars have no reachable intersection")
}
