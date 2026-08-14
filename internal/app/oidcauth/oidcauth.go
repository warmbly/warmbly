// Package oidcauth is generic OpenID Connect login for self-hosted
// deployments.
//
// It matters more than convenience here: it is the only sign-in path that does
// not depend on outbound mail, so it is what makes a deployment with no relay
// fully usable. Sign in with Google and Apple do not fill this gap, because
// both require registering an OAuth app per deployment, which nobody does for
// a homelab. What self-hosters actually run is Authentik, Keycloak, Zitadel or
// Pocket ID, and all of them speak plain OIDC.
//
// Built on discovery plus the existing JWKS verifier in internal/pkg/idtoken
// and the PKCE helpers in golang.org/x/oauth2, so it adds no dependency.
// RS256 only, which every mainstream provider issues by default.
package oidcauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/pkg/idtoken"
	"golang.org/x/oauth2"
)

// Config is the operator-facing configuration, using the variable names that
// recur across Outline, Documenso, n8n, Gitea, Immich and Zulip.
type Config struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string

	// AllowedDomains restricts sign-in to these email domains. Empty allows
	// any address the provider vouches for.
	AllowedDomains []string

	// DefaultOrgID makes every JIT-provisioned user join one existing
	// organization. Without it a corporate IdP produces one single-member org
	// per employee, because the signup path creates an org per new user.
	DefaultOrgID string

	// ProviderName is the label the login button shows.
	ProviderName string
}

// discovery is the subset of the provider metadata document we use.
type discovery struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	JWKSURI               string   `json:"jwks_uri"`
	IDTokenSigningAlgs    []string `json:"id_token_signing_alg_values_supported"`
}

type Service struct {
	cfg      Config
	oauth    *oauth2.Config
	verifier *idtoken.Verifier
	issuer   string
}

// New performs discovery against the issuer and returns a ready service.
// A provider that is unreachable at boot is a configuration error worth
// failing on: a login button that always errors is worse than no button.
func New(ctx context.Context, cfg Config) (*Service, error) {
	if cfg.IssuerURL == "" || cfg.ClientID == "" {
		return nil, errors.New("oidcauth: OIDC_ISSUER_URL and OIDC_CLIENT_ID are required")
	}
	if cfg.RedirectURL == "" {
		return nil, errors.New("oidcauth: OIDC_REDIRECT_URL is required")
	}

	doc, err := discover(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, err
	}

	// RFC 8414 requires the issuer in the document to match the one requested;
	// a mismatch is an issuer-substitution attempt or a misconfigured proxy.
	if doc.Issuer != strings.TrimRight(cfg.IssuerURL, "/") && doc.Issuer != cfg.IssuerURL {
		return nil, fmt.Errorf("oidcauth: discovery issuer %q does not match OIDC_ISSUER_URL %q", doc.Issuer, cfg.IssuerURL)
	}
	if len(doc.IDTokenSigningAlgs) > 0 && !contains(doc.IDTokenSigningAlgs, "RS256") {
		return nil, fmt.Errorf("oidcauth: provider signs ID tokens with %v; only RS256 is supported", doc.IDTokenSigningAlgs)
	}

	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "email"}
	}

	return &Service{
		cfg: cfg,
		oauth: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       scopes,
			Endpoint: oauth2.Endpoint{
				AuthURL:  doc.AuthorizationEndpoint,
				TokenURL: doc.TokenEndpoint,
			},
		},
		verifier: idtoken.NewVerifier(doc.JWKSURI, []string{doc.Issuer}, []string{cfg.ClientID}),
		issuer:   doc.Issuer,
	}, nil
}

func discover(ctx context.Context, issuer string) (*discovery, error) {
	url := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidcauth: discovery request to %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidcauth: discovery at %s returned %d", url, resp.StatusCode)
	}

	var doc discovery
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("oidcauth: decoding discovery document: %w", err)
	}
	if doc.AuthorizationEndpoint == "" || doc.TokenEndpoint == "" || doc.JWKSURI == "" {
		return nil, errors.New("oidcauth: discovery document is missing required endpoints")
	}
	return &doc, nil
}

// Issuer is the verified issuer identifier, used as part of the identity key.
func (s *Service) Issuer() string { return s.issuer }

// ProviderName is the label for the login button.
func (s *Service) ProviderName() string {
	if s.cfg.ProviderName != "" {
		return s.cfg.ProviderName
	}
	return "Single sign-on"
}

// DefaultOrgID is the organization JIT-provisioned users join.
func (s *Service) DefaultOrgID() string { return s.cfg.DefaultOrgID }

// AuthCodeURL builds the authorization request. RFC 9700 makes PKCE mandatory
// for every client type including confidential ones, and requires state to be
// one-time and bound to the user agent. The caller stores verifier, state and
// nonce server-side and hands them back to Exchange.
func (s *Service) AuthCodeURL(state, nonce, verifier string) string {
	return s.oauth.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("nonce", nonce),
	)
}

// Exchange trades the authorization code for a verified identity.
//
// The nonce comparison is done here and is not optional: verifying an ID token
// proves the provider issued it, not that it was issued for this login attempt.
func (s *Service) Exchange(ctx context.Context, code, verifier, expectedNonce string) (*idtoken.Claims, error) {
	tok, err := s.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("oidcauth: code exchange: %w", err)
	}

	rawID, ok := tok.Extra("id_token").(string)
	if !ok || rawID == "" {
		return nil, errors.New("oidcauth: token response carried no id_token")
	}

	claims, err := s.verifier.Verify(ctx, rawID)
	if err != nil {
		return nil, fmt.Errorf("oidcauth: verifying id_token: %w", err)
	}

	if expectedNonce == "" || claims.Nonce != expectedNonce {
		return nil, errors.New("oidcauth: id_token nonce does not match this authorization request")
	}
	if claims.Subject == "" {
		return nil, errors.New("oidcauth: id_token carried no subject")
	}
	if claims.Email == "" {
		return nil, errors.New("oidcauth: id_token carried no email claim; add the email scope")
	}
	// Fail closed on an unverified address: an issuer where anyone can
	// self-register an arbitrary email is exactly how federated login turns
	// into account takeover.
	if !claims.EmailVerified {
		return nil, errors.New("oidcauth: the provider did not report this email address as verified")
	}
	if err := s.domainAllowed(claims.Email); err != nil {
		return nil, err
	}

	return claims, nil
}

func (s *Service) domainAllowed(email string) error {
	if len(s.cfg.AllowedDomains) == 0 {
		return nil
	}
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return errors.New("oidcauth: malformed email claim")
	}
	domain := strings.ToLower(email[at+1:])
	for _, allowed := range s.cfg.AllowedDomains {
		if strings.EqualFold(strings.TrimSpace(allowed), domain) {
			return nil
		}
	}
	return fmt.Errorf("oidcauth: %s is not in OIDC_ALLOWED_DOMAINS", domain)
}

func contains(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}
