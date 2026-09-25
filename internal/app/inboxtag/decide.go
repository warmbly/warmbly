package inboxtag

import (
	"math"
	"sort"
)

// Facts are what the system already knows, established in code before anything
// is asked. They are passed to decide() so a model answer can never overrule
// them.
//
// This is rule 3 made concrete. Given only a body, Jev classified our own
// outbound as a human reply at 0.94 confidence. Nothing about the answer looked
// wrong; the question was wrong. Direction is a fact, so it is a field here and
// not a question in policy.go.
type Facts struct {
	// Outbound is true when this message came from one of our own mailboxes.
	// A message with Outbound set must never reach the model at all; decide()
	// rejects it as a backstop for a caller that forgot.
	Outbound bool

	// DeterministicKind is the verdict of the offline header and lexicon
	// layers (internal/app/replyclassify), when they reached one. Empty means
	// they were inconclusive.
	//
	// These deterministic signals are authoritative in a way the body is not.
	DeterministicKind string
}

// Decision is the whole output: what code concluded, and enough of the raw
// answer to explain why without asking again.
type Decision struct {
	Kind           string
	KindConfidence float64
	// KindSource is "header" when the deterministic layer decided, "model"
	// when the answer came from Jev, and "" when nothing decided.
	KindSource string

	Intent           string
	IntentConfidence float64

	// Labels are the workspace labels to apply, already de-duplicated and
	// ordered. In this phase they are the only output that reaches the world.
	Labels []string

	Relevance int
	Priority  string

	// NeedsReview is set when a Choice came back below ConfFloor. ReviewReason
	// identifies the untrusted part of an otherwise usable decision.
	NeedsReview  bool
	ReviewReason string

	// Signals are the nouls that fired, above Yes.
	Signals []string
	// SignalStrength is every signal's raw noul, fired or not, so an action
	// that needs Strong rather than Yes can read the number rather than the
	// list.
	SignalStrength map[string]float64

	// Scores are the normalised 0..1 positions of each score question, which
	// is what the weights multiply. The raw score and every probability are
	// persisted separately; these are what the arithmetic used.
	Scores map[string]float64
}

// Automated reports a trusted verdict that no person wrote this message. An
// untrusted kind is never automated, so a message the model was unsure about
// stays in the inbox.
func (d Decision) Automated() bool {
	return IsAutomatedKind(d.Kind) && d.ReviewReason != "kind" && !d.Skipped()
}

// Skipped reports a decision that did nothing because the message was ours.
func (d Decision) Skipped() bool { return d.KindSource == "skipped" }

// DecideOutbound is the answer for one of our own sends: nothing, with no call
// made. Kept as a named constructor so the "zero calls for outbound" test has
// something to assert against.
func DecideOutbound() Decision {
	return Decision{KindSource: "skipped"}
}

// Decide turns answers plus facts into labels and a relevance score.
//
// Pure: no I/O, no clock, no randomness. Every fixture in the test suite runs
// through this function against a cached response, so the policy can be
// re-tuned and re-verified offline for free. Re-running the model over history
// costs money; re-running the arithmetic does not.
func Decide(answers map[string]Answer, facts Facts) Decision {
	if facts.Outbound {
		return DecideOutbound()
	}

	d := Decision{Scores: map[string]float64{}, SignalStrength: map[string]float64{}}

	// ── Kind: the deterministic layer wins where it spoke ───────────────────
	kindAnswer, hasKind := answers["kind"]
	if facts.DeterministicKind != "" {
		d.Kind = facts.DeterministicKind
		d.KindConfidence = 1
		d.KindSource = "header"
	} else if hasKind {
		d.Kind = kindAnswer.Choice
		d.KindConfidence = kindAnswer.Confidence
		d.KindSource = "model"
	}

	// ── The confidence floor ───────────────────────────────────────────────
	// A model-sourced kind below the floor is not a weak signal to be used
	// carefully, it is a non-reproducible one: the same input three times
	// returned three different winners while confidence stayed near 0.2.
	// Nothing downstream may read it.
	if d.KindSource == "model" && d.KindConfidence < ConfFloor {
		return Decision{
			Kind:           d.Kind,
			KindConfidence: d.KindConfidence,
			KindSource:     d.KindSource,
			NeedsReview:    true,
			ReviewReason:   "kind",
			Labels:         []string{LabelNeedsReview},
			Priority:       PriorityWhenever,
			Scores:         map[string]float64{},
		}
	}

	// ── Intent: only meaningful for a human reply ──────────────────────────
	// It travels in the same request and is read only when there was a person on
	// the other end. Intent on a bounce is an answer with no subject.
	if d.Kind == KindHumanReply {
		if a, ok := answers["intent"]; ok {
			d.IntentConfidence = a.Confidence
			d.Intent = a.Choice
			if a.Confidence < ConfFloor {
				// An unreadable intent is not an unreadable message. On the
				// first backfill over real mail this discarded a `human_reply`
				// the model was 0.97 sure of, because it was only 0.6 sure
				// whether the person wanted information or wanted pricing.
				//
				// So the confident half is kept: the kind label, the signals
				// and the score all stand, and needs-review is added to say
				// that what they want could not be read. The untrusted answer is
				// retained for review but does not affect labels or relevance.
				d.NeedsReview = true
				d.ReviewReason = "intent"
			}
		}
	}

	// ── Signals ────────────────────────────────────────────────────────────
	for _, id := range SignalIDs() {
		a, ok := answers[id]
		if !ok {
			continue
		}
		d.SignalStrength[id] = a.Noul
		if a.Noul >= Yes {
			d.Signals = append(d.Signals, id)
		}
	}
	sort.Strings(d.Signals)

	// ── Scores, normalised to 0..1 ─────────────────────────────────────────
	// The score is a position on an ordered rubric, not a magnitude. Dividing
	// by the top index is the only arithmetic done with it, and it is done
	// here rather than asked, because Jev is bad at arithmetic.
	for _, id := range ScoreIDs() {
		a, ok := answers[id]
		if !ok {
			continue
		}
		levels := ScoreLevels(id)
		if levels < 2 {
			continue
		}
		d.Scores[id] = clamp(a.Score/float64(levels-1), 0, 1)
	}

	d.Relevance = relevance(d)
	d.Priority = bucket(float64(d.Relevance))
	d.Labels = labelsFor(d)
	if d.NeedsReview {
		d.Labels = append(d.Labels, LabelNeedsReview)
	}

	return d
}

// relevance is the weighted sum, clamped to 0..100. Every term comes from
// Weights in policy.go; this function chooses none of them.
func relevance(d Decision) int {
	var total float64

	// Kind families. A bounce or an auto-reply is not a lead, whatever the
	// body happens to say.
	switch d.Kind {
	case KindBounceHard, KindBounceSoft:
		total += Weights["bounce"]
	case KindAutoReplyOOO, KindAutoReplyTicket:
		total += Weights["auto_reply"]
	}

	// Intent, which is only ever set for a human reply.
	if w, ok := Weights[d.Intent]; ok && d.Intent != "" && d.ReviewReason != "intent" {
		total += w
	}

	// Signals that carry weight. A signal with no weight is still recorded and
	// still shown; it just does not move the number.
	for _, sig := range d.Signals {
		if w, ok := Weights[sig]; ok {
			total += w
		}
	}

	// Scores contribute their weight scaled by position on the rubric.
	for id, norm := range d.Scores {
		if w, ok := Weights[id]; ok {
			total += w * norm
		}
	}

	return int(math.Round(clamp(total, 0, 100)))
}

// labelsFor is the label list this decision writes. Every title comes from
// labelTitles in policy.go; this function never invents one.
func labelsFor(d Decision) []string {
	seen := map[string]bool{}
	var out []string
	add := func(id string) {
		l := LabelFor(id)
		if l == "" || seen[l] {
			return
		}
		seen[l] = true
		out = append(out, l)
	}

	add(d.Kind)
	if d.Kind == KindHumanReply && d.ReviewReason != "intent" {
		add(d.Intent)
	}

	// Signals become labels only on a human reply. The first backfill over
	// real mail put signal labels on bounces and platform notices, and a label
	// that is on everything is not a filter.
	if d.Kind == KindHumanReply {
		for _, sig := range d.Signals {
			add(sig)
		}
	}

	return out
}

func clamp(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}
