package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/apikey"
	"github.com/warmbly/warmbly/internal/app/oauth"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

const (
	APIKeyIDKey                   = "api_key_id"
	APIKeyNameKey                 = "api_key_name"
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
	c.Set(APIKeyNameKey, key.Name)
	c.Set(APIKeyPermissionsKey, key.Permissions)
	c.Set(APIKeyAllowedEmailAccountsKey, key.AllowedEmailAccounts)
	c.Set(UserIDKey, key.UserID.String())
	c.Set(OrganizationIDKey, key.OrganizationID)

	// A key acts for the member who created it: it ends with their membership and never exceeds their current role.
	if h.OrganizationService != nil {
		member, xerr := h.OrganizationService.GetMembership(c.Request.Context(), key.OrganizationID, key.UserID)
		if xerr != nil {
			errx.JSON(c, xerr)
			c.Abort()
			return
		}
		if member == nil || member.AcceptedAt == nil {
			errx.JSON(c, errx.NewWithIdentifier(errx.Unauthorized, "api_key_holder_left",
				"The member who created this API key is no longer in this workspace. Create a new key."))
			c.Abort()
			return
		}
		c.Set(SessionMemberKey, member)
	}

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
	setOAuthCaller(c, claims)
	c.Next()
}

// setOAuthCaller puts an OAuth token's identity on the request. The granting member's
// membership rides along, so every gate holds the token to that member's role as well as its scopes.
func setOAuthCaller(c *gin.Context, claims *oauth.AccessClaims) {
	c.Set(AuthTypeKey, AuthTypeOAuth)
	c.Set(APIKeyPermissionsKey, claims.Scopes)
	c.Set(UserIDKey, claims.UserID.String())
	c.Set(OrganizationIDKey, claims.OrganizationID)
	c.Set(OAuthApplicationIDKey, claims.ApplicationID)
	if claims.Member != nil {
		c.Set(SessionMemberKey, claims.Member)
	}
}

// RefuseOAuth keeps OAuth app tokens off routes that mint or manage credentials.
func RefuseOAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetString(AuthTypeKey) == AuthTypeOAuth {
			errx.JSON(c, errx.NewWithIdentifier(errx.Forbidden, "oauth_token_not_allowed",
				"OAuth app tokens cannot manage API keys or OAuth apps. Use the dashboard or an API key."))
			c.Abort()
			return
		}
		c.Next()
	}
}

// GetMemberPermissions returns the caller's membership permissions resolved at authentication
// (a session or an OAuth token), with the owner holding all of them.
func GetMemberPermissions(c *gin.Context) (models.OrganizationPermission, bool) {
	v, ok := c.Get(SessionMemberKey)
	if !ok {
		return 0, false
	}
	m, ok := v.(*models.OrganizationMember)
	if !ok || m == nil {
		return 0, false
	}
	if m.IsOwner() {
		return models.AllPermissions, true
	}
	return m.Permissions, true
}

// GetAuthMember returns the caller's membership resolved at authentication (session or OAuth token), or nil.
func GetAuthMember(c *gin.Context) *models.OrganizationMember {
	if v, ok := c.Get(SessionMemberKey); ok {
		if m, ok := v.(*models.OrganizationMember); ok {
			return m
		}
	}
	return nil
}

// oauthMemberAllows reports whether the member behind an API key or OAuth token holds any of perms; sessions pass.
func (h *Handler) oauthMemberAllows(c *gin.Context, perms ...models.OrganizationPermission) (bool, *errx.Error) {
	if !bitmaskAuth(c.GetString(AuthTypeKey)) || h.OrganizationService == nil {
		return true, nil
	}
	userID, err := GetUserUUID(c)
	if err != nil {
		return false, errx.ErrUnauthorized
	}
	orgID := GetOrganizationID(c)
	if orgID == nil {
		return false, errx.New(errx.BadRequest, "no organization selected")
	}
	for _, p := range perms {
		has, xerr := h.memberHasPermission(c, *orgID, userID, p)
		if xerr != nil {
			return false, xerr
		}
		if has {
			return true, nil
		}
	}
	return false, nil
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
	if xerr := h.setSessionOrganization(c, session); xerr != nil {
		errx.JSON(c, xerr)
		c.Abort()
		return
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
			if allowed, xerr := h.oauthMemberAllows(c, orgPerm); xerr != nil || !allowed {
				errx.JSON(c, orNotPermitted(xerr))
				c.Abort()
				return
			}
			c.Next()
		default:
			// JWT path: defer to the org-permission gate, which refuses when it cannot check.
			if h.OrganizationService == nil {
				errx.JSON(c, errx.InternalError())
				c.Abort()
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
			has, xerr := h.memberHasPermission(c, *orgID, userID, orgPerm)
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

// RequireKeyHolder passes an API key only while the member who created it holds perm; other callers pass.
func (h *Handler) RequireKeyHolder(perm models.OrganizationPermission) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetString(AuthTypeKey) != AuthTypeAPIKey {
			c.Next()
			return
		}
		userID, err := GetUserUUID(c)
		orgID := GetOrganizationID(c)
		if err != nil || orgID == nil || h.OrganizationService == nil {
			errx.JSON(c, errx.ErrForbidden)
			c.Abort()
			return
		}
		has, xerr := h.memberHasPermission(c, *orgID, userID, perm)
		if xerr != nil {
			errx.JSON(c, xerr)
			c.Abort()
			return
		}
		if !has {
			errx.JSON(c, errx.New(errx.Forbidden, "the member who created this API key does not have this permission"))
			c.Abort()
			return
		}
		c.Next()
	}
}

// orNotPermitted is the lookup failure when there is one, and the member's missing permission otherwise.
func orNotPermitted(xerr *errx.Error) *errx.Error {
	if xerr != nil {
		return xerr
	}
	return errx.New(errx.Forbidden, "the member who authorized this app does not have this permission")
}

// RequireAccessWithQuery applies RequireAccess only when the request carries
// the named query parameter.
func (h *Handler) RequireAccessWithQuery(param string, orgPerm models.OrganizationPermission, apiPerm uint64) gin.HandlerFunc {
	gate := h.RequireAccess(orgPerm, apiPerm)
	return func(c *gin.Context) {
		if strings.TrimSpace(c.Query(param)) == "" {
			c.Next()
			return
		}
		gate(c)
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
			if allowed, xerr := h.oauthMemberAllows(c, orgPerms...); xerr != nil || !allowed {
				errx.JSON(c, orNotPermitted(xerr))
				c.Abort()
				return
			}
			c.Next()
		default:
			if h.OrganizationService == nil {
				errx.JSON(c, errx.InternalError())
				c.Abort()
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
				has, xerr := h.memberHasPermission(c, *orgID, userID, p)
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

// GetAuthType returns "jwt" or "api_key" (empty if unauthenticated).
func GetAuthType(c *gin.Context) string {
	return c.GetString(AuthTypeKey)
}

// GetAPIKeyName returns the authenticating API key's display name, or ""
// for a session (JWT) request.
func GetAPIKeyName(c *gin.Context) string {
	return c.GetString(APIKeyNameKey)
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
