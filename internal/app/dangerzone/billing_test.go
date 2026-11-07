package dangerzone

import (
	"context"
	"testing"

	"github.com/stripe/stripe-go/v86"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/repository"
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

type fakeBilling struct {
	subs      map[string]*stripe.Subscription
	reasserts []string
}

func (f *fakeBilling) GetSubscription(_ context.Context, id string) (*stripe.Subscription, *errx.Error) {
	return f.subs[id], nil
}

func (f *fakeBilling) CancelSubscription(_ context.Context, id string, atPeriodEnd bool) *errx.Error {
	if atPeriodEnd {
		f.reasserts = append(f.reasserts, id)
	}
	return nil
}

// A subscription checked as ending is told to end again after the commit, so a
// resume landing between the check and the commit does not keep billing.
func TestAccountDeletionReassertsEndingSubscriptions(t *testing.T) {
	billing := &fakeBilling{subs: map[string]*stripe.Subscription{
		"sub_ending":   {Status: stripe.SubscriptionStatusActive, CancelAtPeriodEnd: true},
		"sub_canceled": {Status: stripe.SubscriptionStatusCanceled},
	}}
	s := &service{billing: billing}
	var ending []string
	if err := s.billingCheck(&ending)(context.Background(), []string{"sub_ending", "sub_canceled"}); err != nil {
		t.Fatal(err)
	}
	// Still ending at commit: nothing to undo.
	s.keepEnding(context.Background(), ending)
	if len(billing.reasserts) != 0 {
		t.Errorf("reasserted %v on a subscription that never resumed", billing.reasserts)
	}
	// Canceled before the reassertion: left alone, not reported.
	billing.subs["sub_ending"] = &stripe.Subscription{Status: stripe.SubscriptionStatusCanceled}
	s.keepEnding(context.Background(), ending)
	if len(billing.reasserts) != 0 {
		t.Errorf("reasserted %v on a canceled subscription", billing.reasserts)
	}
	// Resumed between the check and the commit: ended again.
	billing.subs["sub_ending"] = &stripe.Subscription{Status: stripe.SubscriptionStatusActive}
	s.keepEnding(context.Background(), ending)
	if len(billing.reasserts) != 1 || billing.reasserts[0] != "sub_ending" {
		t.Errorf("reasserted %v, want [sub_ending]", billing.reasserts)
	}

	billing.subs["sub_resumed"] = &stripe.Subscription{Status: stripe.SubscriptionStatusActive}
	if err := s.billingCheck(&ending)(context.Background(), []string{"sub_resumed"}); err != repository.ErrOwnedWorkspaceBilled {
		t.Errorf("a renewing subscription: got %v, want ErrOwnedWorkspaceBilled", err)
	}
}
