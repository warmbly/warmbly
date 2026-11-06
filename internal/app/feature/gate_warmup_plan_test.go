package feature

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type stubPlans struct{ repository.PlanRepository }

func (stubPlans) GetByID(context.Context, uuid.UUID) (*models.Plan, error) { return nil, nil }

func paidSub(planID uuid.UUID) *models.Subscription {
	id := "sub_test"
	return &models.Subscription{PlanID: planID, StripeSubscriptionID: &id, Status: models.SubscriptionStatusActive}
}

// The Warmup plan buys the premium pool and an uncapped mailbox count, and
// every product gate answers it the way it answers a free workspace.
func TestWarmupPlanUnlocksWarmingAndNothingElse(t *testing.T) {
	ctx := context.Background()
	org := uuid.New()
	g := &featureGateService{subRepo: stubSubs{sub: paidSub(models.WarmupPlanID)}, planRepo: stubPlans{}}
	must := noError(t)

	if ok, _ := g.HasPremiumWarmup(ctx, org); !ok {
		t.Error("the Warmup plan must warm in the premium pool")
	}
	if ok, _ := g.CanAddInbox(ctx, org, models.FreeWorkspaceMailboxLimit+50); !ok {
		t.Error("the Warmup plan must not cap mailboxes")
	}

	for name, ok := range map[string]bool{
		"IsPaidOrganization":     must(g.IsPaidOrganization(ctx, org)),
		"CanSendCampaignEmail":   must(g.CanSendCampaignEmail(ctx, org)),
		"CanUseUnibox":           must(g.CanUseUnibox(ctx, org)),
		"CanUseWritingAssistant": must(g.CanUseWritingAssistant(ctx, org)),
		"CanUseInboxAgent":       must(g.CanUseInboxAgent(ctx, org)),
	} {
		if ok {
			t.Errorf("%s = true on the Warmup plan, want false", name)
		}
	}

	if n, _ := g.GetDailyEmailLimit(ctx, org); n != 0 {
		t.Errorf("daily send limit = %d on the Warmup plan, want 0", n)
	}
	if n, _ := g.GetStorageLimitBytes(ctx, org); n != FreeTierStorageBytes {
		t.Errorf("storage = %d on the Warmup plan, want the free allowance %d", n, FreeTierStorageBytes)
	}
	if st, _ := g.GetSubscriptionStatus(ctx, org); st.IsPaidSubscriber || st.DailyEmailLimit != 0 {
		t.Errorf("status reports paid=%v daily=%d on the Warmup plan, want false and 0", st.IsPaidSubscriber, st.DailyEmailLimit)
	}
}

// A plan that sends keeps every gate, so the split does not cost anyone.
func TestSendingPlanKeepsProductAndPremiumPool(t *testing.T) {
	ctx := context.Background()
	org := uuid.New()
	g := &featureGateService{subRepo: stubSubs{sub: paidSub(uuid.New())}}
	must := noError(t)

	for name, ok := range map[string]bool{
		"IsPaidOrganization":   must(g.IsPaidOrganization(ctx, org)),
		"HasPremiumWarmup":     must(g.HasPremiumWarmup(ctx, org)),
		"CanSendCampaignEmail": must(g.CanSendCampaignEmail(ctx, org)),
		"CanUseUnibox":         must(g.CanUseUnibox(ctx, org)),
	} {
		if !ok {
			t.Errorf("%s = false on a sending plan, want true", name)
		}
	}
}

// A grant of the Warmup plan is warmup-only too: entitlements follow the plan in force.
func TestGrantedWarmupPlanIsWarmupOnly(t *testing.T) {
	granted := time.Now().Add(-time.Hour)
	sub := &models.Subscription{PlanID: uuid.New(), ManagedAt: &granted, ManagedPlanID: &models.WarmupPlanID}
	if !sub.IsWarmupOnly() || sub.HasProductPlan() || !sub.HasPaidSubscription() {
		t.Errorf("granted Warmup plan: warmupOnly=%v product=%v paid=%v, want true false true",
			sub.IsWarmupOnly(), sub.HasProductPlan(), sub.HasPaidSubscription())
	}
}

// A Warmup grant must not hide a Starter subscription the workspace pays Stripe for.
func TestStripeSendingPlanOutranksAWarmupGrant(t *testing.T) {
	granted := time.Now().Add(-time.Hour)
	sub := paidSub(uuid.New())
	sub.ManagedAt, sub.ManagedPlanID = &granted, &models.WarmupPlanID
	if sub.IsWarmupOnly() || !sub.HasProductPlan() {
		t.Errorf("Stripe sending plan under a Warmup grant: warmupOnly=%v product=%v, want false true", sub.IsWarmupOnly(), sub.HasProductPlan())
	}
}

func TestNoSubscriptionHasNoPremiumPool(t *testing.T) {
	g := &featureGateService{subRepo: stubSubs{sub: nil}}
	if ok, xerr := g.HasPremiumWarmup(context.Background(), uuid.New()); xerr != nil || ok {
		t.Errorf("HasPremiumWarmup = %v (err %v) with no subscription, want false", ok, xerr)
	}
}

func noError(t *testing.T) func(bool, *errx.Error) bool {
	return func(ok bool, xerr *errx.Error) bool {
		t.Helper()
		if xerr != nil {
			t.Fatalf("unexpected error: %v", xerr)
		}
		return ok
	}
}

// The Partnership plan carries the Warmup plan's entitlements, bought or granted.
func TestPartnershipPlanWarmsLikeTheWarmupPlan(t *testing.T) {
	ctx := context.Background()
	org := uuid.New()
	g := &featureGateService{subRepo: stubSubs{sub: paidSub(models.PartnershipPlanID)}, planRepo: stubPlans{}}
	must := noError(t)

	if !must(g.HasPremiumWarmup(ctx, org)) {
		t.Error("the Partnership plan must warm in the premium pool")
	}
	if !must(g.CanAddInbox(ctx, org, models.FreeWorkspaceMailboxLimit+50)) {
		t.Error("the Partnership plan must not cap mailboxes")
	}
	if must(g.CanSendCampaignEmail(ctx, org)) || must(g.IsPaidOrganization(ctx, org)) {
		t.Error("the Partnership plan must not unlock sending")
	}

	granted := time.Now().Add(-time.Hour)
	sub := &models.Subscription{PlanID: uuid.New(), ManagedAt: &granted, ManagedPlanID: &models.PartnershipPlanID}
	if !sub.IsWarmupOnly() || sub.HasProductPlan() {
		t.Errorf("granted Partnership plan: warmupOnly=%v product=%v, want true false", sub.IsWarmupOnly(), sub.HasProductPlan())
	}
}

// A free workspace warms only if it kept the free allowance; a new one needs a plan.
func TestFreeWarmupFollowsTheKeptAllowance(t *testing.T) {
	ctx := context.Background()
	org := uuid.New()
	must := noError(t)

	kept := &featureGateService{subRepo: stubSubs{sub: &models.Subscription{KeepsFreeWarmup: true}}}
	if !must(kept.CanUseWarmup(ctx, org)) {
		t.Error("a workspace that kept the free allowance must still warm")
	}
	if !must(kept.CanAddInbox(ctx, org, models.FreeWorkspaceMailboxLimit-1)) || must(kept.CanAddInbox(ctx, org, models.FreeWorkspaceMailboxLimit)) {
		t.Errorf("a kept free workspace must hold exactly %d mailboxes", models.FreeWorkspaceMailboxLimit)
	}
	if must(kept.HasPremiumWarmup(ctx, org)) {
		t.Error("a kept free workspace warms in the free pool, not the premium one")
	}

	fresh := &featureGateService{subRepo: stubSubs{sub: &models.Subscription{}}}
	if must(fresh.CanUseWarmup(ctx, org)) {
		t.Error("a new free workspace must not warm without a plan")
	}
	if must(fresh.CanAddInbox(ctx, org, 0)) {
		t.Error("a new free workspace must not connect a mailbox without a plan")
	}

	none := &featureGateService{subRepo: stubSubs{}}
	if must(none.CanUseWarmup(ctx, org)) || must(none.CanAddInbox(ctx, org, 0)) {
		t.Error("a workspace with no subscription row must not warm or connect mailboxes")
	}
}

// A trial warms in the free pool and holds TrialMailboxLimit mailboxes; once it ends, both stop.
func TestTrialWarmsTwoMailboxesUntilItEnds(t *testing.T) {
	ctx := context.Background()
	org := uuid.New()
	must := noError(t)

	trial := &featureGateService{subRepo: stubSubs{sub: trialSub()}}
	if !must(trial.CanUseWarmup(ctx, org)) {
		t.Error("a workspace in its trial must warm")
	}
	if must(trial.HasPremiumWarmup(ctx, org)) {
		t.Error("a trial warms in the free pool, not the premium one")
	}
	if !must(trial.CanAddInbox(ctx, org, models.TrialMailboxLimit-1)) || must(trial.CanAddInbox(ctx, org, models.TrialMailboxLimit)) {
		t.Errorf("a trial must hold exactly %d mailboxes", models.TrialMailboxLimit)
	}

	past := time.Now().Add(-time.Hour)
	ended := trialSub()
	ended.FreeTrialEndsAt = &past
	over := &featureGateService{subRepo: stubSubs{sub: ended}}
	if must(over.CanUseWarmup(ctx, org)) || must(over.CanAddInbox(ctx, org, 0)) {
		t.Error("an ended trial must neither warm nor connect mailboxes")
	}
}
