package token

import (
	"context"
	"net/netip"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/mileusna/useragent"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

type TokenClaims struct {
	UserID    uuid.UUID `json:"sub"`
	SessionID uuid.UUID `json:"sid"`
	Email     string    `json:"email"`
	Nonce     string    `json:"nonce"`
	jwt.RegisteredClaims
}

func (s *tokenService) GenerateToken(userID, sessionID uuid.UUID, email, nonce string, issuedAt, expiresAt time.Time) (string, error) {
	claims := TokenClaims{
		UserID:    userID,
		SessionID: sessionID,
		Email:     email,
		Nonce:     nonce,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(issuedAt),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.AuthSecret))
}

func (s *tokenService) VerifyToken(tokenStr string) (*TokenClaims, *errx.Error) {
	token, err := jwt.ParseWithClaims(tokenStr, &TokenClaims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errx.ErrToken
		}
		return []byte(s.AuthSecret), nil
	})

	if err != nil {
		return nil, errx.ErrToken
	}

	claims, ok := token.Claims.(*TokenClaims)
	if !ok || !token.Valid {
		return nil, errx.ErrToken
	}

	if claims.ExpiresAt == nil || time.Now().After(claims.ExpiresAt.Time) {
		return nil, errx.ErrToken
	}

	return claims, nil
}

func (s *tokenService) GenerateSession(ctx context.Context, userID uuid.UUID, email, ipaddr, userAgent, authProvider string) (*models.Token, *errx.Error) {
	return s.GenerateSessionWithOrg(ctx, userID, email, ipaddr, userAgent, authProvider, nil)
}

func (s *tokenService) GenerateSessionWithOrg(ctx context.Context, userID uuid.UUID, email, ipaddr, userAgent, authProvider string, orgID *uuid.UUID) (*models.Token, *errx.Error) {
	ip, err := netip.ParseAddr(ipaddr)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	ipinfo, err := s.geo.Lookup(ip)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	userAgentInfo := useragent.Parse(userAgent)

	session := &models.Session{
		ID:                    uuid.New(),
		UserID:                userID,
		CurrentOrganizationID: orgID,

		LocationCity:       ipinfo.City,
		LocationRegion:     ipinfo.Region,
		LocationCountry:    ipinfo.Country,
		LocationPostalCode: ipinfo.PostalCode,

		BrowserName: userAgentInfo.Name,
		OSName:      userAgentInfo.Name,
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return nil, errx.InternalError()
	}
	defer tx.Rollback(ctx)

	issuedAt := time.Now()
	session.LastRefreshedAt = issuedAt
	session.CreatedAt = issuedAt

	accessTokenExpiresAt := issuedAt.Add(10 * time.Minute)
	accessNonce, err := crypt.Nonce()
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	session.AccessNonce = accessNonce

	accessToken, err := s.GenerateToken(userID, session.ID, email, accessNonce, issuedAt, accessTokenExpiresAt)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	refreshTokenExpiresAt := issuedAt.Add(2 * 30 * 24 * time.Hour)
	session.ExpiresAt = &refreshTokenExpiresAt
	refreshNonce, err := crypt.Nonce()
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	session.RefreshNonce = refreshNonce

	refreshToken, err := s.GenerateToken(userID, session.ID, email, refreshNonce, issuedAt, refreshTokenExpiresAt)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	if err := s.tokenRepository.GenerateSession(ctx, tx, session); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return nil, errx.InternalError()
	}

	return &models.Token{
		AccessToken:           accessToken,
		AccessTokenExpiresAt:  accessTokenExpiresAt,
		RefreshToken:          refreshToken,
		RefreshTokenExpiresAt: refreshTokenExpiresAt,
	}, nil
}

// SwitchOrganization updates the current organization for a session
func (s *tokenService) SwitchOrganization(ctx context.Context, sessionID uuid.UUID, orgID *uuid.UUID) *errx.Error {
	// Update in database
	if err := s.tokenRepository.UpdateCurrentOrganization(ctx, sessionID, orgID); err != nil {
		return err
	}

	// Invalidate cache for this session
	s.deleteSession(ctx, sessionID)

	return nil
}

// GetCurrentOrganization retrieves the current organization for a session
func (s *tokenService) GetCurrentOrganization(ctx context.Context, sessionID uuid.UUID) (*uuid.UUID, *errx.Error) {
	session, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return session.CurrentOrganizationID, nil
}
