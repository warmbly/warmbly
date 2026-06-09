package integration

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/warmbly/warmbly/internal/models"
)

// OAuthManager owns the OAuth 2.0 authorization-code machinery for every
// provider that supports it. Client credentials are read from the environment
// at construction (one app per provider, registered in that provider's
// developer console). A provider with no credentials configured is reported
// as not-Configured so the dashboard renders it as "coming soon" rather than a
// dead Connect button — the framework lights up the moment real credentials
// are supplied, no code change required.
//
// This mirrors the mailbox OAuth flow in internal/app/email/oauth.go: start →
// provider popup → callback page postMessages code+state → finish exchanges and
// persists encrypted tokens.
type OAuthManager struct {
	redirectURL string
	providers   map[models.IntegrationProvider]*oauthProvider
	http        *http.Client
}

// identifyFunc resolves the connected external account (id + display name) and
// the scopes actually granted, given a fresh token.
type identifyFunc func(ctx context.Context, m *OAuthManager, tok *oauth2.Token) (extID, extName string, scopes []string, err error)

type oauthProvider struct {
	provider models.IntegrationProvider
	config   *oauth2.Config
	scopes   []string
	usePKCE  bool
	identify identifyFunc
}

// NewOAuthManager builds the provider registry from environment variables. For
// each provider it reads <PREFIX>_OAUTH_CLIENT_ID / <PREFIX>_OAUTH_CLIENT_SECRET
// (e.g. HUBSPOT_OAUTH_CLIENT_ID). The shared redirect/callback URL comes from
// INTEGRATIONS_OAUTH_REDIRECT_URL, else BACKEND_PUBLIC_URL + the callback path,
// else a localhost default for dev.
func NewOAuthManager() *OAuthManager {
	redirect := strings.TrimSpace(os.Getenv("INTEGRATIONS_OAUTH_REDIRECT_URL"))
	if redirect == "" {
		base := strings.TrimRight(strings.TrimSpace(os.Getenv("BACKEND_PUBLIC_URL")), "/")
		if base == "" {
			base = "http://localhost:8080"
		}
		redirect = base + "/integrations/oauth/callback"
	}

	m := &OAuthManager{
		redirectURL: redirect,
		providers:   map[models.IntegrationProvider]*oauthProvider{},
		http:        &http.Client{Timeout: 15 * time.Second},
	}

	register := func(p models.IntegrationProvider, envPrefix string, ep oauth2.Endpoint, scopes []string, usePKCE bool, id identifyFunc) {
		clientID := strings.TrimSpace(os.Getenv(envPrefix + "_OAUTH_CLIENT_ID"))
		clientSecret := strings.TrimSpace(os.Getenv(envPrefix + "_OAUTH_CLIENT_SECRET"))
		op := &oauthProvider{provider: p, scopes: scopes, usePKCE: usePKCE, identify: id}
		if clientID != "" && clientSecret != "" {
			op.config = &oauth2.Config{
				ClientID:     clientID,
				ClientSecret: clientSecret,
				Endpoint:     ep,
				RedirectURL:  redirect,
				Scopes:       scopes,
			}
		}
		m.providers[p] = op
	}

	register(models.IntegrationHubSpot, "HUBSPOT", oauth2.Endpoint{
		AuthURL:  "https://app.hubspot.com/oauth/authorize",
		TokenURL: "https://api.hubapi.com/oauth/v1/token",
	}, []string{"oauth", "crm.objects.contacts.read", "crm.objects.contacts.write"}, false, identifyHubSpot)

	register(models.IntegrationSlack, "SLACK", oauth2.Endpoint{
		AuthURL:  "https://slack.com/oauth/v2/authorize",
		TokenURL: "https://slack.com/api/oauth.v2.access",
	}, []string{"chat:write", "channels:read", "groups:read"}, false, identifySlack)

	register(models.IntegrationGoogleSheets, "GOOGLE_SHEETS", oauth2.Endpoint{
		AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL: "https://oauth2.googleapis.com/token",
	}, []string{
		"https://www.googleapis.com/auth/spreadsheets",
		"https://www.googleapis.com/auth/userinfo.email",
	}, true, identifyGoogle)

	register(models.IntegrationPipedrive, "PIPEDRIVE", oauth2.Endpoint{
		AuthURL:  "https://oauth.pipedrive.com/oauth/authorize",
		TokenURL: "https://oauth.pipedrive.com/oauth/token",
	}, []string{"contacts:full", "deals:full"}, false, identifyPipedrive)

	register(models.IntegrationSalesforce, "SALESFORCE", oauth2.Endpoint{
		AuthURL:  "https://login.salesforce.com/services/oauth2/authorize",
		TokenURL: "https://login.salesforce.com/services/oauth2/token",
	}, []string{"api", "refresh_token"}, true, identifySalesforce)

	return m
}

// SupportsOAuth reports whether the provider has an OAuth flow at all.
func (m *OAuthManager) SupportsOAuth(p models.IntegrationProvider) bool {
	_, ok := m.providers[p]
	return ok
}

// Configured reports whether the provider has client credentials wired.
func (m *OAuthManager) Configured(p models.IntegrationProvider) bool {
	op, ok := m.providers[p]
	return ok && op.config != nil
}

// Scopes returns the requested scopes for a provider (empty if none/unknown).
func (m *OAuthManager) Scopes(p models.IntegrationProvider) []string {
	if op, ok := m.providers[p]; ok {
		return op.scopes
	}
	return nil
}

// AuthCodeURL builds the provider authorization URL. It returns the URL plus
// the PKCE verifier to persist (empty when the provider doesn't use PKCE).
func (m *OAuthManager) AuthCodeURL(p models.IntegrationProvider, state string) (authURL, verifier string, err error) {
	op, ok := m.providers[p]
	if !ok || op.config == nil {
		return "", "", fmt.Errorf("oauth not configured for provider %s", p)
	}
	opts := []oauth2.AuthCodeOption{oauth2.AccessTypeOffline, oauth2.ApprovalForce}
	if op.usePKCE {
		verifier = randomURLToken(32)
		sum := sha256.Sum256([]byte(verifier))
		challenge := base64.RawURLEncoding.EncodeToString(sum[:])
		opts = append(opts,
			oauth2.SetAuthURLParam("code_challenge", challenge),
			oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		)
	}
	return op.config.AuthCodeURL(state, opts...), verifier, nil
}

// Exchange swaps an authorization code for tokens and resolves the connected
// account identity.
func (m *OAuthManager) Exchange(ctx context.Context, p models.IntegrationProvider, code, verifier string) (*models.IntegrationTokens, extAccount, error) {
	op, ok := m.providers[p]
	if !ok || op.config == nil {
		return nil, extAccount{}, fmt.Errorf("oauth not configured for provider %s", p)
	}
	var opts []oauth2.AuthCodeOption
	if op.usePKCE && verifier != "" {
		opts = append(opts, oauth2.SetAuthURLParam("code_verifier", verifier))
	}
	tok, err := op.config.Exchange(ctx, code, opts...)
	if err != nil {
		return nil, extAccount{}, fmt.Errorf("token exchange failed: %w", err)
	}

	extID, extName, grantedScopes, idErr := "", "", []string(nil), error(nil)
	if op.identify != nil {
		extID, extName, grantedScopes, idErr = op.identify(ctx, m, tok)
		if idErr != nil {
			// Identity is best-effort: a connected token is still usable even
			// if the profile lookup hiccups. We just won't show the account name.
			grantedScopes = nil
		}
	}
	if len(grantedScopes) == 0 {
		grantedScopes = scopesFromToken(tok, op.scopes)
	}

	tokens := &models.IntegrationTokens{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		Scopes:       grantedScopes,
	}
	if !tok.Expiry.IsZero() {
		exp := tok.Expiry.UTC()
		tokens.ExpiresAt = &exp
	}

	acct := extAccount{ID: extID, Name: extName}
	// Salesforce (and other per-tenant APIs) return the org's API host as an
	// "instance_url" extra on the token. Capture it so action handlers know
	// which host to call — the value is persisted in the connection's
	// non-secret display fields by OAuthFinish.
	if iu, ok := tok.Extra("instance_url").(string); ok {
		acct.InstanceURL = strings.TrimRight(strings.TrimSpace(iu), "/")
	}
	return tokens, acct, nil
}

// RefreshIfNeeded returns a valid access token for the connection, refreshing
// via the stored refresh token when the access token is within 60s of expiry.
// It reports whether the token was refreshed (so the caller can persist it).
func (m *OAuthManager) RefreshIfNeeded(ctx context.Context, p models.IntegrationProvider, current models.IntegrationTokens) (models.IntegrationTokens, bool, error) {
	op, ok := m.providers[p]
	if !ok || op.config == nil {
		return current, false, fmt.Errorf("oauth not configured for provider %s", p)
	}
	stillValid := current.ExpiresAt == nil || time.Until(*current.ExpiresAt) > 60*time.Second
	if stillValid || current.RefreshToken == "" {
		return current, false, nil
	}

	src := op.config.TokenSource(ctx, &oauth2.Token{
		AccessToken:  current.AccessToken,
		RefreshToken: current.RefreshToken,
		Expiry:       time.Now().Add(-time.Minute),
	})
	tok, err := src.Token()
	if err != nil {
		return current, false, fmt.Errorf("token refresh failed: %w", err)
	}
	refreshed := models.IntegrationTokens{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		Scopes:       current.Scopes,
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = current.RefreshToken // some providers omit it on refresh
	}
	if !tok.Expiry.IsZero() {
		exp := tok.Expiry.UTC()
		refreshed.ExpiresAt = &exp
	}
	return refreshed, true, nil
}

// extAccount is the resolved external identity for a connection.
type extAccount struct {
	ID   string
	Name string
	// InstanceURL is the provider-specific API host returned at token-exchange
	// time (Salesforce's per-org domain). Empty for providers with a fixed host.
	InstanceURL string
}

// --- identity resolvers -----------------------------------------------------

func identifyHubSpot(ctx context.Context, m *OAuthManager, tok *oauth2.Token) (string, string, []string, error) {
	var out struct {
		HubID     int64    `json:"hub_id"`
		HubDomain string   `json:"hub_domain"`
		User      string   `json:"user"`
		Scopes    []string `json:"scopes"`
	}
	url := "https://api.hubapi.com/oauth/v1/access-tokens/" + tok.AccessToken
	if err := m.getJSON(ctx, url, "", &out); err != nil {
		return "", "", nil, err
	}
	name := out.HubDomain
	if name == "" {
		name = out.User
	}
	return fmt.Sprintf("%d", out.HubID), name, out.Scopes, nil
}

func identifySlack(ctx context.Context, m *OAuthManager, tok *oauth2.Token) (string, string, []string, error) {
	var out struct {
		OK     bool   `json:"ok"`
		Team   string `json:"team"`
		TeamID string `json:"team_id"`
		URL    string `json:"url"`
		Error  string `json:"error"`
	}
	if err := m.getJSON(ctx, "https://slack.com/api/auth.test", tok.AccessToken, &out); err != nil {
		return "", "", nil, err
	}
	if !out.OK {
		return "", "", nil, fmt.Errorf("slack auth.test: %s", out.Error)
	}
	return out.TeamID, out.Team, nil, nil
}

func identifyGoogle(ctx context.Context, m *OAuthManager, tok *oauth2.Token) (string, string, []string, error) {
	var out struct {
		Email string `json:"email"`
		ID    string `json:"id"`
	}
	if err := m.getJSON(ctx, "https://www.googleapis.com/oauth2/v2/userinfo", tok.AccessToken, &out); err != nil {
		return "", "", nil, err
	}
	return out.ID, out.Email, nil, nil
}

func identifyPipedrive(ctx context.Context, m *OAuthManager, tok *oauth2.Token) (string, string, []string, error) {
	var out struct {
		Data struct {
			ID          int64  `json:"id"`
			Name        string `json:"name"`
			CompanyName string `json:"company_name"`
			Email       string `json:"email"`
		} `json:"data"`
	}
	if err := m.getJSON(ctx, "https://api.pipedrive.com/v1/users/me", tok.AccessToken, &out); err != nil {
		return "", "", nil, err
	}
	name := out.Data.CompanyName
	if name == "" {
		name = out.Data.Email
	}
	return fmt.Sprintf("%d", out.Data.ID), name, nil, nil
}

// identifySalesforce resolves the connected Salesforce org + username by GETting
// the identity URL Salesforce returns as the token's "id" extra. Best-effort:
// the connection is usable even if this lookup hiccups (the caller treats an
// error as "no profile" and still persists the token). The org's API host is
// captured separately from the token's "instance_url" extra in Exchange.
func identifySalesforce(ctx context.Context, m *OAuthManager, tok *oauth2.Token) (string, string, []string, error) {
	idURL, _ := tok.Extra("id").(string)
	idURL = strings.TrimSpace(idURL)
	if idURL == "" {
		return "", "", nil, nil
	}
	var out struct {
		OrganizationID string `json:"organization_id"`
		Username       string `json:"username"`
		DisplayName    string `json:"display_name"`
	}
	if err := m.getJSON(ctx, idURL, tok.AccessToken, &out); err != nil {
		return "", "", nil, err
	}
	name := out.Username
	if name == "" {
		name = out.DisplayName
	}
	return out.OrganizationID, name, nil, nil
}

// --- helpers ----------------------------------------------------------------

func (m *OAuthManager) getJSON(ctx context.Context, url, bearer string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	return json.Unmarshal(body, dst)
}

// scopesFromToken pulls the granted scopes out of the token's "scope" extra
// field (space- or comma-delimited), falling back to the requested scopes.
func scopesFromToken(tok *oauth2.Token, requested []string) []string {
	raw, _ := tok.Extra("scope").(string)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return requested
	}
	sep := " "
	if strings.Contains(raw, ",") && !strings.Contains(raw, " ") {
		sep = ","
	}
	parts := strings.Split(raw, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return requested
	}
	return out
}

func randomURLToken(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// rand.Read essentially never fails; degrade to a time-seeded value
		// only to keep the flow alive rather than panic.
		return base64.RawURLEncoding.EncodeToString([]byte(time.Now().UTC().String()))
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}
