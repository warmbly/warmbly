// Package advisor continuously evaluates an organization's sending posture and
// turns what it finds into specific, fixable advice.
//
// The split is deliberate:
//
//   - Detection is pure Go over a single snapshot (see repository.LoadSnapshot).
//     Detectors are total functions with no I/O, no model calls, and explicit
//     sample floors, so the same data always produces the same findings and the
//     Advisor keeps working with AI switched off entirely.
//   - Narration is the only AI step. It rewrites a finding's title/detail/remedy
//     from the evidence the detector already computed, in the org's voice, and
//     is cached per (detector, evidence shape). It can never invent a finding,
//     change a severity, or alter an action.
//   - Remediation runs through the shared aitools registry as the invoking user,
//     after they confirm a rendered before/after preview.
//
// Every threshold here traces to the sending-safety policy in CLAUDE.md or to
// the provider guidance it cites (Google's 0.1%/0.3% complaint bands, SES's 5%
// /10% bounce bands). When they disagree, the stricter one wins: a shared
// warmup pool has to act before the mailbox providers do.
package advisor

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Finding is one detector's output before it is persisted. The Title/Detail/
// Remedy written here are the deterministic fallback: complete, specific, and
// good enough to ship on their own. Narration only makes them warmer.
type Finding struct {
	Key      string
	Category models.AdvisorCategory
	Severity models.AdvisorSeverity
	Surface  models.AdvisorSurface

	EntityType  string
	EntityID    *uuid.UUID
	EntityLabel string
	// ParentType / ParentID let a finding show up on the page someone would
	// actually look at: a step's copy problem is listed on its campaign.
	ParentType string
	ParentID   *uuid.UUID

	// Impact ranks findings of equal severity (0-100). Detectors set it from
	// the size of the thing at stake — volume at risk, contacts affected.
	Impact int

	Title string
	// GroupTitle is how this finding names itself when several of its kind are
	// shown together, with a {count} placeholder. Set it on any detector that
	// can plausibly fire on many entities at once: without it, one
	// misconfiguration repeated across a fleet becomes a wall of identical
	// cards that people learn to scroll past.
	GroupTitle string
	Detail     string
	Remedy     string
	// Steps is the ordered how-to for a finding the platform cannot fix itself.
	// Set it wherever there is no Action: "open the mailbox and work through the
	// errors" is a summary, not an instruction, and the person reading it is
	// usually the one who does not already know the answer.
	Steps []string

	// Evidence is the numbers the detector fired on. It is shown in the card,
	// hashed for narration caching, and is the ONLY input the narrator gets, so
	// it must be self-contained and already rounded into bands.
	Evidence map[string]any

	Action *models.AdvisorAction
}

// Fingerprint is the finding's stable identity across runs.
func (f Finding) Fingerprint() string {
	entity := "org"
	if f.EntityID != nil {
		entity = f.EntityType + ":" + f.EntityID.String()
	}
	return f.Key + "|" + entity
}

// toModel converts a detector finding into its persistable form.
func (f Finding) toModel(orgID uuid.UUID) *models.AdvisorFinding {
	evidence, err := json.Marshal(f.Evidence)
	if err != nil || len(f.Evidence) == 0 {
		evidence = []byte(`{}`)
	}
	return &models.AdvisorFinding{
		OrganizationID: orgID,
		Fingerprint:    f.Fingerprint(),
		DetectorKey:    f.Key,
		Category:       f.Category,
		Severity:       f.Severity,
		Surface:        f.Surface,
		EntityType:     f.EntityType,
		EntityID:       f.EntityID,
		EntityLabel:    f.EntityLabel,
		ParentType:     f.ParentType,
		ParentID:       f.ParentID,
		Impact:         f.Impact,
		Title:          f.Title,
		GroupTitle:     f.GroupTitle,
		Detail:         f.Detail,
		Remedy:         f.Remedy,
		Steps:          f.Steps,
		Evidence:       json.RawMessage(evidence),
		Action:         f.Action,
	}
}

// Detector is one named check.
type Detector struct {
	Key      string
	Category models.AdvisorCategory
	// About is the one-line statement of what this detector looks for and why
	// it matters. It grounds the narrator so the copy explains the real rule
	// rather than paraphrasing the numbers back at the user.
	About string
	Run   func(s *repository.AdvisorSnapshot) []Finding
}

// AllDetectors returns every detector in evaluation order. Order does not
// affect correctness (findings are sorted by severity for display) but keeping
// the categories grouped makes the run log readable.
func AllDetectors() []Detector {
	var all []Detector
	all = append(all, deliverabilityDetectors()...)
	all = append(all, mailboxDetectors()...)
	all = append(all, warmupDetectors()...)
	all = append(all, campaignDetectors()...)
	all = append(all, copyDetectors()...)
	all = append(all, listDetectors()...)
	return all
}

// DetectorAbout indexes the detector descriptions by key, for the narrator.
func DetectorAbout() map[string]string {
	out := map[string]string{}
	for _, d := range AllDetectors() {
		out[d.Key] = d.About
	}
	return out
}

// Detect runs every detector that the org's settings leave enabled and returns
// the findings sorted most-urgent-first.
func Detect(s *repository.AdvisorSnapshot, settings *models.AdvisorSettings) []Finding {
	muted := map[string]bool{}
	for _, k := range settings.MutedDetectors {
		muted[k] = true
	}
	mutedCat := map[string]bool{}
	for _, c := range settings.MutedCategories {
		mutedCat[c] = true
	}

	out := []Finding{}
	for _, d := range AllDetectors() {
		if muted[d.Key] || mutedCat[string(d.Category)] {
			continue
		}
		for _, f := range d.Run(s) {
			if !f.Severity.AtLeast(settings.MinSeverity) {
				continue
			}
			// Detectors declare their own category only when it differs from
			// their registration, which no detector currently does; defaulting
			// here keeps a new detector from silently landing in "".
			if f.Category == "" {
				f.Category = d.Category
			}
			if f.Key == "" {
				f.Key = d.Key
			}
			out = append(out, f)
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Severity.Rank() != out[j].Severity.Rank() {
			return out[i].Severity.Rank() > out[j].Severity.Rank()
		}
		return out[i].Impact > out[j].Impact
	})
	return out
}

// --- shared helpers -------------------------------------------------------

// rate returns n/total as a percentage, or 0 when there is nothing to divide by.
func rate(n, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(n) / float64(total) * 100
}

// band rounds a rate to a coarse bucket for the evidence hash, so ordinary
// drift (4.1% -> 4.3% bounce) does not invalidate cached narration while a real
// move (4% -> 9%) does.
func band(v float64) float64 {
	switch {
	case v >= 10:
		return float64(int(v/5) * 5)
	case v >= 1:
		return float64(int(v))
	default:
		return float64(int(v*20)) / 20
	}
}

// clampImpact keeps a detector's impact weight in range.
func clampImpact(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// pct formats a rate for the fallback copy.
func pct(v float64) string {
	if v < 1 {
		return fmt.Sprintf("%.2f%%", v)
	}
	return fmt.Sprintf("%.1f%%", v)
}

// ref returns a pointer to a UUID, for Finding.EntityID.
func ref(id uuid.UUID) *uuid.UUID { return &id }

// mailboxAction builds an update_mailbox one-click fix with its preview.
func mailboxAction(id uuid.UUID, label string, args map[string]any, preview ...models.AdvisorPreviewChange) *models.AdvisorAction {
	args["email_account_id"] = id.String()
	return toolAction("update_mailbox", label, args, preview...)
}

// campaignAction builds an update_campaign one-click fix with its preview.
func campaignAction(id uuid.UUID, label string, args map[string]any, preview ...models.AdvisorPreviewChange) *models.AdvisorAction {
	args["campaign_id"] = id.String()
	return toolAction("update_campaign", label, args, preview...)
}

func toolAction(tool, label string, args map[string]any, preview ...models.AdvisorPreviewChange) *models.AdvisorAction {
	raw, err := json.Marshal(args)
	if err != nil {
		return nil
	}
	return &models.AdvisorAction{
		Tool:    tool,
		Args:    json.RawMessage(raw),
		Label:   label,
		Preview: preview,
	}
}

// withUndo attaches the reverting tool call to an action, so an applied fix can
// be rolled back from the card without hunting for the original value.
func withUndo(a *models.AdvisorAction, args map[string]any) *models.AdvisorAction {
	if a == nil {
		return nil
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return a
	}
	a.Undo = &models.AdvisorUndo{Tool: a.Tool, Args: json.RawMessage(raw)}
	return a
}

// change is shorthand for one preview line.
func change(field, from, to string) models.AdvisorPreviewChange {
	return models.AdvisorPreviewChange{Field: field, From: from, To: to}
}
