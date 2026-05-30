package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	RequestIDContextKey = "request_id"
	RequestIDHeader     = "X-Request-Id"
)

// RequestIDMiddleware attaches a stable request ID to every request and
// response. Clients may provide one for cross-system tracing; invalid or
// oversized values are replaced so logs and responses stay safe.
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := sanitizeRequestID(c.GetHeader(RequestIDHeader))
		if requestID == "" {
			requestID = uuid.NewString()
		}

		c.Set(RequestIDContextKey, requestID)
		c.Header(RequestIDHeader, requestID)
		c.Next()
	}
}

func sanitizeRequestID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return ""
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.' || r == ':':
		default:
			return ""
		}
	}
	return value
}
