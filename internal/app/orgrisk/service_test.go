package orgrisk

import (
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

func sig(weight int, detail string) map[string]any {
	return map[string]any{"weight": float64(weight), "detail": detail}
}

func TestBandFor(t *testing.T) {
	for _, tt := range []struct {
		score int
		want  models.OrgRiskState
	}{
		{0, models.OrgRiskTrusted},
		{WatchScore - 1, models.OrgRiskTrusted},
		{WatchScore, models.OrgRiskWatch},
		{RestrictedScore - 1, models.OrgRiskWatch},
		{RestrictedScore, models.OrgRiskRestricted},
		{SuspendedScore - 1, models.OrgRiskRestricted},
		{SuspendedScore, models.OrgRiskSuspended},
		{100, models.OrgRiskSuspended},
	} {
		if got := BandFor(tt.score); got != tt.want {
			t.Errorf("BandFor(%d) = %q, want %q", tt.score, got, tt.want)
		}
	}
}

func TestScoreFusesSignalsAndCaps(t *testing.T) {
	if got := Score(nil); got != 0 {
		t.Errorf("Score(nil) = %d, want 0", got)
	}
	// The whole point: several individually-tolerable signals add up to a band
	// none of them would reach alone.
	signals := map[string]any{
		"signup_email_risk": sig(20, "disposable signup domain"),
		"list_quality":      sig(20, "imported list is 30% invalid"),
		"placement":         sig(20, "mailboxes landing in spam"),
	}
	if got := Score(signals); got != 60 {
		t.Errorf("Score() = %d, want 60", got)
	}
	if got := BandFor(Score(signals)); got != models.OrgRiskRestricted {
		t.Errorf("three tolerable signals should restrict, got %q", got)
	}

	signals["more"] = sig(80, "and another")
	if got := Score(signals); got != 100 {
		t.Errorf("Score() = %d, want it capped at 100", got)
	}
}

func TestScoreIgnoresMalformedEvidence(t *testing.T) {
	// The blob is operator-visible and hand-editable; a bad entry must not
	// panic or silently score as something arbitrary.
	signals := map[string]any{
		"good":       sig(30, "real"),
		"a string":   "not an object",
		"no weight":  map[string]any{"detail": "missing weight"},
		"bad weight": map[string]any{"weight": "heavy", "detail": "text weight"},
		"null":       nil,
	}
	if got := Score(signals); got != 30 {
		t.Errorf("Score() = %d, want only the well-formed 30", got)
	}
}

func TestReasonIsStableAndHeaviestFirst(t *testing.T) {
	signals := map[string]any{
		"a": sig(10, "light thing"),
		"b": sig(40, "heavy thing"),
		"c": sig(25, "middling thing"),
		"d": sig(5, "trivial thing"),
	}
	want := "heavy thing; middling thing; light thing"
	// Map iteration order is random, so run it enough that an unsorted
	// implementation would be caught rather than passing by luck.
	for i := 0; i < 50; i++ {
		if got := Reason(signals); got != want {
			t.Fatalf("Reason() = %q, want %q", got, want)
		}
	}
	if got := Reason(nil); got != "" {
		t.Errorf("Reason(nil) = %q, want empty", got)
	}
}

func TestBandEffects(t *testing.T) {
	if models.OrgRiskTrusted.CapMultiplier() != 1 || models.OrgRiskWatch.CapMultiplier() != 1 {
		t.Error("watch must not change what a customer can feel")
	}
	if models.OrgRiskWatch.ForcesFreeWarmupPool() || models.OrgRiskWatch.BlocksSending() {
		t.Error("watch must not restrict anything")
	}
	if !models.OrgRiskRestricted.ForcesFreeWarmupPool() || models.OrgRiskRestricted.BlocksSending() {
		t.Error("restricted lowers volume and leaves the paid pool, but still sends")
	}
	if !models.OrgRiskSuspended.BlocksSending() || models.OrgRiskSuspended.CapMultiplier() != 0 {
		t.Error("suspended must stop sending")
	}
}
