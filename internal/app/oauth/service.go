// Package oauth implements Warmbly's OAuth2 authorization server: third-party
// app registration, the authorization-code flow (client secret required, PKCE
// optional), token issue/refresh/revoke, and bearer-token validation. Issued
// access tokens carry an API-permission bitmask, so they authenticate through the
// same route gates as API keys.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/webhook"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/whdomain"
	"github.com/warmbly/warmbly/internal/repository"
)

// AppWebhookSyncer materializes/removes an app's managed webhook endpoints (one
// per authorized org). Satisfied by *webhook.Service's repository; a local
// interface so the OAuth package stays decoupled from the webhook package.
type AppWebhookSyncer interface {
	UpsertAppEndpoint(ctx context.Context, orgID, appID uuid.UUID, url, secret string, eventTypes []string) error
	DeleteAppEndpointsExcept(ctx context.Context, appID uuid.UUID, keepOrgIDs []uuid.UUID) error
}

// Service is the OAuth authorization server.
type Service struct {
	repo        repository.OAuthRepository
	webhookSync AppWebhookSyncer
}

func NewService(repo repository.OAuthRepository) *Service {
	return &Service{repo: repo}
}

// WireWebhookSync attaches the app-webhook materializer so authorizing/revoking
// an app, or changing its webhook config, reconciles the per-org endpoints.
func (s *Service) WireWebhookSync(w AppWebhookSyncer) {
	s.webhookSync = w
}

// --- credential helpers (mirror the api_key hash-at-rest scheme) ---

func randomToken(prefix string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// verifyPKCE checks the S256 transform: base64url(sha256(verifier)) == challenge.
func verifyPKCE(verifier, challenge string) bool {
	if verifier == "" || challenge == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:]) == challenge
}

// --- application management ---

// RegisterApplication creates a new OAuth client. Every app is issued a client
// secret, returned exactly once here.
func (s *Service) RegisterApplication(ctx context.Context, orgID, userID uuid.UUID, w models.OAuthApplicationWrite) (*models.OAuthApplicationWithSecret, error) {
	name := strings.TrimSpace(w.Name)
	if name == "" {
		return nil, fmt.Errorf("a name is required")
	}
	uris, err := validateRedirectURIs(w.RedirectURIs)
	if err != nil {
		return nil, err
	}
	domains, err := validateWebhookDomains(w.AllowedWebhookDomains)
	if err != nil {
		return nil, err
	}
	scopes := w.Scopes & models.AllAPIPermissionsMask
	if scopes == 0 {
		return nil, fmt.Errorf("select at least one scope")
	}
	clientID, err := randomToken(models.OAuthClientIDPrefix)
	if err != nil {
		return nil, err
	}
	app := &models.OAuthApplication{
		OrganizationID:        orgID,
		CreatedBy:             userID,
		Name:                  name,
		Description:           strings.TrimSpace(w.Description),
		LogoURL:               strings.TrimSpace(w.LogoURL),
		WebsiteURL:            strings.TrimSpace(w.WebsiteURL),
		ClientID:              clientID,
		RedirectURIs:          uris,
		AllowedWebhookDomains: domains,
		Scopes:                scopes,
		Status:                models.OAuthAppActive,
	}
	if err := applyWebhookConfig(app, w); err != nil {
		return nil, err
	}
	secret, err := randomToken(models.OAuthClientSecretPrefix)
	if err != nil {
		return nil, err
	}
	app.ClientSecretHash = hashToken(secret)
	if err := s.repo.CreateApplication(ctx, app); err != nil {
		return nil, err
	}
	// No grants yet at creation, so nothing to materialize; reconcile is a no-op
	// here and runs on the first authorize.
	return &models.OAuthApplicationWithSecret{OAuthApplication: *app, ClientSecret: secret}, nil
}

func (s *Service) ListApplications(ctx context.Context, orgID uuid.UUID) ([]models.OAuthApplication, error) {
	return s.repo.ListApplications(ctx, orgID)
}

func (s *Service) GetApplication(ctx context.Context, orgID, id uuid.UUID) (*models.OAuthApplication, error) {
	return s.repo.GetApplication(ctx, orgID, id)
}

// UpdateApplication edits an app's display fields, redirect URIs, scopes, and
// status. client_id and the secret are immutable here (rotate the secret
// separately).
func (s *Service) UpdateApplication(ctx context.Context, orgID, id uuid.UUID, w models.OAuthApplicationWrite) (*models.OAuthApplication, error) {
	app, err := s.repo.GetApplication(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if app == nil {
		return nil, fmt.Errorf("application not found")
	}
	name := strings.TrimSpace(w.Name)
	if name == "" {
		return nil, fmt.Errorf("a name is required")
	}
	uris, err := validateRedirectURIs(w.RedirectURIs)
	if err != nil {
		return nil, err
	}
	domains, err := validateWebhookDomains(w.AllowedWebhookDomains)
	if err != nil {
		return nil, err
	}
	scopes := w.Scopes & models.AllAPIPermissionsMask
	if scopes == 0 {
		return nil, fmt.Errorf("select at least one scope")
	}
	app.Name = name
	app.Description = strings.TrimSpace(w.Description)
	app.LogoURL = strings.TrimSpace(w.LogoURL)
	app.WebsiteURL = strings.TrimSpace(w.WebsiteURL)
	app.RedirectURIs = uris
	app.AllowedWebhookDomains = domains
	app.Scopes = scopes
	if err := applyWebhookConfig(app, w); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateApplication(ctx, app); err != nil {
		return nil, err
	}
	// Re-materialize the app's per-org endpoints from the new config (URL/events).
	s.ReconcileAppEndpoints(ctx, id)
	return s.repo.GetApplication(ctx, orgID, id)
}

// applyWebhookConfig validates the app's webhook URL (HTTPS + SSRF + inside the
// app's own allowed domains) and event filter, and mints a signing secret the
// first time a URL is set. Clearing the URL keeps the secret (so re-enabling
// reuses it) but stops materialization.
func applyWebhookConfig(app *models.OAuthApplication, w models.OAuthApplicationWrite) error {
	url := strings.TrimSpace(w.WebhookURL)
	app.WebhookURL = url
	app.WebhookEvents = []string{}
	if url == "" {
		return nil
	}
	if err := webhook.ValidateOutboundURL(url); err != nil {
		return fmt.Errorf("webhook url: %w", err)
	}
	if !whdomain.HostAllowed(hostOf(url), app.AllowedWebhookDomains) {
		return fmt.Errorf("webhook url host must be within the app's allowed webhook domains")
	}
	for _, e := range w.WebhookEvents {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if !models.IsValidWebhookEventType(e) {
			return fmt.Errorf("unknown webhook event type: %s", e)
		}
		app.WebhookEvents = append(app.WebhookEvents, e)
	}
	if app.WebhookSecret == "" {
		app.WebhookSecret = genWebhookSecret()
	}
	return nil
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func genWebhookSecret() string {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	return "whsec_" + hex.EncodeToString(buf)
}

// ReconcileAppEndpoints rebuilds the app's managed per-org webhook endpoints from
// its current webhook config and active grants. Best-effort (logs nothing on the
// hot path; callers fire it after authorize/revoke/config-change). No-op when the
// webhook syncer is not wired.
func (s *Service) ReconcileAppEndpoints(ctx context.Context, appID uuid.UUID) {
	if s.webhookSync == nil {
		return
	}
	app, err := s.repo.GetApplicationByID(ctx, appID)
	if err != nil || app == nil {
		return
	}
	if strings.TrimSpace(app.WebhookURL) == "" || app.WebhookSecret == "" {
		_ = s.webhookSync.DeleteAppEndpointsExcept(ctx, appID, nil)
		return
	}
	grants, err := s.repo.ListActiveGrantOrgs(ctx, appID)
	if err != nil {
		return
	}
	keep := make([]uuid.UUID, 0, len(grants))
	for _, g := range grants {
		events := models.AppSubscribedEventTypes(app.WebhookEvents, g.Scopes)
		// An empty computed set means this org's grant allows none of the app's
		// events. Do NOT materialize an endpoint: an empty event_types row would
		// be treated as "all events" by the matcher (a scope bypass). Leaving it
		// out of `keep` also removes any stale endpoint for that org below.
		if len(events) == 0 {
			continue
		}
		if err := s.webhookSync.UpsertAppEndpoint(ctx, g.OrgID, appID, app.WebhookURL, app.WebhookSecret, events); err == nil {
			keep = append(keep, g.OrgID)
		}
	}
	_ = s.webhookSync.DeleteAppEndpointsExcept(ctx, appID, keep)
}

// WebhookSecret returns the app's signing secret (for the reveal UI), generating
// one if a webhook URL is configured but no secret exists yet.
func (s *Service) WebhookSecret(ctx context.Context, orgID, id uuid.UUID) (string, error) {
	app, err := s.repo.GetApplication(ctx, orgID, id)
	if err != nil {
		return "", err
	}
	if app == nil {
		return "", fmt.Errorf("application not found")
	}
	return app.WebhookSecret, nil
}

// RotateWebhookSecret mints a new app webhook signing secret and re-materializes
// the app's endpoints so every install gets the new secret.
func (s *Service) RotateWebhookSecret(ctx context.Context, orgID, id uuid.UUID) (string, error) {
	app, err := s.repo.GetApplication(ctx, orgID, id)
	if err != nil {
		return "", err
	}
	if app == nil {
		return "", fmt.Errorf("application not found")
	}
	app.WebhookSecret = genWebhookSecret()
	if err := s.repo.UpdateApplication(ctx, app); err != nil {
		return "", err
	}
	s.ReconcileAppEndpoints(ctx, id)
	return app.WebhookSecret, nil
}

// RotateSecret mints a new client secret (returned once) and invalidates the old
// one.
func (s *Service) RotateSecret(ctx context.Context, orgID, id uuid.UUID) (string, error) {
	app, err := s.repo.GetApplication(ctx, orgID, id)
	if err != nil {
		return "", err
	}
	if app == nil {
		return "", fmt.Errorf("application not found")
	}
	secret, err := randomToken(models.OAuthClientSecretPrefix)
	if err != nil {
		return "", err
	}
	if err := s.repo.UpdateApplicationSecret(ctx, orgID, id, hashToken(secret)); err != nil {
		return "", err
	}
	return secret, nil
}

func (s *Service) DeleteApplication(ctx context.Context, orgID, id uuid.UUID) error {
	return s.repo.DeleteApplication(ctx, orgID, id)
}

// AllowedWebhookDomains returns an app's webhook-domain allowlist by id, for the
// webhook service's delivery-time and write-time enforcement (wired via
// webhook.WireAppDomainResolver).
func (s *Service) AllowedWebhookDomains(ctx context.Context, appID uuid.UUID) ([]string, error) {
	return s.repo.GetAllowedWebhookDomains(ctx, appID)
}

func (s *Service) ListAuthorizedApps(ctx context.Context, orgID, userID uuid.UUID) ([]models.OAuthAuthorizedApp, error) {
	return s.repo.ListAuthorizedApps(ctx, orgID, userID)
}

func (s *Service) RevokeAuthorization(ctx context.Context, orgID, userID, appID uuid.UUID) error {
	if err := s.repo.RevokeAuthorization(ctx, orgID, userID, appID); err != nil {
		return err
	}
	// The org may have lost its last grant for this app — reconcile its endpoint.
	s.ReconcileAppEndpoints(ctx, appID)
	return nil
}

// validateRedirectURIs enforces the OAuth 2.1 redirect rules: present, absolute,
// HTTPS (loopback HTTP allowed for native apps), and no fragment.
func validateRedirectURIs(uris []string) ([]string, error) {
	out := make([]string, 0, len(uris))
	seen := map[string]bool{}
	for _, u := range uris {
		u = strings.TrimSpace(u)
		if u == "" || seen[u] {
			continue
		}
		parsed, err := url.Parse(u)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, fmt.Errorf("invalid redirect URI: %s", u)
		}
		if parsed.Fragment != "" {
			return nil, fmt.Errorf("a redirect URI must not contain a fragment: %s", u)
		}
		if parsed.Scheme != "https" && !isLoopbackRedirect(parsed) {
			return nil, fmt.Errorf("a redirect URI must use https: %s", u)
		}
		seen[u] = true
		out = append(out, u)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one redirect URI is required")
	}
	return out, nil
}

func isLoopbackRedirect(u *url.URL) bool {
	h := u.Hostname()
	return u.Scheme == "http" && (h == "127.0.0.1" || h == "::1" || h == "localhost")
}

// validateWebhookDomains normalizes the per-app allowed-webhook-domain list. An
// empty list is allowed (the app simply cannot register webhooks). Each entry is
// reduced to a host: a leading-dot entry is subdomain-inclusive, a bare entry is
// exact (see internal/pkg/whdomain).
func validateWebhookDomains(domains []string) ([]string, error) {
	out, err := whdomain.NormalizeList(domains)
	if err != nil {
		return nil, err
	}
	return out, nil
}
