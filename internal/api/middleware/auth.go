package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

const (
	UserIDKey         = "user_id"
	AccessTokenKey    = "access_token"
	SessionKey        = "session"
	OrganizationIDKey = "organization_id"
)

func (h *Handler) AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			errx.Handle(c, errx.ErrAuth)
			c.Abort()
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")

		session, err := h.TokenService.ValidateAccessToken(c.Request.Context(), token)
		if err != nil {
			errx.Handle(c, err)
			c.Abort()
			return
		}

		c.Set(UserIDKey, session.UserID.String())
		c.Set(SessionKey, session)
		c.Set(AccessTokenKey, token)

		// Set organization context if available
		if session.CurrentOrganizationID != nil {
			c.Set(OrganizationIDKey, *session.CurrentOrganizationID)
		}

		c.Next()
	}
}

func GetUserID(c *gin.Context) string {
	return c.GetString(UserIDKey)
}

func GetUserUUID(c *gin.Context) (uuid.UUID, error) {
	return uuid.Parse(c.GetString(UserIDKey))
}

func GetAccessToken(c *gin.Context) string {
	return c.GetString(AccessTokenKey)
}

func GetSession(c *gin.Context) *models.Session {
	if session, exists := c.Get(SessionKey); exists {
		if s, ok := session.(*models.Session); ok {
			return s
		}
	}
	return nil
}

func GetOrganizationID(c *gin.Context) *uuid.UUID {
	if orgID, exists := c.Get(OrganizationIDKey); exists {
		if id, ok := orgID.(uuid.UUID); ok {
			return &id
		}
	}
	return nil
}
