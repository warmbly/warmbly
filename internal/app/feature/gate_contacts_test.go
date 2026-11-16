package feature

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type contactPlans struct {
	repository.PlanRepository
	plan *models.Plan
}

func (p contactPlans) GetByID(context.Context, uuid.UUID) (*models.Plan, error) { return p.plan, nil }

type contactOverrides struct{ maxContacts int }

func (o contactOverrides) GetOrganizationLimitOverrides(context.Context, uuid.UUID) (*models.OrganizationLimitOverrides, error) {
	return &models.OrganizationLimitOverrides{MaxContacts: o.maxContacts}, nil
}

func TestContactLimitResolvesOverrideThenPlanThenCap(t *testing.T) {
	ctx := context.Background()
	org := uuid.New()
	starter := &models.Plan{ID: uuid.New(), MaxContacts: 1000}

	cases := []struct {
		name string
		gate *featureGateService
		want int
	}{
		{"plan", &featureGateService{subRepo: stubSubs{sub: paidSub(starter.ID)}, planRepo: contactPlans{plan: starter}}, 1000},
		{"override wins over plan", &featureGateService{subRepo: stubSubs{sub: paidSub(starter.ID)}, planRepo: contactPlans{plan: starter}, overrides: contactOverrides{5000}}, 5000},
		{"zero override inherits plan", &featureGateService{subRepo: stubSubs{sub: paidSub(starter.ID)}, planRepo: contactPlans{plan: starter}, overrides: contactOverrides{0}}, 1000},
		{"no subscription", &featureGateService{subRepo: stubSubs{}, planRepo: contactPlans{}}, config.HardCapContacts},
		{"self-host ignores the seeded plan", &featureGateService{selfHost: true, subRepo: stubSubs{sub: paidSub(starter.ID)}, planRepo: contactPlans{plan: starter}}, 0},
		{"self-host keeps an operator override", &featureGateService{selfHost: true, overrides: contactOverrides{300}}, 300},
	}
	for _, tc := range cases {
		got, xerr := tc.gate.ContactLimit(ctx, org)
		if xerr != nil {
			t.Fatalf("%s: %v", tc.name, xerr.Message)
		}
		if got != tc.want {
			t.Errorf("%s: limit = %d, want %d", tc.name, got, tc.want)
		}
	}
}
