package vendorconn

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/pkg/mailvendor"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestReconnectSignsInAgainAsTheLinkedMailboxes(t *testing.T) {
	client := &consentClient{fakeClient: fakeClient{boxes: []mailvendor.Mailbox{
		{ID: "ws:m1", Email: "ava@acme.io", Domain: "acme.io", Admin: true}, {ID: "ws:m2", Email: "leo@acme.io", Domain: "acme.io"},
	}}, trustState: mailvendor.AuthorizationCompleted}
	s, links, org, conn := consentFixture(t, client)
	s.kickReconnect = nil
	ctx := context.Background()
	repo := s.repo.(*memVendors)
	ava, leo := uuid.New(), uuid.New()
	repo.linked = []repository.VendorReconnect{
		{AccountID: ava, OrgID: org, ConnectionID: conn.ID, VendorMailboxID: "ws:m1", Email: "ava@acme.io"},
		{AccountID: leo, OrgID: org, ConnectionID: conn.ID, VendorMailboxID: "ws:m2", Email: "leo@acme.io"},
	}
	key := reconnectJobKey(org, conn.ID)
	t.Cleanup(func() {
		_ = s.cache.Del(ctx, key).Err()
		_ = s.cache.SRem(ctx, reconnectJobsIndex, key).Err()
	})

	if _, xerr := s.Reconnect(ctx, uuid.New(), uuid.New(), conn.ID, nil); xerr == nil {
		t.Fatal("another workspace reconnected the vendor account")
	}
	other := uuid.New()
	if _, xerr := s.Reconnect(ctx, org, uuid.New(), conn.ID, &other); xerr == nil || xerr.Identifier != ErrIDReconnectNone {
		t.Fatalf("a mailbox not linked to the connection = %v", xerr)
	}

	member := uuid.New()
	out, xerr := s.Reconnect(ctx, org, member, conn.ID, &ava)
	if xerr != nil || len(out) != 1 || out[0].EmailAccountID != ava || out[0].Status != ReconnectQueued {
		t.Fatalf("reconnect one = %+v, %v", out, xerr)
	}
	s.driveReconnectKey(ctx, key)
	if got, _ := s.ReconnectStatus(ctx, org, conn.ID); len(got) != 1 || got[0].Status != ReconnectTrusting || client.trusts != 1 || client.consents != 0 {
		t.Fatalf("first step = %+v, %d trusts, %d consents", got, client.trusts, client.consents)
	}
	s.driveReconnectKey(ctx, key)
	if len(links.minted) != 1 || links.minted[0].EmailAccountID == nil || *links.minted[0].EmailAccountID != ava ||
		links.minted[0].UserID != member || links.minted[0].OrganizationID != org || client.consents != 1 {
		t.Fatalf("minted %+v, %d consents", links.minted, client.consents)
	}
	got, _ := s.ReconnectStatus(ctx, org, conn.ID)
	if len(got) != 1 || got[0].Status != ReconnectSigningIn {
		t.Fatalf("status after the first step = %+v", got)
	}

	// Asking for every mailbox keeps the open one and adds the other; nothing is sent twice.
	if out, xerr := s.Reconnect(ctx, org, member, conn.ID, nil); xerr != nil || len(out) != 2 {
		t.Fatalf("reconnect all = %+v, %v", out, xerr)
	}
	s.driveReconnectKey(ctx, key)
	if client.consents != 2 {
		t.Fatalf("%d consent requests, want one per mailbox", client.consents)
	}

	s.ConsentSettled(ctx, org, conn.ID, "ava@acme.io", "")
	s.ConsentSettled(ctx, org, conn.ID, "leo@acme.io", "the account that signed in is someone else")
	s.driveReconnectKey(ctx, key)
	got, _ = s.ReconnectStatus(ctx, org, conn.ID)
	status := map[string]string{}
	for _, m := range got {
		status[m.Email] = m.Status
	}
	if status["ava@acme.io"] != ReconnectConnected || status["leo@acme.io"] != ReconnectFailed {
		t.Fatalf("final statuses = %+v", got)
	}
	if on, _ := s.cache.SIsMember(ctx, reconnectJobsIndex, key).Result(); on {
		t.Fatal("a finished job stayed in the loop's index")
	}
	if other, _ := s.ReconnectStatus(ctx, uuid.New(), conn.ID); other != nil {
		t.Fatal("another workspace read the reconnect")
	}
}

func TestReconnectNeedsAVendorThatSignsIn(t *testing.T) {
	s, _, org, conn := consentFixture(t, &consentClient{})
	s.kickReconnect = nil
	s.newClient = func(string, map[string]string) (mailvendor.Client, error) { return &fakeClient{}, nil }
	s.forget(conn.ID)
	if _, xerr := s.Reconnect(context.Background(), org, uuid.New(), conn.ID, nil); xerr == nil || xerr.Identifier != ErrIDReconnectUnsupported {
		t.Fatalf("a vendor that cannot sign in = %v", xerr)
	}
	s.consent = nil
	if _, xerr := s.Reconnect(context.Background(), org, uuid.New(), conn.ID, nil); xerr == nil || xerr.Identifier != ErrIDReconnectUnavailable ||
		!strings.Contains(xerr.Message, "Google sign-in") {
		t.Fatalf("no Google sign-in here = %v", xerr)
	}
}
