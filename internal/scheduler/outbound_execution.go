package scheduler

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/behavior"
)

// OutboundExecutionGate is the start check the tasks service asks of the scheduler.
type OutboundExecutionGate interface {
	OutboundExecutionNotBefore(ctx context.Context, mailbox, campaign, contact, task uuid.UUID, now time.Time) (time.Time, error)
}

var _ OutboundExecutionGate = (*schedulerService)(nil)

// OutboundExecutionNotBefore is when a reserved campaign send may start. contact
// is the lead, uuid.Nil when unknown; a recipient-local campaign reads its
// window on that lead's clock, the one the send was placed on.
func (s *schedulerService) OutboundExecutionNotBefore(ctx context.Context, mailbox, campaign, contact, task uuid.UUID, now time.Time) (time.Time, error) {
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
	clock := loadLocation(c.EffectiveTimezone)
	if c.SendsInRecipientTimezone() && contact != uuid.Nil && s.contactRepo != nil {
		lead, cerr := s.contactRepo.GetByID(ctx, contact)
		if cerr != nil {
			// Refuse rather than check the send against a clock it was not placed on.
			return time.Time{}, cerr
		}
		clock = scheduleClock(c, clock, lead)
	}
	at := nextScheduleSlot(now, effectiveWindows(c), clock)
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
