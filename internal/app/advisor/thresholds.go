package advisor

import "regexp"

// Thresholds. Every value here traces to the sending-safety and paid-pool
// policy in CLAUDE.md, or to the provider guidance that policy cites. They are
// deliberately stricter than the point where mailbox providers start
// penalising a sender: advice that arrives at the same moment Google throttles
// you is advice that arrived too late.
const (
	// --- sample floors -----------------------------------------------------
	// Below these, a rate is noise and the detector stays silent. A single
	// bounce out of three sends is not a 33% bounce rate worth alarming about.

	// minSendsForRate is the floor for bounce/complaint rates on a mailbox or
	// campaign. CLAUDE.md's complaint floor is 100 delivered in 30 days; we use
	// a lower bar for bounces because hard bounces are unambiguous and a bad
	// list shows itself early.
	minSendsForComplaintRate = 100
	minSendsForBounceRate    = 50
	// minWarmupDeliveriesForPlacement mirrors the documented spam-placement
	// floor: at least 20 warmup deliveries in the last 7 days.
	minWarmupDeliveriesForPlacement = 20

	// --- deliverability bands ---------------------------------------------
	// Google: keep user-reported spam below 0.10%, never reach 0.30%.
	// SES: complaint 0.1% puts an account under review, 0.5% can pause it.
	complaintRateWarn     = 0.03
	complaintRateCritical = 0.10

	// SES: bounce below 5%; 5% is review, 10% can pause sending.
	bounceRateWarn     = 3.0
	bounceRateCritical = 5.0

	// Spam-folder placement at Google, Microsoft and Yahoo. The pool only
	// slows a mailbox at these; the advice is what the member may choose.
	spamPlacementWarn     = 10.0
	spamPlacementSlowdown = 20.0
	spamPlacementBlock    = 50.0

	// --- mailbox volume ----------------------------------------------------
	// The repo defaults: 50/day cold cap, 600s minimum gap.
	defaultColdCap  = 50
	defaultMinGap   = 600
	safeColdCapBand = 50
	// newMailboxDays is how long a mailbox counts as new. Postmark puts domain
	// warmup at 3-6 weeks; 30 days is the conservative middle.
	newMailboxDays = 30
	// newMailboxSafeCap is the recommended ceiling for a mailbox in that
	// window: the documented "start cold outreach around 10-20/day".
	newMailboxSafeCap = 20
	// minSafeGapSeconds is the floor below which sends read as bursty from one
	// mailbox regardless of the daily total.
	minSafeGapSeconds = 300

	// --- warmup ------------------------------------------------------------
	// Repo defaults: base 10/day, ceiling 40/day, +1/day ramp, 30% reply rate.
	defaultWarmupBase = 10
	defaultWarmupMax  = 40

	// --- list hygiene ------------------------------------------------------
	roleAddressShareWarn = 15.0
	missingNameShareWarn = 10.0

	// --- copy --------------------------------------------------------------
	maxLinksInBody = 2
)

// spamTriggerPhrases are the phrases that most reliably move cold B2B mail into
// spam filters and, more importantly, read as bulk mail to a human. The list is
// deliberately short: a 200-word spam-word list produces noise findings on
// perfectly good copy, which trains people to ignore the Advisor.
var spamTriggerPhrases = []string{
	"act now",
	"buy now",
	"click here",
	"congratulations",
	"credit card",
	"dear friend",
	"double your",
	"earn extra cash",
	"free trial",
	"guarantee",
	"limited time",
	"make money",
	"no obligation",
	"no strings attached",
	"once in a lifetime",
	"risk free",
	"special promotion",
	"this is not spam",
	"urgent",
	"winner",
	"100% free",
	"$$$",
}

type spamPattern struct {
	phrase string
	re     *regexp.Regexp
}

// spamTriggerPatterns match each phrase at the start of a word, so "act now"
// is not found inside "contact now" while "guarantee" still finds "guaranteed".
var spamTriggerPatterns = func() []spamPattern {
	out := make([]spamPattern, 0, len(spamTriggerPhrases))
	for _, p := range spamTriggerPhrases {
		out = append(out, spamPattern{p, regexp.MustCompile(`(?i)(?:^|[^\pL\pN])` + regexp.QuoteMeta(p))})
	}
	return out
}()
