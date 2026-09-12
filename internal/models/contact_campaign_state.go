package models

import (
	"time"

	"github.com/google/uuid"
)

// ContactCampaignState is one campaign a contact is a lead of: steps with
// this contact's progress, the derived lead status, and the next action.
type ContactCampaignState struct {
	CampaignID     uuid.UUID `json:"campaign_id"`
	CampaignName   string    `json:"campaign_name"`
	CampaignStatus string    `json:"campaign_status"`
	// LeadStatus follows ContactCampaignProgress.Status exactly.
	LeadStatus string `json:"lead_status"`

	// SenderID / SenderEmail are the mailbox this lead's whole sequence sends
	// from, fixed when its first email went out. Empty until then: rotation
	// picks it for the first step and every follow-up follows.
	SenderID    *uuid.UUID `json:"sender_id,omitempty"`
	SenderEmail string     `json:"sender_email,omitempty"`
	// FailureReason is the worker's reason for the last failed send.
	FailureReason string `json:"failure_reason,omitempty"`

	Steps          []ContactCampaignStep `json:"steps"`
	CompletedSteps int                   `json:"completed_steps"`
	TotalSteps     int                   `json:"total_steps"`

	// CurrentStep is the latest step sent.
	CurrentStep  *ContactCampaignStep `json:"current_step,omitempty"`
	LastAction   string               `json:"last_action,omitempty"`
	LastActionAt *time.Time           `json:"last_action_at,omitempty"`

	// Next is nil once the flow has ended for the contact; EndedReason says why.
	Next        *ContactNextAction `json:"next,omitempty"`
	EndedReason string             `json:"ended_reason,omitempty"`
}

// ContactCampaignStep is one flow node with this contact's progress on it.
type ContactCampaignStep struct {
	ID       uuid.UUID `json:"id"`
	Label    string    `json:"label"`
	Kind     string    `json:"kind"`
	Position int       `json:"position"`
	Subject  string    `json:"subject,omitempty"`

	SentAt    *time.Time `json:"sent_at,omitempty"`
	OpenedAt  *time.Time `json:"opened_at,omitempty"`
	ClickedAt *time.Time `json:"clicked_at,omitempty"`
	RepliedAt *time.Time `json:"replied_at,omitempty"`
	BouncedAt *time.Time `json:"bounced_at,omitempty"`
	FailedAt  *time.Time `json:"failed_at,omitempty"`
	// Attempts counts failed sends; InFlight is a reservation with no result yet.
	Attempts int  `json:"attempts,omitempty"`
	InFlight bool `json:"in_flight,omitempty"`
}

// ContactNextActionState says how firm the next action's timing is; only
// "due" carries the campaign's next wakeup.
type ContactNextActionState string

const (
	NextActionDue     ContactNextActionState = "due"
	NextActionWaiting ContactNextActionState = "waiting"
	NextActionPaused  ContactNextActionState = "paused"
	NextActionBlocked ContactNextActionState = "blocked"
)

// ContactNextAction is derived on read through the scheduler; never stored.
type ContactNextAction struct {
	// StepID is nil while a branch condition is still undecided.
	StepID    *uuid.UUID `json:"step_id,omitempty"`
	StepLabel string     `json:"step_label"`
	Kind      string     `json:"kind,omitempty"`
	Subject   string     `json:"subject,omitempty"`

	State ContactNextActionState `json:"state"`
	// ScheduledAt is set only when due, and is then when the campaign's chain
	// next wakes up and works its queue — a stored wakeup, so it reads the same
	// on every refresh instead of a freshly paced slot that walked forward
	// every time the drawer polled (issue #437). NotBefore is the earliest the
	// hard constraints allow; Constraint names the gate in user-facing words.
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
	NotBefore   *time.Time `json:"not_before,omitempty"`
	Constraint  string     `json:"constraint,omitempty"`
}

type ContactCampaignStatesResult struct {
	Data []ContactCampaignState `json:"data"`
}
