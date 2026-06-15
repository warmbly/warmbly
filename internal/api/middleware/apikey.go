package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/apikey"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

const (
	APIKeyIDKey                   = "api_key_id"
	APIKeyPermissionsKey          = "api_key_permissions"
	APIKeyAllowedEmailAccountsKey = "api_key_allowed_email_accounts"
	APIKeyUserIDKey               = "api_key_user_id"
	OAuthApplicationIDKey         = "oauth_application_id"
	AuthTypeKey                   = "auth_type"
	AuthTypeJWT                   = "jwt"
	AuthTypeAPIKey                = "api_key"
	AuthTypeOAuth                 = "oauth"
)

// GetOAuthApplicationID returns the OAuth application id when the caller
// authenticated with an OAuth access token, or nil otherwise. Used to bind
// app-registered webhook endpoints (and enforce their domain allowlist).
func GetOAuthApplicationID(c *gin.Context) *uuid.UUID {
	v, ok := c.Get(OAuthApplicationIDKey)
	if !ok {
		return nil
	}
	id, ok := v.(uuid.UUID)
	if !ok || id == uuid.Nil {
		return nil
	}
	return &id
}

// bitmaskAuth reports whether a caller's permissions come from a bitmask of API
// scopes (API keys and OAuth tokens) rather than an org role (JWT sessions).
func bitmaskAuth(authType string) bool {
	return authType == AuthTypeAPIKey || authType == AuthTypeOAuth
}

// APIKeyMiddleware accepts only API key auth ("Bearer wmbly_..."). Reserved
// for endpoints that should never accept browser sessions — none today, but
// useful if we add API-only routes (e.g. partner integrations).
func (h *Handler) APIKeyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")

		if strings.HasPrefix(authHeader, "Bearer "+apikey.KeyPrefix) {
			key := strings.TrimPrefix(authHeader, "Bearer ")
			h.validateAPIKey(c, key)
			return
		}

		errx.Handle(c, errx.ErrAuth)
		c.Abort()
	}
}

// CombinedAuthMiddleware accepts either a JWT or an API key. The two paths
// set the same context keys (UserIDKey, OrganizationIDKey) so downstream
// handlers don't need to branch on auth_type unless they care about it.
func (h *Handler) CombinedAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")

		switch {
		case strings.HasPrefix(authHeader, "Bearer "+apikey.KeyPrefix):
			key := strings.TrimPrefix(authHeader, "Bearer ")
			h.validateAPIKey(c, key)
		case strings.HasPrefix(authHeader, "Bearer "+models.OAuthAccessTokenPrefix):
			token := strings.TrimPrefix(authHeader, "Bearer ")
			h.validateOAuthToken(c, token)
		case strings.HasPrefix(authHeader, "Bearer "):
			token := strings.TrimPrefix(authHeader, "Bearer ")
			h.validateJWT(c, token)
		default:
			errx.Handle(c, errx.ErrAuth)
			c.Abort()
		}
	}
}

func (h *Handler) validateAPIKey(c *gin.Context, rawKey string) {
	if h.APIKeyService == nil {
		errx.Handle(c, errx.ErrAuth)
		c.Abort()
		return
	}

	key, xerr := h.APIKeyService.ValidateKey(c.Request.Context(), rawKey)
	if xerr != nil {
		errx.Handle(c, xerr)
		c.Abort()
		return
	}

	if !h.APIKeyService.ValidateKeyIP(key, c.ClientIP()) {
		errx.Handle(c, errx.New(errx.Forbidden, "IP not allowed for this API key"))
		c.Abort()
		return
	}

	// Per-key minute-window rate limit. Surfaces rate-limit headers on
	// every API-key request so well-behaved clients can self-throttle
	// before hitting 429. Fails open on cache errors.
	remaining, retryAfter, allowed := h.APIKeyService.CheckAndIncrementRateLimit(c.Request.Context(), key)
	limit := key.RateLimitPerMinute
	if limit <= 0 {
		limit = 60
	}
	c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
	c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
	c.Header("X-RateLimit-Policy", fmt.Sprintf("%d;w=60", limit))
	if !allowed {
		c.Header("Retry-After", fmt.Sprintf("%d", retryAfter))
		c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
			"error":       "rate_limit_exceeded",
			"message":     fmt.Sprintf("API key exceeded %d requests per minute", limit),
			"code":        "rate_limit_exceeded",
			"request_id":  c.GetString(RequestIDContextKey),
			"retry_after": retryAfter,
		})
		return
	}

	c.Set(AuthTypeKey, AuthTypeAPIKey)
	c.Set(APIKeyIDKey, key.ID.String())
	c.Set(APIKeyPermissionsKey, key.Permissions)
	c.Set(APIKeyAllowedEmailAccountsKey, key.AllowedEmailAccounts)
	c.Set(UserIDKey, key.UserID.String())
	c.Set(OrganizationIDKey, key.OrganizationID)

	// UpdateLastUsed is itself fire-and-forget; also remembers the caller
	// IP so the dashboard can show "last called from".
	h.APIKeyService.UpdateLastUsed(c.Request.Context(), key.ID, c.ClientIP())

	c.Next()
}

// validateOAuthToken authenticates an OAuth 2.1 bearer access token. It sets the
// same context keys as an API key (UserIDKey, OrganizationIDKey, and the granted
// scope bitmask in APIKeyPermissionsKey) so every existing route gate applies
// unchanged; auth_type is "oauth" so usage/last-used logic can tell them apart.
func (h *Handler) validateOAuthToken(c *gin.Context, token string) {
	if h.OAuthService == nil {
		errx.Handle(c, errx.ErrAuth)
		c.Abort()
		return
	}
	claims, err := h.OAuthService.ValidateAccessToken(c.Request.Context(), token)
	if err != nil {
		errx.Handle(c, errx.ErrAuth)
		c.Abort()
		return
	}
	c.Set(AuthTypeKey, AuthTypeOAuth)
	c.Set(APIKeyPermissionsKey, claims.Scopes)
	c.Set(UserIDKey, claims.UserID.String())
	c.Set(OrganizationIDKey, claims.OrganizationID)
	c.Set(OAuthApplicationIDKey, claims.ApplicationID)
	c.Next()
}

func (h *Handler) validateJWT(c *gin.Context, token string) {
	session, err := h.TokenService.ValidateAccessToken(c.Request.Context(), token)
	if err != nil {
		errx.Handle(c, err)
		c.Abort()
		return
	}

	c.Set(AuthTypeKey, AuthTypeJWT)
	c.Set(UserIDKey, session.UserID.String())
	c.Set(SessionKey, session)
	c.Set(AccessTokenKey, token)
	if session.CurrentOrganizationID != nil {
		c.Set(OrganizationIDKey, *session.CurrentOrganizationID)
	}
	c.Next()
}

// RequireAPIPermission gates a route on a single API permission bit. JWT
// callers are waved through — for them, the relevant gate is the
// OrganizationPermission check (RequirePermission / RequireAccess).
func RequireAPIPermission(perm uint64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !bitmaskAuth(c.GetString(AuthTypeKey)) {
			c.Next()
			return
		}

		perms, exists := c.Get(APIKeyPermissionsKey)
		if !exists {
			errx.Handle(c, errx.ErrForbidden)
			c.Abort()
			return
		}
		permissions, ok := perms.(uint64)
		if !ok || !models.HasAPIPermission(permissions, perm) {
			errx.Handle(c, errx.New(errx.Forbidden, "insufficient API key permissions"))
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequireAccess is the dual-auth permission gate. On a JWT request it
// enforces the caller's organization role (orgPerm); on an API key request
// it enforces the key's permission bit (apiPerm). Use this on routes that
// accept both auth types but need an explicit permission.
func (h *Handler) RequireAccess(orgPerm models.OrganizationPermission, apiPerm uint64) gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.GetString(AuthTypeKey) {
		case AuthTypeAPIKey, AuthTypeOAuth:
			perms, exists := c.Get(APIKeyPermissionsKey)
			if !exists {
				errx.Handle(c, errx.ErrForbidden)
				c.Abort()
				return
			}
			permissions, ok := perms.(uint64)
			if !ok || !models.HasAPIPermission(permissions, apiPerm) {
				errx.Handle(c, errx.New(errx.Forbidden, "insufficient API key permissions"))
				c.Abort()
				return
			}
			c.Next()
		default:
			// JWT path: defer to the org-permission gate.
			if h.OrganizationService == nil {
				c.Next()
				return
			}
			userID, err := GetUserUUID(c)
			if err != nil {
				errx.JSON(c, errx.ErrUnauthorized)
				c.Abort()
				return
			}
			orgID := GetOrganizationID(c)
			if orgID == nil {
				errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
				c.Abort()
				return
			}
			has, xerr := h.OrganizationService.HasPermission(c.Request.Context(), *orgID, userID, orgPerm)
			if xerr != nil {
				errx.JSON(c, xerr)
				c.Abort()
				return
			}
			if !has {
				errx.JSON(c, errx.ErrForbidden)
				c.Abort()
				return
			}
			c.Next()
		}
	}
}

// RequireAnyAccess is like RequireAccess but a JWT caller passes if they hold
// ANY of the listed organization permissions (the API-key path is unchanged: it
// checks the single apiPerm). Use on read routes reachable by multiple roles —
// e.g. integration reads allowed for both settings managers and operational
// integration users.
func (h *Handler) RequireAnyAccess(apiPerm uint64, orgPerms ...models.OrganizationPermission) gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.GetString(AuthTypeKey) {
		case AuthTypeAPIKey, AuthTypeOAuth:
			perms, exists := c.Get(APIKeyPermissionsKey)
			if !exists {
				errx.Handle(c, errx.ErrForbidden)
				c.Abort()
				return
			}
			permissions, ok := perms.(uint64)
			if !ok || !models.HasAPIPermission(permissions, apiPerm) {
				errx.Handle(c, errx.New(errx.Forbidden, "insufficient API key permissions"))
				c.Abort()
				return
			}
			c.Next()
		default:
			if h.OrganizationService == nil {
				c.Next()
				return
			}
			userID, err := GetUserUUID(c)
			if err != nil {
				errx.JSON(c, errx.ErrUnauthorized)
				c.Abort()
				return
			}
			orgID := GetOrganizationID(c)
			if orgID == nil {
				errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
				c.Abort()
				return
			}
			for _, p := range orgPerms {
				has, xerr := h.OrganizationService.HasPermission(c.Request.Context(), *orgID, userID, p)
				if xerr != nil {
					errx.JSON(c, xerr)
					c.Abort()
					return
				}
				if has {
					c.Next()
					return
				}
			}
			errx.JSON(c, errx.ErrForbidden)
			c.Abort()
		}
	}
}

// RequireAPIKeyEmailAccountParam enforces an API key's optional
// allowed_email_accounts allowlist against a route parameter. JWT callers and
// unrestricted API keys pass through.
func RequireAPIKeyEmailAccountParam(param string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetString(AuthTypeKey) != AuthTypeAPIKey {
			c.Next()
			return
		}

		allowed := GetAPIKeyAllowedEmailAccounts(c)
		if len(allowed) == 0 {
			c.Next()
			return
		}

		accountID, err := uuid.Parse(c.Param(param))
		if err != nil {
			errx.Handle(c, errx.ErrUuid)
			c.Abort()
			return
		}

		for _, id := range allowed {
			if id == accountID {
				c.Next()
				return
			}
		}

		errx.Handle(c, errx.New(errx.Forbidden, "email account is not allowed for this API key"))
		c.Abort()
	}
}

// GetAuthType returns "jwt" or "api_key" (empty if unauthenticated).
func GetAuthType(c *gin.Context) string {
	return c.GetString(AuthTypeKey)
}

// GetAPIKeyID returns the authenticating API key's ID, or nil when the
// request came in via JWT (or wasn't authenticated).
func GetAPIKeyID(c *gin.Context) *uuid.UUID {
	idStr := c.GetString(APIKeyIDKey)
	if idStr == "" {
		return nil
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return nil
	}
	return &id
}

// GetAPIKeyPermissions returns the bitmask granted to the authenticating
// API key, or 0 if the request was JWT-authenticated.
func GetAPIKeyPermissions(c *gin.Context) uint64 {
	perms, exists := c.Get(APIKeyPermissionsKey)
	if !exists {
		return 0
	}
	permissions, ok := perms.(uint64)
	if !ok {
		return 0
	}
	return permissions
}

// GetAPIKeyAllowedEmailAccounts returns the optional email-account allowlist
// attached to the authenticating API key. Empty means unrestricted.
func GetAPIKeyAllowedEmailAccounts(c *gin.Context) []uuid.UUID {
	value, exists := c.Get(APIKeyAllowedEmailAccountsKey)
	if !exists {
		return nil
	}
	ids, ok := value.([]uuid.UUID)
	if !ok {
		return nil
	}
	return ids
}
