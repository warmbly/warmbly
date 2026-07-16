package models

import (
	"time"

	"github.com/google/uuid"
)

// ResearchStatus is the run lifecycle. nothing_found is a billable success (the
// agent looked and honestly found nothing worth saving).
type ResearchStatus string

const (
	ResearchQueued       ResearchStatus = "queued"
	ResearchRunning      ResearchStatus = "running"
	ResearchSucceeded    ResearchStatus = "succeeded"
	ResearchFailed       ResearchStatus = "failed"
	ResearchNothingFound ResearchStatus = "nothing_found"
)

// ContactResearchRun is one research attempt against a contact.
type ContactResearchRun struct {
	ID             uuid.UUID      `json:"id"`
	OrgID          uuid.UUID      `json:"org_id"`
	ContactID      uuid.UUID      `json:"contact_id"`
	RequestedBy    *uuid.UUID     `json:"requested_by,omitempty"`
	Status         ResearchStatus `json:"status"`
	Objective      string         `json:"objective"`
	Result         ResearchResult `json:"result"`
	Error          string         `json:"error,omitempty"`
	CreditsCharged int            `json:"credits_charged"`
	ModelUsed      string         `json:"model_used"`
	TokensUsed     int            `json:"tokens_used"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// ResearchResult is the STRICT save_research schema. It is validated at the app
// boundary (every signal and artifact must carry a url) before persistence.
type ResearchResult struct {
	Company            *ResearchCompany  `json:"company,omitempty"`
	Person             *ResearchPerson   `json:"person,omitempty"`
	Signals            []ResearchSignal  `json:"signals,omitempty"`
	Hooks              []ResearchHook    `json:"hooks,omitempty"`
	CustomFieldUpdates map[string]string `json:"custom_field_updates,omitempty"`
	ResearchNotes      string            `json:"research_notes,omitempty"`
	NothingFound       bool              `json:"nothing_found"`
}

type ResearchCompany struct {
	Summary            string   `json:"summary,omitempty"`
	Industry           string   `json:"industry,omitempty"`
	SizeEstimate       string   `json:"size_estimate,omitempty"`
	SellsTo            string   `json:"sells_to,omitempty"`
	TechOrStackSignals []string `json:"tech_or_stack_signals,omitempty"`
}

type ResearchPerson struct {
	RoleConfirmed   bool               `json:"role_confirmed"`
	Title           string             `json:"title,omitempty"`
	PublicArtifacts []ResearchArtifact `json:"public_artifacts,omitempty"`
}

// ResearchArtifact is a public thing about the person; url is required.
type ResearchArtifact struct {
	What  string `json:"what"`
	Where string `json:"where"`
	When  string `json:"when,omitempty"`
	URL   string `json:"url"`
}

// ResearchSignal is a cited fact; url is required, confidence is high|medium|low.
type ResearchSignal struct {
	Type       string `json:"type"`
	Fact       string `json:"fact"`
	When       string `json:"when,omitempty"`
	URL        string `json:"url"`
	Confidence string `json:"confidence"`
}

// ResearchHook is an opener grounded in a signal.
type ResearchHook struct {
	BasedOn     string `json:"based_on"`
	WhyRelevant string `json:"why_relevant"`
	OpenerLine  string `json:"opener_line"`
}

// ResearchBatchProgress is the payload the AI_RESEARCH_PROGRESS realtime event
// carries so teammates see a batch advance live.
type ResearchBatchProgress struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}
