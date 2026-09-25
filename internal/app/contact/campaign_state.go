package contact

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/scheduler"
)

// SetNextSendPreviewer is optional; without it the panel has no next action.
func (s *contactService) SetNextSendPreviewer(p scheduler.ContactSendPreviewer) { s.previewer = p }

// CampaignStates returns the contact's campaigns with a scheduler-backed next action.
func (s *contactService) CampaignStates(ctx context.Context, orgID, contactID uuid.UUID) ([]models.ContactCampaignState, *errx.Error) {
	states, xerr := s.contactRepository.ListCampaignStates(ctx, orgID, contactID)
	if xerr != nil {
		return nil, xerr
	}
	for i := range states {
		s.fillNextAction(ctx, &states[i], contactID)
	}
	return states, nil
}

// endedCopy says why a flow is over, keyed by lead status or route exclusion (they share values).
var endedCopy = map[string]string{
	models.LeadStatusUnsubscribed:  "Unsubscribed, sending stopped",
	models.LeadStatusBounced:       "Bounced, sending stopped",
	models.LeadStatusFailed:        "Dropped after every send attempt failed",
	models.LeadStatusUndeliverable: "Address failed verification, so the campaign skips it",
	"suppressed":                   "Suppressed, sending stopped",
}

func (s *contactService) fillNextAction(ctx context.Context, st *models.ContactCampaignState, contactID uuid.UUID) {
	// Replied and paused are not endings: the router decides both, below.
	if reason, ok := endedCopy[st.LeadStatus]; ok {
		st.EndedReason = reason
		return
	}
	if s.previewer == nil {
		return
	}
	pv, err := s.previewer.PreviewContactSend(ctx, st.CampaignID, contactID)
	if err != nil {
		// The panel still shows the flow and status; only the preview is missing.
		log.Warn().Err(err).Str("campaign_id", st.CampaignID.String()).Str("contact_id", contactID.String()).Msg("contact next-send preview failed")
		return
	}
	route := pv.Route
	// Replied outranks failed and undeliverable in the status, so the exclusion ends those.
	if reason, ok := endedCopy[route.Excluded]; ok {
		st.EndedReason = reason
		return
	}
	current := "the current step"
	if st.CurrentStep != nil {
		current = st.CurrentStep.Label
	}
	if route.Target == nil {
		if route.WaitUntil != nil {
			st.Next = &models.ContactNextAction{
				StepLabel:  "Depends on their response",
				State:      models.NextActionWaiting,
				NotBefore:  route.WaitUntil,
				Constraint: "Waiting to see how they respond to " + current,
			}
			return
		}
		switch {
		case st.LeadStatus == models.LeadStatusReplied:
			st.EndedReason = "Replied, sending stopped"
		case st.LeadStatus == models.LeadStatusCompleted:
			st.EndedReason = "Every step has been sent"
		case st.CurrentStep != nil:
			st.EndedReason = "Reached the end of the flow"
		default:
			st.EndedReason = "The campaign has no steps"
		}
		return
	}

	next := &models.ContactNextAction{
		StepID:      route.Target,
		StepLabel:   "Next step",
		State:       pv.State,
		ScheduledAt: pv.ScheduledAt,
		NotBefore:   pv.NotBefore,
	}
	for _, step := range st.Steps {
		if step.ID == *route.Target {
			next.StepLabel, next.Kind, next.Subject = step.Label, step.Kind, step.Subject
			break
		}
	}
	next.Constraint = constraintCopy(pv.Constraint, st, route.DueAt, current)
	if pv.Constraint == scheduler.ConstraintLeadHold {
		next.Constraint = holdCopy(st.Hold)
	}
	st.Next = next
}

// holdCopy words a per-lead hold for the drawer: why the flow is parked and,
// when the hold has an end, when it lifts. A reason the auto-reply gave (its
// subject line) is the most useful thing on screen, so it leads.
func holdCopy(hold *models.LeadHold) string {
	if hold == nil {
		return "Paused for this contact"
	}
	what := "Paused for this contact"
	if hold.Source == models.LeadHoldSourceOutOfOffice {
		what = "Out of office"
	}
	if r := strings.TrimSpace(hold.Reason); r != "" {
		what += ": " + r
	}
	if hold.Until == nil {
		return what + ", until someone resumes it"
	}
	return what + ", resuming in " + humanizeUntil(*hold.Until)
}

// humanizeUntil renders how long is left until t as the drawer's short phrase
// ("2 days", "4 hours", "a moment"), rounding to the largest whole unit.
func humanizeUntil(t time.Time) string {
	d := time.Until(t)
	if d < time.Minute {
		return "a moment"
	}
	plural := func(n int, unit string) string {
		if n == 1 {
			return fmt.Sprintf("1 %s", unit)
		}
		return fmt.Sprintf("%d %ss", n, unit)
	}
	// Rounding can carry into the next unit (59m40s rounds to 60 minutes), so
	// promote rather than print a value the unit cannot hold.
	switch {
	case d < time.Hour:
		if minutes := int(math.Round(d.Minutes())); minutes < 60 {
			return plural(minutes, "minute")
		}
		return plural(1, "hour")
	case d < 24*time.Hour:
		if hours := int(math.Round(d.Hours())); hours < 24 {
			return plural(hours, "hour")
		}
		return plural(1, "day")
	default:
		return plural(int(math.Round(d.Hours()/24)), "day")
	}
}

// constraintCopy turns the scheduler's gate into the sentence the drawer shows.
func constraintCopy(c scheduler.ContactSendConstraint, st *models.ContactCampaignState, dueAt *time.Time, current string) string {
	switch c {
	case scheduler.ConstraintStepWait:
		if st.CurrentStep != nil && st.CurrentStep.SentAt != nil && dueAt != nil {
			days := int(math.Round(dueAt.Sub(*st.CurrentStep.SentAt).Hours() / 24))
			if days >= 1 {
				unit := "days"
				if days == 1 {
					unit = "day"
				}
				return fmt.Sprintf("Waiting %d %s after %s", days, unit, current)
			}
		}
		return "Waiting for the step's delay after " + current
	case scheduler.ConstraintEntryDelay:
		if dueAt != nil {
			return "Waiting " + humanizeUntil(*dueAt) + " before the first email"
		}
		return "Waiting the campaign's delay before the first email"
	case scheduler.ConstraintConditionWindow:
		return "Waiting to see how they respond to " + current
	case scheduler.ConstraintStartDate:
		return "Waiting for the campaign's start date"
	case scheduler.ConstraintSendingWindow:
		return "Outside the campaign's sending window"
	case scheduler.ConstraintNewLeadCap:
		return "Today's new-lead limit is reached"
	case scheduler.ConstraintCapacity:
		return "No mailbox can take it right now (daily cap, spacing, or health)"
	case scheduler.ConstraintSenderBusy:
		// One mailbox is the gate, not the pool: this contact's whole sequence
		// sends from the address they first heard from.
		if st.SenderEmail != "" {
			return "Waiting for " + st.SenderEmail + ", which sends the rest of this sequence"
		}
		return "Waiting for the mailbox that sends this contact's sequence"
	case scheduler.ConstraintCampaignInactive:
		switch st.CampaignStatus {
		case "draft":
			return "Campaign has not been started"
		case "completed":
			return "Campaign has finished"
		case "paused_guardrail":
			return "Campaign was auto-paused by a guardrail"
		case "paused_undeliverable":
			return "Campaign is paused: verification refused its remaining leads"
		}
		return "Campaign is paused"
	case scheduler.ConstraintNoMailbox:
		return "No sending mailbox is attached to the campaign"
	case scheduler.ConstraintDomainAuth:
		return "Every sending domain fails SPF/DMARC authentication"
	case scheduler.ConstraintCampaignEnded:
		return "Campaign is past its end date"
	}
	return ""
}
