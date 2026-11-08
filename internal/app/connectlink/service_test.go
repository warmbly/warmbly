package connectlink

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestTokenHashing(t *testing.T) {
	token, hash, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(hash) != sha256.Size {
		t.Fatalf("stored hash is %d bytes, want %d", len(hash), sha256.Size)
	}
	if strings.Contains(string(hash), token) {
		t.Fatal("the stored form carries the token")
	}
	got, ok := hashToken(token)
	if !ok || !bytes.Equal(got, hash) {
		t.Fatal("a minted token does not hash to what was stored")
	}
	other, _, _ := newToken()
	if other == token {
		t.Fatal("two tokens collided")
	}
	for name, bad := range map[string]string{
		"empty":    "",
		"short":    token[:42],
		"long":     token + "A",
		"padded":   base64.URLEncoding.EncodeToString(make([]byte, tokenBytes)),
		"std alph": strings.Repeat("+", 43),
		"space":    " " + token[1:],
	} {
		if _, ok := hashToken(bad); ok {
			t.Errorf("%s: accepted %q", name, bad)
		}
	}
}

func TestStatusAt(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	creator := uuid.New()
	revoked := now.Add(-time.Hour)
	link := func(mut func(*models.ClientConnectLink)) *models.ClientConnectLink {
		l := &models.ClientConnectLink{CreatedBy: &creator, MaxMailboxes: 3, MailboxesConnected: 1, ExpiresAt: now.Add(time.Hour)}
		mut(l)
		return l
	}
	cases := []struct {
		name string
		link *models.ClientConnectLink
		want models.ClientConnectLinkStatus
		code string
	}{
		{"active", link(func(*models.ClientConnectLink) {}), models.ClientConnectLinkActive, ""},
		{"expires exactly now", link(func(l *models.ClientConnectLink) { l.ExpiresAt = now }), models.ClientConnectLinkExpired, "client_link_expired"},
		{"full", link(func(l *models.ClientConnectLink) { l.MailboxesConnected = 3 }), models.ClientConnectLinkExhausted, "client_link_exhausted"},
		{"revoked", link(func(l *models.ClientConnectLink) { l.RevokedAt = &revoked }), models.ClientConnectLinkRevoked, "client_link_revoked"},
		{"creator gone", link(func(l *models.ClientConnectLink) { l.CreatedBy = nil }), models.ClientConnectLinkRevoked, "client_link_revoked"},
		{"revoked beats expired and full", link(func(l *models.ClientConnectLink) {
			l.RevokedAt, l.ExpiresAt, l.MailboxesConnected = &revoked, now.Add(-time.Minute), 3
		}), models.ClientConnectLinkRevoked, "client_link_revoked"},
		{"expired beats full", link(func(l *models.ClientConnectLink) {
			l.ExpiresAt, l.MailboxesConnected = now.Add(-time.Minute), 3
		}), models.ClientConnectLinkExpired, "client_link_expired"},
	}
	for _, c := range cases {
		got := c.link.StatusAt(now)
		if got != c.want {
			t.Errorf("%s: status %s, want %s", c.name, got, c.want)
		}
		xerr := statusError(got)
		if (xerr == nil) != (c.code == "") || (xerr != nil && xerr.ResponseCode() != c.code) {
			t.Errorf("%s: refusal %v, want code %q", c.name, xerr, c.code)
		}
	}
}

func TestNormalizeProviders(t *testing.T) {
	got, xerr := normalizeProviders([]string{"smtp_imap", "gmail", "gmail"})
	if xerr != nil || strings.Join(got, ",") != "gmail,smtp_imap" {
		t.Fatalf("got %v %v", got, xerr)
	}
	for _, bad := range [][]string{nil, {}, {"yahoo"}, {"gmail", "imap"}} {
		if _, xerr := normalizeProviders(bad); xerr != ErrInvalidProviders {
			t.Errorf("%v: accepted", bad)
		}
	}
}

// --- fakes ---

type memStates struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (s *memStates) Put(_ context.Context, key string, val []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = val
	return nil
}

func (s *memStates) Take(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	if !ok {
		return nil, redis.Nil
	}
	delete(s.m, key)
	return v, nil
}

func (s *memStates) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	if !ok {
		return nil, redis.Nil
	}
	return v, nil
}

// fakeRepo panics, through the nil interface, on anything a test did not expect to be called.
type fakeRepo struct {
	repository.ClientConnectLinkRepository
	link *models.ClientConnectLink
	uses []models.ClientConnectLinkUse
}

func (r *fakeRepo) Get(_ context.Context, orgID, id uuid.UUID) (*models.ClientConnectLink, *errx.Error) {
	if r.link == nil || r.link.OrganizationID != orgID || r.link.ID != id {
		return nil, nil
	}
	cp := *r.link
	return &cp, nil
}

func (r *fakeRepo) ByTokenHash(_ context.Context, hash []byte) (*models.ClientConnectLink, *errx.Error) {
	if r.link == nil || !bytes.Equal(r.link.TokenHash, hash) {
		return nil, nil
	}
	cp := *r.link
	return &cp, nil
}

func (r *fakeRepo) Reserve(_ context.Context, orgID, id uuid.UUID) (bool, *errx.Error) {
	l := r.link
	if l.OrganizationID != orgID || l.ID != id || l.StatusAt(time.Now()) != models.ClientConnectLinkActive {
		return false, nil
	}
	l.MailboxesConnected++
	return true, nil
}

func (r *fakeRepo) Release(_ context.Context, _, _ uuid.UUID) *errx.Error {
	r.link.MailboxesConnected--
	return nil
}

func (r *fakeRepo) RecordUse(_ context.Context, use *models.ClientConnectLinkUse, _ uuid.UUID, _ string) *errx.Error {
	r.uses = append(r.uses, *use)
	return nil
}

type fakeMailboxes struct {
	Mailboxes
	connectErr *errx.Error
	gotUser    string
	gotOrg     uuid.UUID
	gotCode    string
	gotVerif   string
	calls      int
}

func (m *fakeMailboxes) OAuthCallbackOrigin(models.InboxProvider) (string, *errx.Error) {
	return "https://api.example.test", nil
}

func (m *fakeMailboxes) OAuthAuthorizeURL(_ models.InboxProvider, state, verifier string) (string, *errx.Error) {
	return "https://login.example.test/?state=" + state, nil
}

func (m *fakeMailboxes) OAuthConnectWithCode(_ context.Context, userID string, orgID *uuid.UUID, provider models.InboxProvider, code, verifier string) (*models.Email, *errx.Error) {
	m.calls++
	m.gotUser, m.gotOrg, m.gotCode, m.gotVerif = userID, *orgID, code, verifier
	if m.connectErr != nil {
		return nil, m.connectErr
	}
	return &models.Email{ID: uuid.New(), OrganizationID: orgID, Email: "client@example.test", Provider: string(provider)}, nil
}

type fakeWorkspaces struct {
	Workspaces
	canManage bool
}

func (w *fakeWorkspaces) RequirePermission(context.Context, uuid.UUID, uuid.UUID, models.OrganizationPermission) *errx.Error {
	if !w.canManage {
		return errx.ErrForbidden
	}
	return nil
}

func (w *fakeWorkspaces) MailboxAllowance(context.Context, uuid.UUID) (*models.MailboxAllowance, *errx.Error) {
	return &models.MailboxAllowance{}, nil
}

func newFixture(t *testing.T) (*Service, *fakeRepo, *fakeMailboxes, *fakeWorkspaces, string) {
	t.Helper()
	token, hash, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	creator := uuid.New()
	repo := &fakeRepo{link: &models.ClientConnectLink{
		ID: uuid.New(), OrganizationID: uuid.New(), CreatedBy: &creator, TokenHash: hash,
		AllowedProviders: []string{"outlook", "smtp_imap"}, MaxMailboxes: 2, ExpiresAt: time.Now().Add(time.Hour),
	}}
	mb := &fakeMailboxes{}
	ws := &fakeWorkspaces{canManage: true}
	s := New(Deps{Repo: repo, Mailboxes: mb, Workspaces: ws})
	s.states = &memStates{m: map[string][]byte{}}
	return s, repo, mb, ws, token
}

const returnURL = "https://forms.example.test/connect/x"

// lastResultKey is the result key of the most recent startOutlook.
var lastResultKey string

func startOutlook(t *testing.T, s *Service, token string) string {
	t.Helper()
	link, xerr := s.Resolve(context.Background(), token)
	if xerr != nil {
		t.Fatal(xerr)
	}
	started, xerr := s.StartOAuth(context.Background(), link, models.InboxProviderOutlook, returnURL, "203.0.113.9")
	if xerr != nil {
		t.Fatal(xerr)
	}
	lastResultKey = started.ResultKey
	state := strings.TrimPrefix(started.URL, "https://login.example.test/?state=")
	if !IsOAuthState(state) {
		t.Fatalf("state %q does not carry the connect link prefix", state)
	}
	return state
}

func TestOAuthStateIsBoundAndSingleUse(t *testing.T) {
	s, repo, mb, _, token := newFixture(t)
	state := startOutlook(t, s, token)

	res, back, xerr := s.FinishOAuth(context.Background(), "outlook", "the-code", state, "")
	if xerr != nil {
		t.Fatal(xerr)
	}
	if back != returnURL {
		t.Fatalf("returned to %q", back)
	}
	if mb.gotUser != repo.link.CreatedBy.String() || mb.gotOrg != repo.link.OrganizationID || mb.gotCode != "the-code" || mb.gotVerif == "" {
		t.Fatalf("connected as %s in %s with code %q verifier %q", mb.gotUser, mb.gotOrg, mb.gotCode, mb.gotVerif)
	}
	if res.ActorID != *repo.link.CreatedBy || repo.link.MailboxesConnected != 1 || len(repo.uses) != 1 {
		t.Fatalf("actor %s, connected %d, uses %d", res.ActorID, repo.link.MailboxesConnected, len(repo.uses))
	}

	// The same state again finds nothing, and no second mailbox is attempted.
	if _, _, xerr := s.FinishOAuth(context.Background(), "outlook", "the-code", state, ""); xerr != ErrSigninExpired {
		t.Fatalf("replay answered %v", xerr)
	}
	if mb.calls != 1 {
		t.Fatalf("replay reached the connect path (%d calls)", mb.calls)
	}
}

func TestOAuthStateRefusals(t *testing.T) {
	ctx := context.Background()
	t.Run("unknown state", func(t *testing.T) {
		s, _, _, _, _ := newFixture(t)
		if _, back, xerr := s.FinishOAuth(ctx, "outlook", "c", OAuthStatePrefix+"nope", ""); xerr != ErrSigninExpired || back != "" {
			t.Fatalf("%v %q", xerr, back)
		}
	})
	t.Run("another flow's state", func(t *testing.T) {
		s, _, _, _, _ := newFixture(t)
		if _, _, xerr := s.FinishOAuth(ctx, "outlook", "c", "w.abc", ""); xerr != ErrSigninExpired {
			t.Fatal(xerr)
		}
	})
	t.Run("callback for the other provider", func(t *testing.T) {
		s, _, mb, _, token := newFixture(t)
		state := startOutlook(t, s, token)
		if _, back, xerr := s.FinishOAuth(ctx, "gmail", "c", state, ""); xerr != ErrSigninExpired || back != returnURL || mb.calls != 0 {
			t.Fatalf("%v %q %d", xerr, back, mb.calls)
		}
	})
	t.Run("declined at the provider", func(t *testing.T) {
		s, _, mb, _, token := newFixture(t)
		state := startOutlook(t, s, token)
		if _, _, xerr := s.FinishOAuth(ctx, "outlook", "", state, "access_denied"); xerr != ErrSigninDeclined || mb.calls != 0 {
			t.Fatalf("%v %d", xerr, mb.calls)
		}
	})
	t.Run("revoked while signing in", func(t *testing.T) {
		s, repo, mb, _, token := newFixture(t)
		state := startOutlook(t, s, token)
		now := time.Now()
		repo.link.RevokedAt = &now
		if _, _, xerr := s.FinishOAuth(ctx, "outlook", "c", state, ""); xerr != ErrRevoked || mb.calls != 0 {
			t.Fatalf("%v %d", xerr, mb.calls)
		}
	})
	t.Run("creator lost manage-emails", func(t *testing.T) {
		s, _, mb, ws, token := newFixture(t)
		state := startOutlook(t, s, token)
		ws.canManage = false
		if _, _, xerr := s.FinishOAuth(ctx, "outlook", "c", state, ""); xerr != ErrRevoked || mb.calls != 0 {
			t.Fatalf("%v %d", xerr, mb.calls)
		}
	})
	t.Run("failed connect gives the slot back", func(t *testing.T) {
		s, repo, mb, _, token := newFixture(t)
		state := startOutlook(t, s, token)
		mb.connectErr = errx.ErrEmailOnboardExchange
		if _, _, xerr := s.FinishOAuth(ctx, "outlook", "c", state, ""); xerr != errx.ErrEmailOnboardExchange {
			t.Fatal(xerr)
		}
		if repo.link.MailboxesConnected != 0 || len(repo.uses) != 0 {
			t.Fatalf("connected %d, uses %d", repo.link.MailboxesConnected, len(repo.uses))
		}
	})
}

func TestResolveAndAdmit(t *testing.T) {
	ctx := context.Background()
	s, repo, _, _, token := newFixture(t)
	if _, xerr := s.Resolve(ctx, "not-a-token"); xerr != ErrNotFound {
		t.Fatalf("garbage: %v", xerr)
	}
	other, _, _ := newToken()
	if _, xerr := s.Resolve(ctx, other); xerr != ErrNotFound {
		t.Fatalf("unknown: %v", xerr)
	}
	link, xerr := s.Resolve(ctx, token)
	if xerr != nil || link.Status != models.ClientConnectLinkActive {
		t.Fatalf("live link: %v %v", link, xerr)
	}
	if _, xerr := s.StartOAuth(ctx, link, models.InboxProviderGoogle, returnURL, ""); xerr == nil {
		t.Fatal("a provider the link does not allow was started")
	}

	repo.link.MailboxesConnected = repo.link.MaxMailboxes
	if _, xerr := s.Resolve(ctx, token); xerr != ErrExhausted {
		t.Fatalf("full: %v", xerr)
	}
	repo.link.MailboxesConnected = 0
	repo.link.ExpiresAt = time.Now().Add(-time.Second)
	if _, xerr := s.Resolve(ctx, token); xerr != ErrExpired {
		t.Fatalf("expired: %v", xerr)
	}
}

func TestSigninResultIsCollectedByItsOwnLink(t *testing.T) {
	ctx := context.Background()
	s, repo, _, _, token := newFixture(t)
	state := startOutlook(t, s, token)
	key := lastResultKey
	link := repo.link

	if got, _ := s.SigninResult(ctx, link, key); got.Status != "pending" {
		t.Fatalf("before the callback: %+v", got)
	}
	if _, _, xerr := s.FinishOAuth(ctx, "outlook", "c", state, ""); xerr != nil {
		t.Fatal(xerr)
	}
	got, _ := s.SigninResult(ctx, link, key)
	if got.Status != "connected" || got.Email != "client@example.test" || got.Connected != 1 {
		t.Fatalf("after the callback: %+v", got)
	}
	other := *link
	other.ID = uuid.New()
	if got, _ := s.SigninResult(ctx, &other, key); got.Status != "pending" {
		t.Fatalf("another link read the outcome: %+v", got)
	}

	// A refused sign-in leaves its refusal, not a success.
	state = startOutlook(t, s, token)
	if _, _, xerr := s.FinishOAuth(ctx, "outlook", "", state, "access_denied"); xerr != ErrSigninDeclined {
		t.Fatal(xerr)
	}
	if got, _ := s.SigninResult(ctx, link, lastResultKey); got.Status != "failed" || got.Code != "client_link_signin_declined" {
		t.Fatalf("declined: %+v", got)
	}
}

// Without PORTAL_DOMAIN no connect link is minted, even with a forms host: nothing would serve its page.
func TestCreateNeedsThePortalService(t *testing.T) {
	t.Setenv("FORMS_DOMAIN", "forms.example.test")
	t.Setenv("PORTAL_DOMAIN", "")
	if _, xerr := New(Deps{}).Create(context.Background(), uuid.New(), uuid.New(), models.NewClientConnectLink{Label: "Acme"}); xerr != ErrUnavailable {
		t.Fatalf("create without a portal service = %v, want %v", xerr, ErrUnavailable)
	}
	t.Setenv("PORTAL_DOMAIN", "portal.example.test")
	if got := URL(New(Deps{}).d.BaseURL(context.Background(), uuid.New()), "tok"); got != "https://portal.example.test/connect/tok" {
		t.Fatalf("default connect link = %q", got)
	}
}
