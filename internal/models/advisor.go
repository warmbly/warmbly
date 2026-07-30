package models

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"time"

	"github.com/google/uuid"
)

// AdvisorSeverity ranks how urgently a finding needs attention. It drives
// ordering, the nav-tab badge tone, and whether a new finding is worth a toast.
type AdvisorSeverity string

const (
	// AdvisorCritical means reputation or delivery is actively being damaged
	// right now (complaint/bounce rates in the documented hard-block bands, a
	// mailbox sending cold mail while quarantined from the warmup pool).
	AdvisorCritical AdvisorSeverity = "critical"
	// AdvisorHigh means a real problem that will become critical if ignored.
	AdvisorHigh AdvisorSeverity = "high"
	// AdvisorMedium means a meaningful improvement with no immediate risk.
	AdvisorMedium AdvisorSeverity = "medium"
	// AdvisorLow means a polish-level suggestion.
	AdvisorLow AdvisorSeverity = "low"
)

// severityRank orders severities most-urgent-first for sorting and threshold
// comparisons.
var severityRank = map[AdvisorSeverity]int{
	AdvisorCritical: 4,
	AdvisorHigh:     3,
	AdvisorMedium:   2,
	AdvisorLow:      1,
}

// Rank returns the numeric urgency of a severity (higher is more urgent, 0 for
// an unknown value).
func (s AdvisorSeverity) Rank() int { return severityRank[s] }

// AtLeast reports whether s is at least as urgent as min.
func (s AdvisorSeverity) AtLeast(min AdvisorSeverity) bool { return s.Rank() >= min.Rank() }

// AdvisorCategory groups findings by the kind of problem they describe. Orgs
// can mute a whole category.
type AdvisorCategory string

const (
	AdvisorCategoryDeliverability AdvisorCategory = "deliverability"
	AdvisorCategoryMailbox        AdvisorCategory = "mailbox"
	AdvisorCategoryWarmup         AdvisorCategory = "warmup"
	AdvisorCategoryCampaign       AdvisorCategory = "campaign"
	AdvisorCategoryCopy           AdvisorCategory = "copy"
	AdvisorCategoryList           AdvisorCategory = "list"
)

// AdvisorSurface is the dashboard nav tab a finding belongs to. The badge count
// and the inline strip both key off it, so advice always appears where the fix
// is made rather than in a separate inbox.
type AdvisorSurface string

const (
	AdvisorSurfaceCampaigns      AdvisorSurface = "campaigns"
	AdvisorSurfaceMailboxes      AdvisorSurface = "emails"
	AdvisorSurfaceDeliverability AdvisorSurface = "deliverability"
	AdvisorSurfaceContacts       AdvisorSurface = "contacts"
	AdvisorSurfaceAnalytics      AdvisorSurface = "analytics"
	AdvisorSurfaceSettings       AdvisorSurface = "settings"
)

// AdvisorStatus is a finding's lifecycle state.
type AdvisorStatus string

const (
	// AdvisorStatusOpen is an active, unaddressed finding.
	AdvisorStatusOpen AdvisorStatus = "open"
	// AdvisorStatusSnoozed hides the finding until SnoozedUntil passes. The
	// detector keeps re-confirming it in the background.
	AdvisorStatusSnoozed AdvisorStatus = "snoozed"
	// AdvisorStatusDismissed means a member said this isn't a problem for them.
	// It stays dismissed until the underlying condition clears and recurs.
	AdvisorStatusDismissed AdvisorStatus = "dismissed"
	// AdvisorStatusApplied means the one-click fix ran. It stays in this state
	// until the next evaluation confirms the condition is gone (-> resolved) or
	// still present (-> reopened).
	AdvisorStatusApplied AdvisorStatus = "applied"
	// AdvisorStatusResolved means the detector no longer fires.
	AdvisorStatusResolved AdvisorStatus = "resolved"
)

// AdvisorPreviewChange is one line of the before/after preview shown in the fix
// drawer. Every one-click fix renders its full effect this way before the user
// confirms; nothing is ever applied sight-unseen.
type AdvisorPreviewChange struct {
	Field string `json:"field"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// AdvisorAction is the one-click remedy attached to a finding. It executes
// through the shared AI tool registry, which runs the tool AS the invoking user
// with their permission bits enforced, so the Advisor can never apply a change
// the member could not have made by hand.
type AdvisorAction struct {
	// Tool is the aitools registry name (e.g. "update_mailbox").
	Tool string `json:"tool"`
	// Args is the tool's JSON payload, fully materialized at detection time.
	Args json.RawMessage `json:"args"`
	// Label is the button text ("Lower the daily cap to 35").
	Label string `json:"label"`
	// Auto marks a fix that autopilot may apply unattended. It is true only for
	// a bounded settings change that moves in the safe direction and can be
	// undone: lowering a cap, widening a gap, resuming warmup. Anything that
	// stops sending, edits copy, or cannot be reverted stays false and waits
	// for a person, however obvious the fix looks.
	Auto bool `json:"auto,omitempty"`
	// Preview is the exact before/after the drawer renders.
	Preview []AdvisorPreviewChange `json:"preview,omitempty"`
	// Undo, when set, is the tool call that reverts this one. Surfaced as
	// "Undo" on the applied card.
	Undo *AdvisorUndo `json:"undo,omitempty"`
}

// AdvisorUndo reverts an applied action.
type AdvisorUndo struct {
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args"`
}

// AdvisorFinding is one piece of advice about one entity.
type AdvisorFinding struct {
	ID             uuid.UUID `json:"id"`
	OrganizationID uuid.UUID `json:"organization_id"`

	Fingerprint string          `json:"-"`
	DetectorKey string          `json:"detector_key"`
	Category    AdvisorCategory `json:"category"`
	Severity    AdvisorSeverity `json:"severity"`
	Surface     AdvisorSurface  `json:"surface"`

	EntityType  string     `json:"entity_type,omitempty"`
	EntityID    *uuid.UUID `json:"entity_id,omitempty"`
	EntityLabel string     `json:"entity_label,omitempty"`

	// ParentType / ParentID name the entity this finding belongs to when it
	// differs from the subject: a step's copy problem belongs to its campaign,
	// and the campaign page is where someone goes looking for it.
	ParentType string     `json:"parent_type,omitempty"`
	ParentID   *uuid.UUID `json:"parent_id,omitempty"`

	Status AdvisorStatus `json:"status"`
	Impact int           `json:"impact"`

	Title string `json:"title"`
	// GroupTitle names the finding when several of its kind are listed
	// together, with a {count} placeholder ("{count} mailboxes are capped above
	// the safe band"). Empty means this finding is always shown on its own.
	GroupTitle string `json:"group_title,omitempty"`
	Detail     string `json:"detail"`
	Remedy     string `json:"remedy"`
	// Steps is the ordered manual how-to, set only by checks with no one-click
	// fix. Empty means the remedy prose is the whole answer.
	Steps []string `json:"steps,omitempty"`
	// AgentFixable is true when an agent can resolve this by editing something
	// the platform owns. False for anything that lives outside it, like a DNS
	// record, so the client shows the steps rather than a button that cannot
	// succeed. Computed per request, never stored.
	AgentFixable bool `json:"agent_fixable"`
	// Narrated is false while the card still shows the built-in fallback copy
	// (AI unconfigured, or narration not run yet). The card is fully usable
	// either way; this only tells the client whether to offer "rewrite".
	Narrated bool `json:"narrated"`

	Evidence json.RawMessage `json:"evidence,omitempty"`
	Action   *AdvisorAction  `json:"action,omitempty"`

	FirstSeenAt time.Time  `json:"first_seen_at"`
	LastSeenAt  time.Time  `json:"last_seen_at"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`

	SnoozedUntil  *time.Time `json:"snoozed_until,omitempty"`
	DismissedAt   *time.Time `json:"dismissed_at,omitempty"`
	DismissReason string     `json:"dismiss_reason,omitempty"`

	AppliedAt     *time.Time `json:"applied_at,omitempty"`
	AppliedBy     *uuid.UUID `json:"applied_by,omitempty"`
	AppliedResult string     `json:"applied_result,omitempty"`
}

// EvidenceHash fingerprints the finding's evidence so a re-run can tell
// "same problem, same numbers" (keep the narration) from "same problem, the
// numbers moved" (re-narrate). Detectors round the values they put in Evidence
// into bands, so ordinary drift does not churn the copy.
func (f *AdvisorFinding) EvidenceHash() string {
	sum := sha256.Sum256(f.Evidence)
	return hex.EncodeToString(sum[:8])
}

// AdvisorSurfaceCount is the per-nav-tab badge payload.
type AdvisorSurfaceCount struct {
	Surface AdvisorSurface `json:"surface"`
	// Total is every open finding on this surface; Critical/High drive the
	// badge tone so a tab never screams about a low-severity nit.
	Total    int `json:"total"`
	Critical int `json:"critical"`
	High     int `json:"high"`
}

// AdvisorSummary is the org-wide rollup: what the badges show, plus a single
// health score so the trend is legible at a glance.
type AdvisorSummary struct {
	// Score is 100 minus the weighted cost of every open finding, floored at 0.
	Score    int                   `json:"score"`
	Total    int                   `json:"total"`
	Critical int                   `json:"critical"`
	High     int                   `json:"high"`
	Medium   int                   `json:"medium"`
	Low      int                   `json:"low"`
	Surfaces []AdvisorSurfaceCount `json:"surfaces"`
	// LastRunAt is when the engine last evaluated this org (nil before the
	// first run).
	LastRunAt *time.Time `json:"last_run_at,omitempty"`
}

// advisorSeverityCost is how much each open finding weighs against the org's
// Advisor score. Weighted so one critical finding matters more than a dozen
// low-severity nits, which is the ordering a sender actually cares about.
var advisorSeverityCost = map[AdvisorSeverity]float64{
	AdvisorCritical: 25,
	AdvisorHigh:     10,
	AdvisorMedium:   4,
	AdvisorLow:      1,
}

// advisorScoreScale tunes how fast the score falls. A single critical finding
// lands around 78; the curve then flattens, so the twentieth medium-severity
// suggestion moves the number far less than the first critical one did.
const advisorScoreScale = 100.0

// AdvisorScore folds open-finding counts into a 0-100 health score.
//
// The penalty saturates rather than accumulating linearly. A linear score hits
// zero at around four high-severity findings, after which a workspace with four
// problems and a workspace with forty look identical, and fixing three of them
// changes nothing on screen. An exponential decay keeps the number honest
// across the range a workspace actually lives in: it never reaches zero, and
// every fix moves it until the tail flattens out.
func AdvisorScore(critical, high, medium, low int) int {
	cost := float64(critical)*advisorSeverityCost[AdvisorCritical] +
		float64(high)*advisorSeverityCost[AdvisorHigh] +
		float64(medium)*advisorSeverityCost[AdvisorMedium] +
		float64(low)*advisorSeverityCost[AdvisorLow]
	if cost <= 0 {
		return 100
	}
	score := int(math.Round(100 * math.Exp(-cost/advisorScoreScale)))
	if score < 1 {
		return 1
	}
	return score
}

// AdvisorSettings holds the org's advisor controls.
type AdvisorSettings struct {
	OrganizationID  uuid.UUID       `json:"organization_id"`
	Enabled         bool            `json:"enabled"`
	MutedCategories []string        `json:"muted_categories"`
	MutedDetectors  []string        `json:"muted_detectors"`
	MinSeverity     AdvisorSeverity `json:"min_severity"`
	// Autopilot applies auto-safe fixes on its own. Off by default.
	Autopilot bool `json:"autopilot"`
	// AutopilotActorID is the member autopilot acts as. Set automatically to
	// whoever switches it on; autopilot stops if they leave the org.
	AutopilotActorID *uuid.UUID `json:"autopilot_actor_id,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// AutopilotMaxPerRun bounds how many fixes autopilot may apply in one
// evaluation. A misconfiguration should cost a handful of reversible changes,
// not a whole workspace rewritten while nobody was looking.
const AutopilotMaxPerRun = 10

// DefaultAdvisorSettings is what an org gets before it has ever written a
// settings row: everything on, nothing muted.
func DefaultAdvisorSettings(orgID uuid.UUID) *AdvisorSettings {
	return &AdvisorSettings{
		OrganizationID:  orgID,
		Enabled:         true,
		MutedCategories: []string{},
		MutedDetectors:  []string{},
		MinSeverity:     AdvisorLow,
	}
}

// AdvisorAgentResult is what an agent fix reports back.
type AdvisorAgentResult struct {
	FindingID uuid.UUID `json:"finding_id"`
	// Applied is true only when the agent called a tool that changed something.
	// A run that read the campaign and decided nothing was wrong reports false,
	// so the finding stays open.
	Applied bool `json:"applied"`
	// Summary is the agent's own account of what it did.
	Summary string `json:"summary"`
	// Steps are the tool calls it made, in order. This is the part the member
	// can check against the audit log.
	Steps []string `json:"steps,omitempty"`
}

// AdvisorFeedback is a member's verdict on one finding.
type AdvisorFeedback struct {
	FindingID uuid.UUID `json:"finding_id"`
	Helpful   bool      `json:"helpful"`
	Reason    string    `json:"reason,omitempty"`
}

// AdvisorSnoozeRequest snoozes a finding for a bounded window.
type AdvisorSnoozeRequest struct {
	// Days must be 1-90. Anything else is rejected: an unbounded snooze is a
	// dismissal wearing a disguise, and the two should stay distinguishable.
	Days int `json:"days" binding:"required,min=1,max=90"`
}

// AdvisorDismissRequest records why a member rejected the advice. The reason is
// optional but feeds detector tuning.
type AdvisorDismissRequest struct {
	Reason string `json:"reason"`
}
