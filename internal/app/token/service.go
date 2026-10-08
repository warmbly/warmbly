package token

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/cache"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/geo"
	"github.com/warmbly/warmbly/internal/repository"
)

type TokenService interface {
	GenerateToken(userID, sessionID uuid.UUID, email, nonce string, issuedAt, expiresAt time.Time) (string, error)
	// GenerateTokenFor mints a token for a named purpose (see the Purpose*
	// constants). Every verifier requires the purpose it expects, so a token
	// minted for one flow cannot be spent on another.
	GenerateTokenFor(purpose string, userID, sessionID uuid.UUID, email, nonce string, issuedAt, expiresAt time.Time) (string, error)
	GenerateWebsocketProxyProof(ticket *TokenClaims, clientIP string) (string, error)
	VerifyToken(tokenStr string) (*TokenClaims, *errx.Error)
	// VerifyTokenFor also requires the token to have been minted for this
	// purpose, so one flow's token cannot be spent on another.
	VerifyTokenFor(purpose, tokenStr string) (*TokenClaims, *errx.Error)
	GenerateSession(ctx context.Context, userID uuid.UUID, email, ipaddr, userAgent, authProvider string) (*models.Token, *errx.Error)
	GenerateSessionWithOrg(ctx context.Context, userID uuid.UUID, email, ipaddr, userAgent, authProvider string, orgID *uuid.UUID) (*models.Token, *errx.Error)
	// GenerateMFASession is GenerateSession for a sign-in that presented a
	// second factor. The flag is recorded on the session so a later request can
	// require it, which is what the admin panel does.
	GenerateMFASession(ctx context.Context, userID uuid.UUID, email, ipaddr, userAgent, authProvider string) (*models.Token, *errx.Error)
	WireSignInAlerter(a SignInAlerter)
	WireSessionObserver(o SessionObserver)
	WireRevocationPublisher(p RevocationPublisher)
	GetSession(ctx context.Context, sessionID uuid.UUID) (*models.Session, *errx.Error)
	ValidateAccessToken(ctx context.Context, accessToken string) (*models.Session, *errx.Error)
	RefreshToken(ctx context.Context, refreshToken string) (*models.Token, *errx.Error)

	RevokeSession(ctx context.Context, accessToken string) *errx.Error
	RevokeAllSession(ctx context.Context, accessToken string) *errx.Error

	// Self-service session management (account security page)
	ListSessions(ctx context.Context, userID, currentSessionID uuid.UUID) ([]SessionView, *errx.Error)
	RevokeSessionByID(ctx context.Context, userID, sessionID, currentSessionID uuid.UUID) *errx.Error
	RevokeOtherSessions(ctx context.Context, userID, currentSessionID uuid.UUID) *errx.Error
	// ReissueSession ends every one of the user's sessions, the current one
	// included, and returns the token pair of a fresh session for the same
	// device. A credential change calls it so nothing issued before the change
	// is accepted after it. current is the caller's session as the middleware
	// resolved it; nil means no context to carry over.
	ReissueSession(ctx context.Context, userID uuid.UUID, current *models.Session, ipaddr, userAgent string) (*models.Token, *errx.Error)
	// StampReauth records that this session just re-proved the account holder,
	// which is what RequireFreshAuth checks before a sensitive change.
	StampReauth(ctx context.Context, sessionID uuid.UUID) *errx.Error

	// Organization switching
	SwitchOrganization(ctx context.Context, sessionID uuid.UUID, orgID *uuid.UUID) *errx.Error
	GetCurrentOrganization(ctx context.Context, sessionID uuid.UUID) (*uuid.UUID, *errx.Error)
	// LeaveOrganization deselects an organization the user no longer belongs to
	// on every one of their sessions, so the dashboard asks them to pick again.
	LeaveOrganization(ctx context.Context, userID, orgID uuid.UUID) *errx.Error
}

type tokenService struct {
	db              *db.DB
	tokenRepository repository.TokenRepository
	geo             *geo.Client
	cache           *cache.Cache
	signInAlert     SignInAlerter
	sessionObserver SessionObserver
	revocations     RevocationPublisher

	AuthSecret string
}

// SignInAlerter fires a "new device" notification when a session is created
// from a device the user has not signed in from before. Satisfied by an
// adapter over the notification service; wired post-construction (nil = off).
type SignInAlerter interface {
	NewSignIn(ctx context.Context, userID uuid.UUID, browser, os, city, country string)
}

// WireSignInAlerter attaches the new-device alerter after construction.
func (s *tokenService) WireSignInAlerter(a SignInAlerter) { s.signInAlert = a }

// SessionStart describes a completed sign-in. A session re-minted for an
// already signed-in device (password change) is not one.
type SessionStart struct {
	UserID       uuid.UUID
	IP           string
	Browser      string
	OS           string
	City         string
	Country      string
	AuthProvider string
	MFAVerified  bool
	NewDevice    bool
}

// SessionObserver hears about every completed sign-in, off the request path.
type SessionObserver interface {
	SessionStarted(ctx context.Context, start SessionStart)
}

// WireSessionObserver attaches the sign-in observer after construction.
func (s *tokenService) WireSessionObserver(o SessionObserver) { s.sessionObserver = o }

// RevocationPublisher announces that a user's sessions were revoked, so the
// realtime service drops their open sockets. Satisfied by the streaming publisher.
type RevocationPublisher interface {
	PublishSessionsRevoked(ctx context.Context, userID uuid.UUID)
}

// WireRevocationPublisher attaches the revocation announcer (nil = off).
func (s *tokenService) WireRevocationPublisher(p RevocationPublisher) { s.revocations = p }

func NewService(db *db.DB, tokenRepository repository.TokenRepository, cache *cache.Cache, geo *geo.Client, authSecret string) TokenService {
	return &tokenService{
		db:              db,
		tokenRepository: tokenRepository,
		geo:             geo,
		cache:           cache,
		AuthSecret:      authSecret,
	}
}
