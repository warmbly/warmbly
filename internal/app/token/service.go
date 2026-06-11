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
	VerifyToken(tokenStr string) (*TokenClaims, *errx.Error)
	GenerateSession(ctx context.Context, userID uuid.UUID, email, ipaddr, userAgent, authProvider string) (*models.Token, *errx.Error)
	GenerateSessionWithOrg(ctx context.Context, userID uuid.UUID, email, ipaddr, userAgent, authProvider string, orgID *uuid.UUID) (*models.Token, *errx.Error)
	WireSignInAlerter(a SignInAlerter)
	GetSession(ctx context.Context, sessionID uuid.UUID) (*models.Session, *errx.Error)
	ValidateAccessToken(ctx context.Context, accessToken string) (*models.Session, *errx.Error)
	RefreshToken(ctx context.Context, refreshToken string) (*models.Token, *errx.Error)

	RevokeSession(ctx context.Context, accessToken string) *errx.Error
	RevokeAllSession(ctx context.Context, accessToken string) *errx.Error

	// Self-service session management (account security page)
	ListSessions(ctx context.Context, userID, currentSessionID uuid.UUID) ([]SessionView, *errx.Error)
	RevokeSessionByID(ctx context.Context, userID, sessionID, currentSessionID uuid.UUID) *errx.Error
	RevokeOtherSessions(ctx context.Context, userID, currentSessionID uuid.UUID) *errx.Error

	// Organization switching
	SwitchOrganization(ctx context.Context, sessionID uuid.UUID, orgID *uuid.UUID) *errx.Error
	GetCurrentOrganization(ctx context.Context, sessionID uuid.UUID) (*uuid.UUID, *errx.Error)
}

type tokenService struct {
	db              *db.DB
	tokenRepository repository.TokenRepository
	geo             *geo.Client
	cache           *cache.Cache
	signInAlert     SignInAlerter

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

func NewService(db *db.DB, tokenRepository repository.TokenRepository, cache *cache.Cache, geo *geo.Client, authSecret string) TokenService {
	return &tokenService{
		db:              db,
		tokenRepository: tokenRepository,
		geo:             geo,
		cache:           cache,
		AuthSecret:      authSecret,
	}
}
