package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// Issue #141: the fused org posture. These prove the parts that only the
// database can decide — the concurrent-safe read/derive/write, and that an
// operator's suspension is not undone by a detector clearing.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveOrgRisk -v

func newRiskOrg(t *testing.T) (OrgRiskRepository, uuid.UUID) {
	t.Helper()
	handle, pool := liveContactDB(t)
	ctx := context.Background()
	user, org := uuid.New(), uuid.New()

	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Risk', 'Test')`,
		user, "risk-"+user.String()[:8]+"@test.local"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Risk Test', $2, $3)`,
		org, "risk-"+org.String()[:8], user); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		if _, err := pool.Exec(c, `DELETE FROM organizations WHERE id = $1`, org); err != nil {
			t.Errorf("cleanup org: %v", err)
		}
		if _, err := pool.Exec(c, `DELETE FROM users WHERE id = $1`, user); err != nil {
			t.Errorf("cleanup user: %v", err)
		}
	})
	return NewOrgRiskRepository(handle), org
}

func TestLiveOrgRiskDefaultsToTrusted(t *testing.T) {
	repo, org := newRiskOrg(t)
	risk, err := repo.GetOrgRisk(context.Background(), org)
	if err != nil {
		t.Fatalf("GetOrgRisk: %v", err)
	}
	if risk.State != models.OrgRiskTrusted || risk.Score != 0 {
		t.Errorf("a new workspace reads %q/%d, want trusted/0", risk.State, risk.Score)
	}
	if risk.Restricted() {
		t.Error("a new workspace must not be restricted")
	}
}

func TestLiveOrgRiskDerivesBandFromSignals(t *testing.T) {
	repo, org := newRiskOrg(t)
	ctx := context.Background()

	set := func(key string, weight int) *models.OrgRisk {
		t.Helper()
		risk, err := repo.UpdateOrgRiskSignals(ctx, org,
			func(signals map[string]any) (map[string]any, models.OrgRiskState, int, string) {
				signals[key] = map[string]any{"weight": weight, "detail": key}
				score := 0
				for _, raw := range signals {
					if m, ok := raw.(map[string]any); ok {
						if w, ok := m["weight"].(float64); ok {
							score += int(w)
						} else if w, ok := m["weight"].(int); ok {
							score += w
						}
					}
				}
				state := models.OrgRiskTrusted
				switch {
				case score >= 85:
					state = models.OrgRiskSuspended
				case score >= 50:
					state = models.OrgRiskRestricted
				case score >= 25:
					state = models.OrgRiskWatch
				}
				return signals, state, score, key
			})
		if err != nil {
			t.Fatalf("UpdateOrgRiskSignals: %v", err)
		}
		return risk
	}

	if risk := set("first", 30); risk.State != models.OrgRiskWatch {
		t.Errorf("30 points reads %q, want watch", risk.State)
	}
	// The point of fusing: a second tolerable signal crosses a band neither
	// would reach alone.
	risk := set("second", 30)
	if risk.State != models.OrgRiskRestricted || risk.Score != 60 {
		t.Errorf("two signals read %q/%d, want restricted/60", risk.State, risk.Score)
	}
	if !risk.Restricted() {
		t.Error("a restricted workspace must report as restricted")
	}
	if len(risk.Signals) != 2 {
		t.Errorf("evidence has %d entries, want both signals kept for review", len(risk.Signals))
	}
}

// An operator's suspension outranks the score. A detector clearing must not
// quietly release a workspace a human suspended.
func TestLiveOrgRiskSuspensionSurvivesSignalsClearing(t *testing.T) {
	repo, org := newRiskOrg(t)
	ctx := context.Background()

	if _, err := repo.SetOrgRiskState(ctx, org, models.OrgRiskSuspended, "manual review"); err != nil {
		t.Fatalf("SetOrgRiskState: %v", err)
	}

	risk, err := repo.UpdateOrgRiskSignals(ctx, org,
		func(map[string]any) (map[string]any, models.OrgRiskState, int, string) {
			// Every detector has cleared: score zero, band trusted.
			return map[string]any{}, models.OrgRiskTrusted, 0, ""
		})
	if err != nil {
		t.Fatalf("UpdateOrgRiskSignals: %v", err)
	}
	if risk.State != models.OrgRiskSuspended {
		t.Errorf("state = %q, want the operator's suspension to hold", risk.State)
	}
	if risk.Score != 0 {
		t.Errorf("score = %d, want the derived 0; only the band is pinned", risk.Score)
	}
}

func TestLiveOrgRiskStatesBatch(t *testing.T) {
	repo, org := newRiskOrg(t)
	ctx := context.Background()
	if _, err := repo.SetOrgRiskState(ctx, org, models.OrgRiskRestricted, "test"); err != nil {
		t.Fatalf("SetOrgRiskState: %v", err)
	}

	states, err := repo.GetOrgRiskStates(ctx, []uuid.UUID{org, uuid.New()})
	if err != nil {
		t.Fatalf("GetOrgRiskStates: %v", err)
	}
	if states[org] != models.OrgRiskRestricted {
		t.Errorf("state = %q, want restricted", states[org])
	}
	// An unknown organization must read as the zero value, which the callers
	// treat as unrestricted rather than as an error.
	if len(states) != 1 {
		t.Errorf("got %d states, want only the real organization", len(states))
	}
	if states[org].CapMultiplier() != 0.25 {
		t.Errorf("restricted cap multiplier = %v, want 0.25", states[org].CapMultiplier())
	}
}
