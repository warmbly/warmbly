package vendorconn

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/app/mailboximport"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailvendor"
)

func TestConsentStepWaitsThenEnds(t *testing.T) {
	start := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	st := consentState{RequestID: "ws:1", Started: start, Polled: start, Stage: "queued"}

	if st.duePoll(start.Add(time.Minute)) || !st.duePoll(start.Add(consentPollEvery)) {
		t.Fatal("status polled off its spacing")
	}
	if (consentState{Started: start}).duePoll(start.Add(time.Hour)) {
		t.Fatal("polled a request the vendor never named")
	}

	next, ended, _ := consentStep(st, &mailvendor.AuthorizationStatus{State: mailvendor.AuthorizationPending, Stage: "processing"}, start.Add(10*time.Minute))
	if ended || next.Stage != "processing" || !next.Polled.Equal(start.Add(10*time.Minute)) {
		t.Fatalf("pending step = %+v, %v", next, ended)
	}
	if _, ended, reason := consentStep(st, &mailvendor.AuthorizationStatus{State: mailvendor.AuthorizationFailed, Reason: "Login challenge"}, start.Add(time.Minute)); !ended || reason != "Login challenge" {
		t.Fatalf("failed step = %v %q", ended, reason)
	}

	done, ended, _ := consentStep(st, &mailvendor.AuthorizationStatus{State: mailvendor.AuthorizationCompleted}, start.Add(time.Minute))
	if ended || done.Completed.IsZero() {
		t.Fatalf("a completed request ended before Google could return: %+v", done)
	}
	if _, ended, reason := consentStep(done, nil, done.Completed.Add(authSettle+time.Second)); !ended || !strings.Contains(reason, "never returned") {
		t.Fatalf("a completed request with no callback = %v %q", ended, reason)
	}
	if _, ended, reason := consentStep(consentState{Started: start}, nil, start.Add(consentMaxWait+time.Second)); !ended || !strings.Contains(reason, "six hours") {
		t.Fatalf("an unnamed request past its wait = %v %q", ended, reason)
	}
}

// consentClient is a vendor that trusts and signs in, counting each request.
type consentClient struct {
	fakeClient
	trusts, consents int
	trustState       string
	consentErr       error
	budget           int
	lastURL          string
}

func (c *consentClient) TrustApp(context.Context, mailvendor.AppTrust) (string, error) {
	c.trusts++
	return "ws:t1", nil
}
func (c *consentClient) TrustStatus(context.Context, string) (mailvendor.AuthorizationStatus, error) {
	return mailvendor.AuthorizationStatus{State: c.trustState}, nil
}
func (c *consentClient) RequestConsent(_ context.Context, r mailvendor.ConsentRequest) (string, error) {
	c.consents++
	c.lastURL = r.URL
	if c.consentErr != nil {
		return "", c.consentErr
	}
	return "ws:c1", nil
}
func (c *consentClient) ConsentStatus(context.Context, string) (mailvendor.AuthorizationStatus, error) {
	return mailvendor.AuthorizationStatus{State: mailvendor.AuthorizationPending, Stage: "processing"}, nil
}
func (c *consentClient) ConsentBudget() (int, time.Duration) { return c.budget, 7 * 24 * time.Hour }

type fakeLinks struct{ minted []models.VendorConsentRequest }

func (f *fakeLinks) VendorConsentApp() (string, string, bool) { return "client-1", "Warmbly", true }
func (f *fakeLinks) VendorConsentURL(_ context.Context, req models.VendorConsentRequest) (string, *errx.Error) {
	f.minted = append(f.minted, req)
	return "https://accounts.google.com/o/oauth2/auth?state=vc." + uuid.NewString(), nil
}

func consentFixture(t *testing.T, client *consentClient) (*Service, *fakeLinks, uuid.UUID, *models.VendorConnection) {
	t.Helper()
	redisURL := os.Getenv("WARMBLY_TEST_REDIS")
	if redisURL == "" {
		t.Skip("WARMBLY_TEST_REDIS not set")
	}
	c, err := cache.New(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	links := &fakeLinks{}
	repo := &memVendors{conns: map[uuid.UUID]*models.VendorConnection{}}
	s := NewService(Deps{
		Repo: repo, Cipher: cipher.NewStatic([]byte(strings.Repeat("k", 32))), Cache: c, Consent: links,
		Mailboxes: &fakeMailboxes{existing: map[string]models.EmailRef{}, links: map[uuid.UUID]string{}},
		NewClient: func(string, map[string]string) (mailvendor.Client, error) { return client, nil },
	})
	org := uuid.New()
	conn, xerr := s.Create(context.Background(), org, uuid.New(), CreateInput{Vendor: "inboxkit", Fields: map[string]string{"api_key": "k"}})
	if xerr != nil {
		t.Fatal(xerr)
	}
	return s, links, org, conn
}

func consentRequest(org uuid.UUID, conn *models.VendorConnection, email, mailbox string) mailboximport.VendorAuthorizeRequest {
	return mailboximport.VendorAuthorizeRequest{OrgID: org, UserID: uuid.New(), ConnectionID: conn.ID, ImportID: uuid.New(), Line: 2,
		Email: email, Provider: models.GrantProviderGoogle, VendorMailboxID: mailbox, Consent: true}
}

func TestConsentTrustsTheDomainOnceAndSignsInEachMailboxOnce(t *testing.T) {
	client := &consentClient{fakeClient: fakeClient{boxes: []mailvendor.Mailbox{
		{ID: "ws:m1", Email: "ava@acme.io", Domain: "acme.io"}, {ID: "ws:m2", Email: "leo@acme.io", Domain: "acme.io", Admin: true},
	}}, trustState: mailvendor.AuthorizationPending}
	s, links, org, conn := consentFixture(t, client)
	ctx := context.Background()
	ava, leo := consentRequest(org, conn, "ava@acme.io", "ws:m1"), consentRequest(org, conn, "leo@acme.io", "ws:m2")

	if a := s.AuthorizeMailbox(ctx, ava); !a.Pending || !a.Trusting || !a.Domain {
		t.Fatalf("first ask = %+v", a)
	}
	if a := s.AuthorizeMailbox(ctx, leo); !a.Pending || !a.Trusting || client.trusts != 1 {
		t.Fatalf("second row = %+v, %d trust requests", a, client.trusts)
	}
	client.trustState = mailvendor.AuthorizationCompleted
	a := s.AuthorizeMailbox(ctx, ava)
	if !a.Pending || !a.SigningIn || a.Domain || client.consents != 1 || len(links.minted) != 1 {
		t.Fatalf("after trust = %+v, %d consents, %d links", a, client.consents, len(links.minted))
	}
	if m := links.minted[0]; m.OrganizationID != org || m.ConnectionID != conn.ID || m.Email != "ava@acme.io" || m.ImportID != ava.ImportID || m.Line != 2 {
		t.Fatalf("link minted for %+v", m)
	}
	for i := 0; i < 3; i++ {
		if a := s.AuthorizeMailbox(ctx, ava); !a.Pending || !a.SigningIn {
			t.Fatalf("pending ask = %+v", a)
		}
	}
	if client.consents != 1 || client.trusts != 1 {
		t.Fatalf("a pending mailbox was asked again: %d consents, %d trusts", client.consents, client.trusts)
	}

	s.ConsentSettled(ctx, org, conn.ID, "ava@acme.io", "The account that signed in is not the mailbox this link was made for.")
	if a := s.AuthorizeMailbox(ctx, ava); a.Pending || !strings.Contains(a.Message, "did not connect") || client.consents != 1 {
		t.Fatalf("after a refused callback = %+v, %d consents", a, client.consents)
	}
}

func TestConsentHoldsToTheVendorsBudget(t *testing.T) {
	client := &consentClient{fakeClient: fakeClient{boxes: []mailvendor.Mailbox{{ID: "ws:m1", Email: "ava@acme.io", Domain: "acme.io"}}},
		trustState: mailvendor.AuthorizationCompleted, budget: 1}
	s, _, org, conn := consentFixture(t, client)
	ctx := context.Background()
	t.Cleanup(func() { _ = s.cache.Del(ctx, consentBudgetKey(conn.ID, "ws:m1")).Err() })
	ava := consentRequest(org, conn, "ava@acme.io", "ws:m1")
	_ = s.AuthorizeMailbox(ctx, ava) // trust
	if a := s.AuthorizeMailbox(ctx, ava); !a.SigningIn || client.consents != 1 {
		t.Fatalf("first sign-in = %+v", a)
	}
	s.ConsentSettled(ctx, org, conn.ID, "ava@acme.io", "")
	if a := s.AuthorizeMailbox(ctx, ava); a.Pending || a.Message != "" || client.consents != 1 {
		t.Fatalf("after a connect = %+v, %d consents", a, client.consents)
	}
	// Asked for again, as a reconnect does: the vendor's limit still holds.
	_ = s.cache.Del(ctx, consentKey(org, conn.ID, "ava@acme.io")).Err()
	if a := s.AuthorizeMailbox(ctx, ava); a.Pending || !strings.Contains(a.Message, "1 sign-in requests per mailbox every 7 days") || client.consents != 1 {
		t.Fatalf("over budget = %+v, %d consents", a, client.consents)
	}
}

func TestConsentNeverResendsWhatTheVendorMayHold(t *testing.T) {
	client := &consentClient{fakeClient: fakeClient{boxes: []mailvendor.Mailbox{{ID: "ws:m1", Email: "ava@acme.io", Domain: "acme.io"}}},
		trustState: mailvendor.AuthorizationCompleted, consentErr: mailvendor.ErrInProgress}
	s, _, org, conn := consentFixture(t, client)
	ctx := context.Background()
	ava := consentRequest(org, conn, "ava@acme.io", "ws:m1")
	_ = s.AuthorizeMailbox(ctx, ava)
	for i := 0; i < 3; i++ {
		if a := s.AuthorizeMailbox(ctx, ava); !a.Pending || !a.SigningIn {
			t.Fatalf("in progress = %+v", a)
		}
	}
	if client.consents != 1 {
		t.Fatalf("%d consent requests for a mailbox with one open", client.consents)
	}
}

func TestConsentLeavesAnInFlightDelegationAlone(t *testing.T) {
	client := &consentClient{fakeClient: fakeClient{boxes: []mailvendor.Mailbox{{ID: "ws:m1", Email: "ava@acme.io", Domain: "acme.io", Admin: true}}}}
	s, _, org, conn := consentFixture(t, client)
	ctx := context.Background()
	key := authKey(org, conn.ID, models.GrantProviderGoogle, "acme.io")
	// A domain-wide delegation request an earlier release opened.
	if err := s.cache.SetJSON(ctx, key, authState{RequestID: "ws:old", Started: time.Now()}, time.Minute); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.cache.Del(ctx, key).Err() })
	if !s.delegationInFlight(ctx, org, conn.ID, "acme.io") {
		t.Fatal("the earlier release's request was not read")
	}
	_ = s.AuthorizeMailbox(ctx, consentRequest(org, conn, "ava@acme.io", "ws:m1"))
	if client.trusts != 0 || client.consents != 0 {
		t.Fatalf("consent started over an open delegation: %d trusts, %d consents", client.trusts, client.consents)
	}
}

// delegatingClient is a vendor that would open a domain-wide request if asked.
type delegatingClient struct {
	consentClient
	authorizations int
}

func (c *delegatingClient) AuthorizeApp(context.Context, mailvendor.AppAuthorization) (string, error) {
	c.authorizations++
	return "ws:d1", nil
}
func (c *delegatingClient) AuthorizationStatus(context.Context, string) (mailvendor.AuthorizationStatus, error) {
	return mailvendor.AuthorizationStatus{State: mailvendor.AuthorizationPending}, nil
}

type appGrants struct{}

func (appGrants) AppIdentity(string) (string, []string, []string, bool) {
	return "sa-client", []string{"https://mail.google.com/"}, nil, true
}
func (appGrants) GrantFor(context.Context, uuid.UUID, string, string) (*models.DomainGrant, error) {
	return nil, nil
}
func (appGrants) GrantFromVendor(context.Context, uuid.UUID, uuid.UUID, string, string, string, []string) (*models.DomainGrant, *errx.Error) {
	return nil, errx.ErrNotFound
}

func TestANewGoogleDomainIsNeverDelegated(t *testing.T) {
	client := &delegatingClient{consentClient: consentClient{fakeClient: fakeClient{boxes: []mailvendor.Mailbox{
		{ID: "ws:m1", Email: "ava@acme.io", Domain: "acme.io", Admin: true}}}}}
	s, _, org, conn := consentFixture(t, &client.consentClient)
	s.newClient = func(string, map[string]string) (mailvendor.Client, error) { return client, nil }
	s.forget(conn.ID)
	s.grants, s.consent = appGrants{}, nil
	ctx := context.Background()
	if a := s.AuthorizeMailbox(ctx, consentRequest(org, conn, "ava@acme.io", "ws:m1")); a.Pending || a.GrantID != nil {
		t.Fatalf("google without vendor sign-in = %+v", a)
	}
	if client.authorizations != 0 {
		t.Fatalf("%d domain-wide requests opened for a Google domain", client.authorizations)
	}
	ms := consentRequest(org, conn, "ava@acme.io", "ws:m1")
	ms.Provider = models.GrantProviderMicrosoft
	if a := s.AuthorizeMailbox(ctx, ms); !a.Pending || client.authorizations != 1 {
		t.Fatalf("microsoft = %+v, %d requests", a, client.authorizations)
	}
	_ = s.cache.Del(ctx, authKey(org, conn.ID, models.GrantProviderMicrosoft, "acme.io")).Err()
}
