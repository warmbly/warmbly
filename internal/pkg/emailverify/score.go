package emailverify

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/pkg/dsn"
)

// Evidence kinds. Positive kinds prove the mailbox is live; the recipient
// bounce proves it is not. Nothing here describes interest: a contact who
// never opens or replies has told us nothing about their address.
const (
	EvidenceDelivered        = "delivered"
	EvidenceOpened           = "opened"
	EvidenceClicked          = "clicked"
	EvidenceReplied          = "replied"
	EvidenceAutoReplied      = "auto_replied"
	EvidenceBouncedRecipient = "bounced_recipient"
	EvidenceBouncedOther     = "bounced_other"
)

// Evidence is one observed fact about a mailbox.
type Evidence struct {
	Kind       string
	Detail     string
	ObservedAt time.Time
}

// Verdict is the last probe, provider, import or manual verdict on the
// contact row.
type Verdict struct {
	Status Status
	// Source is the contacts.verification_source value.
	Source string
	// Provider is the contacts.verification_provider value, so a reason can
	// name the verifier.
	Provider  string
	CheckedAt time.Time
}

// Scored is the derived state of an address.
type Scored struct {
	Status Status
	// Confidence is how sure the platform is of Status, 0 to 100.
	Confidence int
	// Reasons are the sentences a member reads, strongest first.
	Reasons []string
	// Decisive reports whether real mail (not a check) decided the status.
	Decisive bool
	// LastPositiveAt is the most recent positive observation.
	LastPositiveAt time.Time
}

// Evidence weights. Weights decay with a half-life so last week's reply
// outranks last year's, but nothing ever reaches zero on its own.
var evidenceWeights = map[string]struct {
	weight   float64
	halfLife time.Duration
	positive bool
	// cap bounds how many observations of this kind count.
	cap int
}{
	EvidenceReplied:          {45, 365 * 24 * time.Hour, true, 3},
	EvidenceAutoReplied:      {30, 180 * 24 * time.Hour, true, 2},
	EvidenceClicked:          {35, 270 * 24 * time.Hour, true, 3},
	EvidenceOpened:           {25, 180 * 24 * time.Hour, true, 3},
	EvidenceDelivered:        {14, 180 * 24 * time.Hour, true, 4},
	EvidenceBouncedRecipient: {70, 365 * 24 * time.Hour, false, 2},
}

// verdictBase is how much a check is trusted on its own, by source.
func verdictBase(v Verdict) (int, string) {
	switch v.Source {
	case "manual":
		return 95, "marked by a teammate"
	case "provider":
		name := ProviderLabel(v.Provider)
		if name == "" {
			name = "the verification service"
		}
		switch v.Status {
		case StatusValid:
			return 88, "verified deliverable with " + name
		case StatusInvalid:
			return 88, name + " reported the address undeliverable"
		case StatusRisky:
			return 55, "flagged risky by " + name
		}
		return 30, name + " could not decide"
	case "imported":
		switch v.Status {
		case StatusValid, StatusInvalid:
			return 80, "verified before it was imported"
		case StatusRisky:
			return 50, "flagged before it was imported"
		}
		return 25, "imported without a decisive result"
	case "probe":
		switch v.Status {
		case StatusValid:
			return 75, "the mail server accepted the address"
		case StatusInvalid:
			return 80, "the mail server rejected the address"
		case StatusRisky:
			return 45, "the domain accepts any address, so the check proves nothing"
		}
		return 20, "the check was inconclusive"
	}
	return 0, "never checked"
}

// PositiveDecisiveScore is the evidence score above which real mail decides
// the address is deliverable regardless of what a check said.
const PositiveDecisiveScore = 20.0

// ProviderOutranksEvidenceAfter is how much newer than all real mail a paid verifier's verdict must be to stand over it.
const ProviderOutranksEvidenceAfter = 30 * 24 * time.Hour

// Score derives an address's status and confidence from its last verdict and
// the evidence ledger. Rules, in order:
//
//  1. A manual verdict wins outright.
//  2. A paid verifier's valid or invalid verdict newer than every observation
//     by ProviderOutranksEvidenceAfter stands.
//  3. A bounce naming the recipient, newer than every positive observation,
//     makes the address undeliverable.
//  4. Enough positive evidence (a reply, a click, a human open, or repeated
//     clean deliveries) makes it deliverable, whatever a probe said.
//  5. Otherwise the verdict stands, with the evidence nudging confidence.
//
// Absence of engagement never appears here: nothing in the ledger says "did
// not open", and Score has no input for it.
func Score(v Verdict, evidence []Evidence, now time.Time) Scored {
	base, baseReason := verdictBase(v)
	out := Scored{Status: v.Status, Confidence: base}
	if out.Status == "" {
		out.Status = StatusUnknown
	}

	sort.Slice(evidence, func(i, j int) bool { return evidence[i].ObservedAt.After(evidence[j].ObservedAt) })
	counts := map[string]int{}
	var positive, negative float64
	var lastPositive, lastNegative time.Time
	var reasons []string
	for _, e := range evidence {
		w, ok := evidenceWeights[e.Kind]
		if !ok {
			continue
		}
		if counts[e.Kind] >= w.cap {
			continue
		}
		counts[e.Kind]++
		age := now.Sub(e.ObservedAt)
		if age < 0 {
			age = 0
		}
		value := w.weight * math.Pow(0.5, age.Hours()/w.halfLife.Hours())
		if w.positive {
			positive += value
			if e.ObservedAt.After(lastPositive) {
				lastPositive = e.ObservedAt
			}
		} else {
			negative += value
			if e.ObservedAt.After(lastNegative) {
				lastNegative = e.ObservedAt
			}
		}
		if counts[e.Kind] == 1 {
			reasons = append(reasons, evidenceReason(e, counts, evidence, now))
		}
	}
	out.LastPositiveAt = lastPositive

	lastSeen := lastPositive
	if lastNegative.After(lastSeen) {
		lastSeen = lastNegative
	}
	switch {
	case v.Source == "manual":
		out.Reasons = append([]string{baseReason}, reasons...)
		out.Confidence = clamp(base + int(positive/4))
		return out
	case v.Source == "provider" && (v.Status == StatusValid || v.Status == StatusInvalid) &&
		!lastSeen.IsZero() && v.CheckedAt.Sub(lastSeen) > ProviderOutranksEvidenceAfter:
		out.Reasons = append([]string{baseReason, "real mail to it is older than this check (last " + humanAge(now.Sub(lastSeen)) + ")"}, reasons...)
		return out
	case !lastNegative.IsZero() && lastNegative.After(lastPositive):
		out.Status = StatusInvalid
		out.Decisive = true
		out.Confidence = clamp(60 + int(negative/2))
		out.Reasons = prepend(reasons, EvidenceBouncedRecipient)
		return out
	case positive >= PositiveDecisiveScore:
		out.Status = StatusValid
		out.Decisive = true
		out.Confidence = clamp(70 + int(positive/2))
		out.Reasons = reasons
		if v.Status == StatusRisky || v.Status == StatusInvalid {
			out.Reasons = append(out.Reasons, "real mail outranks the earlier check ("+baseReason+")")
		}
		return out
	}
	// The verdict stands; a little evidence either way moves confidence.
	switch out.Status {
	case StatusValid:
		out.Confidence = clamp(base + int(positive/2) - int(negative))
	case StatusInvalid:
		out.Confidence = clamp(base + int(negative/2) - int(positive))
	default:
		out.Confidence = clamp(base + int(positive/2))
	}
	out.Reasons = append([]string{baseReason}, reasons...)
	return out
}

func prepend(reasons []string, kind string) []string {
	out := make([]string, 0, len(reasons))
	for _, r := range reasons {
		if strings.HasPrefix(r, "bounced") {
			out = append([]string{r}, out...)
			continue
		}
		out = append(out, r)
	}
	return out
}

func evidenceReason(e Evidence, counts map[string]int, all []Evidence, now time.Time) string {
	n := 0
	for _, x := range all {
		if x.Kind == e.Kind {
			n++
		}
	}
	when := humanAge(now.Sub(e.ObservedAt))
	switch e.Kind {
	case EvidenceReplied:
		return "replied " + when
	case EvidenceAutoReplied:
		return "sent an automatic reply " + when + " (the mailbox is live)"
	case EvidenceClicked:
		return "clicked a link " + when
	case EvidenceOpened:
		return "opened an email " + when
	case EvidenceDelivered:
		if n > 1 {
			return fmt.Sprintf("delivered %d times without a bounce, last %s", n, when)
		}
		return "delivered without a bounce " + when
	case EvidenceBouncedRecipient:
		d := strings.TrimSpace(e.Detail)
		if d != "" {
			return "bounced " + when + ": " + d
		}
		return "bounced " + when + ": the server said the mailbox does not exist"
	}
	return e.Kind + " " + when
}

func humanAge(d time.Duration) string {
	days := int(d.Hours() / 24)
	switch {
	case days <= 0:
		return "today"
	case days == 1:
		return "yesterday"
	case days < 30:
		return fmt.Sprintf("%d days ago", days)
	case days < 365:
		return fmt.Sprintf("%d months ago", days/30)
	}
	return fmt.Sprintf("%d years ago", days/365)
}

func clamp(n int) int {
	if n < 0 {
		return 0
	}
	if n > 100 {
		return 100
	}
	return n
}

// NamesRecipient reports whether a bounce or rejection text says the
// RECIPIENT does not exist, as opposed to a full mailbox, a policy block, a
// reputation rejection or a greeting the server disliked. Only the former is
// evidence against the address.
func NamesRecipient(text string) bool {
	return dsn.Blame(text) == dsn.CauseRecipientInvalid
}
