// Package listgate projects a campaign's bounce rate before it sends anything.
// Per-recipient verification only drops a bad address at send time, so a
// scraped list otherwise reveals itself through the damage it does.
package listgate

import (
	"fmt"

	"github.com/warmbly/warmbly/internal/repository"
)

// Thresholds, in percent of the deliverable audience.
const (
	// BlockBouncePct sits below the 5% at which providers put a sender under
	// review, so the platform acts first.
	BlockBouncePct = 4.0
	// WarnBouncePct is where a launch is allowed but flagged.
	WarnBouncePct = 2.0
	// MinAudience is the size below which a share means nothing. Two bad
	// addresses out of five is not a 40% bounce rate.
	MinAudience = 50
	// UnverifiedAdvisePct is the share of never-checked addresses above which
	// the customer is advised to verify. Advice only: see below.
	UnverifiedAdvisePct = 50.0
)

// Verdict is a projection over one campaign's audience.
type Verdict struct {
	// Deliverable excludes suppressed and unsubscribed leads: they are skipped
	// at send time, so counting them would understate every share.
	Deliverable int
	// ProjectedBouncePct is the estimated hard-bounce rate at launch.
	ProjectedBouncePct float64
	// Block is true when the launch should be refused.
	Block bool
	// Warn is true when it should be flagged but allowed. Set for an
	// unverified list too, so advice about it is actually shown: a preflight
	// report only surfaces checks that did not pass.
	Warn bool
	// UnverifiedPct is the share of the audience nobody has checked. Reported,
	// never blocked on: see Project.
	UnverifiedPct float64
	// Summary is the sentence the customer reads.
	Summary string
	// Remediation is what to do about it.
	Remediation string
}

// Project estimates the audience's bounce rate. A list too small to judge, or
// with nothing deliverable, is never blocked.
func Project(a repository.CampaignAudience) Verdict {
	// Counted in SQL, not derived: a contact can be both suppressed and
	// unsubscribed, and subtracting both counts would remove it twice.
	deliverable := a.Deliverable
	if deliverable < 0 {
		deliverable = 0
	}
	v := Verdict{Deliverable: deliverable}

	if deliverable == 0 {
		v.Summary = "No deliverable recipients: every lead is suppressed or unsubscribed."
		v.Remediation = "Add recipients, or remove the suppressed ones from this campaign."
		return v
	}

	// The projection counts KNOWN-invalid addresses only.
	//
	// Assuming a fraction of unverified addresses will bounce is tempting and
	// wrong: most customers never run verification, so their lists are entirely
	// unverified, and any non-zero weight would refuse essentially every launch.
	// A list nobody has checked is not evidence of a bad list. It is reported
	// as unverified and the customer is advised to check it.
	v.ProjectedBouncePct = float64(a.Invalid) / float64(deliverable) * 100
	v.UnverifiedPct = float64(a.Unknown) / float64(deliverable) * 100

	if deliverable < MinAudience {
		v.Summary = fmt.Sprintf("Audience of %d is too small to judge; sending anyway.", deliverable)
		return v
	}

	switch {
	case v.ProjectedBouncePct >= BlockBouncePct:
		v.Block = true
		v.Summary = fmt.Sprintf(
			"About %.1f%% of this list is likely to hard bounce (%d known-invalid of %d deliverable).",
			v.ProjectedBouncePct, a.Invalid, deliverable)
		v.Remediation = "Verify or clean the list before launching. Mailbox providers put a sender under review at 5%."
	case v.ProjectedBouncePct >= WarnBouncePct:
		v.Warn = true
		v.Summary = fmt.Sprintf(
			"About %.1f%% of this list is likely to hard bounce (%d known-invalid of %d deliverable).",
			v.ProjectedBouncePct, a.Invalid, deliverable)
		v.Remediation = "Consider verifying the list before sending at volume."
	case v.UnverifiedPct >= UnverifiedAdvisePct:
		v.Warn = true
		v.Summary = fmt.Sprintf("%.0f%% of this list has never been verified, so its bounce rate is unknown.", v.UnverifiedPct)
		v.Remediation = "Verify the list to see its real bounce risk before sending at volume."
	default:
		v.Summary = fmt.Sprintf("Projected hard bounce is about %.1f%%.", v.ProjectedBouncePct)
	}
	return v
}
