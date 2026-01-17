package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/errx"
)

const (
	UserIDKey      = "user_id"
	AccessTokenKey = "access_token"
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

		userID, err := h.TokenService.ValidateAccessToken(c.Request.Context(), token)
		if err != nil {
			errx.Handle(c, err)
			c.Abort()
			return
		}

		c.Set(UserIDKey, userID)
		c.Set(AccessTokenKey, token)
		c.Next()
	}
}

func GetUserID(c *gin.Context) string {
	return c.GetString(UserIDKey)
}

func GetAccessToken(c *gin.Context) string {
	return c.GetString(AccessTokenKey)
}
