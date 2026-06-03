package humanlint

import (
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/pkg/warmlint"
)

func TestHumanizeStripsEmDash(t *testing.T) {
	out := Humanize("I tried the new setup — it worked great.", 1)
	if strings.ContainsAny(out, "—–") {
		t.Fatalf("em-dash not removed: %q", out)
	}
}

func TestHumanizeStripsClicheOpener(t *testing.T) {
	out := Humanize("I hope this email finds you well. How did the demo go?", 1)
	if strings.Contains(strings.ToLower(out), "hope this email finds you well") {
		t.Fatalf("cliche opener not stripped: %q", out)
	}
	if !strings.Contains(out, "demo") {
		t.Fatalf("content after opener was lost: %q", out)
	}
}

func TestHumanizeSwapsAIVocabulary(t *testing.T) {
	out := strings.ToLower(Humanize("Can we leverage the robust new framework?", 1))
	if strings.Contains(out, "leverage") || strings.Contains(out, "robust") {
		t.Fatalf("AI vocabulary not swapped: %q", out)
	}
}

func TestHumanizeFlattensNotOnlyButAlso(t *testing.T) {
	out := strings.ToLower(Humanize("It was not only fast but also cheap.", 1))
	if strings.Contains(out, "not only") || strings.Contains(out, "but also") {
		t.Fatalf("not-only-but-also not flattened: %q", out)
	}
}

func TestHumanizeNormalizesQuotes(t *testing.T) {
	out := Humanize("It’s the “best” one…", 1)
	if strings.ContainsAny(out, "‘’“”…") {
		t.Fatalf("smart punctuation not normalized: %q", out)
	}
}

func TestHumanizeAppliesSomeContractions(t *testing.T) {
	// Across several seeds at least one should contract (probabilistic ~0.72).
	in := "I am sure you do not mind. It is fine."
	contracted := false
	for seed := int64(0); seed < 20; seed++ {
		if strings.Contains(Humanize(in, seed), "'") {
			contracted = true
			break
		}
	}
	if !contracted {
		t.Fatal("no contractions applied across 20 seeds")
	}
}

func TestHumanizeIsDeterministic(t *testing.T) {
	in := "I am reaching out. It is not only fast but also smooth — really."
	if Humanize(in, 42) != Humanize(in, 42) {
		t.Fatal("Humanize is not deterministic for a fixed seed")
	}
}

func TestHumanizeDoesNotIntroduceSpam(t *testing.T) {
	// Humanizing clean warmup-ish copy must never trip the spam gate.
	inputs := []string{
		"I hope this email finds you well. I wanted to reach out about the deck.",
		"We should leverage the robust framework — it is seamless.",
		"How did Tuesday go? No rush, just curious.",
	}
	for _, in := range inputs {
		out := Humanize(in, 7)
		if err := warmlint.Check("quick note", out, false); err != nil {
			t.Fatalf("humanized output tripped warmlint for %q -> %q: %v", in, out, err)
		}
	}
}

func TestScoreFlagsRoboticHigherThanHuman(t *testing.T) {
	robotic := "I hope this email finds you well. I wanted to reach out to underscore that our robust, seamless, and comprehensive framework will leverage synergy. It is worth noting that this is not only pivotal but also transformative."
	human := "hey, how did Tuesday go? no rush. I'll send the second draft over after lunch."
	rs, _ := Score(robotic)
	hs, _ := Score(human)
	if rs <= hs {
		t.Fatalf("expected robotic (%d) > human (%d)", rs, hs)
	}
	if !LooksRobotic(robotic) {
		t.Fatalf("robotic text not flagged (score %d)", rs)
	}
	if LooksRobotic(human) {
		t.Fatalf("human text wrongly flagged (score %d)", hs)
	}
}
