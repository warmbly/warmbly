package dangerzone

import (
	"testing"

	"github.com/stripe/stripe-go/v86"
)

func TestSubscriptionRenews(t *testing.T) {
	for _, tc := range []struct {
		name string
		sub  stripe.Subscription
		want bool
	}{
		{"active", stripe.Subscription{Status: stripe.SubscriptionStatusActive}, true},
		{"past due", stripe.Subscription{Status: stripe.SubscriptionStatusPastDue}, true},
		{"ending at period end", stripe.Subscription{Status: stripe.SubscriptionStatusActive, CancelAtPeriodEnd: true}, false},
		{"ending on a date", stripe.Subscription{Status: stripe.SubscriptionStatusActive, CancelAt: 1}, false},
		{"canceled", stripe.Subscription{Status: stripe.SubscriptionStatusCanceled}, false},
		{"incomplete expired", stripe.Subscription{Status: stripe.SubscriptionStatusIncompleteExpired}, false},
	} {
		if got := subscriptionRenews(&tc.sub); got != tc.want {
			t.Errorf("%s: renews=%v, want %v", tc.name, got, tc.want)
		}
	}
}
