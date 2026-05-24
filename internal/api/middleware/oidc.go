package middleware

import (
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/warmbly/warmbly/internal/errx"
)

const (
	googleIssuer = "https://accounts.google.com"
)

type OidcHandler struct {
	ServiceAccount string
	KeySet         keyfunc.Keyfunc
	AppEnv         string
}

func (h *OidcHandler) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.AppEnv == "dev" {
			c.Next()
			return
		}

		auth := c.GetHeader("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			errx.Handle(c, errx.ErrForbidden)
			return
		}

		tokenStr := strings.TrimPrefix(auth, "Bearer ")

		token, err := jwt.Parse(tokenStr, h.KeySet.Keyfunc, jwt.WithLeeway(10*time.Second))
		if err != nil {
			errx.Handle(c, errx.ErrForbidden)
			return
		}

		if !token.Valid {
			errx.Handle(c, errx.ErrForbidden)
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			errx.Handle(c, errx.ErrForbidden)
			return
		}

		if iss, _ := claims.GetIssuer(); iss != googleIssuer {
			errx.Handle(c, errx.ErrForbidden)
			return
		}

		if sAccount, _ := claims.GetSubject(); sAccount != h.ServiceAccount {
			errx.Handle(c, errx.ErrForbidden)
			return
		}

		c.Next()
	}
}
