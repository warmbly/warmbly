package listgate

import (
	"testing"

	"github.com/warmbly/warmbly/internal/repository"
)

// The case that decides whether this gate is usable at all. Most customers
// never run verification, so their lists are entirely unverified. Assuming any
// fraction of those will bounce would refuse essentially every launch.
func TestProjectNeverBlocksAnUnverifiedList(t *testing.T) {
	v := Project(repository.CampaignAudience{Total: 1000, Deliverable: 1000, Unknown: 1000})
	if v.Block {
		t.Fatalf("a never-verified list was blocked at %.1f%%; that is every ordinary customer", v.ProjectedBouncePct)
	}
	// Warn is set so the advice is actually shown: a preflight report only
	// surfaces checks that did not pass.
	if !v.Warn {
		t.Errorf("a never-verified list must warn, or its advice is never displayed: %+v", v)
	}
	if v.ProjectedBouncePct != 0 {
		t.Errorf("projected = %.1f%%, want 0: unverified is not evidence of bad", v.ProjectedBouncePct)
	}
	if v.UnverifiedPct != 100 {
		t.Errorf("unverified = %.1f%%, want 100", v.UnverifiedPct)
	}
	if v.Remediation == "" {
		t.Error("an unverified list should still suggest verifying it")
	}
}

// A verified-clean list is silent.
func TestProjectCleanVerifiedListIsQuiet(t *testing.T) {
	v := Project(repository.CampaignAudience{Total: 1000, Deliverable: 1000, Unknown: 0})
	if v.Block || v.Warn || v.Remediation != "" {
		t.Errorf("a clean list should be quiet: %+v", v)
	}
}

func TestProjectBlocksAKnownBadList(t *testing.T) {
	// 200 known-invalid out of 1000 is 20%: unambiguously a scraped list.
	v := Project(repository.CampaignAudience{Total: 1000, Deliverable: 1000, Invalid: 200, Unknown: 0})
	if !v.Block {
		t.Errorf("a 20%% invalid list was not blocked: %+v", v)
	}
	if v.Remediation == "" {
		t.Error("a blocked launch must say what to do about it")
	}
}

func TestProjectWarnsInTheMiddleBand(t *testing.T) {
	v := Project(repository.CampaignAudience{Total: 1000, Deliverable: 1000, Invalid: 25})
	if v.Block {
		t.Errorf("2.5%% should warn, not block: %+v", v)
	}
	if !v.Warn {
		t.Errorf("2.5%% should warn: %+v", v)
	}
}

// The failure that would hurt most: refusing a launch on a sample too small to
// mean anything.
func TestProjectNeverBlocksATinyAudience(t *testing.T) {
	for _, n := range []int{1, 5, 20, MinAudience - 1} {
		v := Project(repository.CampaignAudience{Total: n, Deliverable: n, Invalid: n})
		if v.Block {
			t.Errorf("an audience of %d was blocked at 100%% invalid; too small to judge", n)
		}
	}
	// At the floor it starts judging.
	v := Project(repository.CampaignAudience{Total: MinAudience, Deliverable: MinAudience, Invalid: MinAudience})
	if !v.Block {
		t.Errorf("an audience of %d at 100%% invalid should block", MinAudience)
	}
}

// Suppressed and unsubscribed leads are never sent to, so counting them in the
// denominator would understate every share and let a bad list through.
func TestProjectExcludesLeadsThatWillNeverBeSentTo(t *testing.T) {
	// 500 suppressed and 400 unsubscribed OVERLAP; SQL counts the 100 that are
	// neither. Subtracting both counts would have said 100 too, by luck, but
	// says -300 clamped to 0 when they overlap fully.
	a := repository.CampaignAudience{Total: 1000, Deliverable: 100, Invalid: 40, Suppressed: 500, Unsubscribed: 400}
	v := Project(a)
	if v.Deliverable != 100 {
		t.Fatalf("deliverable = %d, want 100", v.Deliverable)
	}
	// 40 invalid of 100 deliverable is 40%, not 4% of the raw 1000.
	if v.ProjectedBouncePct < 39 || v.ProjectedBouncePct > 41 {
		t.Errorf("projected = %.1f%%, want about 40%%", v.ProjectedBouncePct)
	}
	if !v.Block {
		t.Error("a list that is 40% invalid among its deliverable leads must block")
	}
}

func TestProjectWithNothingDeliverable(t *testing.T) {
	v := Project(repository.CampaignAudience{Total: 100, Deliverable: 0, Suppressed: 100})
	if v.Block {
		t.Error("a fully-suppressed campaign should be explained, not blocked as a bounce risk")
	}
	if v.Summary == "" || v.Remediation == "" {
		t.Errorf("an undeliverable audience must be explained: %+v", v)
	}
}

// The bug this guards: Suppressed and Unsubscribed overlap, so deriving
// deliverable as Total - Suppressed - Unsubscribed removes those contacts
// twice and inflates every share computed against the remainder.
func TestProjectUsesTheCountedDeliverableNotASubtraction(t *testing.T) {
	// 600 leads are BOTH suppressed and unsubscribed; 400 are sendable.
	a := repository.CampaignAudience{
		Total: 1000, Deliverable: 400, Suppressed: 600, Unsubscribed: 600, Invalid: 12,
	}
	v := Project(a)
	if v.Deliverable != 400 {
		t.Fatalf("deliverable = %d, want the counted 400; a subtraction would give -200", v.Deliverable)
	}
	// 12 of 400 is 3%: a warning. Against a double-subtracted 0 it would have
	// been a divide-by-zero or an infinite rate.
	if !v.Warn || v.Block {
		t.Errorf("12 invalid of 400 should warn, not block: %+v", v)
	}
	if v.ProjectedBouncePct < 2.9 || v.ProjectedBouncePct > 3.1 {
		t.Errorf("projected = %.2f%%, want about 3%%", v.ProjectedBouncePct)
	}
}
