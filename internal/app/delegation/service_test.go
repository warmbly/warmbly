package delegation

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/domainproof"
)

// ---- fakes ----------------------------------------------------------------

type memGrants struct {
	mu     sync.Mutex
	grants map[uuid.UUID]*models.DomainGrant
	// awaiting is a domain with imported mailboxes waiting on their grant.
	awaiting bool
}

func (m *memGrants) Upsert(_ context.Context, g *models.DomainGrant, _ uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.grants {
		if e.OrganizationID == g.OrganizationID && e.Provider == g.Provider && e.Tenant == g.Tenant {
			g.ID = e.ID
		}
	}
	cp := *g
	cp.Status = "active"
	m.grants[g.ID] = &cp
	return nil
}
func (m *memGrants) List(_ context.Context, org uuid.UUID) ([]models.DomainGrant, error) {
	var out []models.DomainGrant
	for _, g := range m.grants {
		if g.OrganizationID == org {
			out = append(out, *g)
		}
	}
	return out, nil
}
func (m *memGrants) Get(_ context.Context, org, id uuid.UUID) (*models.DomainGrant, error) {
	if g, ok := m.grants[id]; ok && g.OrganizationID == org {
		cp := *g
		return &cp, nil
	}
	return nil, nil
}
func (m *memGrants) GetByID(_ context.Context, id uuid.UUID) (*models.DomainGrant, error) {
	if g, ok := m.grants[id]; ok {
		cp := *g
		return &cp, nil
	}
	return nil, nil
}
func (m *memGrants) SetStatus(_ context.Context, id uuid.UUID, status, last string) error {
	if g, ok := m.grants[id]; ok {
		g.Status, g.LastError = status, last
	}
	return nil
}
func (m *memGrants) Delete(_ context.Context, org, id uuid.UUID) ([]uuid.UUID, error) {
	delete(m.grants, id)
	return nil, nil
}
func (m *memGrants) ForDomain(_ context.Context, org uuid.UUID, provider, domain string) (*models.DomainGrant, error) {
	for _, g := range m.grants {
		if g.OrganizationID == org && g.Provider == provider && g.Status == "active" {
			for _, d := range g.Domains {
				if d == domain {
					cp := *g
					return &cp, nil
				}
			}
		}
	}
	return nil, nil
}
func (m *memGrants) ListActive(context.Context) ([]models.DomainGrant, error) { return nil, nil }
func (m *memGrants) InactiveMailboxes(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}
func (m *memGrants) AwaitsGoogleGrant(context.Context, uuid.UUID, string) (bool, error) {
	return m.awaiting, nil
}

type fakeMailboxes struct{ connected []models.NewDelegatedAccount }

func (f *fakeMailboxes) ConnectDelegated(_ context.Context, _ string, _ *uuid.UUID, d models.NewDelegatedAccount) (*models.Email, *errx.Error) {
	f.connected = append(f.connected, d)
	return &models.Email{ID: uuid.New(), Email: d.Email, Provider: string(d.Provider)}, nil
}
func (f *fakeMailboxes) ReactivateDelegated(context.Context, uuid.UUID) (*models.Email, *errx.Error) {
	return nil, nil
}
func (f *fakeMailboxes) Update(context.Context, string, string, string, *models.UpdateEmail) (*models.Email, *errx.Error) {
	return nil, nil
}

type fakeStore struct {
	delegation *models.DelegatedMailbox
	refs       map[string]models.EmailRef
	retiring   []models.MigrationMailbox
}

func (f *fakeStore) GetByID(context.Context, uuid.UUID) (*models.Email, *errx.Error) { return nil, nil }
func (f *fakeStore) GetDelegation(context.Context, uuid.UUID) (*models.DelegatedMailbox, *errx.Error) {
	return f.delegation, nil
}
func (f *fakeStore) FindManyInOrganization(context.Context, uuid.UUID, []string) (map[string]models.EmailRef, *errx.Error) {
	if f.refs == nil {
		return map[string]models.EmailRef{}, nil
	}
	return f.refs, nil
}
func (f *fakeStore) ListSigninRetiring(context.Context, uuid.UUID) ([]models.MigrationMailbox, *errx.Error) {
	return f.retiring, nil
}

type memStates struct {
	m map[string]ConsentState
	// issued keeps every state ever put, so the fake token endpoint can answer for a verifier.
	issued []ConsentState
}

func (s *memStates) Put(_ context.Context, k string, v ConsentState, _ time.Duration) error {
	s.m[k] = v
	s.issued = append(s.issued, v)
	return nil
}
func (s *memStates) Peek(_ context.Context, k string) (ConsentState, bool) {
	v, ok := s.m[k]
	return v, ok
}
func (s *memStates) Take(_ context.Context, k string) (ConsentState, bool) {
	v, ok := s.m[k]
	delete(s.m, k)
	return v, ok
}

// ---- fake providers ---------------------------------------------------------

type fakeProviders struct {
	*httptest.Server
	// googleRefuse makes the token endpoint answer unauthorized_client.
	googleRefuse bool
	// googleRefuseScope refuses only an assertion asking for this scope, as Google does for one the entry lacks.
	googleRefuseScope string
	subjects          []string
	// What the next sign-in or consent token says.
	signinEmail, signinHD, consentTenant string
	// consentRoles are the directory role template ids in the consent's ID token; nil means a Global Administrator.
	consentRoles []any
	// rolesInGraph moves the roles from the ID token to the Graph access token, issued for graphTenant (empty: the consent tenant).
	rolesInGraph bool
	graphTenant  string
	// states lets the token endpoints echo the nonce of the sign-in a verifier belongs to.
	states *memStates
	// wrongNonce makes the next ID tokens carry a nonce no sign-in was started with.
	wrongNonce bool
}

// pkceNonce answers for the code_verifier a token request presents: the nonce of its sign-in, or false.
func (f *fakeProviders) pkceNonce(r *http.Request) (string, bool) {
	v := r.Form.Get("code_verifier")
	if v == "" || f.states == nil {
		return "", false
	}
	for _, st := range f.states.issued {
		if st.Verifier == v {
			if f.wrongNonce {
				return "another", true
			}
			return st.Nonce, true
		}
	}
	return "", false
}

func refusePKCE(w http.ResponseWriter) {
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"code_verifier does not match"}`))
}

func fakeIDToken(claims map[string]any) string {
	payload, _ := json.Marshal(claims)
	return "eyJhbGciOiJSUzI1NiJ9." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

type fakeTXT struct{ records map[string][]string }

func (f *fakeTXT) LookupTXT(_ context.Context, name string) ([]string, error) {
	if v, ok := f.records[name]; ok {
		return v, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func newProviders(t *testing.T) *fakeProviders {
	t.Helper()
	f := &fakeProviders{}
	mux := http.NewServeMux()
	mux.HandleFunc("/google/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if f.googleRefuse || (f.googleRefuseScope != "" && strings.Contains(assertionScope(r.Form.Get("assertion")), f.googleRefuseScope)) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized_client","error_description":"Client is unauthorized to retrieve access tokens"}`))
			return
		}
		f.subjects = append(f.subjects, r.Form.Get("assertion"))
		writeJSON(w, map[string]any{"access_token": "g-token", "token_type": "Bearer", "expires_in": 3600})
	})
	mux.HandleFunc("/gmail/users/me/profile", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"emailAddress": "Alex@acme.io"})
	})
	mux.HandleFunc("/admin/users", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"users": []map[string]any{
			{"id": "1", "primaryEmail": "alex@acme.io", "name": map[string]string{"fullName": "Alex Rivera"}},
			{"id": "2", "primaryEmail": "sam@acme-sales.io", "suspended": true, "name": map[string]string{"fullName": "Sam Lee"}},
		}})
	})
	mux.HandleFunc("/admin/users/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"primaryEmail": strings.TrimPrefix(r.URL.Path, "/admin/users/"), "isAdmin": !strings.HasPrefix(r.URL.Path, "/admin/users/user")})
	})
	mux.HandleFunc("/gsignin/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		nonce, ok := f.pkceNonce(r)
		if !ok {
			refusePKCE(w)
			return
		}
		writeJSON(w, map[string]any{"access_token": "x", "token_type": "Bearer", "expires_in": 3600,
			"id_token": fakeIDToken(map[string]any{"email": f.signinEmail, "email_verified": true, "hd": f.signinHD, "nonce": nonce})})
	})
	mux.HandleFunc("/ms/organizations/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		nonce, ok := f.pkceNonce(r)
		if !ok {
			refusePKCE(w)
			return
		}
		id, access := map[string]any{"tid": f.consentTenant, "wids": f.roles(), "nonce": nonce}, "x"
		if f.rolesInGraph {
			tid := f.graphTenant
			if tid == "" {
				tid = f.consentTenant
			}
			access = fakeIDToken(map[string]any{"tid": tid, "wids": f.roles()})
			delete(id, "wids")
		}
		writeJSON(w, map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 3600, "id_token": fakeIDToken(id)})
	})
	mux.HandleFunc("/ms/contoso.com/v2.0/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"issuer": "https://login.microsoftonline.com/11111111-1111-1111-1111-111111111111/v2.0"})
	})
	mux.HandleFunc("/ms/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "11111111-1111-1111-1111-111111111111") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"unauthorized_client","error_description":"AADSTS700016: Application not found in the directory"}`))
			return
		}
		writeJSON(w, map[string]any{"access_token": "m-token", "token_type": "Bearer", "expires_in": 3600})
	})
	mux.HandleFunc("/graph/organization", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"value": []any{map[string]any{"verifiedDomains": []any{
			map[string]any{"name": "Contoso.com"}, map[string]any{"name": "contoso.onmicrosoft.com"},
		}}}})
	})
	mux.HandleFunc("/graph/users", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("$filter") != "" {
			writeJSON(w, map[string]any{"value": []any{map[string]any{"id": "u-2", "mail": "sales@contoso.com", "displayName": "Sales", "accountEnabled": true}}})
			return
		}
		writeJSON(w, map[string]any{"value": []any{
			map[string]any{"id": "u-1", "mail": "sam@contoso.com", "userPrincipalName": "sam@Contoso.onmicrosoft.com", "displayName": "Sam", "accountEnabled": true},
			map[string]any{"id": "u-3", "displayName": "Room", "userPrincipalName": "guest_x.com#EXT#@contoso.onmicrosoft.com", "accountEnabled": true},
			map[string]any{"id": "u-4", "mail": "guest@partner.com", "userType": "Guest", "displayName": "Guest", "accountEnabled": true},
		}})
	})
	mux.HandleFunc("/graph/users/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/mailFolders/inbox"):
			writeJSON(w, map[string]any{"id": "inbox"})
		case strings.HasSuffix(r.URL.Path, "/sam@contoso.com"):
			writeJSON(w, map[string]any{"id": "u-1", "mail": "sam@contoso.com", "displayName": "Sam", "accountEnabled": true})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func serviceAccountKey(t *testing.T, tokenURL string) []byte {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
	raw, _ := json.Marshal(map[string]string{
		"type": "service_account", "client_id": "123456789", "client_email": "warmbly@project.iam.gserviceaccount.com",
		"private_key": string(pemKey), "private_key_id": "kid", "token_uri": tokenURL,
	})
	return raw
}

func TestGrantOriginBoundToUnconsumedState(t *testing.T) {
	t.Setenv("APP_URL", "https://app.warmbly.com")
	t.Setenv("APP_ORIGIN", "")
	t.Setenv("CORS_ALLOW_ORIGINS", "https://second.example.com")
	s, p, _, _, _ := newTestService(t)
	ctx := config.WithDashboardOrigin(context.Background(), "https://second.example.com")
	org, user := uuid.New(), uuid.New()
	holdLegacyGoogle(t, s, org)
	g, xerr := s.StartGoogle(ctx, org, user, "acme.io", "admin@acme.io")
	if xerr != nil {
		t.Fatal(xerr)
	}
	_, ms, xerr := s.StartMicrosoft(ctx, org, user)
	if xerr != nil {
		t.Fatal(xerr)
	}
	for _, state := range []string{g.State, ms} {
		if got := s.OAuthReturnOrigin(context.Background(), state); got != "https://second.example.com" {
			t.Fatalf("origin = %q", got)
		}
		st, ok := p.states.Peek(ctx, "mailbox_grant_state:"+state)
		if !ok || st.Verifier == "" || st.Nonce == "" || st.OrgID != org || st.UserID != user {
			t.Fatal("routing must preserve identity and PKCE state")
		}
	}
	if s.MicrosoftSigninURL(context.Background(), ms) == "" || s.OAuthReturnOrigin(ctx, ms) != "https://second.example.com" {
		t.Fatal("second Microsoft sign-in must retain origin")
	}
	p.states.Take(ctx, "mailbox_grant_state:"+ms)
	if s.OAuthReturnOrigin(ctx, ms) != "" || s.OAuthReturnOrigin(ctx, "gac_unknown") != "" {
		t.Fatal("unknown/consumed state must not route")
	}
	t.Setenv("CORS_ALLOW_ORIGINS", "https://app.warmbly.com")
	if s.OAuthReturnOrigin(ctx, g.State) != "" {
		t.Fatal("removed origin must not route")
	}
}

var testTXT = &fakeTXT{records: map[string][]string{}}

func newTestService(t *testing.T) (*Service, *fakeProviders, *memGrants, *fakeMailboxes, *fakeStore) {
	t.Helper()
	p := newProviders(t)
	grants := &memGrants{grants: map[uuid.UUID]*models.DomainGrant{}}
	mb := &fakeMailboxes{}
	st := &fakeStore{}
	testTXT.records = map[string][]string{}
	p.states = &memStates{m: map[string]ConsentState{}}
	s := NewService(Deps{
		Repo: grants, Mailboxes: mb, Store: st, States: p.states,
		GoogleKey:         serviceAccountKey(t, p.URL+"/google/token"),
		MicrosoftClientID: "app-id", MicrosoftSecret: "secret", MicrosoftRedirect: "https://api.example/addresses/outlook/callback",
		GoogleSignin: &oauth2.Config{ClientID: "gid", ClientSecret: "gsecret", RedirectURL: "https://api.example/addresses/google/callback",
			Scopes: []string{"openid", "email"}, Endpoint: oauth2.Endpoint{AuthURL: p.URL + "/gsignin/auth", TokenURL: p.URL + "/gsignin/token"}},
		Prover: domainproof.New("test-secret"), TXT: testTXT,
	})
	s.googleTokenURL = p.URL + "/google/token"
	s.adminBase, s.gmailBase = p.URL+"/admin", p.URL+"/gmail"
	s.msLoginBase, s.graphBase = p.URL+"/ms", p.URL+"/graph"
	return s, p, grants, mb, st
}

// ---- tests ----------------------------------------------------------------

func stateOf(u string) string {
	state := u[strings.Index(u, "state=")+len("state="):]
	if i := strings.Index(state, "&"); i >= 0 {
		state = state[:i]
	}
	return state
}

func TestConfigNamesTheClientAndScopes(t *testing.T) {
	s, _, _, _, _ := newTestService(t)
	c := s.Config()
	if !c.GoogleEnabled || c.GoogleClientID != "123456789" || !c.MicrosoftEnabled || len(c.GoogleScopes) != 3 || len(c.GoogleMissing) != 0 {
		t.Fatalf("config = %+v", c)
	}
	off := NewService(Deps{GoogleKey: []byte(`{"type":"authorized_user"}`)})
	c = off.Config()
	if c.GoogleEnabled || c.MicrosoftEnabled || len(c.GoogleMissing) != 1 || len(c.MicrosoftMissing) != 2 {
		t.Fatalf("off config = %+v", c)
	}
}

// A domain the workspace holds no Google grant for is refused at start and finish; Microsoft is untouched.
func TestNewGoogleGrantsAreRetired(t *testing.T) {
	s, _, _, _, _ := newTestService(t)
	org, user := uuid.New(), uuid.New()
	if _, xerr := s.StartGoogle(context.Background(), org, user, "acme.io", "admin"+"@acme.io"); xerr == nil || xerr.Identifier != ErrIDGoogleRetired {
		t.Fatalf("start on a new domain = %v", xerr)
	}
	testTXT.records["_warmbly.acme.io"] = []string{s.proof.Value(org, "acme.io")}
	if _, xerr := s.FinishGoogle(context.Background(), org, user, GoogleFinish{Domain: "acme.io", AdminEmail: "admin" + "@acme.io"}); xerr == nil || xerr.Identifier != ErrIDGoogleRetired {
		t.Fatalf("finish on a new domain = %v", xerr)
	}
	if _, _, xerr := s.StartMicrosoft(context.Background(), org, user); xerr != nil {
		t.Fatalf("microsoft consent = %v", xerr)
	}
}

// Mailboxes that arrived in a workspace import without their grant let the domain be proved again.
func TestImportedMailboxesReopenTheirGoogleGrant(t *testing.T) {
	s, _, grants, _, _ := newTestService(t)
	grants.awaiting = true
	org, user := uuid.New(), uuid.New()
	if _, xerr := s.StartGoogle(context.Background(), org, user, "acme.io", "admin"+"@acme.io"); xerr != nil {
		t.Fatalf("start for imported mailboxes = %v", xerr)
	}
}

// The per-mailbox sign-in client is named whenever one is configured, so a reconnect can show it too.
func TestConfigNamesTheMailboxClient(t *testing.T) {
	s := NewService(Deps{GoogleMailboxClientID: "mailbox-client.apps.googleusercontent.com"})
	if got := s.Config().GoogleMailboxClientID; got != "mailbox-client.apps.googleusercontent.com" {
		t.Fatalf("client = %q", got)
	}
	if got := NewService(Deps{}).Config().GoogleMailboxClientID; got != "" {
		t.Fatalf("no client = %q", got)
	}
}

// holdLegacyGoogle gives org an existing (failing) Google grant on acme.io, the only kind that may be proved again.
func holdLegacyGoogle(t *testing.T, s *Service, org uuid.UUID) {
	t.Helper()
	m := s.repo.(*memGrants)
	id := uuid.New()
	m.grants[id] = &models.DomainGrant{ID: id, OrganizationID: org, Provider: models.GrantProviderGoogle, Tenant: "acme.io", Domains: []string{"acme.io"}, Status: "invalid"}
}

// googleGrant proves acme.io again for org, which holds a grant there, and returns the grant.
func googleGrant(t *testing.T, s *Service, p *fakeProviders, org, user uuid.UUID) *models.DomainGrant {
	t.Helper()
	holdLegacyGoogle(t, s, org)
	start, xerr := s.StartGoogle(context.Background(), org, user, "Acme.io", "admin@acme.io")
	if xerr != nil || start.Method != "signin" {
		t.Fatalf("start = %+v, %v", start, xerr)
	}
	p.signinEmail, p.signinHD = "admin@acme.io", "acme.io"
	g, xerr := s.FinishGoogle(context.Background(), org, user, GoogleFinish{State: start.State, Code: "code"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	return g
}

func TestGoogleGrantNeedsTheAdminsOwnSignin(t *testing.T) {
	s, p, _, _, _ := newTestService(t)
	org, user := uuid.New(), uuid.New()
	g := googleGrant(t, s, p, org, user)
	if g.Provider != models.GrantProviderGoogle || strings.Join(g.Domains, ",") != "acme-sales.io,acme.io" {
		t.Fatalf("grant = %+v", g)
	}

	// Someone else's Google account does not prove the domain.
	start, _ := s.StartGoogle(context.Background(), org, user, "acme.io", "admin@acme.io")
	p.signinEmail = "someone@acme.io"
	if _, xerr := s.FinishGoogle(context.Background(), org, user, GoogleFinish{State: start.State, Code: "code"}); xerr == nil || xerr.Identifier != ErrIDProof {
		t.Fatalf("a different account = %v", xerr)
	}
	// A state belongs to the workspace and person that started it, once.
	start, _ = s.StartGoogle(context.Background(), org, user, "acme.io", "admin@acme.io")
	p.signinEmail = "admin@acme.io"
	if _, xerr := s.FinishGoogle(context.Background(), uuid.New(), user, GoogleFinish{State: start.State, Code: "code"}); xerr == nil || xerr.Identifier != ErrIDState {
		t.Fatalf("another workspace's finish = %v", xerr)
	}
	if _, xerr := s.FinishGoogle(context.Background(), org, user, GoogleFinish{State: start.State, Code: "code"}); xerr == nil {
		t.Fatal("a spent state was accepted")
	}
	// An address the directory does not list as an administrator is refused.
	start, _ = s.StartGoogle(context.Background(), org, user, "acme.io", "user1@acme.io")
	p.signinEmail = "user1@acme.io"
	if _, xerr := s.FinishGoogle(context.Background(), org, user, GoogleFinish{State: start.State, Code: "code"}); xerr == nil || xerr.Identifier != ErrIDProof {
		t.Fatalf("a non-admin = %v", xerr)
	}
}

func TestGoogleGrantByDNSIsBoundToTheWorkspace(t *testing.T) {
	s, _, _, _, _ := newTestService(t)
	victim, attacker := uuid.New(), uuid.New()
	holdLegacyGoogle(t, s, victim)
	start, xerr := s.StartGoogle(context.Background(), victim, uuid.New(), "acme.io", "admin@acme.io")
	if xerr != nil || start.TXTName != "_warmbly.acme.io" {
		t.Fatalf("start = %+v, %v", start, xerr)
	}
	testTXT.records["_warmbly.acme.io"] = []string{start.TXTValue}
	if _, xerr := s.FinishGoogle(context.Background(), attacker, uuid.New(), GoogleFinish{Domain: "acme.io", AdminEmail: "admin@acme.io"}); xerr == nil || xerr.Identifier != ErrIDProof {
		t.Fatalf("the victim's public record proved the domain for another workspace: %v", xerr)
	}
	if _, xerr := s.FinishGoogle(context.Background(), victim, uuid.New(), GoogleFinish{Domain: "acme.io", AdminEmail: "admin@acme.io"}); xerr != nil {
		t.Fatalf("the workspace's own record = %v", xerr)
	}
}

func TestGoogleRefusalExplainsTheAdminConsole(t *testing.T) {
	s, p, _, _, _ := newTestService(t)
	start, _ := s.StartGoogle(context.Background(), uuid.New(), uuid.New(), "acme.io", "admin@acme.io")
	_ = start
	p.googleRefuse = true
	_, xerr := s.verifyGoogle(context.Background(), "acme.io", "admin@acme.io")
	if xerr == nil || xerr.Identifier != ErrIDGoogleUnauthorized || !strings.Contains(xerr.Message, "Domain-wide delegation") {
		t.Fatalf("got %v", xerr)
	}
}

func TestGoogleConnectStoresADelegatedMailbox(t *testing.T) {
	s, p, _, mb, _ := newTestService(t)
	org := uuid.New()
	g := googleGrant(t, s, p, org, uuid.New())
	if _, xerr := s.Connect(context.Background(), org, uuid.NewString(), g.ID, "alex@acme.io", ""); xerr != nil {
		t.Fatal(xerr)
	}
	d := mb.connected[0]
	if d.Provider != models.InboxProviderGoogle || d.Subject != "alex@acme.io" || d.GrantID != g.ID || d.MailHost != "google_workspace" {
		t.Fatalf("connected = %+v", d)
	}
	if _, xerr := s.Connect(context.Background(), org, uuid.NewString(), g.ID, "x@elsewhere.io", ""); xerr == nil || xerr.Identifier != ErrIDNotCovered {
		t.Fatalf("an uncovered domain = %v", xerr)
	}
	if _, xerr := s.Connect(context.Background(), uuid.New(), uuid.NewString(), g.ID, "alex@acme.io", ""); xerr == nil {
		t.Fatal("another workspace used the grant")
	}
}

func microsoftGrant(t *testing.T, s *Service, p *fakeProviders, org uuid.UUID) *models.DomainGrant {
	t.Helper()
	user := uuid.New()
	u, state, xerr := s.StartMicrosoft(context.Background(), org, user)
	if xerr != nil || stateOf(u) != state || !strings.Contains(u, "/organizations/v2.0/adminconsent?") || !strings.Contains(u, "scope=https%3A%2F%2Fgraph.microsoft.com%2F.default") || strings.Contains(u, "prompt=") {
		t.Fatalf("start = %s, %v", u, xerr)
	}
	if in := s.MicrosoftSigninURL(context.Background(), state); stateOf(in) != state || !strings.Contains(in, "/organizations/oauth2/v2.0/authorize?") || strings.Contains(in, "prompt=") ||
		!strings.Contains(in, "code_challenge_method=S256") || !strings.Contains(in, "nonce=") {
		t.Fatalf("sign-in after consent = %s", in)
	}
	p.consentTenant = "11111111-1111-1111-1111-111111111111"
	g, xerr := s.FinishMicrosoft(context.Background(), org, user, state, "code")
	if xerr != nil {
		t.Fatal(xerr)
	}
	return g
}

func TestMicrosoftTenantComesFromTheConsentToken(t *testing.T) {
	s, p, _, _, _ := newTestService(t)
	org, user := uuid.New(), uuid.New()
	g := microsoftGrant(t, s, p, org)
	if g.Tenant != "11111111-1111-1111-1111-111111111111" || strings.Join(g.Domains, ",") != "contoso.com,contoso.onmicrosoft.com" {
		t.Fatalf("grant = %+v (a guest's domain must not be added)", g)
	}
	_, state, _ := s.StartMicrosoft(context.Background(), org, user)
	if _, xerr := s.FinishMicrosoft(context.Background(), uuid.New(), user, state, "code"); xerr == nil || xerr.Identifier != ErrIDState {
		t.Fatalf("another workspace finished the consent: %v", xerr)
	}
	_, state, _ = s.StartMicrosoft(context.Background(), org, user)
	p.consentTenant = "22222222-2222-2222-2222-222222222222"
	if _, xerr := s.FinishMicrosoft(context.Background(), org, user, state, "code"); xerr == nil || xerr.Identifier != ErrIDMicrosoftConsent {
		t.Fatalf("a tenant that did not consent = %v", xerr)
	}
	_, state, _ = s.StartMicrosoft(context.Background(), org, user)
	if _, xerr := s.FinishMicrosoft(context.Background(), org, user, state, ""); xerr == nil {
		t.Fatal("a finish with no code was accepted")
	}
}

func TestMicrosoftConnectResolvesTheGraphUser(t *testing.T) {
	s, p, _, mb, _ := newTestService(t)
	org := uuid.New()
	g := microsoftGrant(t, s, p, org)
	if _, xerr := s.Connect(context.Background(), org, uuid.NewString(), g.ID, "sam@contoso.com", ""); xerr != nil {
		t.Fatal(xerr)
	}
	if _, xerr := s.Connect(context.Background(), org, uuid.NewString(), g.ID, "sales@contoso.com", ""); xerr != nil {
		t.Fatal(xerr)
	}
	if mb.connected[0].Subject != "u-1" || mb.connected[1].Subject != "u-2" || mb.connected[0].Name != "Sam" {
		t.Fatalf("connected = %+v", mb.connected)
	}
	users, xerr := s.Users(context.Background(), org, g.ID)
	if xerr != nil || len(users) != 1 || users[0].ID != "u-1" {
		t.Fatalf("users = %+v, %v (no mailbox and guests are left out)", users, xerr)
	}
}

func TestAccessTokenOnlyForCoveredMailboxesOfTheGrantsWorkspace(t *testing.T) {
	s, p, _, _, st := newTestService(t)
	org := uuid.New()
	g := microsoftGrant(t, s, p, org)

	if _, handled, _ := s.AccessToken(context.Background(), uuid.New()); handled {
		t.Fatal("a mailbox with no delegation was claimed")
	}
	st.delegation = &models.DelegatedMailbox{AccountID: uuid.New(), OrganizationID: org, Provider: models.InboxProviderOutlook, GrantID: g.ID, Subject: "u-1", Email: "sam@contoso.com"}
	tok, handled, xerr := s.AccessToken(context.Background(), st.delegation.AccountID)
	if !handled || xerr != nil || tok.AccessToken != "m-token" {
		t.Fatalf("token = %+v handled = %v err = %v", tok, handled, xerr)
	}
	st.delegation.Email = "ceo@victim.com"
	if _, _, xerr := s.AccessToken(context.Background(), st.delegation.AccountID); xerr == nil {
		t.Fatal("a mailbox outside the grant's domains drew a token")
	}
	st.delegation.Email, st.delegation.OrganizationID = "sam@contoso.com", uuid.New()
	if _, _, xerr := s.AccessToken(context.Background(), st.delegation.AccountID); xerr == nil {
		t.Fatal("a mailbox of another workspace drew a token from this grant")
	}
}

func TestSigninMailboxesMoveOntoTheGrant(t *testing.T) {
	s, p, _, _, st := newTestService(t)
	org, user := uuid.New(), uuid.New()
	g := googleGrant(t, s, p, org, user)

	st.refs = map[string]models.EmailRef{"alex@acme.io": {ID: uuid.New(), Provider: "gmail", AuthMethod: models.MailAuthOAuth}}
	users, xerr := s.Users(context.Background(), org, g.ID)
	if xerr != nil || len(users) == 0 || !users[0].Connected || !users[0].Upgrade {
		t.Fatalf("a signed-in mailbox is not offered for the move: %+v, %v", users, xerr)
	}
	st.refs["alex@acme.io"] = models.EmailRef{ID: uuid.New(), Provider: "gmail", AuthMethod: models.MailAuthOAuth, Managed: true}
	if users, _ = s.Users(context.Background(), org, g.ID); users[0].Upgrade {
		t.Fatal("a Warmbly Cloud mailbox was offered for the move")
	}
	st.refs["alex@acme.io"] = models.EmailRef{ID: uuid.New(), Provider: "smtp_imap", AuthMethod: models.MailAuthAppPassword}
	if users, _ = s.Users(context.Background(), org, g.ID); users[0].Upgrade {
		t.Fatal("an app-password mailbox was offered for the move")
	}

	st.retiring = []models.MigrationMailbox{
		{ID: uuid.New(), Email: "alex@acme.io"}, {ID: uuid.New(), Email: "me@gmail.com"},
		{ID: uuid.New(), Email: "sam@other.io"}, {ID: uuid.New(), Email: "kim@acme.io"},
	}
	groups, xerr := s.SigninMigration(context.Background(), org)
	if xerr != nil || len(groups) != 3 {
		t.Fatalf("groups = %+v, %v", groups, xerr)
	}
	byDomain := map[string]models.SigninMigration{}
	for _, g := range groups {
		byDomain[g.Domain] = g
	}
	if a := byDomain["acme.io"]; a.Kind != "workspace" || a.GrantID == nil || *a.GrantID != g.ID || len(a.Mailboxes) != 2 {
		t.Fatalf("acme.io = %+v", a)
	}
	if o := byDomain["other.io"]; o.Kind != "workspace" || o.GrantID != nil {
		t.Fatalf("other.io = %+v", o)
	}
	if byDomain["gmail.com"].Kind != "personal" {
		t.Fatalf("gmail.com = %+v", byDomain["gmail.com"])
	}
	if groups, _ := s.SigninMigration(context.Background(), uuid.New()); len(groups) != 3 || groups[0].GrantID != nil {
		t.Fatal("another workspace's grant was offered")
	}
}

func (f *fakeProviders) roles() []any {
	if f.consentRoles == nil {
		return []any{"b79fbf4d-3ef9-4689-8143-76b194e85509", "62e90394-69f5-4237-9190-012177145e10"}
	}
	return f.consentRoles
}

func TestMicrosoftGrantNeedsAnAdministratorsSignin(t *testing.T) {
	s, p, grants, _, _ := newTestService(t)
	org, user := uuid.New(), uuid.New()
	p.consentTenant = "11111111-1111-1111-1111-111111111111"
	for _, roles := range [][]any{{}, {"b79fbf4d-3ef9-4689-8143-76b194e85509"}, {"9b895d92-2cd3-44c7-9d02-a6ac2d5ea5c3"}} {
		p.consentRoles = roles
		_, state, _ := s.StartMicrosoft(context.Background(), org, user)
		if _, xerr := s.FinishMicrosoft(context.Background(), org, user, state, "code"); xerr == nil || xerr.Identifier != ErrIDProof {
			t.Fatalf("roles %v recorded a grant: %v", roles, xerr)
		}
	}
	if len(grants.grants) != 0 {
		t.Fatal("a refused consent left a grant behind")
	}
	p.consentRoles = []any{"E8611AB8-C189-46E8-94E1-60213AB1F814"}
	_, state, _ := s.StartMicrosoft(context.Background(), org, user)
	if _, xerr := s.FinishMicrosoft(context.Background(), org, user, state, "code"); xerr != nil {
		t.Fatalf("a Privileged Role Administrator was refused: %v", xerr)
	}
}

func TestMicrosoftAdminRoleReadFromTheGraphToken(t *testing.T) {
	s, p, grants, _, _ := newTestService(t)
	org, user := uuid.New(), uuid.New()
	p.consentTenant, p.rolesInGraph = "11111111-1111-1111-1111-111111111111", true
	p.graphTenant = "22222222-2222-2222-2222-222222222222"
	_, state, _ := s.StartMicrosoft(context.Background(), org, user)
	if _, xerr := s.FinishMicrosoft(context.Background(), org, user, state, "code"); xerr == nil || xerr.Identifier != ErrIDProof {
		t.Fatalf("roles from another tenant's token were accepted: %v", xerr)
	}
	if len(grants.grants) != 0 {
		t.Fatal("a refused consent left a grant behind")
	}
	p.graphTenant = ""
	_, state, _ = s.StartMicrosoft(context.Background(), org, user)
	if _, xerr := s.FinishMicrosoft(context.Background(), org, user, state, "code"); xerr != nil {
		t.Fatalf("an administrator whose ID token lists no roles was refused: %v", xerr)
	}
}

func TestMicrosoftSigninURLOnlyForItsOwnState(t *testing.T) {
	s, _, _, _, _ := newTestService(t)
	if u := s.MicrosoftSigninURL(context.Background(), "gac_x"); u != "" {
		t.Fatalf("a Google state got a Microsoft sign-in: %s", u)
	}
	if u := s.MicrosoftSigninURL(context.Background(), "mac_unknown"); u != "" {
		t.Fatalf("a state nobody started got a sign-in: %s", u)
	}
}

func TestSigninsRequirePKCEAndNonce(t *testing.T) {
	s, p, _, _, _ := newTestService(t)
	org, user := uuid.New(), uuid.New()
	holdLegacyGoogle(t, s, org)
	start, xerr := s.StartGoogle(context.Background(), org, user, "acme.io", "admin@acme.io")
	if xerr != nil || !strings.Contains(start.URL, "code_challenge_method=S256") || !strings.Contains(start.URL, "nonce=") {
		t.Fatalf("google start = %+v, %v", start, xerr)
	}
	p.signinEmail, p.signinHD, p.wrongNonce = "admin@acme.io", "acme.io", true
	if _, xerr := s.FinishGoogle(context.Background(), org, user, GoogleFinish{State: start.State, Code: "code"}); xerr == nil || xerr.Identifier != ErrIDState {
		t.Fatalf("a google ID token with another nonce = %v", xerr)
	}
	_, state, _ := s.StartMicrosoft(context.Background(), org, user)
	p.consentTenant = "11111111-1111-1111-1111-111111111111"
	if _, xerr := s.FinishMicrosoft(context.Background(), org, user, state, "code"); xerr == nil || xerr.Identifier != ErrIDMicrosoftConsent {
		t.Fatalf("a microsoft ID token with another nonce = %v", xerr)
	}
}
