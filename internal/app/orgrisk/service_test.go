package orgrisk

import (
	"context"
	"testing"

	"github.com/google/uuid"

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

type recordingAudit struct {
	entries []string
}

func (r *recordingAudit) LogAction(_ context.Context, orgID, _ uuid.UUID, _ models.AuditAction,
	entityType models.AuditEntityType, _ *uuid.UUID, _, _ string, changes, _ map[string]string) {
	r.entries = append(r.entries, string(entityType)+":"+changes["risk_state"])
	_ = orgID
}

type stubRiskRepo struct {
	risk *models.OrgRisk
}

func (s *stubRiskRepo) GetOrgRisk(context.Context, uuid.UUID) (*models.OrgRisk, error) {
	copy := *s.risk
	return &copy, nil
}
func (s *stubRiskRepo) GetOrgRiskStates(context.Context, []uuid.UUID) (map[uuid.UUID]models.OrgRiskState, error) {
	return nil, nil
}
func (s *stubRiskRepo) UpdateOrgRiskSignals(_ context.Context, _ uuid.UUID,
	derive func(map[string]any) (map[string]any, models.OrgRiskState, int, string)) (*models.OrgRisk, error) {
	signals, state, score, reason := derive(s.risk.Signals)
	s.risk.Signals, s.risk.State, s.risk.Score, s.risk.Reason = signals, state, score, reason
	copy := *s.risk
	return &copy, nil
}
func (s *stubRiskRepo) SetOrgRiskState(_ context.Context, _ uuid.UUID, state models.OrgRiskState, reason string) (*models.OrgRisk, error) {
	s.risk.State, s.risk.Reason = state, reason
	copy := *s.risk
	return &copy, nil
}

// A transition has to reach the audit spine, or the banner never updates for a
// teammate and there is no trail of who was restricted when.
func TestTransitionsAreAudited(t *testing.T) {
	repo := &stubRiskRepo{risk: &models.OrgRisk{State: models.OrgRiskTrusted, Signals: map[string]any{}}}
	rec := &recordingAudit{}
	svc := NewService(repo)
	svc.(AuditAware).WireAudit(rec)

	org := uuid.New()
	if _, err := svc.RecordSignal(context.Background(), org, Signal{Key: "a", Weight: 30, Detail: "something"}); err != nil {
		t.Fatalf("RecordSignal: %v", err)
	}
	if len(rec.entries) != 1 || rec.entries[0] != "org_risk:trusted -> watch" {
		t.Fatalf("entries = %v, want one trusted -> watch", rec.entries)
	}

	// A detector re-recording the same finding is not a transition and must not
	// fill the feed with no-ops.
	if _, err := svc.RecordSignal(context.Background(), org, Signal{Key: "a", Weight: 30, Detail: "something"}); err != nil {
		t.Fatalf("RecordSignal: %v", err)
	}
	if len(rec.entries) != 1 {
		t.Errorf("a no-op re-record logged again: %v", rec.entries)
	}

	if _, err := svc.SetState(context.Background(), org, models.OrgRiskSuspended, "manual"); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if len(rec.entries) != 2 || rec.entries[1] != "org_risk:watch -> suspended" {
		t.Errorf("entries = %v, want the operator transition logged", rec.entries)
	}
}
