package listgate

import (
	"testing"

	"github.com/warmbly/warmbly/internal/repository"
)

// The case that decides whether this gate is usable at all. Most customers
// never run verification, so their lists are entirely unverified. Assuming any
// fraction of those will bounce would refuse essentially every launch.
func TestProjectNeverBlocksAnUnverifiedList(t *testing.T) {
	v := Project(repository.CampaignAudience{Total: 1000, Unknown: 1000})
	if v.Block {
		t.Fatalf("a never-verified list was blocked at %.1f%%; that is every ordinary customer", v.ProjectedBouncePct)
	}
	if v.Warn {
		t.Errorf("a never-verified list should be advised, not warned as a bounce risk: %+v", v)
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
	v := Project(repository.CampaignAudience{Total: 1000, Unknown: 0})
	if v.Block || v.Warn || v.Remediation != "" {
		t.Errorf("a clean list should be quiet: %+v", v)
	}
}

func TestProjectBlocksAKnownBadList(t *testing.T) {
	// 200 known-invalid out of 1000 is 20%: unambiguously a scraped list.
	v := Project(repository.CampaignAudience{Total: 1000, Invalid: 200, Unknown: 0})
	if !v.Block {
		t.Errorf("a 20%% invalid list was not blocked: %+v", v)
	}
	if v.Remediation == "" {
		t.Error("a blocked launch must say what to do about it")
	}
}

func TestProjectWarnsInTheMiddleBand(t *testing.T) {
	v := Project(repository.CampaignAudience{Total: 1000, Invalid: 25})
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
		v := Project(repository.CampaignAudience{Total: n, Invalid: n})
		if v.Block {
			t.Errorf("an audience of %d was blocked at 100%% invalid; too small to judge", n)
		}
	}
	// At the floor it starts judging.
	v := Project(repository.CampaignAudience{Total: MinAudience, Invalid: MinAudience})
	if !v.Block {
		t.Errorf("an audience of %d at 100%% invalid should block", MinAudience)
	}
}

// Suppressed and unsubscribed leads are never sent to, so counting them in the
// denominator would understate every share and let a bad list through.
func TestProjectExcludesLeadsThatWillNeverBeSentTo(t *testing.T) {
	a := repository.CampaignAudience{Total: 1000, Invalid: 40, Suppressed: 500, Unsubscribed: 400}
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
	v := Project(repository.CampaignAudience{Total: 100, Suppressed: 100})
	if v.Block {
		t.Error("a fully-suppressed campaign should be explained, not blocked as a bounce risk")
	}
	if v.Summary == "" || v.Remediation == "" {
		t.Errorf("an undeliverable audience must be explained: %+v", v)
	}
}
