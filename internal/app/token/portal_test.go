package token

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// The sign-in request's Origin decides the binding: a verified portal origin binds, anything else does not.
func TestSignInOriginDecidesPortalBinding(t *testing.T) {
	t.Setenv("APP_URL", "https://app.example.test")
	const portalHost = "portal.agency.test"
	config.SetPortalHosts(func(host string) bool { return host == portalHost })
	t.Cleanup(func() { config.SetPortalHosts(nil) })

	agency := uuid.New()
	s := &tokenService{portals: func(_ context.Context, host string) (*uuid.UUID, models.PortalSignIn, error) {
		if host == portalHost {
			return &agency, models.PortalSignInPortal, nil
		}
		return nil, "", nil
	}}
	from := func(origin string) context.Context {
		return config.WithDashboardOrigin(context.Background(), origin)
	}

	for _, tc := range []struct {
		name   string
		ctx    context.Context
		bound  bool
		wanted uuid.UUID
	}{
		{"no origin", context.Background(), false, uuid.Nil},
		{"main origin", from("https://app.example.test"), false, uuid.Nil},
		{"unverified host", from("https://other.agency.test"), false, uuid.Nil},
		{"verified portal", from("https://" + portalHost), true, agency},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, xerr := s.portalBinding(tc.ctx)
			if xerr != nil {
				t.Fatalf("error: %v", xerr)
			}
			if (got != nil) != tc.bound || (got != nil && *got != tc.wanted) {
				t.Fatalf("binding = %v, want bound=%v %v", got, tc.bound, tc.wanted)
			}
		})
	}

	explicit := uuid.New()
	if got, _ := s.portalBinding(WithPortalAgency(from("https://app.example.test"), explicit)); got == nil || *got != explicit {
		t.Fatalf("explicit binding = %v, want %v", got, explicit)
	}

	failing := &tokenService{portals: func(context.Context, string) (*uuid.UUID, models.PortalSignIn, error) {
		return nil, "", errors.New("down")
	}}
	if _, xerr := failing.portalBinding(from("https://" + portalHost)); xerr == nil {
		t.Fatal("an unresolvable portal origin minted an unbound session")
	}
	if _, xerr := (&tokenService{}).portalBinding(from("https://" + portalHost)); xerr == nil {
		t.Fatal("a portal origin with no resolver minted an unbound session")
	}

	// A portal whose agency signs clients in on the main address mints nothing from its own origin.
	elsewhere := &tokenService{portals: func(context.Context, string) (*uuid.UUID, models.PortalSignIn, error) {
		return &agency, models.PortalSignInWarmbly, nil
	}}
	if _, xerr := elsewhere.portalBinding(from("https://" + portalHost)); xerr == nil || xerr.Identifier != "portal_sign_in_elsewhere" {
		t.Fatalf("sign-in on a main-address portal = %v, want portal_sign_in_elsewhere", xerr)
	}
	if got, xerr := elsewhere.portalBinding(WithPortalAgency(from("https://"+portalHost), agency)); xerr != nil || got == nil {
		t.Fatalf("the handoff's explicit binding was refused: %v", xerr)
	}
}

type rotatingRepo struct {
	repository.TokenRepository
	session *models.Session
}

func (r *rotatingRepo) GetSession(context.Context, uuid.UUID) (*models.Session, *errx.Error) {
	return r.session, nil
}

func (r *rotatingRepo) RefreshToken(context.Context, uuid.UUID, string, string, string, time.Time) *errx.Error {
	return nil
}

// A portal session's tokens carry their own purposes, which a release that only knows access, refresh and ws refuses.
func TestPortalSessionTokensCarryTheirOwnPurposes(t *testing.T) {
	agency := uuid.New()
	issued := time.Now().Truncate(time.Second)
	expires := issued.Add(time.Hour)
	row := &models.Session{ID: uuid.New(), UserID: uuid.New(), ExpiresAt: &expires, LastRefreshedAt: issued,
		AccessNonce: "a", RefreshNonce: "r", PortalAgencyID: &agency}
	down := &cache.Cache{Client: redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 100 * time.Millisecond})}
	s := &tokenService{AuthSecret: "test-secret-at-least-32-characters-long", tokenRepository: &rotatingRepo{session: row}, cache: down}
	mint := func(purpose, nonce string) string {
		tok, err := s.GenerateTokenFor(purpose, row.UserID, row.ID, "", nonce, issued, expires)
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}

	if _, xerr := s.ValidateAccessToken(context.Background(), mint(PurposePortalAccess, "a")); xerr != nil {
		t.Fatalf("portal access token refused on its own session: %v", xerr)
	}
	if _, xerr := s.ValidateAccessToken(context.Background(), mint(PurposeAccess, "a")); xerr == nil {
		t.Fatal("a plain access token was accepted for a portal session")
	}
	for _, check := range []struct{ purpose, token string }{
		{PurposeAccess, mint(PurposePortalAccess, "a")},
		{PurposeRefresh, mint(PurposePortalRefresh, "r")},
		{PurposeWebSocket, mint(PurposePortalWebSocket, "n")},
	} {
		if _, xerr := s.VerifyTokenFor(check.purpose, check.token); xerr == nil {
			t.Fatalf("the %s check accepted a portal token", check.purpose)
		}
	}

	rotated, xerr := s.RefreshToken(context.Background(), mint(PurposePortalRefresh, "r"))
	if xerr != nil {
		t.Fatalf("portal refresh refused: %v", xerr)
	}
	for tok, want := range map[string]string{rotated.AccessToken: PurposePortalAccess, rotated.RefreshToken: PurposePortalRefresh} {
		if claims, _ := s.VerifyToken(tok); claims == nil || claims.Purpose != want {
			t.Fatalf("rotated token purpose = %v, want %s", claims, want)
		}
	}
	if _, xerr := s.RefreshToken(context.Background(), mint(PurposeRefresh, "r")); xerr == nil {
		t.Fatal("a plain refresh token rotated a portal session")
	}

	ticket, err := s.GenerateWebsocketTicket(row.UserID, uuid.New(), row.ID, "n", issued, expires, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, xerr := s.VerifyWebsocketTicket(ticket); xerr != nil {
		t.Fatalf("portal socket ticket refused: %v", xerr)
	}

	// A session signed in on the main address refuses portal tokens.
	row.PortalAgencyID = nil
	if _, xerr := s.ValidateAccessToken(context.Background(), mint(PurposePortalAccess, "a")); xerr == nil {
		t.Fatal("a portal access token was accepted for a main-address session")
	}
}
