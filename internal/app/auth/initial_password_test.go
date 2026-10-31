package auth

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
	"github.com/warmbly/warmbly/internal/pkg/captcha"
	"github.com/warmbly/warmbly/internal/repository"
)

// initialAccount is one row of users as the auth repository reads it.
type initialAccount struct {
	id        uuid.UUID
	email     string
	hash      string
	expiresAt *time.Time
}

// initialRepo mirrors the SQL in pg_auth.go: an expired password is no password.
type initialRepo struct {
	repository.AuthRepository
	mu   sync.Mutex
	acct *initialAccount
}

func (r *initialRepo) live() string {
	if r.acct.expiresAt != nil && !r.acct.expiresAt.After(time.Now()) {
		return ""
	}
	return r.acct.hash
}

func (r *initialRepo) IsValidCredentials(_ context.Context, email, password string) (uuid.UUID, *errx.Error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if email != r.acct.email || r.live() == "" {
		return uuid.Nil, errx.ErrCredentials
	}
	if ok, _ := argon2.Verify(password, r.live()); !ok {
		return uuid.Nil, errx.ErrCredentials
	}
	return r.acct.id, nil
}

func (r *initialRepo) GetPasswordHash(context.Context, uuid.UUID) (string, *errx.Error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.live(), nil
}

func (r *initialRepo) InitialPasswordPending(context.Context, uuid.UUID) (bool, *errx.Error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.acct.expiresAt != nil && r.acct.expiresAt.After(time.Now()), nil
}

func (r *initialRepo) ReplaceInitialPassword(_ context.Context, _ uuid.UUID, oldHash, newHash string) (bool, *errx.Error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.acct.hash != oldHash || r.acct.expiresAt == nil || !r.acct.expiresAt.After(time.Now()) {
		return false, nil
	}
	r.acct.hash, r.acct.expiresAt = newHash, nil
	return true, nil
}

type initialUsers struct {
	repository.UserRepository
}

func (initialUsers) IsLoginCodeExempt(context.Context, uuid.UUID) (bool, error) { return true, nil }
func (initialUsers) GetBanState(context.Context, uuid.UUID) (uint32, error)     { return 0, nil }

// initialTokens signs for real and records what would end or open a session.
type initialTokens struct {
	token.TokenService
	revoked, sessions int
}

func (t *initialTokens) RevokeOtherSessions(context.Context, uuid.UUID, uuid.UUID) *errx.Error {
	t.revoked++
	return nil
}

func (t *initialTokens) GenerateSession(context.Context, uuid.UUID, string, string, string, string) (*models.Token, *errx.Error) {
	t.sessions++
	return &models.Token{AccessToken: "access"}, nil
}

type initialTwoFA struct{ enabled bool }

func (f initialTwoFA) IsEnabled(context.Context, uuid.UUID) (bool, error) { return f.enabled, nil }
func (f initialTwoFA) CreatePendingChallenge(context.Context, uuid.UUID, string, *models.UserIdentity) (string, int, *errx.Error) {
	return "2fa-pending", 300, nil
}

const testIP = "203.0.113." + "7"

const issuedPassword = "Tester-abcdefghjkmnpqrstuvw"

func initialFixture(t *testing.T, expiresIn time.Duration) (*authService, *initialRepo, *initialTokens) {
	t.Helper()
	url := os.Getenv("WARMBLY_TEST_REDIS")
	if url == "" {
		t.Skip("WARMBLY_TEST_REDIS not set")
	}
	c, err := cache.New(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	hash, herr := argon2.Hash(issuedPassword)
	if herr != nil {
		t.Fatal(herr)
	}
	expires := time.Now().Add(expiresIn)
	acct := &initialAccount{id: uuid.New(), email: uuid.NewString() + "@example.test", hash: hash, expiresAt: &expires}
	repo := &initialRepo{acct: acct}
	tokens := &initialTokens{TokenService: token.NewService(nil, nil, nil, nil, strings.Repeat("s", 40))}
	s := &authService{
		authRepository: repo,
		userRepository: initialUsers{},
		tokenService:   tokens,
		cache:          c,
		captcha:        captcha.NewTurnstileFromConfig(captcha.TurnstileConfig{Disabled: true}),
		policy:         &config.AuthPolicy{LoginCode: config.LoginCodeAlways},
		mailDelivers:   true,
	}
	t.Cleanup(func() { s.clearLoginFailures(context.Background(), acct.email) })
	return s, repo, tokens
}

func signIn(s *authService, email, password string) (*models.AuthSession, *errx.Error) {
	return s.LoginStart(context.Background(), &AuthData{Email: email, Password: password}, testIP, "test")
}

func choose(s *authService, pending, password string) (*models.AuthSession, *errx.Error) {
	d := &InitialPasswordData{}
	d.PendingToken, d.Password = pending, password
	return s.CompleteInitialPassword(context.Background(), d, testIP, "test")
}

const chosenPassword = "a-brand-new-passphrase-4471"

// An issued password signs nobody in: it answers with the change step and no session.
func TestInitialPasswordSignInReturnsTheChangeStep(t *testing.T) {
	s, repo, tokens := initialFixture(t, 48*time.Hour)
	res, err := signIn(s, repo.acct.email, issuedPassword)
	if err != nil {
		t.Fatal(err)
	}
	if !res.PasswordChangeRequired || res.PendingToken == "" || res.Token != nil || res.TwoFARequired || res.CodeRequired || res.Session != "" {
		t.Fatalf("an issued password answered %+v", res)
	}
	if tokens.sessions != 0 {
		t.Fatal("an issued password opened a session")
	}
}

// Past its expiry an issued password is answered exactly like a wrong one.
func TestExpiredInitialPasswordFailsLikeAWrongPassword(t *testing.T) {
	s, repo, tokens := initialFixture(t, -time.Minute)
	_, expired := signIn(s, repo.acct.email, issuedPassword)
	_, wrong := signIn(s, repo.acct.email, "not-the-password-at-all")
	if expired != errx.ErrCredentials || wrong != errx.ErrCredentials {
		t.Fatalf("expired answered %v, wrong answered %v; want both ErrCredentials", expired, wrong)
	}
	if tokens.sessions != 0 {
		t.Fatal("an expired issued password opened a session")
	}
}

// The change-step token is spent only on its own endpoint, and that endpoint takes no other token.
func TestInitialPasswordTokenOpensNothingElse(t *testing.T) {
	s, repo, _ := initialFixture(t, 48*time.Hour)
	ctx := context.Background()
	res, err := signIn(s, repo.acct.email, issuedPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, xerr := s.LoginConfirm(ctx, &ConfirmData{Code: "000000"}, res.PendingToken, testIP, "test"); xerr == nil {
		t.Fatal("the login-code step accepted a change-step token")
	}
	ld := &SSOLinkData{}
	ld.PendingToken, ld.Password = res.PendingToken, issuedPassword
	if _, xerr := s.SSOLinkConfirm(ctx, ld, testIP, "test"); xerr != errx.ErrSSOLinkExpired {
		t.Fatalf("the link step answered %v to a change-step token", xerr)
	}

	now := time.Now()
	for _, purpose := range []string{token.PurposeLoginCode, token.PurposeSSOLink, token.PurposeTwoFAPending, token.PurposePasswordReset, token.PurposeAccess} {
		other, gerr := s.tokenService.GenerateTokenFor(purpose, repo.acct.id, uuid.New(), "", "nonce", now, now.Add(time.Minute))
		if gerr != nil {
			t.Fatal(gerr)
		}
		if _, xerr := choose(s, other, chosenPassword); xerr != errx.ErrPasswordChangeExpired {
			t.Fatalf("a %s token answered %v", purpose, xerr)
		}
	}
	if repo.acct.expiresAt == nil {
		t.Fatal("a foreign token replaced the issued password")
	}
}

// Weak, breached and reused passwords are refused without spending the challenge.
func TestInitialPasswordRefusesWeakBreachedAndReused(t *testing.T) {
	s, repo, tokens := initialFixture(t, 48*time.Hour)
	res, err := signIn(s, repo.acct.email, issuedPassword)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		password string
		want     *errx.Error
	}{
		{"short", errx.ErrPassword},
		{"password123", errx.ErrPasswordBreached},
		{issuedPassword, errx.ErrPasswordReused},
	} {
		if _, xerr := choose(s, res.PendingToken, tc.password); xerr != tc.want {
			t.Errorf("%q answered %v, want %v", tc.password, xerr, tc.want)
		}
	}
	if repo.acct.expiresAt == nil || tokens.revoked != 0 || tokens.sessions != 0 {
		t.Fatalf("a refused password changed state: expires %v, revoked %d, sessions %d", repo.acct.expiresAt, tokens.revoked, tokens.sessions)
	}
	if _, xerr := choose(s, res.PendingToken, chosenPassword); xerr != nil {
		t.Fatalf("the challenge did not survive refusals: %v", xerr)
	}
}

// A good password replaces the issued one, clears its expiry, ends other
// sessions and signs in; the token is then spent and the account keeps working.
func TestInitialPasswordChangeClearsAndSignsIn(t *testing.T) {
	s, repo, tokens := initialFixture(t, 48*time.Hour)
	res, err := signIn(s, repo.acct.email, issuedPassword)
	if err != nil {
		t.Fatal(err)
	}
	done, xerr := choose(s, res.PendingToken, chosenPassword)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if done.Token == nil || done.PasswordChangeRequired || tokens.sessions != 1 || tokens.revoked != 1 {
		t.Fatalf("result %+v, sessions %d, revoked %d", done, tokens.sessions, tokens.revoked)
	}
	if repo.acct.expiresAt != nil {
		t.Fatal("the chosen password still expires")
	}
	if ok, _ := argon2.Verify(chosenPassword, repo.acct.hash); !ok {
		t.Fatal("the chosen password was not stored")
	}
	if _, xerr := choose(s, res.PendingToken, "another-new-passphrase-8812"); xerr != errx.ErrPasswordChangeExpired {
		t.Fatalf("a replayed token answered %v", xerr)
	}
	if _, xerr := signIn(s, repo.acct.email, issuedPassword); xerr != errx.ErrCredentials {
		t.Fatalf("the issued password still answered %v", xerr)
	}
	again, xerr := signIn(s, repo.acct.email, chosenPassword)
	if xerr != nil || again.Token == nil {
		t.Fatalf("the chosen password signed in with %+v, %v", again, xerr)
	}
}

// The change never skips a factor: an enrolled second factor is still asked for.
func TestInitialPasswordChangeKeepsTheSecondFactor(t *testing.T) {
	s, repo, tokens := initialFixture(t, 48*time.Hour)
	s.twofa = initialTwoFA{enabled: true}
	res, err := signIn(s, repo.acct.email, issuedPassword)
	if err != nil {
		t.Fatal(err)
	}
	done, xerr := choose(s, res.PendingToken, chosenPassword)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if !done.TwoFARequired || done.Token != nil || tokens.sessions != 0 {
		t.Fatalf("a 2FA account got %+v with %d sessions", done, tokens.sessions)
	}
}

// A challenge whose issued password ran out meanwhile writes nothing.
func TestInitialPasswordChangeAfterExpiryIsRefused(t *testing.T) {
	s, repo, tokens := initialFixture(t, 48*time.Hour)
	res, err := signIn(s, repo.acct.email, issuedPassword)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Second)
	repo.acct.expiresAt = &past
	if _, xerr := choose(s, res.PendingToken, chosenPassword); xerr != errx.ErrPasswordChangeExpired {
		t.Fatalf("answered %v", xerr)
	}
	if tokens.sessions != 0 || tokens.revoked != 0 {
		t.Fatal("an expired challenge changed state")
	}
}

// A provider link cannot be completed by presenting an issued password.
func TestSSOLinkRefusesAnInitialPassword(t *testing.T) {
	s, repo, tokens := initialFixture(t, 48*time.Hour)
	ctx := context.Background()
	link, xerr := s.createLinkChallenge(ctx, repo.acct.id, models.UserIdentity{
		Provider: models.IdentityProviderGoogle, Issuer: "https://accounts.google.com", Subject: "s-1", Email: repo.acct.email,
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	ld := &SSOLinkData{}
	ld.PendingToken, ld.Password = link.PendingToken, issuedPassword
	if _, xerr := s.SSOLinkConfirm(ctx, ld, testIP, "test"); xerr != errx.ErrPasswordChangeRequired {
		t.Fatalf("answered %v", xerr)
	}
	if tokens.sessions != 0 {
		t.Fatal("an issued password completed a provider link")
	}
}

// A new password issued while a challenge is open voids it: only the reissued password can sign in.
func TestInitialPasswordReissueVoidsOpenChallenge(t *testing.T) {
	s, repo, tokens := initialFixture(t, 48*time.Hour)
	res, err := signIn(s, repo.acct.email, issuedPassword)
	if err != nil {
		t.Fatal(err)
	}
	reissued, herr := argon2.Hash("Tester-" + strings.Repeat("r", 20))
	if herr != nil {
		t.Fatal(herr)
	}
	repo.mu.Lock()
	repo.acct.hash = reissued
	repo.mu.Unlock()

	if _, xerr := choose(s, res.PendingToken, chosenPassword); xerr != errx.ErrPasswordChangeExpired {
		t.Fatalf("a challenge opened before the reissue answered %v", xerr)
	}
	if repo.acct.hash != reissued || repo.acct.expiresAt == nil || tokens.sessions != 0 {
		t.Fatal("a challenge opened before the reissue replaced the reissued password")
	}
}
