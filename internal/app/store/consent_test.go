package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/delegation"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// A purchased Microsoft 365 mailbox whose tenant has not approved the app is approved in the background with the
// domain's admin mailbox and connects, with nothing for anyone to do.
func TestMicrosoftMailboxApprovesItsTenantInTheBackground(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	h.delegator.awaitConsent = true
	_, o := h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderMicrosoft, 2))
	h.settle(t)
	got, err := h.s.GetOrder(ctx, h.org, o.ID)
	if err != nil || len(got.Mailboxes) != 2 {
		t.Fatalf("order = %+v %v", got, err)
	}
	admin := ""
	for _, m := range got.Mailboxes {
		if m.ConnectState != models.StoreConnectConnected || m.ActionRequired != nil || m.ActionReason != nil || m.EmailAccountID == nil {
			t.Fatalf("mailbox = %+v", m)
		}
		if m.IsAdmin {
			admin = m.Email
		}
	}
	// Both mailboxes may ask at once; the delegation service runs one consent per tenant for them.
	if admin == "" || len(h.delegator.autoConsents) == 0 {
		t.Fatalf("admin %q, automatic consents %v", admin, h.delegator.autoConsents)
	}
	for _, c := range h.delegator.autoConsents {
		if c != "acmeoutreach.com "+admin+" true" {
			t.Fatalf("automatic consent %q, want %s's sign-in", c, admin)
		}
	}
}

// When Microsoft wants something only a person can give, the mailbox waits on its administrator with the reason,
// nothing signs in again in the background, the approval link still works, and the consent wakes it.
func TestMicrosoftMailboxFallsBackToItsAdministrator(t *testing.T) {
	t.Setenv("APP_URL", "https://app.warmbly.test")
	t.Setenv("APP_ORIGIN", "")
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	h.delegator.awaitConsent = true
	h.delegator.autoConsent = &delegation.AutoConsentError{Reason: models.ConsentReasonMFA}
	_, o := h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderMicrosoft, 1))
	h.settle(t)
	got, err := h.s.GetOrder(ctx, h.org, o.ID)
	if err != nil || len(got.Mailboxes) != 1 {
		t.Fatalf("order = %+v %v", got, err)
	}
	m := got.Mailboxes[0]
	if m.ConnectState != models.StoreConnectWaiting || m.ActionRequired == nil || *m.ActionRequired != models.ActionMicrosoftConsent ||
		m.ActionReason == nil || *m.ActionReason != models.ConsentReasonMFA || m.EmailAccountID != nil {
		t.Fatalf("waiting mailbox = %+v", m)
	}
	calls := len(h.delegator.calls)
	h.settle(t)
	if len(h.delegator.calls) != calls {
		t.Fatalf("a mailbox waiting on its administrator was retried at once: %v", h.delegator.calls)
	}
	// The half-hourly check looks again, but never signs in with the password a second time.
	h.clock.advance(31 * time.Minute)
	h.settle(t)
	if len(h.delegator.calls) == calls || len(h.delegator.autoConsents) != 1 {
		t.Fatalf("after half an hour: connects %v, automatic consents %v", h.delegator.calls, h.delegator.autoConsents)
	}
	got, _ = h.s.GetOrder(ctx, h.org, o.ID)
	if m := got.Mailboxes[0]; m.ActionReason == nil || *m.ActionReason != models.ConsentReasonMFA {
		t.Fatalf("reason after the recheck = %+v", m)
	}

	link, err := h.s.MicrosoftConsent(ctx, h.org, h.user, m.DomainID, ConsentRequest{OrderID: o.ID.String()})
	if err != nil || link.URL == nil || link.Consented {
		t.Fatalf("link = %+v %v", link, err)
	}
	if h.delegator.consents[0] != "acmeoutreach.com https://app.warmbly.test/app/emails/buy/orders/"+o.ID.String() {
		t.Fatalf("consent = %q", h.delegator.consents[0])
	}
	// An order that is not the workspace's lands on the domains page.
	if _, err := h.s.MicrosoftConsent(ctx, h.org, h.user, m.DomainID, ConsentRequest{OrderID: uuid.NewString()}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(h.delegator.consents[1], " https://app.warmbly.test/app/emails/domains") {
		t.Fatalf("consent = %q", h.delegator.consents[1])
	}
	if _, err := h.s.MicrosoftConsent(ctx, uuid.New(), h.user, m.DomainID, ConsentRequest{}); code(err) != "not_found" {
		t.Fatalf("another workspace's domain = %v", err)
	}

	h.delegator.awaitConsent = false
	h.s.WakeMicrosoftConsent(ctx, h.org, []string{"acmeoutreach.com"})
	h.settle(t)
	got, _ = h.s.GetOrder(ctx, h.org, o.ID)
	if m := got.Mailboxes[0]; m.ConnectState != models.StoreConnectConnected || m.ActionRequired != nil || m.ActionReason != nil || m.EmailAccountID == nil {
		t.Fatalf("after consent = %+v", m)
	}
}

// Microsoft not answering is retried soon and never shown as a step for the administrator.
func TestMicrosoftMailboxRetriesAnUnavailableApproval(t *testing.T) {
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	h.delegator.awaitConsent = true
	h.delegator.autoConsent = &delegation.AutoConsentError{Reason: delegation.AutoConsentUnavailable}
	_, o := h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderMicrosoft, 1))
	h.settle(t)
	got, _ := h.s.GetOrder(ctx, h.org, o.ID)
	if m := got.Mailboxes[0]; m.ActionRequired != nil || m.ActionReason != nil || m.EmailAccountID != nil || m.ConnectState != models.StoreConnectFailed {
		t.Fatalf("unavailable = %+v", m)
	}
	h.delegator.autoConsent = nil
	h.clock.advance(11 * time.Minute)
	h.settle(t)
	got, _ = h.s.GetOrder(ctx, h.org, o.ID)
	if m := got.Mailboxes[0]; m.ConnectState != models.StoreConnectConnected || m.EmailAccountID == nil {
		t.Fatalf("after Microsoft answered = %+v", m)
	}
	if len(h.delegator.autoConsents) != 2 {
		t.Fatalf("automatic consents = %v, want one per attempt", h.delegator.autoConsents)
	}
}
