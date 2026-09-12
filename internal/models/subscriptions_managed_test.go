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
