package oauth

import (
	"context"
	"crypto/subtle"
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
	ClientID    string `json:"client_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	LogoURL     string `json:"logo_url"`
	WebsiteURL  string `json:"website_url"`
	RedirectURI string `json:"redirect_uri"`
	// Scopes is what approving grants: the request narrowed to the approving member's role.
	Scopes []string `json:"scopes"`
	// WithheldScopes were requested but fall outside the member's role, so they are not granted.
	WithheldScopes []string `json:"withheld_scopes"`
	State          string   `json:"state"`
	// OrganizationName is the workspace that receives the grant.
	OrganizationName string `json:"organization_name"`
	// Verified is a registered app the instance's operators feature in the directory.
	Verified bool `json:"verified"`
	// SelfRegistered is a client that registered itself (RFC 7591), so nobody vouches for its name.
	SelfRegistered bool `json:"self_registered"`
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
	// Member is the granting member's current membership.
	Member *models.OrganizationMember
}

// grantableScopes narrows a request to what an app may hold and the member's role covers.
func grantableScopes(requested, roleCap uint64) (granted, withheld uint64) {
	granted = requested & roleCap & models.AppGrantableScopes
	return granted, requested &^ granted
}

// AuthorizeDetails validates the authorize request and returns consent info for
// a member whose role covers roleCap, or an *OAuthError describing what's wrong.
func (s *Service) AuthorizeDetails(ctx context.Context, roleCap uint64, req AuthorizeRequest) (*ConsentInfo, error) {
	app, requested, err := s.validateAuthorize(ctx, req)
	if err != nil {
		return nil, err
	}
	granted, withheld := grantableScopes(requested, roleCap)
	if granted == 0 {
		return nil, errNothingGrantable()
	}
	// Rows written before the website rule existed are re-checked on the way out.
	website, werr := appWebsite(app.WebsiteURL)
	if werr != nil {
		website = ""
	}
	name := app.Name
	verified := false
	if app.DynamicallyRegistered {
		// Rows stored before these rules are held to them on the way out too.
		name = dcrClientName(app.Name)
		website = ""
	} else if featured, ferr := s.repo.IsFeatured(ctx, app.ID); ferr == nil {
		verified = featured
	}
	return &ConsentInfo{
		ClientID:       app.ClientID,
		Name:           name,
		Description:    app.Description,
		LogoURL:        app.LogoURL,
		WebsiteURL:     website,
		RedirectURI:    req.RedirectURI,
		Scopes:         ScopeList(granted),
		WithheldScopes: ScopeList(withheld),
		State:          req.State,
		Verified:       verified,
		SelfRegistered: app.DynamicallyRegistered,
	}, nil
}

// IssueAuthorizationCode is called after the user approves consent. It re-checks
// the request, mints a single-use PKCE-bound code for this user+org carrying only
// the scopes roleCap covers, and returns the redirect URL for the browser to follow.
func (s *Service) IssueAuthorizationCode(ctx context.Context, orgID, userID uuid.UUID, roleCap uint64, req AuthorizeRequest) (string, error) {
	app, requested, err := s.validateAuthorize(ctx, req)
	if err != nil {
		return "", err
	}
	scopes, _ := grantableScopes(requested, roleCap)
	if scopes == 0 {
		return "", errNothingGrantable()
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
	if app == nil || !app.Usable() {
		return nil, 0, errUnauthorizedClient("unknown, disabled or suspended client")
	}
	if !redirectAllowed(app, req.RedirectURI) {
		return nil, 0, errInvalidRequest("redirect_uri does not match a registered URI")
	}
	// Public clients hold no secret, so PKCE is the only thing binding the code to
	// the caller: it is mandatory for them (and S256-only for everyone).
	if app.IsPublic && strings.TrimSpace(req.CodeChallenge) == "" {
		return nil, 0, errInvalidRequest("code_challenge is required for public clients")
	}
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
	// A public client's code must carry a PKCE challenge (validateAuthorize enforces
	// this at issue time; re-check here so a code can never be exchanged without it).
	if app.IsPublic && ac.CodeChallenge == "" {
		return nil, errInvalidGrant("PKCE is required for public clients")
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
	if g == nil {
		// A refresh token the grant already rotated away from was replayed: end the grant (RFC 9700 4.14.2).
		if revoked, rerr := s.repo.RevokeGrantByPreviousRefresh(ctx, app.ID, hashToken(refreshToken)); rerr == nil && revoked {
			s.ReconcileAppEndpoints(ctx, app.ID)
		}
		return nil, errInvalidGrant("invalid refresh token")
	}
	if g.RevokedAt != nil || g.ApplicationID != app.ID {
		return nil, errInvalidGrant("invalid refresh token")
	}
	if g.RefreshExpiresAt != nil && g.RefreshExpiresAt.Before(time.Now().UTC()) {
		return nil, errInvalidGrant("refresh token expired")
	}
	scopes := g.Scopes & models.APIPermissionsFor(g.Holder) & models.AppGrantableScopes
	if scopes == 0 {
		return nil, errInvalidGrant("the member who authorized this app no longer holds any of its permissions")
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
	rotated, err := s.repo.RotateGrantTokens(ctx, g.ID, g.RefreshTokenHash, hashToken(access), hashToken(refresh), accessExp, &refreshExp)
	if err != nil {
		return nil, errServer("could not rotate token")
	}
	if !rotated {
		// A refresh token presented twice ends the grant (RFC 9700 4.14.2).
		if rerr := s.repo.RevokeGrant(ctx, g.ID); rerr == nil {
			s.ReconcileAppEndpoints(ctx, g.ApplicationID)
		}
		return nil, errInvalidGrant("invalid refresh token")
	}
	return &TokenResponse{
		AccessToken:  access,
		TokenType:    "Bearer",
		ExpiresIn:    int(models.OAuthAccessTokenTTL.Seconds()),
		RefreshToken: refresh,
		Scope:        ScopeString(scopes),
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
	// A token never acts beyond what its member's role covers now.
	return &AccessClaims{
		GrantID:        g.ID,
		ApplicationID:  g.ApplicationID,
		OrganizationID: g.OrganizationID,
		UserID:         g.UserID,
		Scopes:         g.Scopes & models.APIPermissionsFor(g.Holder) & models.AppGrantableScopes,
		Member:         g.Holder,
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

// authenticateClient resolves and authenticates the OAuth client. Confidential
// clients present a matching secret; public clients (PKCE, no secret — every
// dynamically-registered MCP client) present none, and their identity is proven
// by the PKCE verifier at code exchange instead.
func (s *Service) authenticateClient(ctx context.Context, clientID, clientSecret string) (*models.OAuthApplication, error) {
	app, err := s.repo.GetApplicationByClientID(ctx, strings.TrimSpace(clientID))
	if err != nil {
		return nil, errServer("client lookup failed")
	}
	if app == nil || !app.Usable() {
		return nil, errInvalidClient("unknown, disabled or suspended client")
	}
	if app.IsPublic {
		return app, nil
	}
	if clientSecret == "" || app.ClientSecretHash == "" ||
		subtle.ConstantTimeCompare([]byte(hashToken(clientSecret)), []byte(app.ClientSecretHash)) != 1 {
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
