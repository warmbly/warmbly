package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/idtoken"
	"github.com/warmbly/warmbly/internal/repository"
)

// claimsProvider answers every exchange with fixed claims and records the nonce it was asked for.
type claimsProvider struct {
	claims *idtoken.Claims
	nonces map[string]string
}

func (p *claimsProvider) AuthCodeURL(state, nonce, verifier string) string {
	p.nonces[state] = nonce
	return "https://provider/authorize?state=" + url.QueryEscape(state)
}

func (p *claimsProvider) Exchange(ctx context.Context, code, verifier, nonce string) (*idtoken.Claims, error) {
	if p.claims == nil {
		return nil, errors.New("exchange failed")
	}
	return p.claims, nil
}

func (p *claimsProvider) ProviderName() string { return "Google" }

type identityStub struct {
	owners map[string]uuid.UUID
	linked map[uuid.UUID][]models.UserIdentity
}

func (r *identityStub) FindUserByIdentity(ctx context.Context, issuer, subject string) (uuid.UUID, error) {
	return r.owners[issuer+"|"+subject], nil
}
func (r *identityStub) Link(ctx context.Context, userID uuid.UUID, identity models.UserIdentity) error {
	return errors.New("re-authentication must never link an identity")
}
func (r *identityStub) HasIdentityForIssuer(ctx context.Context, userID uuid.UUID, issuer string) (bool, error) {
	return false, nil
}
func (r *identityStub) TouchLogin(ctx context.Context, issuer, subject string) error { return nil }
func (r *identityStub) ListForUser(ctx context.Context, userID uuid.UUID) ([]models.UserIdentity, error) {
	return r.linked[userID], nil
}

// banUsers reports a ban scope for every user.
type banUsers struct {
	repository.UserRepository
	scope uint32
}

func (u banUsers) GetBanState(context.Context, uuid.UUID) (uint32, error) { return u.scope, nil }

type tokenVerifierStub struct{ claims *idtoken.Claims }

func (v tokenVerifierStub) Verify(ctx context.Context, raw string) (*idtoken.Claims, error) {
	if raw != "valid" {
		return nil, errors.New("bad token")
	}
	return v.claims, nil
}

type reauthFixture struct {
	s        *authService
	provider *claimsProvider
	owner    uuid.UUID
	session  uuid.UUID
}

func newReauthFixture(t *testing.T) *reauthFixture {
	t.Helper()
	redisURL := os.Getenv("WARMBLY_TEST_REDIS")
	if redisURL == "" {
		t.Skip("WARMBLY_TEST_REDIS not set")
	}
	c, err := cache.New(redisURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	owner := uuid.New()
	provider := &claimsProvider{
		claims: &idtoken.Claims{Issuer: "https://accounts.google.com", Subject: "owner-sub", Email: "owner@example.com"},
		nonces: map[string]string{},
	}
	s := svc(banUsers{}, "")
	s.cache = c
	s.identities = &identityStub{
		owners: map[string]uuid.UUID{"https://accounts.google.com|owner-sub": owner},
		linked: map[uuid.UUID][]models.UserIdentity{owner: {{Provider: models.IdentityProviderGoogle, Issuer: "https://accounts.google.com", Subject: "owner-sub"}}},
	}
	s.WireFederatedProvider(models.IdentityProviderGoogle, provider)
	return &reauthFixture{s: s, provider: provider, owner: owner, session: uuid.New()}
}

// stateOf pulls the state back out of the authorization URL begin returned.
func stateOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("state")
}

func (f *reauthFixture) begin(t *testing.T) (state, binding string) {
	t.Helper()
	redirect, err := f.s.ReauthSSOBegin(context.Background(), models.IdentityProviderGoogle, f.owner, f.session)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	return stateOf(t, redirect.URL), redirect.Binding
}

func (f *reauthFixture) callback(state string) (*SSOCallbackResult, error) {
	res, err := f.s.SSOCallbackComplete(context.Background(), SSOCallback{Provider: models.IdentityProviderGoogle, Code: "c", State: state})
	if err != nil {
		return res, err
	}
	return res, nil
}

func TestReauthSSOConfirmsOnlyTheSessionThatBeganIt(t *testing.T) {
	f := newReauthFixture(t)
	ctx := context.Background()
	state, binding := f.begin(t)
	if !IsReauthState(state) {
		t.Fatalf("state %q does not route to the re-authentication landing", state)
	}

	res, err := f.callback(state)
	if err != nil || res == nil || !res.Reauth || res.Code == "" {
		t.Fatalf("callback: res=%+v err=%v", res, err)
	}

	// A wrong binding, user or session refuses without spending the code.
	if xerr := f.s.ReauthSSOFinish(ctx, res.Code, "not-the-binding", f.owner, f.session); xerr == nil {
		t.Fatal("a forwarded code was redeemed without the binding")
	}
	if xerr := f.s.ReauthSSOFinish(ctx, res.Code, binding, f.owner, uuid.New()); xerr == nil {
		t.Fatal("another session of the same user redeemed the code")
	}
	if xerr := f.s.ReauthSSOFinish(ctx, res.Code, binding, uuid.New(), f.session); xerr == nil {
		t.Fatal("another user redeemed the code")
	}

	if xerr := f.s.ReauthSSOFinish(ctx, res.Code, binding, f.owner, f.session); xerr != nil {
		t.Fatalf("the initiating session was refused: %v", xerr)
	}
	if xerr := f.s.ReauthSSOFinish(ctx, res.Code, binding, f.owner, f.session); xerr == nil {
		t.Fatal("the code was redeemed twice")
	}
}

func TestReauthSSORefusesAnotherAccountsIdentity(t *testing.T) {
	f := newReauthFixture(t)
	f.provider.claims = &idtoken.Claims{Issuer: "https://accounts.google.com", Subject: "someone-else", Email: "owner@example.com"}
	state, _ := f.begin(t)

	res, err := f.callback(state)
	if err == nil {
		t.Fatal("an identity not linked to the account confirmed it, on the email address alone")
	}
	if res == nil || !res.Reauth || res.Code != "" {
		t.Fatalf("a refused re-authentication must still land on its own page with no code: %+v", res)
	}
}

// The state's prefix only routes the browser; the stored purpose is what counts.
func TestSSOCallbackRefusesAPurposeTheStateDoesNotCarry(t *testing.T) {
	f := newReauthFixture(t)
	ctx := context.Background()
	plant := func(state string, flow ssoFlow) {
		raw, _ := json.Marshal(flow)
		if err := f.s.cache.SetEx(ctx, ssoStateKey(state), raw, time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
	}

	plant("plain-state", ssoFlow{Provider: models.IdentityProviderGoogle, Purpose: ssoPurposeReauth, UserID: f.owner, SessionID: f.session, Binding: "b"})
	if res, err := f.callback("plain-state"); err == nil {
		t.Fatalf("a re-authentication flow completed as a sign-in: %+v", res)
	}

	plant(reauthStatePrefix+"login-state", ssoFlow{Provider: models.IdentityProviderGoogle, Binding: "b"})
	if res, err := f.callback(reauthStatePrefix + "login-state"); err == nil {
		t.Fatalf("a sign-in flow completed as a re-authentication: %+v", res)
	}
}

func TestReauthNativeNeedsTheSessionNonceOnce(t *testing.T) {
	f := newReauthFixture(t)
	ctx := context.Background()
	claims := &idtoken.Claims{Issuer: "https://accounts.google.com", Subject: "owner-sub"}
	f.s.googleIDTokens = tokenVerifierStub{claims: claims}

	if xerr := f.s.ReauthNativeVerify(ctx, models.IdentityProviderGoogle, "valid", f.owner, f.session); xerr == nil {
		t.Fatal("a token was accepted with no nonce issued")
	}

	nonce, xerr := f.s.ReauthNativeBegin(ctx, f.session)
	if xerr != nil {
		t.Fatal(xerr)
	}
	claims.Nonce = "something-else"
	if xerr := f.s.ReauthNativeVerify(ctx, models.IdentityProviderGoogle, "valid", f.owner, f.session); xerr == nil {
		t.Fatal("a token carrying another nonce was accepted")
	}

	nonce, _ = f.s.ReauthNativeBegin(ctx, f.session)
	claims.Nonce = nonce
	if xerr := f.s.ReauthNativeVerify(ctx, models.IdentityProviderGoogle, "valid", f.owner, uuid.New()); xerr == nil {
		t.Fatal("another session's nonce was accepted")
	}
	if xerr := f.s.ReauthNativeVerify(ctx, models.IdentityProviderGoogle, "valid", uuid.New(), f.session); xerr == nil {
		t.Fatal("an identity linked to someone else confirmed the account")
	}

	nonce, _ = f.s.ReauthNativeBegin(ctx, f.session)
	claims.Nonce = nonce
	if xerr := f.s.ReauthNativeVerify(ctx, models.IdentityProviderGoogle, "valid", f.owner, f.session); xerr != nil {
		t.Fatalf("the owner's token with the session nonce was refused: %v", xerr)
	}
	if xerr := f.s.ReauthNativeVerify(ctx, models.IdentityProviderGoogle, "valid", f.owner, f.session); xerr == nil {
		t.Fatal("one nonce confirmed twice")
	}
}

func TestReauthIdentitiesListsOnlyLinkedAndConfigured(t *testing.T) {
	f := newReauthFixture(t)
	f.s.appleIDTokens = tokenVerifierStub{}

	browser, native, xerr := f.s.ReauthIdentities(context.Background(), f.owner)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if len(browser) != 1 || browser[0] != models.IdentityProviderGoogle {
		t.Fatalf("browser = %v, want only google", browser)
	}
	// Apple is configured but not linked; Google is linked but has no native verifier.
	if len(native) != 0 {
		t.Fatalf("native = %v, want none", native)
	}

	browser, native, _ = f.s.ReauthIdentities(context.Background(), uuid.New())
	if len(browser) != 0 || len(native) != 0 {
		t.Fatalf("an account with no identity was offered %v / %v", browser, native)
	}
}

func TestReauthRefusesASuspendedAccount(t *testing.T) {
	f := newReauthFixture(t)
	f.s.userRepository = banUsers{scope: uint32(models.BanScopeLogin)}
	state, binding := f.begin(t)
	res, err := f.callback(state)
	if err != nil {
		t.Fatal(err)
	}
	if xerr := f.s.ReauthSSOFinish(context.Background(), res.Code, binding, f.owner, f.session); xerr == nil {
		t.Fatal("a suspended account confirmed a sensitive change")
	}
}

func (r *identityStub) Unlink(context.Context, uuid.UUID, string) error { return nil }

type unlinkStub struct {
	identityStub
	err error
}

func (u *unlinkStub) Unlink(context.Context, uuid.UUID, string) error { return u.err }

// A refusal keeps its own code, and a provider already gone converges to done.
func TestUnlinkIdentityMapsRefusals(t *testing.T) {
	cases := map[error]*errx.Error{
		nil:                             nil,
		repository.ErrIdentityNotLinked: nil,
		repository.ErrLastSignInMethod:  errx.ErrLastSignInMethod,
	}
	for in, want := range cases {
		s := &authService{identities: &unlinkStub{err: in}}
		if got := s.UnlinkIdentity(context.Background(), uuid.New(), uuid.New(), "apple"); got != want {
			t.Fatalf("%v: got %v, want %v", in, got, want)
		}
	}
	s := &authService{identities: &unlinkStub{}}
	if got := s.UnlinkIdentity(context.Background(), uuid.New(), uuid.New(), "github"); got != ErrIdentityNotFound {
		t.Fatalf("unknown provider: got %v, want ErrIdentityNotFound", got)
	}
}

type sessionsStub struct {
	token.TokenService
	sessions []token.SessionView
	revoked  []string
}

func (t *sessionsStub) ListSessions(context.Context, uuid.UUID, uuid.UUID) ([]token.SessionView, *errx.Error) {
	return t.sessions, nil
}

func (t *sessionsStub) RevokeSessionByID(_ context.Context, _, id, _ uuid.UUID) *errx.Error {
	t.revoked = append(t.revoked, id.String())
	return nil
}

// Removing a provider signs out the other devices that signed in through it, and only those.
func TestUnlinkIdentityEndsSessionsFromThatProvider(t *testing.T) {
	current, viaApple, viaGoogle := uuid.New(), uuid.New(), uuid.New()
	tokens := &sessionsStub{sessions: []token.SessionView{
		{ID: current.String(), Current: true, AuthProvider: token.AuthProviderApple},
		{ID: viaApple.String(), AuthProvider: token.AuthProviderApple},
		{ID: viaGoogle.String(), AuthProvider: token.AuthProviderGoogle},
	}}
	s := &authService{identities: &unlinkStub{}, tokenService: tokens}
	if err := s.UnlinkIdentity(context.Background(), uuid.New(), current, models.IdentityProviderApple); err != nil {
		t.Fatal(err)
	}
	if len(tokens.revoked) != 1 || tokens.revoked[0] != viaApple.String() {
		t.Fatalf("revoked %v, want only %s", tokens.revoked, viaApple)
	}
}
