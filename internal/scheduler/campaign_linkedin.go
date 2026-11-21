package scheduler

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// LinkedInWake names the mailbox a LinkedIn campaign's wake-ups are filed under when it has no sender pool.
type LinkedInWake interface {
	CampaignWakeAccount(ctx context.Context, campaignID uuid.UUID) (uuid.UUID, bool, error)
}

// LinkedInAware is the optional capability the caller uses to attach it.
type LinkedInAware interface {
	WireLinkedIn(l LinkedInWake)
}

// WireLinkedIn lets a campaign with LinkedIn accounts and no sending mailbox run its steps that need none.
func (s *schedulerService) WireLinkedIn(l LinkedInWake) {
	s.linkedIn = l
}

// placeWithoutMailbox routes a campaign that has no sending mailbox. Steps that send no email (LinkedIn,
// actions, waits, and email steps for a contact with no address) are placed on the campaign's own calendar;
// only when every due lead needs a mailbox is the pool's absence the answer.
func (s *schedulerService) placeWithoutMailbox(ctx context.Context, campaign *models.Campaign) (time.Time, *repository.ContactSequencePair, uuid.UUID, error) {
	if s.linkedIn == nil {
		return time.Time{}, nil, uuid.Nil, ErrNoEmailAccounts
	}
	wake, ok, err := s.linkedIn.CampaignWakeAccount(ctx, campaign.ID)
	if err != nil {
		return time.Time{}, nil, uuid.Nil, err
	}
	if !ok {
		return time.Time{}, nil, uuid.Nil, ErrNoEmailAccounts
	}
	orderField := ""
	if campaign.ContactOrderField != nil {
		orderField = *campaign.ContactOrderField
	}
	candidates, recheckAt, _, err := s.campaignProgressRepo.FindRoutedPairs(ctx, campaign.ID, campaign.ContactOrderBy, campaign.ContactOrderDir,
		orderField, campaign.PrioritizeNewLeads, false, nil, openClocks(campaign, time.Now()), config.CampaignPlacementCandidates)
	if err != nil {
		return time.Time{}, nil, uuid.Nil, err
	}
	if len(candidates) == 0 {
		if recheckAt != nil {
			s.logCampaignDecisionOnce(ctx, campaign.ID, "awaiting_next_step", "No step is due yet; re-checking when the next wait elapses",
				map[string]interface{}{"recheck_at": recheckAt.UTC().Format(time.RFC3339)})
			return *recheckAt, nil, wake, ErrCampaignDeferred
		}
		return time.Time{}, nil, uuid.Nil, ErrCampaignCompleted
	}
	var soonest time.Time
	for i := range candidates {
		pair := &candidates[i]
		seq, serr := s.campaignRepo.GetSequenceByID(ctx, pair.SequenceID)
		if serr != nil {
			return time.Time{}, nil, uuid.Nil, serr
		}
		var contact *models.Contact
		if s.contactRepo != nil {
			if c, cerr := s.contactRepo.GetByID(ctx, pair.ContactID); cerr == nil {
				contact = c
			}
		}
		if (seq.Kind == "" || seq.Kind == "email") && (contact == nil || strings.TrimSpace(contact.Email) != "") {
			continue
		}
		at, perr := s.calendarSlot(ctx, campaign, pair, seq, contact)
		if perr != nil {
			return time.Time{}, nil, uuid.Nil, perr
		}
		if time.Until(at) <= config.CampaignNotDueGraceSeconds*time.Second {
			return finalSlot(at), pair, wake, nil
		}
		if soonest.IsZero() || at.Before(soonest) {
			soonest = at
		}
	}
	if soonest.IsZero() {
		return time.Time{}, nil, uuid.Nil, ErrNoEmailAccounts
	}
	return soonest, nil, wake, ErrCampaignDeferred
}

// calendarSlot is the earliest moment the campaign's own schedule allows a step: its wait, routing's floor,
// the start and end dates and the sending windows.
func (s *schedulerService) calendarSlot(ctx context.Context, campaign *models.Campaign, pair *repository.ContactSequencePair, seq *models.Sequence, contact *models.Contact) (time.Time, error) {
	at := time.Now()
	last, err := s.campaignProgressRepo.GetContactLastSequenceTime(ctx, pair.ContactID, campaign.ID)
	if err != nil {
		return time.Time{}, err
	}
	if last != nil {
		if t := last.Add(24 * time.Hour * time.Duration(repository.BranchWaitAfter(seq.WaitAfter, pair.Instant))); t.After(at) {
			at = t
		}
	}
	if pair.NotBefore != nil && pair.NotBefore.After(at) {
		at = *pair.NotBefore
	}
	if campaign.StartDate != nil && at.Before(*campaign.StartDate) {
		at = *campaign.StartDate
	}
	if campaign.EndDate != nil && at.After(*campaign.EndDate) {
		return time.Time{}, ErrCampaignEnded
	}
	tz := scheduleClock(campaign, loadLocation(campaign.ClockTimezone()), contact)
	return nextScheduleSlot(at, effectiveWindows(campaign), tz), nil
}
