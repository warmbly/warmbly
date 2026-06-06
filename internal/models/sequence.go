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

	// Kind is "email" (default — subject/body are rendered and sent) or a
	// non-email control node: "action" (Action.Type names the side effect) or
	// "wait" (delay only). Routing (Conditions) is identical regardless of Kind.
	Kind string `json:"kind"`
	// Action is the typed config for non-email nodes; an empty object for email
	// nodes. Stored in the sequences.action jsonb column.
	Action json.RawMessage `json:"action,omitempty"`

	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

// ActionConfig is the persisted config for a non-email (action/wait) node. Type
// is the switch the task executes on; the remaining fields are type-scoped.
type ActionConfig struct {
	Type string `json:"type"` // wait | add_tag | remove_tag | unsubscribe | notify | end

	// wait
	WaitMinutes *int `json:"wait_minutes,omitempty"`

	// add_tag / remove_tag — a contact category id (product "tags" == categories)
	CategoryID *uuid.UUID `json:"category_id,omitempty"`

	// notify — webhook / integration fan-out
	NotifyEvent string         `json:"notify_event,omitempty"`
	NotifyData  map[string]any `json:"notify_data,omitempty"`
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

	// Kind / Action, when non-nil, switch the node between email and action/wait.
	Kind   *string       `json:"kind"`
	Action *ActionConfig `json:"action"`
}

// BranchConditions is the typed branching tree persisted in the sequence
// `conditions` jsonb column. Branches are evaluated in declared order; the first
// branch whose conditions ALL match wins. A winning branch routes the contact to
// its TargetSequenceID (any step in the campaign), or stops them when the target
// is nil. When no branch matches (or Branches is empty) the scheduler keeps the
// default linear progression (advance to the next step by position).
type BranchConditions struct {
	Branches []Branch `json:"branches,omitempty"`
}

// Branch is a single conditional route out of a step ("if <conditions> -> go to
// target, else stop"). A branch with no conditions is an unconditional catch-all
// ("otherwise").
type Branch struct {
	// BranchID is a stable client-supplied identifier (for editor diffing /
	// logging). Kept as a free-form string: the editor uses crypto.randomUUID()
	// when available but falls back to a non-UUID token, so this must NOT be a
	// strict uuid.UUID or unmarshalling the PATCH body would fail.
	BranchID string `json:"branch_id"`
	// TargetSequenceID is the step to route to when this branch matches. nil
	// means STOP (send the contact no further step). A target that no longer
	// exists (a deleted step) is treated as STOP at schedule time.
	TargetSequenceID *uuid.UUID `json:"target_sequence_id"`
	// Conditions are ANDed together — every condition must hold for the branch
	// to match. An empty list is an unconditional/catch-all branch ("otherwise").
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
