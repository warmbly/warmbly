package oauth

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

// Dynamic Client Registration (RFC 7591). An MCP client (Claude Code, Cursor,
// Claude Desktop, ...) registers itself at runtime as a PUBLIC client — PKCE, no
// secret — so a customer connects Warmbly's MCP endpoint with one `claude mcp add`
// and a browser sign-in. Registration mints only a client_id and does NOT grant
// access: a human still approves the exact scopes at the consent screen, and the
// org/user are bound to the resulting grant, not to the client.

// dcrMaxPerIPPerHour caps registrations from one source IP so the open endpoint
// can't be used to bloat the clients table.
const dcrMaxPerIPPerHour = 20

// DCRRequest is the RFC 7591 client-registration request (only the members we
// honor; unknown members are ignored per the spec).
type DCRRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope"`
	ClientURI               string   `json:"client_uri"`
	LogoURI                 string   `json:"logo_uri"`
}

// DCRResponse is the RFC 7591 client-information response. client_secret is set
// only for a confidential registration (token_endpoint_auth_method != "none").
type DCRResponse struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope"`
}

// MCPRegistrableScopes is the widest scope set a self-registered client may hold.
// It excludes SEND_CAMPAIGNS (a self-registered assistant must never be able to
// send real mail) and API_KEYS (never let it mint further credentials). The human
// still narrows this at consent, and the MCP endpoint drops send-class tools
// regardless — this is the outer, structural cap.
func MCPRegistrableScopes() uint64 {
	return models.AllAPIPermissionsMask &^ (models.APIPermSendCampaigns | models.APIPermAPIKeys)
}

// RegisterDynamicClient creates a client at runtime from an RFC 7591 request.
func (s *Service) RegisterDynamicClient(ctx context.Context, clientIP string, req DCRRequest) (*DCRResponse, error) {
	if err := s.checkDCRRate(ctx, clientIP); err != nil {
		return nil, err
	}

	name := strings.TrimSpace(req.ClientName)
	if name == "" {
		name = "MCP client"
	}
	if len(name) > 200 {
		name = name[:200]
	}

	uris, err := validateDCRRedirectURIs(req.RedirectURIs)
	if err != nil {
		return nil, err
	}

	// Default to a public (PKCE, no-secret) client — the native-app shape every MCP
	// client uses. A caller may opt into a confidential client by asking for a
	// secret-based auth method, in which case we mint and return a secret once.
	isPublic := req.TokenEndpointAuthMethod == "" || req.TokenEndpointAuthMethod == "none"
	authMethod := "none"
	if !isPublic {
		authMethod = "client_secret_basic"
	}

	registrable := MCPRegistrableScopes()
	requested, unknown := ParseScopes(req.Scope)
	if len(unknown) > 0 {
		return nil, errInvalidClientMetadata("unknown scope: " + strings.Join(unknown, " "))
	}
	scopes := registrable
	if requested != 0 {
		scopes = requested & registrable
	}
	if scopes == 0 {
		scopes = registrable
	}

	clientID, err := randomToken(models.OAuthClientIDPrefix)
	if err != nil {
		return nil, errServer("could not mint client id")
	}

	app := &models.OAuthApplication{
		Name:                  name,
		WebsiteURL:            strings.TrimSpace(req.ClientURI),
		LogoURL:               strings.TrimSpace(req.LogoURI),
		ClientID:              clientID,
		RedirectURIs:          uris,
		AllowedWebhookDomains: []string{},
		WebhookEvents:         []string{},
		Scopes:                scopes,
		Status:                models.OAuthAppActive,
		IsPublic:              isPublic,
		DynamicallyRegistered: true,
	}

	var secret string
	if !isPublic {
		secret, err = randomToken(models.OAuthClientSecretPrefix)
		if err != nil {
			return nil, errServer("could not mint client secret")
		}
		app.ClientSecretHash = hashToken(secret)
	}

	if err := s.repo.CreateApplication(ctx, app); err != nil {
		return nil, errServer("could not store client")
	}

	return &DCRResponse{
		ClientID:                clientID,
		ClientSecret:            secret,
		ClientIDIssuedAt:        time.Now().UTC().Unix(),
		ClientName:              name,
		RedirectURIs:            uris,
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: authMethod,
		Scope:                   ScopeString(scopes),
	}, nil
}

// validateDCRRedirectURIs accepts the redirect shapes native apps use: https
// (non-loopback), loopback http on any port (RFC 8252), and private-use / custom
// schemes (cursor://, vscode://, com.example.app://). Plain http to a non-loopback
// host is rejected. Matching at authorize/token time is still exact-string.
func validateDCRRedirectURIs(uris []string) ([]string, error) {
	out := make([]string, 0, len(uris))
	seen := map[string]bool{}
	for _, u := range uris {
		u = strings.TrimSpace(u)
		if u == "" || seen[u] {
			continue
		}
		if len(u) > 2048 {
			return nil, errInvalidRedirectURI("redirect URI too long")
		}
		parsed, err := url.Parse(u)
		if err != nil || parsed.Scheme == "" {
			return nil, errInvalidRedirectURI("invalid redirect URI: " + u)
		}
		if parsed.Fragment != "" {
			return nil, errInvalidRedirectURI("a redirect URI must not contain a fragment: " + u)
		}
		switch {
		case parsed.Scheme == "https" && parsed.Host != "":
			// hosted https callback (e.g. https://claude.ai/api/mcp/auth_callback)
		case isLoopbackRedirect(parsed):
			// loopback http on any port (native apps)
		case parsed.Scheme != "http" && parsed.Scheme != "https":
			// Private-use / custom scheme for native apps (cursor://, vscode://,
			// com.example.app://callback). Reject dangerous pseudo-schemes and any
			// opaque form (javascript:payload, data:...): the consent page navigates
			// to this URI, so an executable scheme here is stored XSS in our origin.
			if dangerousRedirectScheme(parsed.Scheme) || parsed.Opaque != "" || (parsed.Host == "" && parsed.Path == "") {
				return nil, errInvalidRedirectURI("unsupported redirect URI scheme: " + u)
			}
		default:
			return nil, errInvalidRedirectURI("a redirect URI must be https, loopback, or a private-use scheme: " + u)
		}
		seen[u] = true
		out = append(out, u)
	}
	if len(out) == 0 {
		return nil, errInvalidRedirectURI("at least one redirect URI is required")
	}
	if len(out) > 12 {
		return nil, errInvalidClientMetadata("too many redirect URIs")
	}
	return out, nil
}

// dangerousRedirectScheme rejects pseudo-schemes that would execute script or
// read local resources if a page ever navigated to the redirect URI.
func dangerousRedirectScheme(scheme string) bool {
	switch strings.ToLower(scheme) {
	case "javascript", "data", "vbscript", "file", "blob", "about", "filesystem":
		return true
	}
	return false
}

// checkDCRRate enforces a per-IP hourly cap on the open registration endpoint.
// Fails open when the cache is unavailable so a Redis hiccup can't block a genuine
// first-time connect.
func (s *Service) checkDCRRate(ctx context.Context, ip string) error {
	if s.cache == nil || ip == "" {
		return nil
	}
	bucket := time.Now().UTC().Unix() / 3600
	key := fmt.Sprintf("oauth:dcr:%s:%d", ip, bucket)
	n, err := s.cache.Incr(ctx, key).Result()
	if err != nil {
		return nil
	}
	if n == 1 {
		s.cache.Expire(ctx, key, time.Hour)
	}
	if n > dcrMaxPerIPPerHour {
		return errTooManyRegistrations()
	}
	return nil
}
