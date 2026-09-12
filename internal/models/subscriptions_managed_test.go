package models

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func managed(at *time.Time, until *time.Time) *Subscription {
	reason := "internal workspace"
	by := uuid.New()
	return &Subscription{ManagedAt: at, ManagedUntil: until, ManagedReason: &reason, ManagedBy: &by}
}

func ptr(t time.Time) *time.Time { return &t }

// The whole point: a granted plan is paid without Stripe. Before this there
// was no way to make a workspace paid except a Stripe subscription id.
func TestManagedPlanCountsAsPaid(t *testing.T) {
	now := time.Now()

	cases := []struct {
		name string
		sub  *Subscription
		paid bool
	}{
		{"open-ended grant", managed(ptr(now.Add(-time.Hour)), nil), true},
		{"grant with time left", managed(ptr(now.Add(-time.Hour)), ptr(now.Add(24*time.Hour))), true},
		{"lapsed grant", managed(ptr(now.Add(-48*time.Hour)), ptr(now.Add(-time.Hour))), false},
		{"never granted", &Subscription{}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.sub.HasPaidSubscription(); got != tc.paid {
				t.Errorf("HasPaidSubscription() = %v, want %v", got, tc.paid)
			}
		})
	}
}

// A grant that ran out must be distinguishable from one that never existed,
// or the admin panel drops a workspace back to free with no explanation.
func TestManagedExpiredIsDistinctFromNeverGranted(t *testing.T) {
	now := time.Now()

	lapsed := managed(ptr(now.Add(-48*time.Hour)), ptr(now.Add(-time.Hour)))
	if !lapsed.ManagedExpired() {
		t.Error("a lapsed grant should report as expired")
	}
	if lapsed.IsManaged() {
		t.Error("a lapsed grant is not in force")
	}

	never := &Subscription{}
	if never.ManagedExpired() {
		t.Error("a workspace that was never granted has not expired")
	}

	open := managed(ptr(now.Add(-time.Hour)), nil)
	if open.ManagedExpired() {
		t.Error("an open-ended grant never expires")
	}
}

// A real Stripe subscription keeps working exactly as before.
func TestStripeSubscriptionUnaffected(t *testing.T) {
	id := "sub_123"
	sub := &Subscription{StripeSubscriptionID: &id, Status: SubscriptionStatusActive}
	if !sub.HasPaidSubscription() {
		t.Error("an active Stripe subscription must still be paid")
	}

	cancelled := &Subscription{StripeSubscriptionID: &id, Status: SubscriptionStatusCanceled}
	if cancelled.HasPaidSubscription() {
		t.Error("a cancelled Stripe subscription must not be paid")
	}
}

// nil receivers are reached through optional subscription pointers.
func TestManagedHelpersTolerateNil(t *testing.T) {
	var sub *Subscription
	if sub.IsManaged() || sub.ManagedExpired() {
		t.Error("nil must answer false rather than panic")
	}
}

// A workspace paying Stripe for one plan and granted another must go back to
// the plan it pays for when the grant ends. Writing the grant over plan_id
// would have stranded it on the granted plan forever.
func TestEffectivePlanIDKeepsThePaidPlanIntact(t *testing.T) {
	paid := uuid.New()
	granted := uuid.New()
	now := time.Now()

	sub := managed(ptr(now.Add(-time.Hour)), nil)
	sub.PlanID = paid
	sub.ManagedPlanID = &granted

	if got := sub.EffectivePlanID(); got != granted {
		t.Errorf("an in-force grant should decide entitlements, got %v want %v", got, granted)
	}

	// The grant lapses; the plan they pay for is untouched underneath.
	sub.ManagedUntil = ptr(now.Add(-time.Minute))
	if got := sub.EffectivePlanID(); got != paid {
		t.Errorf("a lapsed grant must fall back to the paid plan, got %v want %v", got, paid)
	}
	if sub.PlanID != paid {
		t.Error("the paid plan must never be overwritten by a grant")
	}
}

// A grant recorded without a plan id entitles nothing, so it must not shadow
// the real plan. The migration's CHECK refuses to store one, but the helper
// should not depend on that to be safe.
func TestEffectivePlanIDIgnoresAGrantWithNoPlan(t *testing.T) {
	paid := uuid.New()
	sub := managed(ptr(time.Now().Add(-time.Hour)), nil)
	sub.PlanID = paid
	sub.ManagedPlanID = nil

	if got := sub.EffectivePlanID(); got != paid {
		t.Errorf("got %v, want the paid plan %v", got, paid)
	}
}

func TestEffectivePlanIDTeleratesNil(t *testing.T) {
	var sub *Subscription
	if got := sub.EffectivePlanID(); got != uuid.Nil {
		t.Errorf("nil should answer uuid.Nil, got %v", got)
	}
}
