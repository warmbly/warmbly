package middleware

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/app/apikey"
	"github.com/warmbly/warmbly/internal/models"
)

// MCPAuthMiddleware authenticates the MCP streamable-HTTP endpoint with either an
// API key (the static-header path) or an OAuth 2.1 access token (the one-command
// `claude mcp add` path). On any missing/invalid credential it returns 401 with an
// RFC 9728 WWW-Authenticate challenge pointing at the protected-resource metadata,
// so a spec-compliant MCP client discovers the authorization server and completes
// the OAuth flow without the user pasting anything.
func (h *Handler) MCPAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		switch {
		case strings.HasPrefix(authHeader, "Bearer "+apikey.KeyPrefix):
			// Reuse the full API-key path (IP allowlist, per-key rate limit, usage).
			h.validateAPIKey(c, strings.TrimPrefix(authHeader, "Bearer "))
		case strings.HasPrefix(authHeader, "Bearer "+models.OAuthAccessTokenPrefix):
			h.mcpValidateOAuth(c, strings.TrimPrefix(authHeader, "Bearer "))
		default:
			mcpChallenge(c)
		}
	}
}

// mcpValidateOAuth mirrors validateOAuthToken but answers auth failures with the
// discovery challenge (instead of a bare 401) so an expired token nudges the
// client back through the flow rather than dead-ending.
func (h *Handler) mcpValidateOAuth(c *gin.Context, token string) {
	if h.OAuthService == nil {
		mcpChallenge(c)
		return
	}
	claims, err := h.OAuthService.ValidateAccessToken(c.Request.Context(), token)
	if err != nil {
		mcpChallenge(c)
		return
	}
	c.Set(AuthTypeKey, AuthTypeOAuth)
	c.Set(APIKeyPermissionsKey, claims.Scopes)
	c.Set(UserIDKey, claims.UserID.String())
	c.Set(OrganizationIDKey, claims.OrganizationID)
	c.Set(OAuthApplicationIDKey, claims.ApplicationID)
	c.Next()
}

// mcpChallenge writes the 401 + WWW-Authenticate resource_metadata pointer.
func mcpChallenge(c *gin.Context) {
	c.Header("WWW-Authenticate", `Bearer resource_metadata="`+mcpResourceMetadataURL(c)+`"`)
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
		"error":      "unauthorized",
		"message":    "authenticate with an OAuth 2.1 access token or API key",
		"code":       "unauthorized",
		"request_id": c.GetString(RequestIDContextKey),
	})
}

// mcpResourceMetadataURL is the absolute RFC 9728 metadata URL for /v1/mcp, from
// API_PUBLIC_URL with a request-host fallback (matches the discovery handlers).
func mcpResourceMetadataURL(c *gin.Context) string {
	base := strings.TrimRight(os.Getenv("API_PUBLIC_URL"), "/")
	if base == "" {
		scheme := "https"
		if c.Request.TLS == nil && c.Request.Header.Get("X-Forwarded-Proto") == "" {
			scheme = "http"
		}
		base = scheme + "://" + c.Request.Host
	}
	return base + "/.well-known/oauth-protected-resource"
}
