package tasks

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type poolTypeRiskRepo struct {
	repository.OrgRiskRepository
	states map[uuid.UUID]models.OrgRiskState
	err    error
}

func (r *poolTypeRiskRepo) GetOrgRiskStates(_ context.Context, _ []uuid.UUID) (map[uuid.UUID]models.OrgRiskState, error) {
	return r.states, r.err
}

type poolTypeGate struct {
	poolReconcileGate
	paid map[uuid.UUID]bool
}

func (g *poolTypeGate) HasPremiumWarmup(_ context.Context, orgID uuid.UUID) (bool, *errx.Error) {
	return g.paid[orgID], nil
}

func poolTypeAccount(orgID uuid.UUID, tier string) *Email {
	return &Email{ID: uuid.New(), OrganizationID: &orgID, Status: "active", WarmupPoolType: tier}
}

// The headline defect (issue #242): the stored tier is always set, so checking
// it first meant a restricted organization never left the premium pool.
func TestResolveWarmupPoolTypeDemotesARestrictedOrganizationWhateverItsTier(t *testing.T) {
	for _, state := range []models.OrgRiskState{models.OrgRiskRestricted, models.OrgRiskSuspended} {
		t.Run(string(state), func(t *testing.T) {
			org := uuid.New()
			svc := &tasksService{orgRiskRepo: &poolTypeRiskRepo{states: map[uuid.UUID]models.OrgRiskState{org: state}}}

			if got := svc.resolveWarmupPoolType(context.Background(), poolTypeAccount(org, "premium")); got != "free" {
				t.Fatalf("resolved %q for a %s organization, want free", got, state)
			}
		})
	}
}

// Lifting the restriction hands the mailbox its paid tier back on the next resolution.
func TestResolveWarmupPoolTypeKeepsTheStoredTierForATrustedOrganization(t *testing.T) {
	org := uuid.New()
	svc := &tasksService{orgRiskRepo: &poolTypeRiskRepo{states: map[uuid.UUID]models.OrgRiskState{org: models.OrgRiskWatch}}}

	if got := svc.resolveWarmupPoolType(context.Background(), poolTypeAccount(org, "premium")); got != "premium" {
		t.Fatalf("resolved %q, want the stored premium tier", got)
	}
	if got := svc.resolveWarmupPoolType(context.Background(), poolTypeAccount(org, "free")); got != "free" {
		t.Fatalf("resolved %q, want the stored free tier", got)
	}
}

// An unreadable posture must not demote: the risk read fails open everywhere else too.
func TestResolveWarmupPoolTypeFailsOpenWhenRiskCannotBeRead(t *testing.T) {
	org := uuid.New()
	svc := &tasksService{orgRiskRepo: &poolTypeRiskRepo{err: errors.New("connection reset by peer")}}

	if got := svc.resolveWarmupPoolType(context.Background(), poolTypeAccount(org, "premium")); got != "premium" {
		t.Fatalf("resolved %q on a risk lookup failure, want the stored tier", got)
	}
}

// With no stored tier the subscription decides, and no organization means free.
func TestResolveWarmupPoolTypeFallsBackToEntitlementWithoutAStoredTier(t *testing.T) {
	paid, trial := uuid.New(), uuid.New()
	svc := &tasksService{featureGate: &poolTypeGate{paid: map[uuid.UUID]bool{paid: true, trial: false}}}

	if got := svc.resolveWarmupPoolType(context.Background(), poolTypeAccount(paid, "")); got != "premium" {
		t.Fatalf("paid organization resolved %q, want premium", got)
	}
	if got := svc.resolveWarmupPoolType(context.Background(), poolTypeAccount(trial, "")); got != "free" {
		t.Fatalf("trial organization resolved %q, want free", got)
	}
	if got := svc.resolveWarmupPoolType(context.Background(), &Email{ID: uuid.New(), WarmupPoolType: "premium"}); got != "free" {
		t.Fatalf("mailbox with no organization resolved %q, want free", got)
	}
}

// The reconciler is what moves a mailbox that is not actively warming (a
// recipient-only member, say), so restriction has to reach it there too. Every
// workspace's risk is read in one query per pass.
func TestPoolReconcileMovesARestrictedOrganizationToTheFreePool(t *testing.T) {
	f := newPoolReconcileFixture()
	restricted := f.mailbox(f.orgPaid, "premium", "premium")
	other := uuid.New()
	f.gate.entitled[other] = true
	f.mailbox(other, "premium", "premium")
	f.risk.states[f.orgPaid] = models.OrgRiskRestricted

	moved, removed := f.run(t)

	if moved != 1 || removed != 0 {
		t.Fatalf("moved %d removed %d, want 1 and 0", moved, removed)
	}
	if f.warmup.moved[restricted] != "free" {
		t.Fatalf("moved to %q, want free", f.warmup.moved[restricted])
	}
	if f.risk.calls != 1 {
		t.Fatalf("read risk %d times, want once per pass", f.risk.calls)
	}
}
