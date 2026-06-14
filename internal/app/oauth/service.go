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

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Service is the OAuth authorization server.
type Service struct {
	repo repository.OAuthRepository
}

func NewService(repo repository.OAuthRepository) *Service {
	return &Service{repo: repo}
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
	scopes := w.Scopes & models.AllAPIPermissionsMask
	if scopes == 0 {
		return nil, fmt.Errorf("select at least one scope")
	}
	clientID, err := randomToken(models.OAuthClientIDPrefix)
	if err != nil {
		return nil, err
	}
	app := &models.OAuthApplication{
		OrganizationID: orgID,
		CreatedBy:      userID,
		Name:           name,
		Description:    strings.TrimSpace(w.Description),
		LogoURL:        strings.TrimSpace(w.LogoURL),
		WebsiteURL:     strings.TrimSpace(w.WebsiteURL),
		ClientID:       clientID,
		RedirectURIs:   uris,
		Scopes:         scopes,
		Status:         models.OAuthAppActive,
	}
	secret, err := randomToken(models.OAuthClientSecretPrefix)
	if err != nil {
		return nil, err
	}
	app.ClientSecretHash = hashToken(secret)
	if err := s.repo.CreateApplication(ctx, app); err != nil {
		return nil, err
	}
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
	scopes := w.Scopes & models.AllAPIPermissionsMask
	if scopes == 0 {
		return nil, fmt.Errorf("select at least one scope")
	}
	app.Name = name
	app.Description = strings.TrimSpace(w.Description)
	app.LogoURL = strings.TrimSpace(w.LogoURL)
	app.WebsiteURL = strings.TrimSpace(w.WebsiteURL)
	app.RedirectURIs = uris
	app.Scopes = scopes
	if err := s.repo.UpdateApplication(ctx, app); err != nil {
		return nil, err
	}
	return s.repo.GetApplication(ctx, orgID, id)
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

func (s *Service) ListAuthorizedApps(ctx context.Context, orgID, userID uuid.UUID) ([]models.OAuthAuthorizedApp, error) {
	return s.repo.ListAuthorizedApps(ctx, orgID, userID)
}

func (s *Service) RevokeAuthorization(ctx context.Context, orgID, userID, appID uuid.UUID) error {
	return s.repo.RevokeAuthorization(ctx, orgID, userID, appID)
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
