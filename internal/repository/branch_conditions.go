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

// BranchState is the three-valued result of evaluating a branch (or a single
// condition) at a point in time. Crucially, an engagement window that has not
// elapsed yet is UNDECIDED — neither matched nor not — so the scheduler waits
// and re-checks instead of guessing. This is what makes "if didn't open within
// N days" actually wait N days before firing.
type BranchState int

const (
	BranchNoMatch   BranchState = iota // definitively does not apply
	BranchMatch                        // definitively applies now
	BranchUndecided                    // not knowable yet; re-check at the returned time
)

// evaluateBranchState evaluates one branch send-relative to when the current
// step was sent. AND semantics: any NoMatch condition fails the branch; any
// Undecided condition leaves the branch Undecided (re-check at the latest
// pending window). An empty condition list is the catch-all (always matches).
func evaluateBranchState(b *models.Branch, prog *CampaignContactProgress, sentAt, now time.Time) (BranchState, time.Time) {
	if len(b.Conditions) == 0 {
		return BranchMatch, time.Time{}
	}
	state := BranchMatch
	var recheck time.Time
	for i := range b.Conditions {
		cs, wend := conditionState(b.Conditions[i], prog, b.BranchID, sentAt, now)
		if cs == BranchNoMatch {
			return BranchNoMatch, time.Time{}
		}
		if cs == BranchUndecided {
			state = BranchUndecided
			if wend.After(recheck) {
				recheck = wend
			}
		}
	}
	return state, recheck
}

// conditionState evaluates a single predicate send-relative. For "within_days"
// the window is [sentAt, sentAt+N days]:
//   - positive (opened/clicked/replied): Match if it happened in the window;
//     Undecided while the window is still open; NoMatch once it closes unmet.
//   - negative (not_*): NoMatch if it happened in the window; Undecided while the
//     window is still open; Match once it closes without the signal.
//
// "ever"/"always" (legacy, no window) decides immediately. random is instant.
func conditionState(cond models.BranchCondition, prog *CampaignContactProgress, branchID string, sentAt, now time.Time) (BranchState, time.Time) {
	if cond.Field == "random" {
		if randomHolds(cond, prog.ContactID, branchID) {
			return BranchMatch, time.Time{}
		}
		return BranchNoMatch, time.Time{}
	}

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
		return BranchNoMatch, time.Time{}
	}

	if cond.Operator == "within_days" {
		days := 0
		if cond.Value != nil {
			days = *cond.Value
		}
		windowEnd := sentAt.Add(time.Hour * 24 * time.Duration(days))
		happened := ts != nil && !ts.After(windowEnd)
		if negate {
			if happened {
				return BranchNoMatch, time.Time{}
			}
			if now.Before(windowEnd) {
				return BranchUndecided, windowEnd
			}
			return BranchMatch, time.Time{}
		}
		if happened {
			return BranchMatch, time.Time{}
		}
		if now.Before(windowEnd) {
			return BranchUndecided, windowEnd
		}
		return BranchNoMatch, time.Time{}
	}

	// "ever" / "always" / unknown operator: decide immediately, no window.
	happened := ts != nil
	if negate {
		happened = !happened
	}
	if happened {
		return BranchMatch, time.Time{}
	}
	return BranchNoMatch, time.Time{}
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
