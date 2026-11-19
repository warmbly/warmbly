package partner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/app/webhook"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
	"github.com/warmbly/warmbly/internal/repository"
)

type memCodes struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (s *memCodes) Put(_ context.Context, key string, v []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = v
	return nil
}

func (s *memCodes) Take(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.m[key]
	delete(s.m, key)
	return v, nil
}

// testSessions signs real purpose-bound JWTs and records the sessions it would mint.
type testSessions struct {
	token.TokenService
	minted  []uuid.UUID
	revoked []uuid.UUID
}

func (s *testSessions) GenerateSessionWithOrg(_ context.Context, userID uuid.UUID, _, _, _, provider string, orgID *uuid.UUID) (*models.Token, *errx.Error) {
	if provider != token.AuthProviderPartner || orgID == nil {
		return nil, errx.InternalError()
	}
	s.minted = append(s.minted, userID)
	return &models.Token{AccessToken: "session-for-" + userID.String()}, nil
}

func (s *testSessions) RevokeOtherSessions(_ context.Context, userID, _ uuid.UUID) *errx.Error {
	s.revoked = append(s.revoked, userID)
	return nil
}

type liveFixture struct {
	pool     *pgxpool.Pool
	handle   *db.DB
	svc      *Service
	sessions *testSessions
	a, b     *models.Partner
}

func liveSetup(t *testing.T) *liveFixture {
	t.Helper()
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	ctx := context.Background()
	handle, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(handle.Pool.Close)
	t.Setenv("APP_URL", "https://app.warmbly.test")
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	sealer, err := encrypt.NewEncrypter(key)
	if err != nil {
		t.Fatal(err)
	}
	sessions := &testSessions{TokenService: token.NewService(nil, nil, nil, nil, "test-secret-at-least-32-characters-long")}
	svc := NewService(Deps{
		Repo: repository.NewPartnerRepository(handle.Pool), Orgs: repository.NewOrganizationRepository(handle.Pool),
		Sessions: sessions, Codes: &memCodes{m: map[string][]byte{}}, Sealer: sealer,
	})
	f := &liveFixture{pool: handle.Pool, handle: handle, svc: svc, sessions: sessions}
	suffix := hex.EncodeToString(key[:4])
	for i, p := range []**models.Partner{&f.a, &f.b} {
		created, xerr := svc.CreatePartner(ctx, CreatePartnerInput{Slug: "t" + suffix + string(rune('a'+i)), Name: "Partner " + string(rune('A'+i)),
			AllowedHosts: []string{"app.partner.test"}}, nil)
		if xerr != nil {
			t.Fatalf("create partner: %v", xerr)
		}
		*p = created
	}
	t.Cleanup(func() {
		bg := context.Background()
		for _, p := range []*models.Partner{f.a, f.b} {
			_, _ = handle.Pool.Exec(bg, `DELETE FROM organizations WHERE id IN (SELECT organization_id FROM partner_organizations WHERE partner_id = $1)`, p.ID)
			_, _ = handle.Pool.Exec(bg, `DELETE FROM partner_organizations WHERE partner_id = $1`, p.ID)
			_, _ = handle.Pool.Exec(bg, `DELETE FROM users WHERE id IN (SELECT user_id FROM partner_identities WHERE partner_id = $1)`, p.ID)
			_, _ = handle.Pool.Exec(bg, `DELETE FROM partners WHERE id = $1`, p.ID)
			_, _ = handle.Pool.Exec(bg, `DELETE FROM users WHERE id = $1`, p.ServiceUserID)
		}
	})
	return f
}

func (f *liveFixture) caller(t *testing.T, p *models.Partner, mode models.PartnerMode) *Caller {
	t.Helper()
	k, xerr := f.svc.CreateKey(context.Background(), p.ID, mode, "", nil)
	if xerr != nil {
		t.Fatal(xerr)
	}
	c, xerr := f.svc.Authenticate(context.Background(), k.Secret)
	if xerr != nil {
		t.Fatal(xerr)
	}
	return c
}

func orgInput(state string) OrganizationInput {
	return OrganizationInput{Name: "Acme Corp", Plan: models.PartnerManagedPlanKey, State: state,
		Limits: &Limits{CampaignSendsPerMailboxPerDay: 50}, Owner: &PersonInput{ExternalID: "owner-1", Email: "ada@acme.test", Name: "Ada Lovelace", Role: "owner"}}
}

func TestLivePartnerKeyAuthenticatesOnlyItsOwnMode(t *testing.T) {
	f := liveSetup(t)
	ctx := context.Background()
	live, xerr := f.svc.CreateKey(ctx, f.a.ID, models.PartnerModeLive, "", nil)
	if xerr != nil {
		t.Fatal(xerr)
	}
	c, xerr := f.svc.Authenticate(ctx, live.Secret)
	if xerr != nil || c.Mode != models.PartnerModeLive || c.Partner.ID != f.a.ID {
		t.Fatalf("live key = %+v %v", c, xerr)
	}
	var stored string
	if err := f.pool.QueryRow(ctx, `SELECT key_hash FROM partner_api_keys WHERE id = $1`, live.ID).Scan(&stored); err != nil || stored == live.Secret || stored != HashKey(live.Secret) {
		t.Fatalf("stored %q for the key", stored)
	}
	relabelled := TestKeyPrefix + strings.TrimPrefix(live.Secret, LiveKeyPrefix)
	if _, xerr := f.svc.Authenticate(ctx, relabelled); xerr == nil {
		t.Fatal("a live key relabelled as a test key authenticated")
	}
	if xerr := f.svc.RevokeKey(ctx, f.a.ID, live.ID); xerr != nil {
		t.Fatal(xerr)
	}
	if _, xerr := f.svc.Authenticate(ctx, live.Secret); xerr == nil {
		t.Fatal("a revoked key authenticated")
	}
	suspended := models.PartnerStatusSuspended
	test, _ := f.svc.CreateKey(ctx, f.a.ID, models.PartnerModeTest, "", nil)
	if _, xerr := f.svc.UpdatePartner(ctx, f.a.ID, UpdatePartnerInput{Status: &suspended}); xerr != nil {
		t.Fatal(xerr)
	}
	if _, xerr := f.svc.Authenticate(ctx, test.Secret); xerr == nil || xerr.Identifier != "partner_suspended" {
		t.Fatalf("a suspended partner's key answered %v", xerr)
	}
}

// Every read and write that names an organization is scoped to the caller's partner and mode.
func TestLivePartnerOrganizationsAreScopedToPartnerAndMode(t *testing.T) {
	f := liveSetup(t)
	ctx := context.Background()
	aLive := f.caller(t, f.a, models.PartnerModeLive)
	aTest := f.caller(t, f.a, models.PartnerModeTest)
	bLive := f.caller(t, f.b, models.PartnerModeLive)

	v, created, xerr := f.svc.Upsert(ctx, aLive, "ws-1", orgInput("active"))
	if xerr != nil || !created || v.State != "active" || v.Plan != models.PartnerManagedPlanKey {
		t.Fatalf("create = %+v %v %v", v, created, xerr)
	}
	if _, created, xerr := f.svc.Upsert(ctx, aLive, "ws-1", orgInput("active")); xerr != nil || created {
		t.Fatalf("second upsert created=%v %v", created, xerr)
	}
	for name, c := range map[string]*Caller{"same partner, test mode": aTest, "another partner": bLive} {
		if _, xerr := f.svc.Get(ctx, c, "ws-1"); xerr == nil || xerr.Code != errx.NotFound {
			t.Errorf("%s read the workspace: %v", name, xerr)
		}
		if _, xerr := f.svc.Cancel(ctx, c, "ws-1"); xerr == nil || xerr.Code != errx.NotFound {
			t.Errorf("%s cancelled the workspace: %v", name, xerr)
		}
		if _, xerr := f.svc.IssueLoginLink(ctx, c, "ws-1", LoginInput{Person: PersonInput{ExternalID: "x", Email: "x@acme.test"}}); xerr == nil || xerr.Code != errx.NotFound {
			t.Errorf("%s signed someone in: %v", name, xerr)
		}
		if xerr := f.svc.RemovePerson(ctx, c, "ws-1", "owner-1"); xerr == nil || xerr.Code != errx.NotFound {
			t.Errorf("%s removed a person: %v", name, xerr)
		}
		if _, xerr := f.svc.CreateAPIKey(ctx, c, "ws-1", APIKeyInput{Permissions: []string{"READ_CAMPAIGNS"}}, ""); xerr == nil {
			t.Errorf("%s minted a key", name)
		}
		page, xerr := f.svc.List(ctx, c, "", 100)
		if xerr != nil {
			t.Fatal(xerr)
		}
		for _, o := range page.Data {
			if o.ExternalID == "ws-1" {
				t.Errorf("%s listed the workspace", name)
			}
		}
	}
	// The same external id under another partner is a different workspace.
	other, created, xerr := f.svc.Upsert(ctx, bLive, "ws-1", orgInput("active"))
	if xerr != nil || !created || other.ID == v.ID {
		t.Fatalf("partner B's ws-1 = %+v %v %v", other, created, xerr)
	}
	// The owner is an admin bound by the partner's id, not an owner, and not matched on email.
	var role string
	if err := f.pool.QueryRow(ctx, `
		SELECT m.role FROM organization_members m JOIN partner_identities i ON i.user_id = m.user_id
		WHERE m.organization_id = $1 AND i.partner_id = $2 AND i.external_id = 'owner-1'`, uuid.MustParse(v.ID), f.a.ID).Scan(&role); err != nil || role != "Admin" {
		t.Fatalf("owner membership role = %q %v", role, err)
	}
	var ownerEmail string
	var ownerContact *string
	_ = f.pool.QueryRow(ctx, `SELECT u.email, u.contact_email FROM users u JOIN partner_identities i ON i.user_id = u.id WHERE i.partner_id = $1 AND i.external_id = 'owner-1'`, f.a.ID).Scan(&ownerEmail, &ownerContact)
	if !strings.HasSuffix(ownerEmail, ".partner.invalid") {
		t.Fatalf("partner-bound account address %q is deliverable", ownerEmail)
	}
	if ownerContact == nil || *ownerContact != "ada@acme.test" {
		t.Fatalf("owner contact email = %v; want the partner's address", ownerContact)
	}
}

// A test key naming a person a live key created gets a separate test person; the live account is untouched.
func TestLivePeopleAreScopedToKeyMode(t *testing.T) {
	f := liveSetup(t)
	ctx := context.Background()
	live := f.caller(t, f.a, models.PartnerModeLive)
	test := f.caller(t, f.a, models.PartnerModeTest)
	if _, _, xerr := f.svc.Upsert(ctx, live, "ws-live", orgInput("active")); xerr != nil {
		t.Fatal(xerr)
	}
	if _, _, xerr := f.svc.Upsert(ctx, test, "ws-test", orgInput("active")); xerr != nil {
		t.Fatal(xerr)
	}
	person := func(mode models.PartnerMode) (uuid.UUID, string) {
		var id uuid.UUID
		var first string
		if err := f.pool.QueryRow(ctx, `SELECT u.id, u.first_name FROM users u JOIN partner_identities i ON i.user_id = u.id
			WHERE i.partner_id = $1 AND i.mode = $2 AND i.external_id = 'owner-1'`, f.a.ID, mode).Scan(&id, &first); err != nil {
			t.Fatalf("%s owner: %v", mode, err)
		}
		return id, first
	}
	liveID, _ := person(models.PartnerModeLive)
	testID, _ := person(models.PartnerModeTest)
	if liveID == testID {
		t.Fatal("a test key resolved the live person")
	}
	if _, xerr := f.svc.IssueLoginLink(ctx, test, "ws-test", LoginInput{Person: PersonInput{ExternalID: "owner-1", Name: "Mallory Test", Role: "admin"}}); xerr != nil {
		t.Fatal(xerr)
	}
	if _, first := person(models.PartnerModeLive); first != "Ada" {
		t.Fatalf("a test key renamed the live person to %q", first)
	}
	var inTest bool
	_ = f.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM organization_members m JOIN partner_organizations po ON po.organization_id = m.organization_id
		WHERE po.partner_id = $1 AND po.external_id = 'ws-test' AND m.user_id = $2)`, f.a.ID, liveID).Scan(&inTest)
	if inTest {
		t.Fatal("a test key made the live person a member of a test workspace")
	}
	f.sessions.revoked = nil
	if xerr := f.svc.RemovePerson(ctx, test, "ws-test", "owner-1"); xerr != nil {
		t.Fatal(xerr)
	}
	for _, id := range f.sessions.revoked {
		if id == liveID {
			t.Fatal("a test key ended the live person's sessions")
		}
	}
}

func TestLiveLoginLinkRefreshesContactEmail(t *testing.T) {
	f := liveSetup(t)
	ctx := context.Background()
	c := f.caller(t, f.a, models.PartnerModeLive)
	if _, _, xerr := f.svc.Upsert(ctx, c, "ws-contact", orgInput("active")); xerr != nil {
		t.Fatal(xerr)
	}
	contact := func() (string, *string) {
		var login string
		var shown *string
		if err := f.pool.QueryRow(ctx, `SELECT u.email, u.contact_email FROM users u JOIN partner_identities i ON i.user_id = u.id
			WHERE i.partner_id = $1 AND i.external_id = 'p-contact'`, f.a.ID).Scan(&login, &shown); err != nil {
			t.Fatal(err)
		}
		return login, shown
	}
	for _, tc := range []struct{ email, want string }{{"cy@acme.test", "cy@acme.test"}, {"Cy <CY2@acme.test>", "cy2@acme.test"}, {"", ""}} {
		person := PersonInput{ExternalID: "p-contact", Email: tc.email, Name: "Cy Young", Role: "member"}
		if _, xerr := f.svc.IssueLoginLink(ctx, c, "ws-contact", LoginInput{Person: person}); xerr != nil {
			t.Fatal(xerr)
		}
		login, shown := contact()
		if !strings.HasSuffix(login, ".partner.invalid") {
			t.Fatalf("sign-in address became %q", login)
		}
		if got := ""; shown != nil {
			got = *shown
			if got != tc.want {
				t.Fatalf("contact email = %q; want %q", got, tc.want)
			}
		} else if tc.want != "" {
			t.Fatalf("contact email cleared; want %q", tc.want)
		}
	}
	bad := PersonInput{ExternalID: "p-contact", Email: "p-1@other.partner.invalid", Name: "Cy Young"}
	if _, xerr := f.svc.IssueLoginLink(ctx, c, "ws-contact", LoginInput{Person: bad}); xerr == nil {
		t.Fatal("an internal address was accepted as a contact email")
	}
}

func TestLiveLoginLinkIsSingleUseAndPurposeBound(t *testing.T) {
	f := liveSetup(t)
	ctx := context.Background()
	c := f.caller(t, f.a, models.PartnerModeLive)
	if _, _, xerr := f.svc.Upsert(ctx, c, "ws-login", orgInput("active")); xerr != nil {
		t.Fatal(xerr)
	}
	person := PersonInput{ExternalID: "p-2", Email: "bo@acme.test", Name: "Bo Diddley", Role: "viewer"}
	if _, xerr := f.svc.IssueLoginLink(ctx, c, "ws-login", LoginInput{Person: person, ReturnTo: "https://evil.test/"}); xerr == nil {
		t.Fatal("a return_to off the partner's hosts was accepted")
	}
	link, xerr := f.svc.IssueLoginLink(ctx, c, "ws-login", LoginInput{Person: person, ReturnTo: "https://app.partner.test/back"})
	if xerr != nil {
		t.Fatal(xerr)
	}
	u, _ := url.Parse(link.URL)
	if u.Host != "app.warmbly.test" || u.Path != "/auth/partner" || time.Until(link.ExpiresAt) > LoginTTL {
		t.Fatalf("link = %+v", link)
	}
	code := u.Query().Get("code")

	claims, verr := f.sessions.VerifyTokenFor(token.PurposePartnerLogin, code)
	if verr != nil {
		t.Fatal(verr)
	}
	// The same claims under another purpose never open a session.
	forged, _ := f.sessions.GenerateTokenFor(token.PurposeLoginCode, claims.UserID, claims.SessionID, "", claims.Nonce, time.Now(), time.Now().Add(time.Minute))
	if _, xerr := f.svc.ExchangeLogin(ctx, forged, "203.0.113.1", "test"); xerr == nil {
		t.Fatal("a login-code token was spent as a partner login link")
	}
	if _, xerr := f.sessions.VerifyTokenFor(token.PurposeAccess, code); xerr == nil {
		t.Fatal("a partner login link verifies as an access token")
	}

	res, xerr := f.svc.ExchangeLogin(ctx, code, "203.0.113.1", "test")
	if xerr != nil || res.ReturnTo != "https://app.partner.test/back" {
		t.Fatalf("exchange = %+v %v", res, xerr)
	}
	if _, xerr := f.svc.ExchangeLogin(ctx, code, "203.0.113.1", "test"); xerr == nil {
		t.Fatal("a login link signed in twice")
	}
	if len(f.sessions.minted) != 1 {
		t.Fatalf("minted %d sessions", len(f.sessions.minted))
	}
	// A cancelled workspace issues no links.
	if _, xerr := f.svc.Cancel(ctx, c, "ws-login"); xerr != nil {
		t.Fatal(xerr)
	}
	if _, xerr := f.svc.IssueLoginLink(ctx, c, "ws-login", LoginInput{Person: person}); xerr == nil || xerr.Identifier != "organization_cancelled" {
		t.Fatalf("cancelled workspace link = %v", xerr)
	}
}

func TestLiveIdempotencyReplaysASealedAnswer(t *testing.T) {
	f := liveSetup(t)
	ctx := context.Background()
	c := f.caller(t, f.a, models.PartnerModeLive)
	hash := RequestHash("POST", "/v1/partner/organizations/x/api-keys", []byte(`{"name":"k"}`))
	claim, r, xerr := f.svc.BeginIdempotent(ctx, c, "key-1", hash)
	if claim == nil || r != nil || xerr != nil {
		t.Fatalf("first claim = %v %v %v", claim, r, xerr)
	}
	if _, _, xerr := f.svc.BeginIdempotent(ctx, c, "key-1", hash); xerr == nil || xerr.Identifier != "idempotency_in_progress" {
		t.Fatalf("claim while running = %v", xerr)
	}
	body := []byte(`{"secret":"wmbly_supersecret"}`)
	f.svc.FinishIdempotent(ctx, c, claim, 201, "application/json", body)
	var sealed string
	if err := f.pool.QueryRow(ctx, `SELECT response_sealed FROM partner_idempotency_keys WHERE partner_id = $1 AND key = 'key-1'`, f.a.ID).Scan(&sealed); err != nil || strings.Contains(sealed, "supersecret") {
		t.Fatalf("stored answer %q (%v) is not sealed", sealed, err)
	}
	_, r, xerr = f.svc.BeginIdempotent(ctx, c, "key-1", hash)
	if xerr != nil || r == nil || r.Status != 201 || string(r.Body) != string(body) {
		t.Fatalf("replay = %+v %v", r, xerr)
	}
	if _, _, xerr := f.svc.BeginIdempotent(ctx, c, "key-1", RequestHash("POST", "/v1/partner/organizations/x/api-keys", []byte(`{"name":"other"}`))); xerr == nil || xerr.Identifier != "idempotency_conflict" {
		t.Fatalf("different body = %v", xerr)
	}
	// Keys are per mode: the same key in test mode is a new request.
	test := f.caller(t, f.a, models.PartnerModeTest)
	tc, r, xerr := f.svc.BeginIdempotent(ctx, test, "key-1", hash)
	if tc == nil || r != nil || xerr != nil {
		t.Fatalf("test-mode claim = %v %v", r, xerr)
	}
	// A server error frees the key so the retry runs again.
	f.svc.FinishIdempotent(ctx, test, tc, 500, "", nil)
	if tc, r, xerr := f.svc.BeginIdempotent(ctx, test, "key-1", hash); tc == nil || r != nil || xerr != nil {
		t.Fatalf("claim after a 500 = %v %v", r, xerr)
	}
}

// Exactly one of many concurrent requests holds a key, including when they race to take over an abandoned claim.
func TestLiveIdempotencyClaimIsExclusive(t *testing.T) {
	f := liveSetup(t)
	ctx := context.Background()
	c := f.caller(t, f.a, models.PartnerModeLive)
	hash := RequestHash("POST", "/v1/partner/organizations", []byte(`{}`))
	race := func(key string) int {
		var wg sync.WaitGroup
		var mu sync.Mutex
		won := 0
		for i := 0; i < 16; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				claim, _, xerr := f.svc.BeginIdempotent(ctx, c, key, hash)
				if xerr != nil && xerr.Identifier != "idempotency_in_progress" {
					t.Errorf("claim: %v", xerr)
				}
				if claim != nil {
					mu.Lock()
					won++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		return won
	}
	if n := race("fresh"); n != 1 {
		t.Fatalf("%d requests hold a fresh key", n)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE partner_idempotency_keys SET created_at = now() - interval '3 minutes' WHERE partner_id = $1 AND key = 'fresh'`, f.a.ID); err != nil {
		t.Fatal(err)
	}
	if n := race("fresh"); n != 1 {
		t.Fatalf("%d requests took over an abandoned claim", n)
	}
}

// A request whose claim was taken over can neither complete nor release the claim that replaced it.
func TestLiveIdempotencyLateFinisherLeavesTheNewClaim(t *testing.T) {
	f := liveSetup(t)
	ctx := context.Background()
	c := f.caller(t, f.a, models.PartnerModeLive)
	hash := RequestHash("POST", "/v1/partner/organizations", []byte(`{}`))
	old, _, xerr := f.svc.BeginIdempotent(ctx, c, "k", hash)
	if old == nil || xerr != nil {
		t.Fatalf("first claim = %v", xerr)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE partner_idempotency_keys SET created_at = now() - interval '3 minutes' WHERE partner_id = $1 AND key = 'k'`, f.a.ID); err != nil {
		t.Fatal(err)
	}
	next, _, xerr := f.svc.BeginIdempotent(ctx, c, "k", hash)
	if next == nil || xerr != nil || next.Token == old.Token {
		t.Fatalf("takeover = %v %v", next, xerr)
	}
	// The abandoned request finishes late, once with an error and once with an answer.
	f.svc.FinishIdempotent(ctx, c, old, 500, "", nil)
	f.svc.FinishIdempotent(ctx, c, old, 201, "application/json", []byte(`{"from":"old"}`))
	if _, _, xerr := f.svc.BeginIdempotent(ctx, c, "k", hash); xerr == nil || xerr.Identifier != "idempotency_in_progress" {
		t.Fatalf("after the late finisher the key reads %v, want still held by the new claim", xerr)
	}
	f.svc.FinishIdempotent(ctx, c, next, 201, "application/json", []byte(`{"from":"new"}`))
	_, r, xerr := f.svc.BeginIdempotent(ctx, c, "k", hash)
	if xerr != nil || r == nil || string(r.Body) != `{"from":"new"}` {
		t.Fatalf("replay = %+v %v, want the new claim's answer", r, xerr)
	}
	// A claim younger than the window is never taken over.
	young, _, _ := f.svc.BeginIdempotent(ctx, c, "young", hash)
	if young == nil {
		t.Fatal("no claim")
	}
	if again, _, xerr := f.svc.BeginIdempotent(ctx, c, "young", hash); again != nil || xerr == nil {
		t.Fatalf("a running claim was taken over: %v", xerr)
	}
}

// A suspended or cancelled workspace stops sending (its risk state reads suspended) and warmup, and resumes when active again.
func TestLiveSuspensionPausesSendingAndWarmup(t *testing.T) {
	f := liveSetup(t)
	ctx := context.Background()
	c := f.caller(t, f.a, models.PartnerModeLive)
	v, _, xerr := f.svc.Upsert(ctx, c, "ws-hold", orgInput("active"))
	if xerr != nil {
		t.Fatal(xerr)
	}
	orgID := uuid.MustParse(v.ID)
	risk := repository.NewOrgRiskRepository(f.handle)
	check := func(wantPaused bool) {
		t.Helper()
		f.svc.forget(orgID)
		states, err := risk.GetOrgRiskStates(ctx, []uuid.UUID{orgID})
		if err != nil {
			t.Fatal(err)
		}
		if states[orgID].BlocksSending() != wantPaused {
			t.Fatalf("risk state %q, want sending paused=%v", states[orgID], wantPaused)
		}
		paused, err := f.svc.WarmupPaused(ctx, orgID)
		if err != nil || paused != wantPaused {
			t.Fatalf("warmup paused = %v %v, want %v", paused, err, wantPaused)
		}
	}
	check(false)
	in := orgInput("suspended")
	in.SuspendedReason = "billing"
	if v, _, xerr = f.svc.Upsert(ctx, c, "ws-hold", in); xerr != nil || v.SuspendedReason == nil || *v.SuspendedReason != "billing" {
		t.Fatalf("suspend = %+v %v", v, xerr)
	}
	check(true)
	if _, _, xerr = f.svc.Upsert(ctx, c, "ws-hold", orgInput("active")); xerr != nil {
		t.Fatal(xerr)
	}
	check(false)
	if v, xerr = f.svc.Cancel(ctx, c, "ws-hold"); xerr != nil || v.State != "cancelled" || v.DeletesAt == nil {
		t.Fatalf("cancel = %+v %v", v, xerr)
	}
	check(true)
	// A PUT within retention restores it.
	if v, _, xerr = f.svc.Upsert(ctx, c, "ws-hold", orgInput("active")); xerr != nil || v.State != "active" || v.DeletesAt != nil {
		t.Fatalf("restore = %+v %v", v, xerr)
	}
	check(false)
}

// Events reach the partner's webhook signed with the current secret and, for a day after a rotation, the previous one too.
func TestLiveWebhookDeliverySignsWithBothSecretsAfterRotation(t *testing.T) {
	f := liveSetup(t)
	ctx := context.Background()
	type hit struct{ sig, event, id string }
	hits := make(chan hit, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var env Envelope
		if json.Unmarshal(body, &env) != nil || env.Type != r.Header.Get("X-Warmbly-Event") || !env.Livemode {
			w.WriteHeader(400)
			return
		}
		hits <- hit{r.Header.Get("X-Warmbly-Signature") + "|" + string(body), env.Type, r.Header.Get("X-Warmbly-Event-Id")}
	}))
	defer srv.Close()
	f.svc.HTTP = srv.Client()
	t.Setenv("WARMBLY_ALLOW_UNSAFE_WEBHOOK_URLS", "true")
	hook := srv.URL
	if _, xerr := f.svc.UpdatePartner(ctx, f.a.ID, UpdatePartnerInput{WebhookURL: &hook}); xerr != nil {
		t.Fatal(xerr)
	}
	first, xerr := f.svc.RotateWebhookSecret(ctx, f.a.ID)
	if xerr != nil {
		t.Fatal(xerr)
	}
	second, _ := f.svc.RotateWebhookSecret(ctx, f.a.ID)
	if err := f.svc.Publish(ctx, f.a.ID, models.PartnerModeLive, models.PartnerEventOrganizationUpdated, "ws-hook", map[string]string{"state": "active"}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeliverDue(ctx); err != nil {
		t.Fatal(err)
	}
	var h hit
	select {
	case h = <-hits:
	default:
		t.Fatal("nothing was delivered")
	}
	header, body, _ := strings.Cut(h.sig, "|")
	parts := strings.Split(header, ",")
	ts := strings.TrimPrefix(parts[0], "t=")
	unix, _ := strconv.ParseInt(ts, 10, 64)
	for i, secret := range []string{second, first} {
		want := "v1=" + webhook.Sign(secret, time.Unix(unix, 0), []byte(body))
		if len(parts) < 3 || parts[i+1] != want {
			t.Fatalf("signature %q lacks %s", header, want)
		}
	}
	if !strings.HasPrefix(h.id, "evt_") || h.event != models.PartnerEventOrganizationUpdated {
		t.Fatalf("headers = %+v", h)
	}
	if err := f.svc.DeliverDue(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case again := <-hits:
		t.Fatalf("a delivered event went out again: %+v", again)
	default:
	}
}

// Once the deletion job removes a cancelled workspace, the partner hears organization.deleted and the link is gone.
func TestLiveDeletedWorkspaceIsReportedAndForgotten(t *testing.T) {
	f := liveSetup(t)
	ctx := context.Background()
	c := f.caller(t, f.a, models.PartnerModeLive)
	v, _, xerr := f.svc.Upsert(ctx, c, "ws-gone", orgInput("active"))
	if xerr != nil {
		t.Fatal(xerr)
	}
	if _, err := f.pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, uuid.MustParse(v.ID)); err != nil {
		t.Fatal(err)
	}
	if _, xerr := f.svc.Get(ctx, c, "ws-gone"); xerr == nil || xerr.Code != errx.NotFound {
		t.Fatalf("a deleted workspace answered GET: %v", xerr)
	}
	if err := f.svc.SweepDeleted(ctx); err != nil {
		t.Fatal(err)
	}
	page, xerr := f.svc.Events(ctx, c, "", 100)
	if xerr != nil {
		t.Fatal(xerr)
	}
	found := false
	for _, e := range page.Data {
		found = found || (e.Type == models.PartnerEventOrganizationDeleted && e.OrganizationExternalID != nil && *e.OrganizationExternalID == "ws-gone")
	}
	if !found {
		t.Fatal("no organization.deleted event")
	}
	if v2, created, xerr := f.svc.Upsert(ctx, c, "ws-gone", orgInput("active")); xerr != nil || !created || v2.ID == v.ID {
		t.Fatalf("recreate after deletion = %+v %v %v", v2, created, xerr)
	}
}
