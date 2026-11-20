package store

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// A purchased Microsoft 365 mailbox whose tenant has not approved the app waits for its administrator instead of
// failing, offers the approval link back to this dashboard, and connects once the consent wakes it.
func TestMicrosoftMailboxWaitsForTenantConsent(t *testing.T) {
	t.Setenv("APP_URL", "https://app.warmbly.test")
	t.Setenv("APP_ORIGIN", "")
	h := newHarness(t)
	h.fund(50)
	ctx := context.Background()
	h.delegator.awaitConsent = true
	_, o := h.buy(t, domainAndMailboxes("acmeoutreach.com", graphkit.ProviderMicrosoft, 1))
	h.settle(t)
	got, err := h.s.GetOrder(ctx, h.org, o.ID)
	if err != nil || len(got.Mailboxes) != 1 {
		t.Fatalf("order = %+v %v", got, err)
	}
	m := got.Mailboxes[0]
	if m.ConnectState != models.StoreConnectWaiting || m.ActionRequired == nil || *m.ActionRequired != models.ActionMicrosoftConsent || m.EmailAccountID != nil {
		t.Fatalf("waiting mailbox = %+v", m)
	}
	calls := len(h.delegator.calls)
	h.settle(t)
	if len(h.delegator.calls) != calls {
		t.Fatalf("a mailbox waiting on its administrator was retried at once: %v", h.delegator.calls)
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
	if m := got.Mailboxes[0]; m.ConnectState != models.StoreConnectConnected || m.ActionRequired != nil || m.EmailAccountID == nil {
		t.Fatalf("after consent = %+v", m)
	}
}
