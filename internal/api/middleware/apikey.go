package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/apikey"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

const (
	APIKeyIDKey          = "api_key_id"
	APIKeyPermissionsKey = "api_key_permissions"
	APIKeyUserIDKey      = "api_key_user_id"
	AuthTypeKey          = "auth_type"
	AuthTypeJWT          = "jwt"
	AuthTypeAPIKey       = "api_key"
)

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

	c.Set(AuthTypeKey, AuthTypeAPIKey)
	c.Set(APIKeyIDKey, key.ID.String())
	c.Set(APIKeyPermissionsKey, key.Permissions)
	c.Set(UserIDKey, key.UserID.String())
	c.Set(OrganizationIDKey, key.OrganizationID)

	// UpdateLastUsed is itself fire-and-forget.
	h.APIKeyService.UpdateLastUsed(c.Request.Context(), key.ID)

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
		if c.GetString(AuthTypeKey) != AuthTypeAPIKey {
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
		case AuthTypeAPIKey:
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
