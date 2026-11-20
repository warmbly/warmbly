package delegation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/warmbly/warmbly/internal/models"
)

const (
	acmeTenant  = "22222222-2222-2222-2222-222222222222"
	otherTenant = "33333333-3333-3333-3333-333333333333"
)

// tenantFake is Microsoft for the tenant consent: getacme.com signs in to acmeTenant, the app's client-credentials
// token works only in consented tenants, and Graph refuses the first graphRefusals calls as a fresh consent does.
type tenantFake struct {
	mu            sync.Mutex
	consented     map[string]bool
	graphRefusals int
	graphCalls    int
	// signinTenant is the tid the administrator's sign-in names; roles are its directory roles (nil: Global Administrator).
	signinTenant string
	roles        []any
	states       *memStates
}

func newTenantFake(t *testing.T, s *Service, states *memStates) *tenantFake {
	t.Helper()
	f := &tenantFake{consented: map[string]bool{}, signinTenant: acmeTenant, states: states}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		_ = r.ParseForm()
		switch p := r.URL.Path; {
		case p == "/ms/getacme.com/v2.0/.well-known/openid-configuration":
			writeJSON(w, map[string]any{"issuer": "https://login.microsoftonline.com/" + acmeTenant + "/v2.0"})
		case p == "/ms/organizations/oauth2/v2.0/token":
			nonce := ""
			for _, st := range f.states.issued {
				if st.Verifier != "" && st.Verifier == r.Form.Get("code_verifier") {
					nonce = st.Nonce
				}
			}
			if nonce == "" {
				refusePKCE(w)
				return
			}
			roles := f.roles
			if roles == nil {
				roles = []any{"62e90394-69f5-4237-9190-012177145e10"}
			}
			writeJSON(w, map[string]any{"access_token": "x", "token_type": "Bearer", "expires_in": 3600,
				"id_token": fakeIDToken(map[string]any{"tid": f.signinTenant, "wids": roles, "nonce": nonce})})
		case strings.HasSuffix(p, "/oauth2/v2.0/token"):
			tenant := strings.Split(strings.TrimPrefix(p, "/ms/"), "/")[0]
			if !f.consented[tenant] {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"AADSTS7000229: The client application is missing service principal in the tenant."}`))
				return
			}
			writeJSON(w, map[string]any{"access_token": "app-" + tenant, "token_type": "Bearer", "expires_in": 3600})
		case p == "/graph/users":
			f.graphCalls++
			if f.graphRefusals > 0 {
				f.graphRefusals--
				w.WriteHeader(http.StatusForbidden)
				return
			}
			writeJSON(w, map[string]any{"value": []any{
				map[string]any{"id": "u-1", "mail": "ada@getacme.com", "userPrincipalName": "ada@getacme.onmicrosoft.com", "accountEnabled": true},
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	s.msLoginBase, s.graphBase = srv.URL+"/ms", srv.URL+"/graph"
	s.consentPause = 0
	return f
}

type wakeLog struct {
	mu    sync.Mutex
	calls []string
}

func (w *wakeLog) wake(_ context.Context, orgID uuid.UUID, domains []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, orgID.String()+" "+strings.Join(domains, ","))
}

func consentHarness(t *testing.T) (*Service, *tenantFake, *memGrants, *memStates, *wakeLog) {
	t.Helper()
	s, p, grants, _, _ := newTestService(t)
	f := newTenantFake(t, s, p.states)
	w := &wakeLog{}
	s.WireConsentWaker(w.wake)
	return s, f, grants, p.states, w
}

func outcome(t *testing.T, to string) url.Values {
	t.Helper()
	u, err := url.Parse(to)
	if err != nil || u.Host != "app.partner.test" || u.Path != "/w/1/mail" {
		t.Fatalf("redirect = %q", to)
	}
	return u.Query()
}

const returnTo = "https://app.partner.test/w/1/mail?tab=warmbly"

func TestPurchasedConsentLinkCarriesOnlyWhatMicrosoftNeeds(t *testing.T) {
	s, _, _, states, _ := consentHarness(t)
	org, user := uuid.New(), uuid.New()
	c, xerr := s.StartPurchasedMicrosoftConsent(context.Background(), org, user, " GetAcme.com ", returnTo)
	if xerr != nil || c.Consented {
		t.Fatalf("start = %+v %v", c, xerr)
	}
	u, err := url.Parse(c.URL)
	if err != nil || !strings.HasSuffix(u.Path, "/ms/organizations/v2.0/adminconsent") {
		t.Fatalf("url = %q", c.URL)
	}
	q := u.Query()
	if q.Get("client_id") != "app-id" || q.Get("scope") != "https://graph.microsoft.com/.default" ||
		q.Get("redirect_uri") != "https://api.example/addresses/outlook/callback" || !IsPurchasedConsentState(q.Get("state")) || len(q) != 4 {
		t.Fatalf("query = %v", q)
	}
	if d := time.Until(c.ExpiresAt); d < 59*time.Minute || d > time.Hour {
		t.Fatalf("expires in %s", d)
	}
	st, ok := states.Peek(context.Background(), "mailbox_grant_state:"+q.Get("state"))
	if !ok || st.OrgID != org || st.UserID != user || st.Domain != "getacme.com" || st.Tenant != acmeTenant || st.ReturnURL != returnTo || st.Verifier != "" {
		t.Fatalf("state = %+v", st)
	}
}

// The full round trip: consent, the administrator's sign-in, a short wait for the consent to reach the token
// endpoint, then the workspace's grant and a wake for the tenant's domains.
func TestPurchasedConsentRecordsTheGrantOnceAnAdministratorSignsIn(t *testing.T) {
	s, f, grants, _, wakes := consentHarness(t)
	ctx := context.Background()
	org, user := uuid.New(), uuid.New()
	c, _ := s.StartPurchasedMicrosoftConsent(ctx, org, user, "getacme.com", returnTo)
	state := stateOf(c.URL)

	f.consented[acmeTenant] = true
	f.graphRefusals = 2
	signin, ok := s.FinishPurchasedMicrosoftConsent(ctx, state, ConsentReturn{AdminConsent: "True", Tenant: acmeTenant})
	if !ok || !strings.Contains(signin, "/ms/organizations/oauth2/v2.0/authorize?") || !strings.Contains(signin, "code_challenge=") {
		t.Fatalf("consent leg = %q %v", signin, ok)
	}
	if len(grants.grants) != 0 {
		t.Fatal("a consent with no sign-in recorded a grant")
	}
	if _, ok := s.FinishPurchasedMicrosoftConsent(ctx, state, ConsentReturn{AdminConsent: "True", Tenant: acmeTenant}); ok {
		t.Fatal("the consent state was used twice")
	}
	to, ok := s.FinishPurchasedMicrosoftConsent(ctx, stateOf(signin), ConsentReturn{Code: "code-1"})
	if !ok {
		t.Fatal("sign-in leg refused")
	}
	if q := outcome(t, to); q.Get("status") != ConsentConnected || q.Get("domain") != "getacme.com" || q.Get("tab") != "warmbly" {
		t.Fatalf("outcome = %v", q)
	}
	if f.graphCalls < 3 {
		t.Fatalf("graph calls = %d, want the propagation retried", f.graphCalls)
	}
	if len(grants.grants) != 1 {
		t.Fatalf("grants = %d", len(grants.grants))
	}
	for _, g := range grants.grants {
		if g.OrganizationID != org || g.Provider != models.GrantProviderMicrosoft || g.Tenant != acmeTenant || !g.Purchased || !slices.Contains(g.Domains, "getacme.com") {
			t.Fatalf("grant = %+v", g)
		}
	}
	if len(wakes.calls) != 1 || !strings.HasPrefix(wakes.calls[0], org.String()+" ") || !strings.Contains(wakes.calls[0], "getacme.com") {
		t.Fatalf("wakes = %v", wakes.calls)
	}
	if _, ok := s.FinishPurchasedMicrosoftConsent(ctx, stateOf(signin), ConsentReturn{Code: "code-1"}); ok {
		t.Fatal("the sign-in state was used twice")
	}

	// With the grant working, a new link is not needed.
	again, xerr := s.StartPurchasedMicrosoftConsent(ctx, org, user, "getacme.com", returnTo)
	if xerr != nil || !again.Consented || again.URL != "" || len(wakes.calls) != 2 {
		t.Fatalf("second start = %+v %v wakes=%v", again, xerr, wakes.calls)
	}
	// Another workspace on the same tenant holds no grant, so it still needs its administrator.
	other, xerr := s.StartPurchasedMicrosoftConsent(ctx, uuid.New(), user, "getacme.com", returnTo)
	if xerr != nil || other.Consented || other.URL == "" {
		t.Fatalf("other workspace = %+v %v", other, xerr)
	}
}

func TestPurchasedConsentOutcomes(t *testing.T) {
	ctx := context.Background()
	start := func(t *testing.T, s *Service) string {
		t.Helper()
		c, xerr := s.StartPurchasedMicrosoftConsent(ctx, uuid.New(), uuid.New(), "getacme.com", returnTo)
		if xerr != nil {
			t.Fatal(xerr)
		}
		return stateOf(c.URL)
	}
	signedIn := func(t *testing.T, s *Service) string {
		t.Helper()
		to, ok := s.FinishPurchasedMicrosoftConsent(ctx, start(t, s), ConsentReturn{AdminConsent: "True", Tenant: acmeTenant})
		if !ok || !strings.Contains(to, "/authorize?") {
			t.Fatalf("consent leg = %q", to)
		}
		return stateOf(to)
	}
	cases := []struct {
		name string
		run  func(t *testing.T, s *Service, f *tenantFake) (string, bool)
		want string
	}{
		{"declined", func(t *testing.T, s *Service, _ *tenantFake) (string, bool) {
			return s.FinishPurchasedMicrosoftConsent(ctx, start(t, s), ConsentReturn{Error: "access_denied"})
		}, ConsentCancelled},
		{"declined at sign-in", func(t *testing.T, s *Service, _ *tenantFake) (string, bool) {
			return s.FinishPurchasedMicrosoftConsent(ctx, signedIn(t, s), ConsentReturn{Error: "access_denied"})
		}, ConsentCancelled},
		{"provider error", func(t *testing.T, s *Service, _ *tenantFake) (string, bool) {
			return s.FinishPurchasedMicrosoftConsent(ctx, start(t, s), ConsentReturn{Error: "server_error"})
		}, ConsentError},
		{"no admin_consent", func(t *testing.T, s *Service, _ *tenantFake) (string, bool) {
			return s.FinishPurchasedMicrosoftConsent(ctx, start(t, s), ConsentReturn{Tenant: acmeTenant})
		}, ConsentError},
		{"consent for another tenant", func(t *testing.T, s *Service, _ *tenantFake) (string, bool) {
			return s.FinishPurchasedMicrosoftConsent(ctx, start(t, s), ConsentReturn{AdminConsent: "True", Tenant: otherTenant})
		}, ConsentError},
		{"sign-in to another tenant", func(t *testing.T, s *Service, f *tenantFake) (string, bool) {
			f.consented[acmeTenant], f.signinTenant = true, otherTenant
			return s.FinishPurchasedMicrosoftConsent(ctx, signedIn(t, s), ConsentReturn{Code: "c"})
		}, ConsentError},
		{"sign-in without an admin role", func(t *testing.T, s *Service, f *tenantFake) (string, bool) {
			f.consented[acmeTenant], f.roles = true, []any{"b79fbf4d-3ef9-4689-8143-76b194e85509"}
			return s.FinishPurchasedMicrosoftConsent(ctx, signedIn(t, s), ConsentReturn{Code: "c"})
		}, ConsentError},
		{"consent never reaches the token endpoint", func(t *testing.T, s *Service, _ *tenantFake) (string, bool) {
			return s.FinishPurchasedMicrosoftConsent(ctx, signedIn(t, s), ConsentReturn{Code: "c"})
		}, ConsentError},
		{"graph keeps refusing", func(t *testing.T, s *Service, f *tenantFake) (string, bool) {
			f.consented[acmeTenant], f.graphRefusals = true, 100
			return s.FinishPurchasedMicrosoftConsent(ctx, signedIn(t, s), ConsentReturn{Code: "c"})
		}, ConsentError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, f, grants, _, wakes := consentHarness(t)
			to, ok := tc.run(t, s, f)
			if !ok {
				t.Fatal("no redirect")
			}
			if q := outcome(t, to); q.Get("status") != tc.want || q.Get("domain") != "getacme.com" {
				t.Fatalf("outcome = %v, want %s", q, tc.want)
			}
			if tc.want != ConsentConnected && (len(grants.grants) != 0 || len(wakes.calls) != 0) {
				t.Fatalf("grants=%d wakes=%v after %s", len(grants.grants), wakes.calls, tc.want)
			}
		})
	}
}

// An unknown, used or expired state redirects nowhere, whatever the query says.
func TestPurchasedConsentUnknownStateRedirectsNowhere(t *testing.T) {
	s, f, grants, states, _ := consentHarness(t)
	ctx := context.Background()
	f.consented[acmeTenant] = true
	for _, state := range []string{"", "mpc_unknown", "mac_whatever", "gac_x"} {
		if to, ok := s.FinishPurchasedMicrosoftConsent(ctx, state, ConsentReturn{AdminConsent: "True", Tenant: acmeTenant}); ok || to != "" {
			t.Fatalf("%q redirected to %q", state, to)
		}
	}
	c, _ := s.StartPurchasedMicrosoftConsent(ctx, uuid.New(), uuid.New(), "getacme.com", returnTo)
	// An hour later the store has dropped it.
	delete(states.m, "mailbox_grant_state:"+stateOf(c.URL))
	if _, ok := s.FinishPurchasedMicrosoftConsent(ctx, stateOf(c.URL), ConsentReturn{AdminConsent: "True", Tenant: acmeTenant}); ok {
		t.Fatal("an expired state redirected")
	}
	if len(grants.grants) != 0 {
		t.Fatal("a grant was recorded")
	}
}

// A purchased Microsoft 365 mailbox whose tenant has not consented is told apart from any other failure.
func TestConnectPurchasedMicrosoftWaitsForTenantConsent(t *testing.T) {
	s, f, grants, _, _ := consentHarness(t)
	ctx := context.Background()
	_, xerr := s.ConnectPurchased(ctx, uuid.New(), uuid.New(), models.GrantProviderMicrosoft, "ada@getacme.com", "Ada", "", false)
	if xerr == nil || xerr.Identifier != ErrIDMicrosoftTenantConsent || xerr.Message != TenantConsentMessage("getacme.com") {
		t.Fatalf("connect = %v", xerr)
	}
	f.consented[acmeTenant], f.graphRefusals = true, 1
	if _, xerr := s.ConnectPurchased(ctx, uuid.New(), uuid.New(), models.GrantProviderMicrosoft, "ada@getacme.com", "Ada", "", false); xerr == nil || xerr.Identifier != ErrIDMicrosoftTenantConsent {
		t.Fatalf("graph 403 = %v", xerr)
	}
	if len(grants.grants) != 0 {
		t.Fatalf("recorded %d grants without consent", len(grants.grants))
	}
}

func TestMicrosoftConsentMissingIsPrecise(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"graph 403":        {&apiError{status: http.StatusForbidden}, true},
		"graph 401":        {&apiError{status: http.StatusUnauthorized}, true},
		"graph 404":        {&apiError{status: http.StatusNotFound}, false},
		"graph 503":        {&apiError{status: http.StatusServiceUnavailable}, false},
		"network":          {context.DeadlineExceeded, false},
		"tenant not found": {retrieveErr("AADSTS90002: Tenant not found"), false},
		"missing SP":       {retrieveErr("AADSTS7000229: missing service principal"), true},
		"app not found":    {retrieveErr("AADSTS700016: Application not found in the directory"), true},
		"not consented":    {retrieveErr("AADSTS65001: The user or administrator has not consented"), true},
		"bad secret":       {retrieveErr("AADSTS7000215: Invalid client secret provided"), false},
	} {
		if got := microsoftConsentMissing(tc.err); got != tc.want {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}

func retrieveErr(desc string) error {
	return &oauth2.RetrieveError{Response: &http.Response{StatusCode: http.StatusBadRequest}, ErrorCode: "invalid_client", ErrorDescription: desc}
}
