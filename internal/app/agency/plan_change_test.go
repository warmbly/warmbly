package agency

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// pricedPlans are agency plans with Stripe prices, keyed by plan id.
type pricedPlans map[uuid.UUID]*models.Plan

func (p pricedPlans) GetByID(_ context.Context, id uuid.UUID) (*models.Plan, error) {
	return p[id], nil
}
func (p pricedPlans) GetByStripePriceID(context.Context, string) (*models.Plan, error) {
	return nil, nil
}
func (p pricedPlans) List(context.Context, bool) ([]*models.Plan, error) { return nil, nil }

func strp(v string) *string { return &v }

func newSwitchService(t *testing.T, a *models.Agency, used int64, now time.Time) (*Service, uuid.UUID) {
	t.Helper()
	actor := uuid.New()
	repo := &operatorRepo{
		fakeRepo: &fakeRepo{agency: a, grants: map[uuid.UUID]uuid.UUID{}, sends: map[uuid.UUID]int64{uuid.New(): used}},
		staff:    map[uuid.UUID]bool{actor: true}, untils: map[uuid.UUID]*time.Time{},
	}
	plans := pricedPlans{
		models.AgencyPlanID:    {ID: models.AgencyPlanID, MonthlySendPool: intp(150_000), StripePriceID: strp("price_agency_m"), StripePriceIDYearly: strp("price_agency_y")},
		models.AgencyProPlanID: {ID: models.AgencyProPlanID, MonthlySendPool: intp(500_000), StripePriceID: strp("price_pro_m")},
	}
	s := NewService(repo, operatorOrgs{}, operatorUsers{}, operatorSubs{}, plans)
	s.billing = true
	s.now = func() time.Time { return now }
	return s, actor
}

func TestPlanChangeNeedsALiveSubscription(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()

	// A plan held without a subscription has nothing of its own to switch.
	at, granted := now.Add(-time.Hour), models.AgencyProPlanID
	a := &models.Agency{ID: uuid.New(), Status: models.SubscriptionStatusIncomplete, ManagedPlanID: &granted, ManagedAt: &at}
	s, actor := newSwitchService(t, a, 0, now)
	if _, xerr := s.planChangeFor(ctx, a.ID, actor, models.AgencyPlanID, false); xerr == nil || xerr.Identifier != "agency_no_subscription" {
		t.Fatalf("no subscription: got %v", xerr)
	}

	// An overdue subscription is not switched either.
	b := activeAgency(now)
	b.Status = models.SubscriptionStatusPastDue
	b.StripeSubscriptionID = strp("sub_1")
	s, actor = newSwitchService(t, b, 0, now)
	if _, xerr := s.planChangeFor(ctx, b.ID, actor, models.AgencyProPlanID, false); xerr == nil || xerr.Identifier != "agency_no_subscription" {
		t.Fatalf("past due: got %v", xerr)
	}
}

func TestPlanChangeRefusesAPlanWithoutThatPrice(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	a := activeAgency(now)
	a.StripeSubscriptionID = strp("sub_1")
	s, actor := newSwitchService(t, a, 0, now)
	// Agency Pro has no yearly price here, and a plan that is not an agency plan has none at all.
	for _, c := range []struct {
		plan   uuid.UUID
		yearly bool
	}{{models.AgencyProPlanID, true}, {uuid.New(), false}} {
		if _, xerr := s.planChangeFor(context.Background(), a.ID, actor, c.plan, c.yearly); xerr == nil || xerr.Identifier != "agency_plan_unavailable" {
			t.Fatalf("%s yearly=%v: got %v", c.plan, c.yearly, xerr)
		}
	}
}

func TestPlanChangeRefusesAPoolThePeriodAlreadyExceeds(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	a := activeAgency(now)
	pro := models.AgencyProPlanID
	a.PlanID = &pro
	a.StripeSubscriptionID = strp("sub_1")
	s, actor := newSwitchService(t, a, 320_000, now)
	if _, xerr := s.planChangeFor(context.Background(), a.ID, actor, models.AgencyPlanID, false); xerr == nil || xerr.Identifier != "agency_pool_exceeds_plan" {
		t.Fatalf("320,000 used against a 150,000 pool: got %v", xerr)
	}
}

func TestPlanChangeRefusesAStalePreview(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	a := activeAgency(now)
	a.StripeSubscriptionID = strp("sub_1")
	s, actor := newSwitchService(t, a, 0, now)
	for _, at := range []time.Time{now.Add(-planChangePreviewTTL - time.Minute), now.Add(10 * time.Minute)} {
		if _, xerr := s.ChangePlan(context.Background(), a.ID, actor, models.AgencyProPlanID, false, PlanChangeQuote{ProrationDate: at.Unix()}); xerr == nil || xerr.Identifier != "agency_plan_preview_expired" {
			t.Fatalf("priced at %s: got %v", at, xerr)
		}
	}
}

func TestSubscriptionInterval(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	month, year := start.AddDate(0, 1, 0), start.AddDate(1, 0, 0)
	a := models.Agency{Status: models.SubscriptionStatusActive, CurrentPeriodStart: &start, CurrentPeriodEnd: &month}
	if got := a.SubscriptionInterval(); got != "month" {
		t.Fatalf("monthly period: got %q", got)
	}
	a.CurrentPeriodEnd = &year
	if got := a.SubscriptionInterval(); got != "year" {
		t.Fatalf("yearly period: got %q", got)
	}
	a.Status = models.SubscriptionStatusCanceled
	if got := a.SubscriptionInterval(); got != "" {
		t.Fatalf("no subscription in force: got %q", got)
	}
}
