package inboxtag

import (
	"sort"
	"strings"
	"testing"
)

// Every label a decision can write must be in AllLabels, or it would be created
// one at a time as it first fires and could not be filtered on before that.
// This is the test that keeps labelsFor and AllLabels from drifting apart.
func TestAllLabelsCoversEverythingDecideCanWrite(t *testing.T) {
	all := map[string]bool{}
	for _, l := range AllLabels() {
		all[l] = true
	}

	// Every kind and intent that files under a label.
	for id := range kindCriteria {
		if l := LabelFor(id); l != "" && !all[l] {
			t.Errorf("kind %q files under %q, which is not in AllLabels", id, l)
		}
	}
	for id := range intentCriteria {
		if l := LabelFor(id); l != "" && !all[l] {
			t.Errorf("intent %q files under %q, which is not in AllLabels", id, l)
		}
	}
	if !all[LabelNeedsReview] {
		t.Error("needs-review is not in AllLabels")
	}

	// And the signals labelsFor actually surfaces, driven through the real
	// function rather than a second copy of the list.
	d := Decision{
		Kind:    KindHumanReply,
		Intent:  IntentAgreed,
		Signals: SignalIDs(),
	}
	for _, label := range labelsFor(d) {
		if !all[label] {
			t.Errorf("labelsFor emits %q, which AllLabels does not create", label)
		}
	}
}

func TestAllLabelsIsSortedAndUnique(t *testing.T) {
	labels := AllLabels()
	if !sort.StringsAreSorted(labels) {
		t.Error("AllLabels is not sorted, so the filter list order would drift between runs")
	}
	seen := map[string]bool{}
	for _, l := range labels {
		if seen[l] {
			t.Errorf("duplicate label %q", l)
		}
		seen[l] = true
	}
	if len(labels) < 15 {
		t.Errorf("only %d labels; expected the full taxonomy", len(labels))
	}
}

// The first backfill over real mail put a signal label on bounces and platform
// notifications: the model answers the question honestly for any text, but the
// answer is meaningless on mail no person wrote, and a label that lands on
// everything is not a filter.
func TestSignalLabelsOnlyOnHumanReplies(t *testing.T) {
	signals := []string{SigNeedsHumanJudgement, SigAsksForCall, SigRequestsRemoval, SigLegalThreat}

	for _, kind := range []string{KindBounceHard, KindBounceSoft, KindNotification, KindAutoReplyOOO} {
		got := labelsFor(Decision{Kind: kind, Signals: signals})
		if len(got) != 1 || got[0] != LabelFor(kind) {
			t.Errorf("kind %s got %v; signals only label a human reply", kind, got)
		}
	}

	human := labelsFor(Decision{Kind: KindHumanReply, Intent: IntentAgreed, Signals: signals})
	want := []string{"Interested", "Meeting", "Unsubscribe", "Legal threat"}
	if len(human) != len(want) {
		t.Fatalf("a human reply got %v, want %v", human, want)
	}
	for i := range want {
		if human[i] != want[i] {
			t.Fatalf("a human reply got %v, want %v", human, want)
		}
	}
}

// A label is read by people who may not speak English well and never learned
// our taxonomy, so it is plain words: capitalised, no identifier syntax.
func TestLabelsAreReadable(t *testing.T) {
	for _, l := range AllLabels() {
		if strings.ContainsAny(l, "_") || l != strings.TrimSpace(l) || strings.ToUpper(l[:1]) != l[:1] {
			t.Errorf("label %q reads like an identifier", l)
		}
		if n := len(strings.Fields(l)); n > 3 {
			t.Errorf("label %q is %d words; a chip needs at most three", l, n)
		}
	}
}

// Only a trusted machine verdict takes a conversation out of the inbox.
func TestAutomated(t *testing.T) {
	cases := []struct {
		name string
		d    Decision
		want bool
	}{
		{"offline out of office", Decide(nil, Facts{DeterministicKind: KindAutoReplyOOO}), true},
		{"confident notification", Decide(map[string]Answer{"kind": {Type: QuestionChoice, Choice: KindNotification, Confidence: 0.9}}, Facts{}), true},
		{"unsure notification", Decide(map[string]Answer{"kind": {Type: QuestionChoice, Choice: KindNotification, Confidence: 0.5}}, Facts{}), false},
		{"a person", Decide(map[string]Answer{"kind": {Type: QuestionChoice, Choice: KindHumanReply, Confidence: 0.9}}, Facts{}), false},
		{"a pitch", Decide(map[string]Answer{"kind": {Type: QuestionChoice, Choice: KindColdInbound, Confidence: 0.9}}, Facts{}), false},
		{"our own send", DecideOutbound(), false},
	}
	for _, tc := range cases {
		if got := tc.d.Automated(); got != tc.want {
			t.Errorf("%s: Automated() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A kind the model is sure of survives an intent it is not sure of. The first
// backfill over real mail threw away four `human_reply` verdicts at 0.97
// confidence because the intent behind them sat at 0.6, which is the confident
// half being discarded along with the doubtful one.
func TestUnreadableIntentKeepsTheConfidentKind(t *testing.T) {
	answers := map[string]Answer{
		"kind":         {Type: QuestionChoice, Choice: KindHumanReply, Confidence: 0.97},
		"intent":       {Type: QuestionChoice, Choice: IntentWantsInfo, Confidence: 0.60},
		SigAsksForCall: {Type: QuestionNoul, Noul: 0.95},
	}
	d := Decide(answers, Facts{})

	if !d.NeedsReview {
		t.Error("an unreadable intent should still raise needs-review")
	}
	if d.ReviewReason != "intent" {
		t.Errorf("review reason = %q, want intent", d.ReviewReason)
	}
	if d.Intent != IntentWantsInfo {
		t.Errorf("intent %q not retained for review", d.Intent)
	}
	if d.Kind != KindHumanReply {
		t.Errorf("kind = %q; a 0.97 verdict should survive", d.Kind)
	}

	has := func(want string) bool {
		for _, l := range d.Labels {
			if l == want {
				return true
			}
		}
		return false
	}
	if !has("Meeting") {
		t.Errorf("lost the confident signal label: %v", d.Labels)
	}
	if !has(LabelNeedsReview) {
		t.Errorf("did not flag it for review: %v", d.Labels)
	}
	if has("Question") {
		t.Errorf("applied the untrusted intent as a label: %v", d.Labels)
	}
	if d.Relevance == 0 {
		t.Error("scored 0 despite a confident kind and a call request")
	}
}

// The dashboard keeps a hand-written explanation per label
// (web/src/lib/unibox/tagMeanings.ts) so a chip reading "Gone quiet" can say
// what it means on hover. That list cannot import this one, so this test pins
// the count: a label added here without an explanation there ships with no
// hover text, which is the state the feature started in.
func TestLabelCountMatchesTheDashboardMeanings(t *testing.T) {
	const documented = 19 // keep in step with EVERY_AUTOMATIC_LABEL in tagMeanings.test.ts
	if got := len(AllLabels()); got != documented {
		t.Fatalf("AllLabels has %d labels but the dashboard explains %d.\n"+
			"Add the new label to web/src/lib/unibox/tagMeanings.ts and to\n"+
			"EVERY_AUTOMATIC_LABEL in tagMeanings.test.ts, then update this count.",
			got, documented)
	}
}
