package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Sequence struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`

	Subject   string `json:"subject"`
	BodyPlain string `json:"body_plain"`
	BodyHTML  string `json:"body_html"`
	BodySync  bool   `json:"body_sync"`
	BodyCode  bool   `json:"body_code"`

	WaitAfter int `json:"wait_after"`
	Position  int `json:"position"`

	// Conditions is the per-step branching tree. When empty (`{}` / no
	// branches), the step keeps the default linear behaviour (advance to the
	// next position). When populated, the scheduler evaluates the contact's
	// engagement against these branches at schedule time to decide which step
	// (or stop) comes next. Stored as a single jsonb column on `sequences`.
	Conditions json.RawMessage `json:"conditions,omitempty"`

	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

type UpdateSequence struct {
	Name    *string `json:"name"`
	Subject *string `json:"subject"`

	BodyPlain *string `json:"body_plain"`
	BodyHTML  *string `json:"body_html"`
	BodySync  *bool   `json:"body_sync"`
	BodyCode  *bool   `json:"body_code"`

	WaitAfter *int `json:"wait_after"`

	// Conditions, when non-nil, replaces the step's branching tree. Send `{}`
	// (or an object with an empty `branches` array) to clear branching and fall
	// back to linear progression.
	Conditions *BranchConditions `json:"conditions"`
}

// BranchConditions is the typed branching tree persisted in the sequence
// `conditions` jsonb column. Branches are evaluated in declared order; the
// first branch whose conditions ALL match wins. A winning branch's
// TargetSequenceID selects the next step, or nil ("stop") ends the contact's
// journey. When no branch matches (or Branches is empty) the scheduler keeps
// the default linear progression.
type BranchConditions struct {
	Branches []Branch `json:"branches,omitempty"`
}

// Branch is a single conditional route out of a step.
type Branch struct {
	// BranchID is a stable client-supplied identifier (for editor diffing /
	// logging). Kept as a free-form string: the editor uses crypto.randomUUID()
	// when available but falls back to a non-UUID token, so this must NOT be a
	// strict uuid.UUID or unmarshalling the PATCH body would fail.
	BranchID string `json:"branch_id"`
	// TargetSequenceID is the step to jump to when this branch matches. nil
	// means "stop" (do not send the contact any further step).
	TargetSequenceID *uuid.UUID `json:"target_sequence_id"`
	// Conditions are ANDed together — every condition must hold for the branch
	// to match. An empty list is an unconditional/catch-all branch.
	Conditions []BranchCondition `json:"conditions,omitempty"`
}

// BranchCondition is a single engagement predicate evaluated against the
// contact's campaign_contact_progress row for the current step.
type BranchCondition struct {
	// Field is the engagement signal: "opened" | "clicked" | "replied" and
	// their negations "not_opened" | "not_clicked" | "not_replied".
	Field string `json:"field"`
	// Operator is the comparison. Currently "within_days" (the signal occurred
	// in the last Value days) and "ever" (the signal occurred at all). For the
	// not_* fields the meaning inverts (did NOT happen within / ever).
	Operator string `json:"operator"`
	// Value is the day window for "within_days". nil for operators that take no
	// argument (e.g. "ever").
	Value *int `json:"value"`
}
