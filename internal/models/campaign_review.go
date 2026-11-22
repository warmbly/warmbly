package models

import (
	"time"

	"github.com/google/uuid"
)

// Review states of one lead, derived from its hold and whether it was emailed.
const (
	// LeadReviewWaiting is held for review: nothing goes out until it is approved.
	LeadReviewWaiting = "waiting"
	// LeadReviewReady has not been emailed yet and nothing keeps it from starting.
	LeadReviewReady = "ready"
	// LeadReviewHeld has not been emailed yet and is paused for another reason.
	LeadReviewHeld = "held"
	// LeadReviewStarted has been emailed, or has a send in flight.
	LeadReviewStarted = "started"
)

// ReviewStatusFilters are the values ?status= accepts on the review lead list.
var ReviewStatusFilters = map[string]bool{
	"": true, "all": true, LeadReviewWaiting: true, LeadReviewReady: true, LeadReviewHeld: true, LeadReviewStarted: true,
}

// MissingFieldNames are the contact fields the readiness check counts blanks of.
var MissingFieldNames = []string{"FirstName", "LastName", "Company", "Phone"}

// CampaignReviewLead is one row of the review list.
type CampaignReviewLead struct {
	ContactID uuid.UUID `json:"contact_id"`
	Email     string    `json:"email"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	Company   string    `json:"company"`
	Status    string    `json:"status"`
	Hold      *LeadHold `json:"hold,omitempty"`
	// Personalized counts the steps with this lead's own copy.
	Personalized int `json:"personalized"`
	// Missing lists the fields the campaign's emails print that this lead has empty.
	Missing []string   `json:"missing,omitempty"`
	AddedAt *time.Time `json:"added_at,omitempty"`
}

// CampaignReviewLeadsResult is the paginated review list.
type CampaignReviewLeadsResult struct {
	Data       []CampaignReviewLead `json:"data"`
	Pagination CPagination          `json:"pagination"`
}

// CampaignReviewLeadsFilter narrows the review list.
type CampaignReviewLeadsFilter struct {
	Status string
	// Missing keeps leads with this field empty; one of MissingFieldNames.
	Missing string
	Search  string
	// Gaps are the fields the emails print with no fallback, which decide Missing on each row.
	Gaps   []string
	Cursor *CampaignReviewCursor
	Limit  int
}

// CampaignReviewCursor is the keyset position of the review list: email, then contact id.
type CampaignReviewCursor struct {
	Email     string
	ContactID uuid.UUID
}

// CampaignReviewCounts is how the campaign's leads split by review state.
type CampaignReviewCounts struct {
	Total        int `json:"total"`
	Waiting      int `json:"waiting"`
	Ready        int `json:"ready"`
	Held         int `json:"held"`
	Started      int `json:"started"`
	Personalized int `json:"personalized"`
}

// CampaignMissingField is one field the emails print that some unstarted leads have empty.
type CampaignMissingField struct {
	Field string `json:"field"`
	Leads int    `json:"leads"`
}

// CampaignAIReadiness is how much of the first email's AI copy is written ahead.
type CampaignAIReadiness struct {
	StepID *uuid.UUID `json:"step_id,omitempty"`
	// Blocks is how many AI blocks the first email has.
	Blocks int `json:"blocks"`
	// Written counts unstarted leads whose first email has every block written.
	Written int `json:"written"`
	// Pending counts unstarted leads with at least one block still to write.
	Pending int `json:"pending"`
	// EstimatedCostUSD is about what writing the pending blocks costs, at the per-block estimate the balance check reserves.
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	// Auto is true while the instance writes the pending leads on its own (an active campaign with AI available).
	Auto bool `json:"auto"`
}

// CampaignReviewSummary is the Review tab's readiness report.
type CampaignReviewSummary struct {
	CampaignID  uuid.UUID              `json:"campaign_id"`
	ReviewLeads bool                   `json:"review_leads"`
	Counts      CampaignReviewCounts   `json:"counts"`
	Missing     []CampaignMissingField `json:"missing"`
	// Undeliverable counts leads whose address verification rejected.
	Undeliverable int `json:"undeliverable"`
	// Unsubscribed counts leads who opted out; sending skips them.
	Unsubscribed int                 `json:"unsubscribed"`
	AI           CampaignAIReadiness `json:"ai"`
	// TemplateErrors names email steps whose copy does not render as written.
	TemplateErrors []CampaignStepIssue `json:"template_errors,omitempty"`
}

// CampaignStepIssue names a step and what is wrong with it.
type CampaignStepIssue struct {
	StepID   uuid.UUID `json:"step_id"`
	Position int       `json:"position"`
	Message  string    `json:"message"`
}

// CampaignReviewChange is the answer to a review write: how many leads moved.
type CampaignReviewChange struct {
	CampaignID  uuid.UUID `json:"campaign_id"`
	ReviewLeads bool      `json:"review_leads"`
	// Held counts leads put into review; Released counts leads approved.
	Held     int `json:"held"`
	Released int `json:"released"`
}

// LeadStepOverride is one lead's own copy of one email step.
type LeadStepOverride struct {
	CampaignID uuid.UUID  `json:"campaign_id"`
	ContactID  uuid.UUID  `json:"contact_id"`
	StepID     uuid.UUID  `json:"step_id"`
	Subject    string     `json:"subject"`
	BodyHTML   string     `json:"body_html"`
	BodyPlain  string     `json:"body_plain"`
	EditedBy   *uuid.UUID `json:"edited_by,omitempty"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// LeadStepOverrideInput is the body of PUT .../emails/:stepId.
type LeadStepOverrideInput struct {
	Subject   string `json:"subject"`
	BodyHTML  string `json:"body_html"`
	BodyPlain string `json:"body_plain"`
}

// LeadEmailAIBlock is one AI block in a step, and whether its copy is written yet.
type LeadEmailAIBlock struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Written bool   `json:"written"`
}

// LeadEmailFrom is the mailbox a lead's emails go out from, when it is known.
type LeadEmailFrom struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// LeadEmailStep is one email step rendered for one lead.
type LeadEmailStep struct {
	StepID    uuid.UUID `json:"step_id"`
	Position  int       `json:"position"`
	Name      string    `json:"name"`
	Subject   string    `json:"subject"`
	BodyHTML  string    `json:"body_html"`
	BodyPlain string    `json:"body_plain"`
	// Source is the template this lead's copy renders from: their own copy, the
	// A/B arm drawn for them, or the step's. What a personalization starts from.
	Source LeadStepOverrideInput `json:"source"`
	// Threaded is a follow-up that replies in the lead's thread, so its subject is the conversation's.
	Threaded bool `json:"threaded"`
	// Personalized is this lead's own copy, which replaces the step's.
	Personalized bool       `json:"personalized"`
	EditedAt     *time.Time `json:"edited_at,omitempty"`
	// StepChanged is a personalized step whose shared copy was edited after it.
	StepChanged bool `json:"step_changed"`
	// VariantID is the A/B variant this lead gets, absent for the step's own copy.
	VariantID *uuid.UUID         `json:"variant_id,omitempty"`
	AIBlocks  []LeadEmailAIBlock `json:"ai_blocks,omitempty"`
	// Missing lists fields the copy prints that this lead has empty.
	Missing []string   `json:"missing,omitempty"`
	Errors  []string   `json:"errors,omitempty"`
	Sent    bool       `json:"sent"`
	SentAt  *time.Time `json:"sent_at,omitempty"`
}

// LeadEmails is every email step of a campaign rendered for one lead.
type LeadEmails struct {
	CampaignID uuid.UUID          `json:"campaign_id"`
	Lead       CampaignReviewLead `json:"lead"`
	From       *LeadEmailFrom     `json:"from,omitempty"`
	Steps      []LeadEmailStep    `json:"steps"`
}

// LeadAIBatchResult is one pass of writing AI copy ahead for several leads.
type LeadAIBatchResult struct {
	Written int `json:"written"`
	Failed  int `json:"failed"`
	// Remaining counts leads in the request's scope still to write after this pass.
	Remaining int `json:"remaining"`
	// Stopped names why the pass ended early: insufficient_credits or usage_cap_exceeded.
	Stopped string `json:"stopped,omitempty"`
	// Next is the cursor the following pass sends as after, so leads that failed are not retried in a loop.
	Next string `json:"next,omitempty"`
}
