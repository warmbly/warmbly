package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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

// APIKeyMiddleware validates API keys and extracts permissions
// Only allows API key authentication, not JWT
func (h *Handler) APIKeyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")

		// Check for API key format: "Bearer wmbly_..."
		if strings.HasPrefix(authHeader, "Bearer wmbly_") {
			key := strings.TrimPrefix(authHeader, "Bearer ")
			h.validateAPIKey(c, key)
			return
		}

		errx.Handle(c, errx.ErrAuth)
		c.Abort()
	}
}

// CombinedAuthMiddleware allows either JWT or API key authentication
func (h *Handler) CombinedAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")

		if strings.HasPrefix(authHeader, "Bearer wmbly_") {
			// API Key path
			key := strings.TrimPrefix(authHeader, "Bearer ")
			h.validateAPIKey(c, key)
			return
		} else if strings.HasPrefix(authHeader, "Bearer ") {
			// JWT path
			token := strings.TrimPrefix(authHeader, "Bearer ")
			h.validateJWT(c, token)
			return
		}

		errx.Handle(c, errx.ErrAuth)
		c.Abort()
	}
}

func (h *Handler) validateAPIKey(c *gin.Context, rawKey string) {
	if h.APIKeyService == nil {
		errx.Handle(c, errx.ErrAuth)
		c.Abort()
		return
	}

	// Validate the key
	apiKey, xerr := h.APIKeyService.ValidateKey(c.Request.Context(), rawKey)
	if xerr != nil {
		errx.Handle(c, xerr)
		c.Abort()
		return
	}

	// Check IP if restricted
	clientIP := c.ClientIP()
	if !h.APIKeyService.ValidateKeyIP(apiKey, clientIP) {
		errx.Handle(c, errx.New(errx.Forbidden, "IP not allowed for this API key"))
		c.Abort()
		return
	}

	// Set context values
	c.Set(AuthTypeKey, AuthTypeAPIKey)
	c.Set(APIKeyIDKey, apiKey.ID.String())
	c.Set(APIKeyPermissionsKey, apiKey.Permissions)
	c.Set(UserIDKey, apiKey.UserID.String())
	c.Set(OrganizationIDKey, apiKey.OrganizationID)

	// Update last used (async)
	h.APIKeyService.UpdateLastUsed(c.Request.Context(), apiKey.ID)

	c.Next()
}

func (h *Handler) validateJWT(c *gin.Context, token string) {
	userID, err := h.TokenService.ValidateAccessToken(c.Request.Context(), token)
	if err != nil {
		errx.Handle(c, err)
		c.Abort()
		return
	}

	c.Set(AuthTypeKey, AuthTypeJWT)
	c.Set(UserIDKey, userID)
	c.Set(AccessTokenKey, token)
	c.Next()
}

// RequireAPIPermission checks if the current auth has specific permission
// Only applies to API key authentication - JWT users have full access
func RequireAPIPermission(perm uint64) gin.HandlerFunc {
	return func(c *gin.Context) {
		authType := c.GetString(AuthTypeKey)

		if authType == AuthTypeAPIKey {
			perms, exists := c.Get(APIKeyPermissionsKey)
			if !exists {
				errx.Handle(c, errx.ErrForbidden)
				c.Abort()
				return
			}

			permissions, ok := perms.(uint64)
			if !ok || !models.HasAPIPermission(permissions, perm) {
				errx.Handle(c, errx.New(errx.Forbidden, "Insufficient API key permissions"))
				c.Abort()
				return
			}
		}
		// JWT users have full access to their own resources
		c.Next()
	}
}

// GetAuthType returns the authentication type ("jwt" or "api_key")
func GetAuthType(c *gin.Context) string {
	return c.GetString(AuthTypeKey)
}

// GetAPIKeyID returns the API key ID if authenticated via API key
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

// GetAPIKeyPermissions returns the API key permissions bitmask
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
