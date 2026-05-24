package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/models"
)

// APIKeyUsageMiddleware records one usage-log row per API-key request.
// Mounted once on the protected group so any route that accepts API key
// auth gets logged. JWT requests are skipped — they're tracked elsewhere
// (audit log, session activity).
//
// Logging is best-effort and runs after the response is written. We don't
// fail the request if the insert fails; the service layer captures errors
// to Sentry.
func (h *Handler) APIKeyUsageMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()

		if c.GetString(AuthTypeKey) != AuthTypeAPIKey {
			return
		}
		if h.APIKeyService == nil {
			return
		}

		keyID := GetAPIKeyID(c)
		if keyID == nil {
			return
		}

		// FullPath gives the matched route pattern (e.g. /campaigns/:id),
		// which is far more useful in the log than the concrete URL.
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		h.APIKeyService.LogUsage(c.Request.Context(), &models.APIKeyUsageLog{
			APIKeyID:     *keyID,
			Endpoint:     path,
			Method:       c.Request.Method,
			IPAddress:    c.ClientIP(),
			UserAgent:    c.Request.UserAgent(),
			ResponseCode: c.Writer.Status(),
			ResponseTime: int(time.Since(started).Milliseconds()),
		})
	}
}
