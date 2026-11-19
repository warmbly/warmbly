package feature

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type stubHold struct {
	paused bool
	err    error
}

func (h stubHold) WarmupPaused(context.Context, uuid.UUID) (bool, error) { return h.paused, h.err }

// A reseller's suspension stops warmup even where billing is off and every other gate is open.
func TestPartnerHoldStopsWarmupEvenWithoutBilling(t *testing.T) {
	g := &featureGateService{selfHost: true}
	ctx, org := context.Background(), uuid.New()
	if ok, _ := g.CanUseWarmup(ctx, org); !ok {
		t.Fatal("an unheld self-host workspace should warm")
	}
	WithPartnerHold(g, stubHold{paused: true})
	if ok, xerr := g.CanUseWarmup(ctx, org); ok || xerr != nil {
		t.Fatalf("held workspace: ok=%v err=%v, want a definite no", ok, xerr)
	}
	WithPartnerHold(g, stubHold{err: errors.New("db down")})
	if ok, xerr := g.CanUseWarmup(ctx, org); ok || xerr == nil {
		t.Fatalf("unreadable hold: ok=%v err=%v, want an unknown entitlement", ok, xerr)
	}
	WithPartnerHold(g, stubHold{})
	if ok, _ := g.CanUseWarmup(ctx, org); !ok {
		t.Fatal("a resumed workspace should warm again")
	}
}
