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

// SocialWake names the mailbox a social campaign's wake-ups are filed under when it has no sender pool.
type SocialWake interface {
	CampaignWakeAccount(ctx context.Context, campaignID uuid.UUID) (uuid.UUID, bool, error)
}

// SocialAware is the optional capability the caller uses to attach it.
type SocialAware interface {
	WireSocial(l SocialWake)
}

// WireSocial lets a campaign with social accounts and no sending mailbox run its steps that need none.
func (s *schedulerService) WireSocial(l SocialWake) {
	s.social = l
}

// placeWithoutMailbox routes a campaign that has no sending mailbox. Steps that send no email (social,
// actions, waits, and email steps for a contact with no address) are placed on the campaign's own calendar;
// only when every due lead needs a mailbox is the pool's absence the answer.
func (s *schedulerService) placeWithoutMailbox(ctx context.Context, campaign *models.Campaign) (time.Time, *repository.ContactSequencePair, uuid.UUID, error) {
	if s.social == nil {
		return time.Time{}, nil, uuid.Nil, ErrNoEmailAccounts
	}
	wake, ok, err := s.social.CampaignWakeAccount(ctx, campaign.ID)
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
	// The daily new-lead cap holds here as on the mailbox path.
	excludeNewLeads := false
	if campaign.MaxNewLeadsPerDay > 0 {
		started, nerr := s.campaignRepo.CountNewLeadsStartedToday(ctx, campaign.ID)
		if nerr != nil {
			return time.Time{}, nil, uuid.Nil, nerr
		}
		excludeNewLeads = started >= campaign.MaxNewLeadsPerDay
	}
	open := openClocks(campaign, time.Now())
	candidates, recheckAt, _, err := s.campaignProgressRepo.FindRoutedPairs(ctx, campaign.ID, campaign.ContactOrderBy, campaign.ContactOrderDir,
		orderField, campaign.PrioritizeNewLeads, excludeNewLeads, nil, open, config.CampaignPlacementCandidates)
	if err != nil {
		return time.Time{}, nil, uuid.Nil, err
	}
	if len(candidates) == 0 && excludeNewLeads {
		again, againDue, _, aerr := s.campaignProgressRepo.FindRoutedPairs(ctx, campaign.ID, campaign.ContactOrderBy, campaign.ContactOrderDir,
			orderField, campaign.PrioritizeNewLeads, false, nil, open, 1)
		switch {
		case aerr != nil:
		case len(again) > 0:
			s.logCampaignDecision(ctx, campaign.ID, "new_lead_cap_reached",
				"Daily new-lead cap reached; deferring remaining new leads to tomorrow",
				map[string]interface{}{"max_new_leads_per_day": campaign.MaxNewLeadsPerDay})
			deferTime := s.deferToNextDay(campaign, false)
			if recheckAt != nil && recheckAt.Before(deferTime) {
				deferTime = *recheckAt
			}
			return deferTime, nil, wake, ErrCampaignDeferred
		case againDue != nil && (recheckAt == nil || againDue.Before(*recheckAt)):
			recheckAt = againDue
		}
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
