package scheduler

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/behavior"
)

func (s *schedulerService) OutboundExecutionNotBefore(ctx context.Context, mailbox, campaign, task uuid.UUID, now time.Time) (time.Time, error) {
	a, xerr := s.emailRepo.GetByID(ctx, mailbox)
	if xerr != nil {
		return time.Time{}, xerr
	}
	if a == nil {
		return time.Time{}, errors.New("mailbox unavailable")
	}
	c, err := s.campaignRepo.GetByID(ctx, campaign)
	if err != nil {
		return time.Time{}, err
	}
	if c == nil || c.Status != "active" || c.OrganizationID == nil || a.OrganizationID == nil || *c.OrganizationID != *a.OrganizationID {
		return time.Time{}, errors.New("campaign unavailable")
	}
	at := nextScheduleSlot(now, effectiveWindows(c), loadLocation(c.EffectiveTimezone))
	if at.After(now) {
		return at, nil
	}
	bhv := s.behaviorFor(ctx, a)
	if open, ok := behaviorWindow(bhv, now); !ok {
		return time.Time{}, errors.New("mailbox has no working days")
	} else if open.After(now) {
		return open, nil
	}
	if bhv.Enabled {
		loc := bhv.Loc
		start := behavior.PlanDateFor(now, loc)
		hStart, hEnd := behavior.HourWindow(now, loc)
		counter, ok := s.taskRepo.(interface {
			WarmupDispatchUsage(context.Context, uuid.UUID, uuid.UUID, time.Time, time.Time, time.Time, time.Time) (int, int, int, *time.Time, error)
		})
		if !ok {
			return time.Time{}, errors.New("send usage unavailable")
		}
		_, daily, hourly, last, err := counter.WarmupDispatchUsage(ctx, mailbox, task, start, start.AddDate(0, 0, 1), hStart, hEnd)
		if err != nil {
			return time.Time{}, err
		}
		plan := bhv.PlanOn(start)
		if daily >= plan.DailyLimit {
			return start.AddDate(0, 0, 1), nil
		}
		if hourly >= plan.HourlyLimit {
			return hEnd, nil
		}
		if last != nil {
			if next := last.Add(time.Duration(s.behaviorGapFloor(bhv, now, a.MinWaitTime)) * time.Second); next.After(now) {
				return next, nil
			}
		}
	}
	return now, nil
}
