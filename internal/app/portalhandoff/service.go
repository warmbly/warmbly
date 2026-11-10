// Package portalhandoff signs a person in to an agency's portal domain from the main dashboard address:
// the main address issues a one-time code for one verified portal host, and that host exchanges it
// for a session bound to the portal's agency.
package portalhandoff

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/brandkit"
	"github.com/warmbly/warmbly/internal/app/customhost"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

// CodeTTL is how long the portal has to exchange a code; the redirect and the exchange are back to back.
const CodeTTL = 60 * time.Second

const keyPrefix = "portal_handoff:"

var (
	ErrPortalNotFound = errx.NewWithIdentifier(errx.NotFound, "portal_not_found",
		"That address is not a verified agency portal that signs clients in here.")
	ErrHandoffOrigin = errx.NewWithIdentifier(errx.Forbidden, "portal_handoff_origin",
		"A portal sign-in is handed over from the main dashboard address only.")
	ErrHandoffInvalid = errx.NewWithIdentifier(errx.BadRequest, "portal_handoff_invalid",
		"This sign-in link has expired or was already used. Sign in again.")
	errSuspended = errx.ErrAccountSuspended
)

// Store keeps codes; Take reads and deletes in one step and is nil when the key is absent.
type Store interface {
	Put(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Take(ctx context.Context, key string) ([]byte, error)
}

// Portals is the verified portal registry.
type Portals interface {
	Lookup(ctx context.Context, host string) (*brandkit.Portal, error)
}

// Sessions mints and ends sessions.
type Sessions interface {
	GenerateSession(ctx context.Context, userID uuid.UUID, email, ipaddr, userAgent, authProvider string) (*models.Token, *errx.Error)
	RevokeSession(ctx context.Context, accessToken string) *errx.Error
}

// Bans reads a user's ban scope.
type Bans interface {
	GetBanState(ctx context.Context, userID uuid.UUID) (uint32, error)
}

type Service struct {
	store    Store
	portals  Portals
	sessions Sessions
	bans     Bans
}

func NewService(store Store, portals Portals, sessions Sessions, bans Bans) *Service {
	return &Service{store: store, portals: portals, sessions: sessions, bans: bans}
}

// record is what one code stands for.
type record struct {
	UserID   uuid.UUID `json:"user_id"`
	Host     string    `json:"host"`
	AgencyID uuid.UUID `json:"agency_id"`
	Provider string    `json:"provider"`
}

// Handoff is the answer to POST /auth/portal/handoff.
type Handoff struct {
	RedirectURL string  `json:"redirect_url"`
	ExpiresIn   float64 `json:"expires_in"`
}

// Request is one handoff asked for by a signed-in session on the main address.
type Request struct {
	Session     *models.Session
	AccessToken string
	Host        string
	// EndSession ends the requesting session once the code is stored, for a sign-in made only to reach the portal.
	EndSession bool
	// Origin is the request's Origin header.
	Origin string
}

func codeKey(code string) string {
	sum := sha256.Sum256([]byte(code))
	return keyPrefix + hex.EncodeToString(sum[:])
}

// Issue stores a one-time code for the portal at req.Host and returns where to send the browser.
func (s *Service) Issue(ctx context.Context, req Request) (*Handoff, *errx.Error) {
	if req.Session == nil {
		return nil, errx.ErrUnauthorized
	}
	if config.DashboardOrigin(req.Origin) == "" || req.Session.PortalAgencyID != nil {
		return nil, ErrHandoffOrigin
	}
	host := customhost.Normalize(req.Host)
	p, err := s.portals.Lookup(ctx, host)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	if p == nil || p.SignIn != models.PortalSignInWarmbly {
		return nil, ErrPortalNotFound
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	code := base64.RawURLEncoding.EncodeToString(raw)
	payload, err := json.Marshal(record{UserID: req.Session.UserID, Host: p.Host, AgencyID: p.AgencyID, Provider: req.Session.AuthProvider})
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	if err := s.store.Put(ctx, codeKey(code), payload, CodeTTL); err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	if req.EndSession {
		if xerr := s.sessions.RevokeSession(ctx, req.AccessToken); xerr != nil {
			_, _ = s.store.Take(ctx, codeKey(code))
			return nil, xerr
		}
	}
	return &Handoff{
		RedirectURL: config.PortalOriginOn(p.Host) + "/portal/return#code=" + code,
		ExpiresIn:   CodeTTL.Seconds(),
	}, nil
}

// Exchange spends a code once, from the portal origin it was issued for, for a session bound to that portal's agency.
func (s *Service) Exchange(ctx context.Context, code, origin, ipaddr, userAgent string) (*models.Token, *errx.Error) {
	if code == "" || len(code) > 128 {
		return nil, ErrHandoffInvalid
	}
	// Taken before anything is checked, so every attempt spends the code.
	raw, err := s.store.Take(ctx, codeKey(code))
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	if len(raw) == 0 {
		return nil, ErrHandoffInvalid
	}
	var rec record
	if err := json.Unmarshal(raw, &rec); err != nil || rec.UserID == uuid.Nil || rec.AgencyID == uuid.Nil {
		return nil, ErrHandoffInvalid
	}
	if config.PortalOriginHost(origin) != rec.Host || config.PortalOrigin(origin) == "" {
		return nil, ErrHandoffInvalid
	}
	p, err := s.portals.Lookup(ctx, rec.Host)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	if p == nil || p.AgencyID != rec.AgencyID {
		return nil, ErrHandoffInvalid
	}
	scope, err := s.bans.GetBanState(ctx, rec.UserID)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	if models.BanScope(scope).Has(models.BanScopeLogin) {
		return nil, errSuspended
	}
	provider := rec.Provider
	if provider == "" {
		provider = token.AuthProviderEmail
	}
	return s.sessions.GenerateSession(token.WithPortalAgency(ctx, rec.AgencyID), rec.UserID, "", ipaddr, userAgent, provider)
}
