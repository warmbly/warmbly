package inboxtag

import (
	"github.com/warmbly/warmbly/internal/models"
)

// Phases 2 and 3: what a verdict may do beyond labelling. This file plans and
// never executes; advanced.ApplyInboxTagActions runs the plan on the same
// primitives a member's click uses, so every threshold stays in this package.

// StopHoldDays is how long a decline parks a contact. Finite, because every
// automatic hold expires on its own; a person who must never be mailed again
// is suppressed, not held.
const StopHoldDays = 365

// Action names, stored on the verdict row and shown on the review page.
const (
	ActionHold     = "hold"
	ActionStop     = "stop"
	ActionTask     = "task"
	ActionSuppress = "suppress"
)

// Plan is what a verdict is allowed to do.
type Plan struct {
	// HoldDays parks the contact's sequences for this many days. Zero means
	// no timed hold.
	HoldDays int
	// HoldReason is shown on the lead; empty reads as the not-now hold.
	HoldReason string
	// Stop parks the contact's sequences for StopHoldDays.
	Stop bool
	// Task opens a follow-up for the mailbox owner, with this title.
	Task string
	// Suppress adds the sender to the suppression list, with this reason.
	Suppress string
}

// Actions lists the plan as the names the review page shows.
func (p Plan) Actions() []string {
	var out []string
	if p.HoldDays > 0 {
		out = append(out, ActionHold)
	}
	if p.Stop {
		out = append(out, ActionStop)
	}
	if p.Task != "" {
		out = append(out, ActionTask)
	}
	if p.Suppress != "" {
		out = append(out, ActionSuppress)
	}
	return out
}

// Empty reports a plan that does nothing.
func (p Plan) Empty() bool { return len(p.Actions()) == 0 }

// PlanActions turns a decision into the actions the workspace allows. Only a
// confident human reply acts; the intent reads at ConfFloor like the labels,
// and the removal request reads its own noul at Strong because a suppression
// cannot be undone.
func PlanActions(d Decision, s models.InboxTaggingSettings) Plan {
	var p Plan
	if d.Kind != KindHumanReply || d.NeedsReview || d.KindConfidence < ConfFloor {
		return p
	}
	intentTrusted := d.Intent != "" && d.IntentConfidence >= ConfFloor

	if s.HoldOnNotNow && intentTrusted && d.Intent == IntentNotNow {
		p.HoldDays = s.NotNowHoldDays
	}
	if s.StopOnDeclined && intentTrusted && (d.Intent == IntentNotInterested || d.Intent == IntentWrongPerson) {
		p.Stop = true
	}
	if s.TaskOnCallRequest && (hasSignal(d, SigAsksForCall) || (intentTrusted && d.Intent == IntentScheduling)) {
		if intentTrusted && d.Intent == IntentScheduling {
			p.Task = "Confirm the time they proposed"
		} else {
			p.Task = "They asked for a call"
		}
	}
	planCustom(&p, d)
	if s.SuppressOnRemovalRequest && removalStrong(d) {
		p.Suppress = "asked to be removed in a reply"
		// A person who asked to be removed is not held, they are stopped: the
		// suppression already ends every sequence, and a hold on top of it
		// would be a second thing for a member to find and lift.
		p.HoldDays = 0
		p.HoldReason = ""
		p.Stop = false
		p.Task = ""
	}
	return p
}

// removalStrong reads the removal noul at Strong. The opt_out intent alone is
// not enough: it is one of eight choices and its confidence is spread across
// them, while the noul answers exactly the question a suppression needs.
func removalStrong(d Decision) bool {
	return d.SignalStrength[SigRequestsRemoval] >= Strong
}

func hasSignal(d Decision, id string) bool {
	for _, s := range d.Signals {
		if s == id {
			return true
		}
	}
	return false
}
