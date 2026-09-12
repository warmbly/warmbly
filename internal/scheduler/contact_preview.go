package scheduler

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// ContactSendConstraint names the gate holding a step back; the caller words it.
type ContactSendConstraint string

const (
	ConstraintNone             ContactSendConstraint = ""
	ConstraintStepWait         ContactSendConstraint = "step_wait"
	ConstraintEntryDelay       ContactSendConstraint = "entry_delay"
	ConstraintConditionWindow  ContactSendConstraint = "condition_window"
	ConstraintStartDate        ContactSendConstraint = "start_date"
	ConstraintSendingWindow    ContactSendConstraint = "sending_window"
	ConstraintNewLeadCap       ContactSendConstraint = "new_lead_cap"
	ConstraintCapacity         ContactSendConstraint = "capacity"
	ConstraintCampaignInactive ContactSendConstraint = "campaign_inactive"
	ConstraintNoMailbox        ContactSendConstraint = "no_mailbox"
	ConstraintDomainAuth       ContactSendConstraint = "domain_auth"
	ConstraintCampaignEnded    ContactSendConstraint = "campaign_ended"
	// ConstraintSenderBusy is the whole pool being fine and one mailbox not:
	// the lead's sequence belongs to a mailbox that has nothing left today.
	ConstraintSenderBusy ContactSendConstraint = "sender_busy"
)

// ContactSendPreview is a read-only "what happens next" for one contact in
// one campaign. It is a pure read in both senses: it writes nothing, and it
// answers the same unchanged state with the same times on every call, because
// the drawer polls it and a figure that walked forward on every refresh read as
// a product that could not make up its mind (issue #437).
//
// ScheduledAt is set only when the step is due now, and is then the campaign
// chain's own next wakeup. NotBefore is the step's hard floor, never a paced
// slot.
type ContactSendPreview struct {
	Route       *repository.ContactRoute
	State       models.ContactNextActionState
	ScheduledAt *time.Time
	NotBefore   *time.Time
	Constraint  ContactSendConstraint
}

// ContactSendPreviewer is satisfied by the scheduler service.
type ContactSendPreviewer interface {
	PreviewContactSend(ctx context.Context, campaignID, contactID uuid.UUID) (*ContactSendPreview, error)
}

// PreviewContactSend routes and places one contact through the send path's
// own rules without writing anything.
func (s *schedulerService) PreviewContactSend(ctx context.Context, campaignID, contactID uuid.UUID) (*ContactSendPreview, error) {
	campaign, err := s.campaignRepo.GetByID(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	route, err := s.campaignProgressRepo.RouteContact(ctx, campaignID, contactID)
	if err != nil {
		return nil, err
	}
	pv := &ContactSendPreview{Route: route}

	if route.WaitUntil != nil {
		pv.State = models.NextActionWaiting
		pv.NotBefore = route.WaitUntil
		pv.Constraint = ConstraintConditionWindow
		return pv, nil
	}
	if route.Target == nil || route.Excluded != "" {
		return pv, nil
	}
	if campaign.Status != "active" {
		pv.State = models.NextActionPaused
		pv.NotBefore = route.DueAt
		pv.Constraint = ConstraintCampaignInactive
		return pv, nil
	}

	projectRampLevel(campaign, time.Now())

	accounts, meta, err := s.campaignSenders(ctx, campaign)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		pv.State = models.NextActionBlocked
		pv.Constraint = ConstraintNoMailbox
		return pv, nil
	}

	pair := &repository.ContactSequencePair{
		ContactID: contactID, SequenceID: *route.Target, IsNewLead: route.IsNewLead,
		NotBefore: route.DueAt, AssignedSender: route.AssignedSender,
	}
	at, sendable, _, perr := s.placeCampaignSend(ctx, campaign, accounts, meta, pair, nil, true)
	switch {
	case perr == nil && sendable != nil:
		pv.State = models.NextActionDue
		// A due step is not waiting for a time of its own; it is waiting for the
		// campaign's own chain to wake up and reach it. Report THAT instant,
		// which is a stored scheduled_at and therefore the same answer on every
		// read, rather than rolling a fresh slot for a send that is already due
		// (issue #437). No wakeup means the chain is being re-seeded, and the
		// honest answer is no time at all.
		pv.ScheduledAt = s.campaignWakeup(ctx, campaignID)
		return pv, nil
	case errors.Is(perr, ErrCampaignDeferred):
		pv.State = models.NextActionWaiting
		// `at` is the step's hard floor here, not a paced slot: a preview runs
		// placement with the spacing, jitter and distribution layers off, so the
		// same unchanged campaign answers with the same instant every time.
		slot := at
		if route.DueAt != nil && route.DueAt.After(slot) {
			slot = *route.DueAt
		}
		pv.NotBefore = &slot
		pv.Constraint = s.deferralConstraint(ctx, campaign, route)
		// The pool is not the gate here, one mailbox is: say which, rather than
		// blaming "no mailbox can take it" while two others sit idle.
		if errors.Is(perr, ErrSenderBusy) && pv.Constraint == ConstraintCapacity {
			pv.Constraint = ConstraintSenderBusy
		}
		return pv, nil
	case errors.Is(perr, ErrCampaignEnded):
		pv.State = models.NextActionBlocked
		pv.Constraint = ConstraintCampaignEnded
		return pv, nil
	case errors.Is(perr, ErrDomainAuthFailing):
		pv.State = models.NextActionBlocked
		pv.Constraint = ConstraintDomainAuth
		return pv, nil
	case errors.Is(perr, ErrNoEmailAccounts):
		pv.State = models.NextActionBlocked
		pv.Constraint = ConstraintCapacity
		return pv, nil
	}
	return nil, perr
}

// deferralConstraint picks the gate that explains a deferred placement, in
// the order the send path applies them: the step's own wait (or, for a first
// step, the campaign's entry delay), the campaign start date, the sending
// window, today's new-lead cap, and otherwise the mailbox pool (daily caps,
// spacing, health, rest).
func (s *schedulerService) deferralConstraint(ctx context.Context, campaign *models.Campaign, route *repository.ContactRoute) ContactSendConstraint {
	grace := time.Now().Add(config.CampaignNotDueGraceSeconds * time.Second)
	if route.DueAt != nil && route.DueAt.After(grace) {
		// A first step's wait is the campaign's entry delay, not a step wait —
		// the drawer words the two differently.
		if route.IsNewLead {
			return ConstraintEntryDelay
		}
		return ConstraintStepWait
	}
	if campaign.StartDate != nil && campaign.StartDate.After(grace) {
		return ConstraintStartDate
	}
	if nextScheduleSlot(time.Now(), effectiveWindows(campaign), loadLocation(campaign.Timezone)).After(grace) {
		return ConstraintSendingWindow
	}
	if route.IsNewLead && campaign.MaxNewLeadsPerDay > 0 {
		if n, err := s.campaignRepo.CountNewLeadsStartedToday(ctx, campaign.ID); err == nil && n >= campaign.MaxNewLeadsPerDay {
			return ConstraintNewLeadCap
		}
	}
	return ConstraintCapacity
}

// projectRampLevel applies today's ramp advance in memory, mirroring
// campaignRepo.AdvanceRampLevel, so a preview on a new UTC day budgets with
// the level the next real pass will persist instead of yesterday's.
func projectRampLevel(c *models.Campaign, now time.Time) {
	if !c.RampEnabled {
		return
	}
	today := now.UTC().Truncate(24 * time.Hour)
	if c.RampLevelDate != nil && !c.RampLevelDate.UTC().Truncate(24*time.Hour).Before(today) {
		return
	}
	c.RampLevel = min(c.RampCeiling, max(c.RampLevel, c.RampStart)+c.RampIncrement)
	c.RampLevelDate = &today
}

// campaignWakeup is when the campaign's chain next runs: the earliest pending
// campaign task. Nil when the campaign has none, which the reconciler fixes
// within its own interval.
func (s *schedulerService) campaignWakeup(ctx context.Context, campaignID uuid.UUID) *time.Time {
	tasks, err := s.campaignRepo.GetPendingCampaignTasks(ctx, campaignID)
	if err != nil {
		return nil
	}
	var next *time.Time
	for i := range tasks {
		at := tasks[i].ScheduledAt
		if at == nil {
			continue
		}
		if next == nil || at.Before(*next) {
			next = at
		}
	}
	return next
}
