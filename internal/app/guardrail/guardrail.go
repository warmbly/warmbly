// Package guardrail auto-pauses a campaign whose engagement rates leave the
// band its owner set.
//
// The point is to act before a mailbox provider does. Google asks senders to
// stay under a 0.10% complaint rate and never reach 0.30%; Amazon SES puts an
// account under review at a 5% bounce rate and can pause sending at 10%. By the
// time those thresholds are hit the damage is already done to the sending
// domain, so the defaults here sit inside them and the pause is immediate
// rather than advisory.
//
// Evaluation is pure Go over counts the repository already fetched: same
// numbers in, same decision out, no I/O, no model calls. Sample floors are
// mandatory — one bounce out of four sends is not a 25% bounce rate, and a
// guardrail that fires on noise is a guardrail people switch off.
package guardrail

import (
	"fmt"

	"github.com/warmbly/warmbly/internal/repository"
)

// Rule names the check that fired. Stable strings: they are stored on the
// campaign, shown in the dashboard, and carried in the audit trail.
type Rule string

const (
	RuleComplaintRate Rule = "complaint_rate"
	RuleBounceRate    Rule = "bounce_rate"
	RuleReplyRate     Rule = "reply_rate"
)

// Breach is one rule's verdict on one campaign.
type Breach struct {
	Rule      Rule
	Observed  float64 // percent
	Threshold float64 // percent
	Sample    int     // sends the rate was computed over
	// Reason is the sentence stored on the campaign and shown to the user. It
	// names the rule, the number, and the threshold, because "paused
	// automatically" on its own tells nobody what to fix.
	Reason string
}

// rate is a percentage, guarding the zero-sample case.
func rate(part, whole int) float64 {
	if whole <= 0 {
		return 0
	}
	return float64(part) / float64(whole) * 100
}

// Evaluate returns the breach that should pause this campaign, or nil.
//
// Rules are checked worst-first, so a campaign breaching several bands is
// paused with the most damaging reason attached: complaints hurt the sending
// domain most, bounces say the list was never verified, and a dead reply rate
// is a business problem rather than a deliverability one.
//
// A threshold of 0 disables its rule. That is why the reply-rate floor is off
// by default: pausing for weak engagement is a deliberate choice, not something
// to inflict on a campaign whose owner never asked for it.
func Evaluate(c repository.GuardrailCampaign) *Breach {
	if c.Sent < c.MinSample || c.Sent <= 0 {
		return nil
	}

	if c.ComplaintRateMax > 0 {
		if r := rate(c.Complaints, c.Sent); r >= c.ComplaintRateMax {
			return &Breach{
				Rule: RuleComplaintRate, Observed: r, Threshold: c.ComplaintRateMax, Sample: c.Sent,
				Reason: fmt.Sprintf(
					"Paused automatically: spam complaints reached %s across %d sends, at or above the %s limit set for this campaign.",
					pct(r), c.Sent, pct(c.ComplaintRateMax)),
			}
		}
	}

	if c.BounceRateMax > 0 {
		if r := rate(c.Bounced, c.Sent); r >= c.BounceRateMax {
			return &Breach{
				Rule: RuleBounceRate, Observed: r, Threshold: c.BounceRateMax, Sample: c.Sent,
				Reason: fmt.Sprintf(
					"Paused automatically: the bounce rate reached %s across %d sends, at or above the %s limit set for this campaign.",
					pct(r), c.Sent, pct(c.BounceRateMax)),
			}
		}
	}

	if c.ReplyRateMin > 0 {
		if r := rate(c.Replied, c.Sent); r < c.ReplyRateMin {
			return &Breach{
				Rule: RuleReplyRate, Observed: r, Threshold: c.ReplyRateMin, Sample: c.Sent,
				Reason: fmt.Sprintf(
					"Paused automatically: the reply rate is %s across %d sends, below the %s floor set for this campaign.",
					pct(r), c.Sent, pct(c.ReplyRateMin)),
			}
		}
	}

	return nil
}

// pct renders a rate with enough precision to be actionable at the low end,
// where the complaint bands live, without printing noise at the high end.
func pct(v float64) string {
	if v > 0 && v < 1 {
		return fmt.Sprintf("%.2f%%", v)
	}
	return fmt.Sprintf("%.1f%%", v)
}
