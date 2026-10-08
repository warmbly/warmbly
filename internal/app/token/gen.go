package token

import (
	"context"
	"net/netip"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/mileusna/useragent"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
)

type TokenClaims struct {
	UserID    uuid.UUID `json:"sub"`
	SessionID uuid.UUID `json:"sid"`
	Email     string    `json:"email"`
	Nonce     string    `json:"nonce"`
	// Purpose names what this token may be spent on. See the Purpose*
	// constants: one signing key issues all of them, so this is what keeps a
	// password-reset token from being accepted as a session.
	Purpose  string `json:"purpose,omitempty"`
	ClientIP string `json:"client_ip,omitempty"`
	jwt.RegisteredClaims
}

// GenerateToken mints a token for a purpose. Callers use the Purpose*
// constants; a token minted with the wrong one is refused at the verifier that
// expects a different one.
func (s *tokenService) GenerateToken(userID, sessionID uuid.UUID, email, nonce string, issuedAt, expiresAt time.Time) (string, error) {
	return s.GenerateTokenFor(PurposeAccess, userID, sessionID, email, nonce, issuedAt, expiresAt)
}

func (s *tokenService) GenerateTokenFor(purpose string, userID, sessionID uuid.UUID, email, nonce string, issuedAt, expiresAt time.Time) (string, error) {
	claims := TokenClaims{
		UserID:    userID,
		SessionID: sessionID,
		Email:     email,
		Nonce:     nonce,
		Purpose:   purpose,
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
		return []byte(s.AuthSecret), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())

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

// The proof is bound to one ticket and is never accepted as a socket credential.
func (s *tokenService) GenerateWebsocketProxyProof(ticket *TokenClaims, clientIP string) (string, error) {
	now := time.Now()
	claims := TokenClaims{
		UserID: ticket.UserID, SessionID: ticket.SessionID, Nonce: ticket.Nonce,
		Purpose: PurposeWebSocketProxy, ClientIP: clientIP,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(30 * time.Second)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.AuthSecret))
}

func (s *tokenService) GenerateSession(ctx context.Context, userID uuid.UUID, email, ipaddr, userAgent, authProvider string) (*models.Token, *errx.Error) {
	return s.generateSession(ctx, userID, email, ipaddr, userAgent, authProvider, nil, false, true)
}

// GenerateMFASession marks the session as having presented a second factor.
// Only the TOTP and passkey paths may call it.
func (s *tokenService) GenerateMFASession(ctx context.Context, userID uuid.UUID, email, ipaddr, userAgent, authProvider string) (*models.Token, *errx.Error) {
	return s.generateSession(ctx, userID, email, ipaddr, userAgent, authProvider, nil, true, true)
}

func (s *tokenService) GenerateSessionWithOrg(ctx context.Context, userID uuid.UUID, email, ipaddr, userAgent, authProvider string, orgID *uuid.UUID) (*models.Token, *errx.Error) {
	return s.generateSession(ctx, userID, email, ipaddr, userAgent, authProvider, orgID, false, true)
}

// ReissueSession ends every one of the user's sessions, the caller's included,
// then mints a fresh one for the caller's device that keeps its workspace,
// sign-in method and MFA status. The caller's session comes from the request
// (nothing is looked up, so nothing can fail before the revoke), and revoke
// runs before mint so a failed mint leaves the account signed out rather than
// reachable with the old token.
func (s *tokenService) ReissueSession(ctx context.Context, userID uuid.UUID, current *models.Session, ipaddr, userAgent string) (*models.Token, *errx.Error) {
	provider := AuthProviderEmail
	var orgID *uuid.UUID
	mfaVerified := false
	if current != nil && current.UserID == userID {
		provider = current.AuthProvider
		orgID = current.CurrentOrganizationID
		mfaVerified = current.MFAVerified
	}

	err := s.revokeOthers(ctx, userID, uuid.Nil)
	var tok *models.Token
	if err == nil {
		tok, err = s.generateSession(ctx, userID, "", ipaddr, userAgent, provider, orgID, mfaVerified, false)
	}
	// After the new session exists, so the caller's socket reconnects with it.
	s.notifyRevoked(ctx, userID)
	return tok, err
}

// signIn is false when re-minting for a device that is already signed in.
func (s *tokenService) generateSession(ctx context.Context, userID uuid.UUID, email, ipaddr, userAgent, authProvider string, orgID *uuid.UUID, mfaVerified, signIn bool) (*models.Token, *errx.Error) {
	// A session always starts inside a workspace. Without one the caller would
	// reach org-scoped writes with no tenant, and the rows they create are the
	// ones that later skip suppression and the entitlement gate (issue #168).
	if orgID == nil {
		resolved, xerr := s.tokenRepository.DefaultOrganization(ctx, userID)
		if xerr != nil {
			return nil, xerr
		}
		orgID = resolved
	}

	ip, err := netip.ParseAddr(ipaddr)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}

	ipinfo, err := s.geo.Lookup(ip)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}

	userAgentInfo := useragent.Parse(userAgent)

	// New-device check (before inserting this session): does the user already
	// have an active session from this OS+browser? Only meaningful when they
	// have a prior session to compare against, so a first/fresh login is quiet.
	newDevice := false
	if s.signInAlert != nil || s.sessionObserver != nil {
		if prior, perr := s.tokenRepository.ListSessionsByUser(ctx, userID); perr == nil && len(prior) > 0 {
			seen := false
			for _, p := range prior {
				if p.OSName == userAgentInfo.OS && p.BrowserName == userAgentInfo.Name {
					seen = true
					break
				}
			}
			newDevice = !seen
		}
	}

	session := &models.Session{
		ID:                    uuid.New(),
		UserID:                userID,
		CurrentOrganizationID: orgID,

		LocationCity:        ipinfo.City,
		LocationRegion:      ipinfo.Region,
		LocationCountry:     ipinfo.Country,
		LocationCountryCode: ipinfo.CountryCode,
		LocationPostalCode:  ipinfo.PostalCode,

		BrowserName: userAgentInfo.Name,
		OSName:      userAgentInfo.OS,

		AuthProvider: authProvider,
		MFAVerified:  mfaVerified,
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
	// A session is minted only by a completed sign-in, which is itself a fresh confirmation.
	session.ReauthAt = &issuedAt

	accessTokenExpiresAt := issuedAt.Add(AccessTokenLifeTime)
	accessNonce, err := crypt.Nonce()
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	session.AccessNonce = accessNonce

	accessToken, err := s.GenerateTokenFor(PurposeAccess, userID, session.ID, email, accessNonce, issuedAt, accessTokenExpiresAt)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}

	refreshTokenExpiresAt := issuedAt.Add(RefreshTokenLifeTime)
	session.ExpiresAt = &refreshTokenExpiresAt
	refreshNonce, err := crypt.Nonce()
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	session.RefreshNonce = refreshNonce

	refreshToken, err := s.GenerateTokenFor(PurposeRefresh, userID, session.ID, email, refreshNonce, issuedAt, refreshTokenExpiresAt)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}

	if err := s.tokenRepository.GenerateSession(ctx, tx, session); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return nil, errx.InternalError()
	}

	if newDevice && s.signInAlert != nil {
		alerter := s.signInAlert
		uid, browser, osName := userID, session.BrowserName, session.OSName
		city, country := session.LocationCity, session.LocationCountry
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			alerter.NewSignIn(ctx, uid, browser, osName, city, country)
		}()
	}

	if signIn && s.sessionObserver != nil {
		observer := s.sessionObserver
		start := SessionStart{
			UserID: userID, IP: ipaddr,
			Browser: session.BrowserName, OS: session.OSName,
			City: session.LocationCity, Country: session.LocationCountry,
			AuthProvider: authProvider, MFAVerified: mfaVerified, NewDevice: newDevice,
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			observer.SessionStarted(ctx, start)
		}()
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

// LeaveOrganization leaves the session with no workspace rather than moving it
// to another, so a form left open cannot write into a workspace it was not for.
func (s *tokenService) LeaveOrganization(ctx context.Context, userID, orgID uuid.UUID) *errx.Error {
	cleared, xerr := s.tokenRepository.ClearOrganization(ctx, userID, orgID)
	if xerr != nil {
		return xerr
	}
	var failed *errx.Error
	for _, id := range cleared {
		if xerr := s.deleteSession(ctx, id); xerr != nil {
			failed = xerr
		}
	}
	return failed
}

// GetCurrentOrganization retrieves the current organization for a session
func (s *tokenService) GetCurrentOrganization(ctx context.Context, sessionID uuid.UUID) (*uuid.UUID, *errx.Error) {
	session, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return session.CurrentOrganizationID, nil
}
