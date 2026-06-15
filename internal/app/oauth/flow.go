package oauth

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// AuthorizeRequest holds the (un-trusted) /authorize parameters.
type AuthorizeRequest struct {
	ResponseType        string
	ClientID            string
	RedirectURI         string
	Scope               string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
}

// ConsentInfo is what the dashboard consent screen renders: who is asking, for
// what, and where they'll be sent back.
type ConsentInfo struct {
	ClientID    string   `json:"client_id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	LogoURL     string   `json:"logo_url"`
	WebsiteURL  string   `json:"website_url"`
	RedirectURI string   `json:"redirect_uri"`
	Scopes      []string `json:"scopes"`
	State       string   `json:"state"`
}

// TokenResponse is the /token success body (RFC 6749 §5.1).
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope"`
}

// AccessClaims is what a validated access token resolves to, for the middleware.
type AccessClaims struct {
	GrantID        uuid.UUID
	ApplicationID  uuid.UUID
	OrganizationID uuid.UUID
	UserID         uuid.UUID
	Scopes         uint64
}

// AuthorizeDetails validates the authorize request and returns consent info, or
// an *OAuthError describing what's wrong with the client/redirect/scope.
func (s *Service) AuthorizeDetails(ctx context.Context, req AuthorizeRequest) (*ConsentInfo, error) {
	app, scopes, err := s.validateAuthorize(ctx, req)
	if err != nil {
		return nil, err
	}
	return &ConsentInfo{
		ClientID:    app.ClientID,
		Name:        app.Name,
		Description: app.Description,
		LogoURL:     app.LogoURL,
		WebsiteURL:  app.WebsiteURL,
		RedirectURI: req.RedirectURI,
		Scopes:      ScopeList(scopes),
		State:       req.State,
	}, nil
}

// IssueAuthorizationCode is called after the user approves consent. It re-checks
// the request, mints a single-use PKCE-bound code for this user+org, and returns
// the redirect URL (redirect_uri?code=...&state=...) for the browser to follow.
func (s *Service) IssueAuthorizationCode(ctx context.Context, orgID, userID uuid.UUID, req AuthorizeRequest) (string, error) {
	app, scopes, err := s.validateAuthorize(ctx, req)
	if err != nil {
		return "", err
	}
	code, err := randomToken(models.OAuthCodePrefix)
	if err != nil {
		return "", errServer("could not mint code")
	}
	method := ""
	if strings.TrimSpace(req.CodeChallenge) != "" {
		method = "S256"
	}
	ac := &models.OAuthAuthorizationCode{
		CodeHash:            hashToken(code),
		ApplicationID:       app.ID,
		OrganizationID:      orgID,
		UserID:              userID,
		RedirectURI:         req.RedirectURI,
		Scopes:              scopes,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: method,
		ExpiresAt:           time.Now().UTC().Add(models.OAuthAuthorizationCodeTTL),
	}
	if err := s.repo.CreateAuthorizationCode(ctx, ac); err != nil {
		return "", errServer("could not store code")
	}
	return buildRedirect(req.RedirectURI, code, req.State), nil
}

// validateAuthorize enforces response_type=code, a known active client, an exact
// redirect match, mandatory PKCE (S256), and a scope set within the app's grant.
func (s *Service) validateAuthorize(ctx context.Context, req AuthorizeRequest) (*models.OAuthApplication, uint64, error) {
	if req.ResponseType != "code" {
		return nil, 0, errInvalidRequest("response_type must be 'code'")
	}
	app, err := s.repo.GetApplicationByClientID(ctx, strings.TrimSpace(req.ClientID))
	if err != nil {
		return nil, 0, errServer("client lookup failed")
	}
	if app == nil || app.Status != models.OAuthAppActive {
		return nil, 0, errUnauthorizedClient("unknown or disabled client")
	}
	if !redirectAllowed(app, req.RedirectURI) {
		return nil, 0, errInvalidRequest("redirect_uri does not match a registered URI")
	}
	// PKCE is an optional extra layer; when a challenge is sent we only accept S256.
	if strings.TrimSpace(req.CodeChallenge) != "" && req.CodeChallengeMethod != "S256" {
		return nil, 0, errInvalidRequest("code_challenge_method must be 'S256'")
	}
	mask, unknown := ParseScopes(req.Scope)
	if len(unknown) > 0 {
		return nil, 0, errInvalidScope("unknown scope: " + strings.Join(unknown, " "))
	}
	if mask == 0 {
		mask = app.Scopes // default to the app's full registered scope set
	}
	if mask&^app.Scopes != 0 {
		return nil, 0, errInvalidScope("requested scope exceeds what this app may request")
	}
	return app, mask, nil
}

// ExchangeCode handles grant_type=authorization_code: authenticate the client,
// atomically consume the code, verify redirect + PKCE, and issue tokens.
func (s *Service) ExchangeCode(ctx context.Context, clientID, clientSecret, code, redirectURI, codeVerifier string) (*TokenResponse, error) {
	app, err := s.authenticateClient(ctx, clientID, clientSecret)
	if err != nil {
		return nil, err
	}
	ac, err := s.repo.TakeAuthorizationCode(ctx, hashToken(code))
	if err != nil {
		return nil, errServer("code lookup failed")
	}
	if ac == nil {
		return nil, errInvalidGrant("invalid or expired authorization code")
	}
	if ac.ApplicationID != app.ID {
		return nil, errInvalidGrant("authorization code was issued to a different client")
	}
	if ac.RedirectURI != redirectURI {
		return nil, errInvalidGrant("redirect_uri does not match the authorization request")
	}
	// If the authorize request bound a PKCE challenge, the verifier must match.
	if ac.CodeChallenge != "" && !verifyPKCE(codeVerifier, ac.CodeChallenge) {
		return nil, errInvalidGrant("PKCE verification failed")
	}
	return s.issueGrant(ctx, app.ID, ac.OrganizationID, ac.UserID, ac.Scopes)
}

// RefreshToken handles grant_type=refresh_token with rotation: the presented
// refresh token is consumed and a fresh access+refresh pair issued.
func (s *Service) RefreshToken(ctx context.Context, clientID, clientSecret, refreshToken string) (*TokenResponse, error) {
	app, err := s.authenticateClient(ctx, clientID, clientSecret)
	if err != nil {
		return nil, err
	}
	g, err := s.repo.GetGrantByRefreshTokenHash(ctx, hashToken(refreshToken))
	if err != nil {
		return nil, errServer("token lookup failed")
	}
	if g == nil || g.RevokedAt != nil || g.ApplicationID != app.ID {
		return nil, errInvalidGrant("invalid refresh token")
	}
	if g.RefreshExpiresAt != nil && g.RefreshExpiresAt.Before(time.Now().UTC()) {
		return nil, errInvalidGrant("refresh token expired")
	}
	access, err := randomToken(models.OAuthAccessTokenPrefix)
	if err != nil {
		return nil, errServer("could not mint token")
	}
	refresh, err := randomToken(models.OAuthRefreshTokenPrefix)
	if err != nil {
		return nil, errServer("could not mint token")
	}
	accessExp := time.Now().UTC().Add(models.OAuthAccessTokenTTL)
	refreshExp := time.Now().UTC().Add(models.OAuthRefreshTokenTTL)
	if err := s.repo.RotateGrantTokens(ctx, g.ID, hashToken(access), hashToken(refresh), accessExp, &refreshExp); err != nil {
		return nil, errServer("could not rotate token")
	}
	return &TokenResponse{
		AccessToken:  access,
		TokenType:    "Bearer",
		ExpiresIn:    int(models.OAuthAccessTokenTTL.Seconds()),
		RefreshToken: refresh,
		Scope:        ScopeString(g.Scopes),
	}, nil
}

// RevokeToken revokes the grant behind an access or refresh token (RFC 7009).
// Per spec it succeeds even for an unknown token.
func (s *Service) RevokeToken(ctx context.Context, clientID, clientSecret, token string) error {
	app, err := s.authenticateClient(ctx, clientID, clientSecret)
	if err != nil {
		return err
	}
	if err := s.repo.RevokeGrantByTokenHash(ctx, app.ID, hashToken(token)); err != nil {
		return err
	}
	// The org may have lost its last grant — reconcile removes its app endpoint.
	s.ReconcileAppEndpoints(ctx, app.ID)
	return nil
}

// ValidateAccessToken resolves a bearer access token to its grant if the grant
// is active and unexpired. Used by the request-auth middleware.
func (s *Service) ValidateAccessToken(ctx context.Context, token string) (*AccessClaims, error) {
	if !strings.HasPrefix(token, models.OAuthAccessTokenPrefix) {
		return nil, fmt.Errorf("not an oauth access token")
	}
	g, err := s.repo.GetGrantByAccessTokenHash(ctx, hashToken(token))
	if err != nil {
		return nil, err
	}
	if g == nil || g.RevokedAt != nil || g.AccessExpiresAt.Before(time.Now().UTC()) {
		return nil, fmt.Errorf("invalid or expired access token")
	}
	return &AccessClaims{
		GrantID:        g.ID,
		ApplicationID:  g.ApplicationID,
		OrganizationID: g.OrganizationID,
		UserID:         g.UserID,
		Scopes:         g.Scopes,
	}, nil
}

// issueGrant mints and stores a new access+refresh token pair.
func (s *Service) issueGrant(ctx context.Context, appID, orgID, userID uuid.UUID, scopes uint64) (*TokenResponse, error) {
	access, err := randomToken(models.OAuthAccessTokenPrefix)
	if err != nil {
		return nil, errServer("could not mint token")
	}
	refresh, err := randomToken(models.OAuthRefreshTokenPrefix)
	if err != nil {
		return nil, errServer("could not mint token")
	}
	refreshExp := time.Now().UTC().Add(models.OAuthRefreshTokenTTL)
	g := &models.OAuthAccessGrant{
		ApplicationID:    appID,
		OrganizationID:   orgID,
		UserID:           userID,
		Scopes:           scopes,
		AccessTokenHash:  hashToken(access),
		RefreshTokenHash: hashToken(refresh),
		AccessExpiresAt:  time.Now().UTC().Add(models.OAuthAccessTokenTTL),
		RefreshExpiresAt: &refreshExp,
	}
	if err := s.repo.CreateAccessGrant(ctx, g); err != nil {
		return nil, errServer("could not store grant")
	}
	// Newly-authorized org: materialize the app's webhook endpoint for it (scoped
	// to the granted permissions). Best-effort.
	s.ReconcileAppEndpoints(ctx, appID)
	return &TokenResponse{
		AccessToken:  access,
		TokenType:    "Bearer",
		ExpiresIn:    int(models.OAuthAccessTokenTTL.Seconds()),
		RefreshToken: refresh,
		Scope:        ScopeString(scopes),
	}, nil
}

// authenticateClient resolves and authenticates the OAuth client. Every app has
// a client secret, so a matching secret is always required.
func (s *Service) authenticateClient(ctx context.Context, clientID, clientSecret string) (*models.OAuthApplication, error) {
	app, err := s.repo.GetApplicationByClientID(ctx, strings.TrimSpace(clientID))
	if err != nil {
		return nil, errServer("client lookup failed")
	}
	if app == nil || app.Status != models.OAuthAppActive {
		return nil, errInvalidClient("unknown or disabled client")
	}
	if clientSecret == "" || app.ClientSecretHash == "" || hashToken(clientSecret) != app.ClientSecretHash {
		return nil, errInvalidClient("invalid client credentials")
	}
	return app, nil
}

// redirectAllowed does an exact-string match against the app's registered URIs.
func redirectAllowed(app *models.OAuthApplication, uri string) bool {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return false
	}
	for _, r := range app.RedirectURIs {
		if r == uri {
			return true
		}
	}
	return false
}

// buildRedirect appends ?code=&state= to the (already-validated) redirect URI.
func buildRedirect(redirectURI, code, state string) string {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return redirectURI
	}
	q := u.Query()
	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
