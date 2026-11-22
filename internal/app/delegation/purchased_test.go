package delegation

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
)

type brokerStub struct {
	mu        sync.Mutex
	simulated bool
	calls     []string
	refuse    map[string]bool
}

func (b *brokerStub) GoogleToken(_ context.Context, _ uuid.UUID, email string, scopes []string) (*oauth2.Token, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, email+" "+strings.Join(scopes, " "))
	if b.refuse[email] {
		return nil, errors.New("broker: no such mailbox")
	}
	return &oauth2.Token{AccessToken: "brokered-" + email, TokenType: "Bearer", Expiry: time.Now().Add(time.Hour)}, nil
}

func (b *brokerStub) Simulated() bool { return b.simulated }

// gmailRequiring answers Gmail's profile only for the brokered token of the address.
func gmailRequiring(t *testing.T, address string, hits *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		if r.Header.Get("Authorization") != "Bearer brokered-"+address {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		writeJSON(w, map[string]any{"emailAddress": address})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestConnectPurchasedGoogleUsesTheBrokeredToken(t *testing.T) {
	s, p, grants, mb, st := newTestService(t)
	broker := &brokerStub{}
	s.WirePurchasedTokens(broker)
	hits := 0
	s.gmailBase = gmailRequiring(t, "ada@getacme.com", &hits).URL
	org, user := uuid.New(), uuid.New()

	acc, xerr := s.ConnectPurchased(context.Background(), org, user, models.GrantProviderGoogle, "Ada@getacme.com", "Ada Lovelace", "", true)
	if xerr != nil || acc == nil {
		t.Fatalf("connect: %v", xerr)
	}
	if hits == 0 {
		t.Fatal("the connect never proved the brokered token against Gmail")
	}
	if len(p.subjects) != 0 {
		t.Fatalf("the instance's service account minted %d tokens for a purchased mailbox", len(p.subjects))
	}
	if len(mb.connected) != 1 || mb.connected[0].Subject != "ada@getacme.com" || mb.connected[0].Provider != models.InboxProviderGoogle {
		t.Fatalf("connected = %+v", mb.connected)
	}
	if !mb.connected[0].Purchased {
		t.Fatal("a purchased mailbox must connect past the mailbox allowance")
	}
	g := grants.grants[mb.connected[0].GrantID]
	if g == nil || !g.Purchased || g.OrganizationID != org {
		t.Fatalf("grant = %+v, want a purchased grant in the workspace", g)
	}

	// The worker's token for the mailbox comes from the broker too, with the scope it asks for.
	st.delegation = &models.DelegatedMailbox{AccountID: acc.ID, OrganizationID: org, Provider: models.InboxProviderGoogle,
		Email: "ada@getacme.com", Subject: "ada@getacme.com", GrantID: g.ID}
	tok, handled, xerr := s.IMAPAccessToken(context.Background(), acc.ID)
	if !handled || xerr != nil || tok.AccessToken != "brokered-ada@getacme.com" {
		t.Fatalf("imap token = %+v %v %v", tok, handled, xerr)
	}
	if last := broker.calls[len(broker.calls)-1]; !strings.HasSuffix(last, "https://mail.google.com/") {
		t.Fatalf("imap token asked for %q", last)
	}
	if len(p.subjects) != 0 {
		t.Fatal("the worker's token was signed by the instance's service account")
	}
}

func TestConnectPurchasedGoogleInTheSimulatorNeverCallsGoogle(t *testing.T) {
	s, p, _, mb, _ := newTestService(t)
	s.WirePurchasedTokens(&brokerStub{simulated: true})
	hits := 0
	s.gmailBase = gmailRequiring(t, "nobody", &hits).URL

	if _, xerr := s.ConnectPurchased(context.Background(), uuid.New(), uuid.New(), models.GrantProviderGoogle, "bob@getacme.com", "Bob", "", true); xerr != nil {
		t.Fatalf("connect: %v", xerr)
	}
	if hits != 0 || len(p.subjects) != 0 {
		t.Fatalf("the simulator's connect called Google (%d gmail, %d service account)", hits, len(p.subjects))
	}
	if len(mb.connected) != 1 || mb.connected[0].Email != "bob@getacme.com" {
		t.Fatalf("connected = %+v", mb.connected)
	}
}

func TestConnectPurchasedGoogleByDelegationUsesTheInstanceServiceAccount(t *testing.T) {
	s, p, grants, mb, _ := newTestService(t)
	s.WirePurchasedTokens(&brokerStub{})
	id, scopes, ok := s.GoogleAppIdentity()
	if !ok || id != "123456789" || !slices.Contains(scopes, "https://mail.google.com/") || !slices.Contains(scopes, config.GoogleDirectoryScope) {
		t.Fatalf("identity = %q %v %v", id, scopes, ok)
	}
	org := uuid.New()
	acc, xerr := s.ConnectPurchased(context.Background(), org, uuid.New(), models.GrantProviderGoogle, "bob@getacme.com", "Bob", "ada@getacme.com", false)
	if xerr != nil || acc == nil {
		t.Fatalf("connect: %v", xerr)
	}
	if len(p.subjects) == 0 {
		t.Fatal("the instance's service account was never used")
	}
	g := grants.grants[mb.connected[0].GrantID]
	if g == nil || g.Purchased || g.AdminEmail != "ada@getacme.com" || !mb.connected[0].Purchased {
		t.Fatalf("grant = %+v, want an ordinary grant read through the admin mailbox", g)
	}
}

// A brokered token proves one mailbox: one that opens a different mailbox records nothing.
func TestConnectPurchasedRefusesATokenForAnotherMailbox(t *testing.T) {
	s, _, grants, mb, _ := newTestService(t)
	s.WirePurchasedTokens(&brokerStub{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"emailAddress": "someone@partner-infra.test"})
	}))
	t.Cleanup(srv.Close)
	s.gmailBase = srv.URL

	_, xerr := s.ConnectPurchased(context.Background(), uuid.New(), uuid.New(), models.GrantProviderGoogle, "ceo@elsewhere.test", "", "", true)
	if xerr == nil || xerr.Identifier != ErrIDMailboxUnreachable {
		t.Fatalf("connect = %v, want a refusal", xerr)
	}
	if len(grants.grants) != 0 || len(mb.connected) != 0 {
		t.Fatalf("recorded %d grants and %d mailboxes for an unproved address", len(grants.grants), len(mb.connected))
	}
}

// A partner's delegation connects only under a grant the workspace already holds, or on a domain an operator vouched for.
func TestPartnerDelegationNeedsAProvenGrantOrAVouchedDomain(t *testing.T) {
	ctx := context.Background()
	org, other, user := uuid.New(), uuid.New(), uuid.New()

	t.Run("unproven domain records nothing", func(t *testing.T) {
		s, p, grants, mb, _ := newTestService(t)
		s.WirePurchasedTokens(&brokerStub{})
		for _, provider := range []string{models.GrantProviderGoogle, models.GrantProviderMicrosoft} {
			if _, xerr := s.ConnectPartnerDelegated(ctx, org, user, provider, "ceo@victim.test", "", false); xerr == nil || xerr.Identifier != ErrIDProof {
				t.Fatalf("%s connect = %v, want a refusal", provider, xerr)
			}
		}
		if len(grants.grants) != 0 || len(mb.connected) != 0 || len(p.subjects) != 0 {
			t.Fatalf("grants=%d mailboxes=%d service-account tokens=%d", len(grants.grants), len(mb.connected), len(p.subjects))
		}
	})

	t.Run("another workspace's grant or a brokered grant does not count", func(t *testing.T) {
		s, p, grants, mb, _ := newTestService(t)
		s.WirePurchasedTokens(&brokerStub{})
		grants.grants[uuid.New()] = &models.DomainGrant{ID: uuid.New(), OrganizationID: other, Provider: models.GrantProviderGoogle,
			Tenant: "victim.test", Domains: []string{"victim.test"}, Status: "active", CreatedAt: time.Now()}
		brokered := uuid.New()
		grants.grants[brokered] = &models.DomainGrant{ID: brokered, OrganizationID: org, Provider: models.GrantProviderGoogle,
			Tenant: "victim.test", Domains: []string{"victim.test"}, Status: "active", Purchased: true, CreatedAt: time.Now()}
		if _, xerr := s.ConnectPartnerDelegated(ctx, org, user, models.GrantProviderGoogle, "ceo@victim.test", "", false); xerr == nil || xerr.Identifier != ErrIDProof {
			t.Fatalf("connect = %v, want a refusal", xerr)
		}
		if len(grants.grants) != 2 || len(mb.connected) != 0 || len(p.subjects) != 0 {
			t.Fatalf("grants=%d mailboxes=%d service-account tokens=%d", len(grants.grants), len(mb.connected), len(p.subjects))
		}
	})

	t.Run("the workspace's own grant connects without a new one", func(t *testing.T) {
		s, _, grants, mb, _ := newTestService(t)
		proven := uuid.New()
		grants.grants[proven] = &models.DomainGrant{ID: proven, OrganizationID: org, Provider: models.GrantProviderGoogle,
			Tenant: "acme.test", AdminEmail: "admin@acme.test", Domains: []string{"acme.test"}, Status: "active", CreatedAt: time.Now()}
		if _, xerr := s.ConnectPartnerDelegated(ctx, org, user, models.GrantProviderGoogle, "Bob@acme.test", "Bob", false); xerr != nil {
			t.Fatalf("connect: %v", xerr)
		}
		if len(grants.grants) != 1 || len(mb.connected) != 1 || mb.connected[0].GrantID != proven {
			t.Fatalf("grants=%d connected=%+v", len(grants.grants), mb.connected)
		}
	})

	t.Run("a vouched domain may record its grant", func(t *testing.T) {
		s, _, grants, mb, _ := newTestService(t)
		if _, xerr := s.ConnectPartnerDelegated(ctx, org, user, models.GrantProviderGoogle, "bob@partner-infra.test", "Bob", true); xerr != nil {
			t.Fatalf("connect: %v", xerr)
		}
		if len(grants.grants) != 1 || len(mb.connected) != 1 {
			t.Fatalf("grants=%d mailboxes=%d", len(grants.grants), len(mb.connected))
		}
		for _, g := range grants.grants {
			if g.OrganizationID != org || g.Purchased || !slices.Equal(g.Domains, []string{"partner-infra.test"}) {
				t.Fatalf("grant = %+v", g)
			}
		}
	})
}

// A release that cannot broker marks a purchased grant invalid and stops its mailboxes; the health check puts back the
// grant and every mailbox the broker still reaches, and only those, however often it runs.
func TestHealthCheckRestoresPurchasedGrantsAfterARollback(t *testing.T) {
	s, _, grants, mb, st := newTestService(t)
	broker := &brokerStub{refuse: map[string]bool{"gone@getacme.com": true}}
	s.WirePurchasedTokens(broker)
	org, id := uuid.New(), uuid.New()
	g := &models.DomainGrant{ID: id, OrganizationID: org, Provider: models.GrantProviderGoogle, Tenant: "getacme.com",
		AdminEmail: "ada@getacme.com", Domains: []string{"getacme.com"}, Status: "invalid", LastError: "unauthorized", Purchased: true, CreatedAt: time.Now()}
	grants.grants[id] = g
	ada, gone := uuid.New(), uuid.New()
	st.delegations = map[uuid.UUID]*models.DelegatedMailbox{
		ada:  {AccountID: ada, OrganizationID: org, Provider: models.InboxProviderGoogle, Email: "ada@getacme.com", GrantID: id},
		gone: {AccountID: gone, OrganizationID: org, Provider: models.InboxProviderGoogle, Email: "gone@getacme.com", GrantID: id},
	}
	grants.inactive = map[uuid.UUID][]uuid.UUID{id: {ada, gone}}

	if xerr := s.check(context.Background(), g); xerr != nil {
		t.Fatalf("check: %v", xerr)
	}
	if grants.grants[id].Status != "active" {
		t.Fatalf("grant status = %q, want active", grants.grants[id].Status)
	}
	if !slices.Equal(mb.reactivated, []uuid.UUID{ada}) {
		t.Fatalf("reactivated %v, want only the mailbox the broker reaches", mb.reactivated)
	}
	// ada is back at work; gone is still out. A second pass changes nothing.
	grants.inactive[id] = []uuid.UUID{gone}
	if xerr := s.check(context.Background(), grants.grants[id]); xerr != nil {
		t.Fatalf("second check: %v", xerr)
	}
	if !slices.Equal(mb.reactivated, []uuid.UUID{ada}) || grants.grants[id].Status != "active" {
		t.Fatalf("second pass reactivated %v, status %q", mb.reactivated, grants.grants[id].Status)
	}
}
