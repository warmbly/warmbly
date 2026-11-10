package portalhandoff

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/brandkit"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

const (
	mainOrigin = "https://app.example.test"
	portalHost = "portal.agency.test"
	otherHost  = "other.agency.test"
)

type memStore struct {
	mu  sync.Mutex
	now time.Time
	m   map[string]struct {
		v   []byte
		exp time.Time
	}
}

func (s *memStore) Put(_ context.Context, key string, value []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = struct {
		v   []byte
		exp time.Time
	}{value, s.now.Add(ttl)}
	return nil
}

func (s *memStore) Take(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[key]
	delete(s.m, key)
	if !ok || !s.now.Before(e.exp) {
		return nil, nil
	}
	return e.v, nil
}

type fakePortals map[string]*brandkit.Portal

func (f fakePortals) Lookup(_ context.Context, host string) (*brandkit.Portal, error) {
	return f[host], nil
}

type fakeSessions struct {
	minted  []uuid.UUID
	revoked []string
}

func (f *fakeSessions) GenerateSession(ctx context.Context, userID uuid.UUID, _, _, _, _ string) (*models.Token, *errx.Error) {
	agency, ok := token.PortalAgencyFromContext(ctx)
	if !ok {
		return nil, errx.InternalError()
	}
	f.minted = append(f.minted, agency)
	return &models.Token{AccessToken: "portal-" + userID.String()}, nil
}

func (f *fakeSessions) RevokeSession(_ context.Context, accessToken string) *errx.Error {
	f.revoked = append(f.revoked, accessToken)
	return nil
}

type noBans struct{}

func (noBans) GetBanState(context.Context, uuid.UUID) (uint32, error) { return 0, nil }

type fixture struct {
	svc      *Service
	store    *memStore
	sessions *fakeSessions
	agency   uuid.UUID
	user     uuid.UUID
}

func setup(t *testing.T) *fixture {
	t.Helper()
	t.Setenv("APP_URL", mainOrigin)
	config.SetPortalHosts(func(host string) bool { return host == portalHost || host == otherHost })
	t.Cleanup(func() { config.SetPortalHosts(nil) })
	fx := &fixture{agency: uuid.New(), user: uuid.New(), sessions: &fakeSessions{}}
	fx.store = &memStore{now: time.Now(), m: map[string]struct {
		v   []byte
		exp time.Time
	}{}}
	portals := fakePortals{
		portalHost: {Host: portalHost, AgencyID: fx.agency, SignIn: models.PortalSignInWarmbly},
		otherHost:  {Host: otherHost, AgencyID: uuid.New(), SignIn: models.PortalSignInPortal},
	}
	fx.svc = NewService(fx.store, portals, fx.sessions, noBans{})
	return fx
}

func (fx *fixture) issue(t *testing.T, host string) (string, *errx.Error) {
	t.Helper()
	out, xerr := fx.svc.Issue(context.Background(), Request{
		Session: &models.Session{ID: uuid.New(), UserID: fx.user}, AccessToken: "main", Host: host, Origin: mainOrigin,
	})
	if xerr != nil {
		return "", xerr
	}
	u, err := url.Parse(out.RedirectURL)
	if err != nil || u.Host != portalHost || u.Path != "/portal/return" {
		t.Fatalf("redirect = %q, want the verified portal host", out.RedirectURL)
	}
	return strings.TrimPrefix(u.Fragment, "code="), nil
}

func TestHandoffCodeIsSingleUseAndBindsThePortal(t *testing.T) {
	fx := setup(t)
	code, xerr := fx.issue(t, portalHost)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if _, xerr := fx.svc.Exchange(context.Background(), code, "https://"+portalHost, "203.0.113.1", "ua"); xerr != nil {
		t.Fatalf("first exchange: %v", xerr)
	}
	if len(fx.sessions.minted) != 1 || fx.sessions.minted[0] != fx.agency {
		t.Fatalf("minted bindings = %v, want [%v]", fx.sessions.minted, fx.agency)
	}
	if _, xerr := fx.svc.Exchange(context.Background(), code, "https://"+portalHost, "203.0.113.1", "ua"); xerr != ErrHandoffInvalid {
		t.Fatalf("second exchange = %v, want %v", xerr, ErrHandoffInvalid)
	}
}

func TestHandoffCodeExpires(t *testing.T) {
	fx := setup(t)
	code, _ := fx.issue(t, portalHost)
	fx.store.now = fx.store.now.Add(CodeTTL + time.Second)
	if _, xerr := fx.svc.Exchange(context.Background(), code, "https://"+portalHost, "", ""); xerr != ErrHandoffInvalid {
		t.Fatalf("expired exchange = %v, want %v", xerr, ErrHandoffInvalid)
	}
}

func TestHandoffCodeRefusedFromAnotherHost(t *testing.T) {
	fx := setup(t)
	for _, origin := range []string{"https://" + otherHost, mainOrigin, "", "https://unverified.example.test"} {
		code, _ := fx.issue(t, portalHost)
		if _, xerr := fx.svc.Exchange(context.Background(), code, origin, "", ""); xerr != ErrHandoffInvalid {
			t.Fatalf("exchange from %q = %v, want %v", origin, xerr, ErrHandoffInvalid)
		}
		// A refused attempt spends the code.
		if _, xerr := fx.svc.Exchange(context.Background(), code, "https://"+portalHost, "", ""); xerr != ErrHandoffInvalid {
			t.Fatalf("code survived a refused attempt from %q", origin)
		}
	}
	if len(fx.sessions.minted) != 0 {
		t.Fatalf("sessions minted: %v", fx.sessions.minted)
	}
}

func TestHandoffRefusedForUnverifiedOrPortalSignInHost(t *testing.T) {
	fx := setup(t)
	for _, host := range []string{"unverified.example.test", otherHost} {
		if _, xerr := fx.issue(t, host); xerr != ErrPortalNotFound {
			t.Fatalf("issue for %s = %v, want %v", host, xerr, ErrPortalNotFound)
		}
	}
}

func TestHandoffIssuedOnlyFromTheMainAddress(t *testing.T) {
	fx := setup(t)
	agency := uuid.New()
	for _, req := range []Request{
		{Session: &models.Session{UserID: fx.user}, Host: portalHost, Origin: "https://" + portalHost},
		{Session: &models.Session{UserID: fx.user}, Host: portalHost},
		{Session: &models.Session{UserID: fx.user, PortalAgencyID: &agency}, Host: portalHost, Origin: mainOrigin},
	} {
		if _, xerr := fx.svc.Issue(context.Background(), req); xerr != ErrHandoffOrigin {
			t.Fatalf("issue %+v = %v, want %v", req, xerr, ErrHandoffOrigin)
		}
	}
}

func TestHandoffEndsTheSignInMadeForIt(t *testing.T) {
	fx := setup(t)
	_, xerr := fx.svc.Issue(context.Background(), Request{
		Session: &models.Session{UserID: fx.user}, AccessToken: "temporary", Host: portalHost, Origin: mainOrigin, EndSession: true,
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if len(fx.sessions.revoked) != 1 || fx.sessions.revoked[0] != "temporary" {
		t.Fatalf("revoked = %v, want the requesting session", fx.sessions.revoked)
	}
	if _, xerr := fx.issue(t, portalHost); xerr != nil || len(fx.sessions.revoked) != 1 {
		t.Fatal("a continue-as handoff ended the main session")
	}
}
