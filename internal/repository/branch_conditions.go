package repository

import (
	"hash/fnv"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Branching condition vocabulary. Kept in one place so the shape validator
// (write path) and the resolver (schedule path) agree exactly.
var branchConditionFields = map[string]bool{
	"opened":      true,
	"clicked":     true,
	"replied":     true,
	"not_opened":  true,
	"not_clicked": true,
	"not_replied": true,
	// "random" routes a deterministic percentage of contacts down this branch
	// (a random split / split-test). Pairs with operator "chance", Value = %.
	"random": true,
}

var branchConditionOperators = map[string]bool{
	"within_days": true, // signal occurred (or not, for not_*) in the last Value days
	"ever":        true, // signal occurred (or not) at all; Value ignored
	// "always" is the frontend editor's label for "ever (any time)" — accepted as
	// an exact alias of "ever" so the shipped UI contract stays valid.
	"always": true,
	// "chance" pairs with field "random": Value is the percent (1-99) of
	// contacts that take the branch, chosen deterministically per contact.
	"chance": true,
}

// maxBranchesPerStep / maxConditionsPerBranch bound the tree so a single step
// cannot carry an unreasonable amount of branching logic.
const (
	maxBranchesPerStep     = 20
	maxConditionsPerBranch = 20
	maxBranchWithinDays    = 365
)

// validateBranchConditions checks the per-step shape of a branching tree:
// known fields/operators, sane within_days windows, and bounded fan-out. It
// does NOT check cross-step concerns (target exists / same campaign / no
// cycles) — those need the full sequence set and live in the service layer.
func validateBranchConditions(bc *models.BranchConditions) *errx.Error {
	if bc == nil {
		return nil
	}
	if len(bc.Branches) > maxBranchesPerStep {
		return errx.ErrSequenceBranch
	}
	for _, b := range bc.Branches {
		if len(b.Conditions) > maxConditionsPerBranch {
			return errx.ErrSequenceBranch
		}
		for _, cond := range b.Conditions {
			if !branchConditionFields[cond.Field] {
				return errx.ErrSequenceBranch
			}
			if !branchConditionOperators[cond.Operator] {
				return errx.ErrSequenceBranch
			}
			if cond.Operator == "within_days" {
				if cond.Value == nil || *cond.Value < 1 || *cond.Value > maxBranchWithinDays {
					return errx.ErrSequenceBranch
				}
			}
			if cond.Field == "random" {
				if cond.Operator != "chance" || cond.Value == nil || *cond.Value < 1 || *cond.Value > 99 {
					return errx.ErrSequenceBranch
				}
			}
		}
	}
	return nil
}

// evaluateBranchConditions runs a step's branching tree against a contact's
// engagement timestamps and returns the matched branch (in declared order, AND
// semantics within a branch). Returns (nil, false) when no branch matches so
// the caller can fall back to linear progression.
//
// `now` is passed in so evaluation is deterministic for a single resolve call.
func evaluateBranchConditions(bc *models.BranchConditions, prog *CampaignContactProgress, now time.Time) (*models.Branch, bool) {
	if bc == nil || len(bc.Branches) == 0 {
		return nil, false
	}
	for i := range bc.Branches {
		b := &bc.Branches[i]
		if branchMatches(b, prog, now) {
			return b, true
		}
	}
	return nil, false
}

// branchMatches reports whether EVERY condition in the branch holds. An empty
// condition list is an unconditional catch-all (always matches).
func branchMatches(b *models.Branch, prog *CampaignContactProgress, now time.Time) bool {
	for _, cond := range b.Conditions {
		if cond.Field == "random" {
			if !randomHolds(cond, prog.ContactID, b.BranchID) {
				return false
			}
			continue
		}
		if !conditionHolds(cond, prog, now) {
			return false
		}
	}
	return true
}

// randomHolds deterministically routes Value% of contacts down a random-split
// branch. Stable per (contact, branch): the same contact always takes the same
// path for this branch, so re-evaluation at each schedule pass is consistent.
func randomHolds(cond models.BranchCondition, contactID uuid.UUID, branchID string) bool {
	pct := 0
	if cond.Value != nil {
		pct = *cond.Value
	}
	if pct <= 0 {
		return false
	}
	if pct >= 100 {
		return true
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(contactID.String() + ":" + branchID))
	return int(h.Sum32()%100) < pct
}

// conditionHolds evaluates a single engagement predicate. The not_* fields
// invert the positive signal of the same name.
func conditionHolds(cond models.BranchCondition, prog *CampaignContactProgress, now time.Time) bool {
	var ts *time.Time
	negate := false

	switch cond.Field {
	case "opened":
		ts = prog.OpenedAt
	case "clicked":
		ts = prog.ClickedAt
	case "replied":
		ts = prog.RepliedAt
	case "not_opened":
		ts, negate = prog.OpenedAt, true
	case "not_clicked":
		ts, negate = prog.ClickedAt, true
	case "not_replied":
		ts, negate = prog.RepliedAt, true
	default:
		// Unknown field never matches (validation should have caught it).
		return false
	}

	var positive bool
	switch cond.Operator {
	case "ever", "always":
		positive = ts != nil
	case "within_days":
		if ts == nil || cond.Value == nil {
			positive = false
		} else {
			cutoff := now.Add(-time.Hour * 24 * time.Duration(*cond.Value))
			positive = ts.After(cutoff)
		}
	default:
		return false
	}

	if negate {
		return !positive
	}
	return positive
}
