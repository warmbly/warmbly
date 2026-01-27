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
	GetSession(ctx context.Context, sessionID uuid.UUID) (*models.Session, *errx.Error)
	ValidateAccessToken(ctx context.Context, accessToken string) (*models.Session, *errx.Error)
	RefreshToken(ctx context.Context, refreshToken string) (*models.Token, *errx.Error)

	RevokeSession(ctx context.Context, accessToken string) *errx.Error
	RevokeAllSession(ctx context.Context, accessToken string) *errx.Error

	// Organization switching
	SwitchOrganization(ctx context.Context, sessionID uuid.UUID, orgID *uuid.UUID) *errx.Error
	GetCurrentOrganization(ctx context.Context, sessionID uuid.UUID) (*uuid.UUID, *errx.Error)
}

type tokenService struct {
	db              *db.DB
	tokenRepository repository.TokenRepository
	geo             *geo.Client
	cache           *cache.Cache

	AuthSecret string
}

func NewService(db *db.DB, tokenRepository repository.TokenRepository, cache *cache.Cache, geo *geo.Client, authSecret string) TokenService {
	return &tokenService{
		db:              db,
		tokenRepository: tokenRepository,
		geo:             geo,
		cache:           cache,
		AuthSecret:      authSecret,
	}
}
