package middleware

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// RateLimitMiddleware checks rate limits for a given category
func (h *Handler) RateLimitMiddleware(category models.RateLimitCategory) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip if no rate limit service
		if h.RateLimitService == nil {
			c.Next()
			return
		}

		userIDStr := GetUserID(c)
		if userIDStr == "" {
			c.Next()
			return
		}

		userID, err := uuid.Parse(userIDStr)
		if err != nil {
			c.Next()
			return
		}

		// Check and record the request atomically
		status, err := h.RateLimitService.CheckAndRecord(c.Request.Context(), userID, category)
		if err != nil {
			// Log error but allow request (fail open)
			sentry.CaptureException(err)
			c.Next()
			return
		}

		// Set rate limit headers
		c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", status.Limit))
		c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", status.Remaining))
		c.Header("X-RateLimit-Reset", fmt.Sprintf("%d", status.ResetAt.Unix()))

		// Check if rate limited
		if status.Remaining <= 0 && status.RetryAfterMs != nil {
			c.Header("Retry-After", fmt.Sprintf("%d", *status.RetryAfterMs/1000))
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":          "rate_limit_exceeded",
				"message":        fmt.Sprintf("Rate limit exceeded for %s operations", category),
				"retry_after_ms": status.RetryAfterMs,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// RateLimitRead is a convenience wrapper for read operations
func (h *Handler) RateLimitRead() gin.HandlerFunc {
	return h.RateLimitMiddleware(models.RateLimitRead)
}

// RateLimitWrite is a convenience wrapper for write operations
func (h *Handler) RateLimitWrite() gin.HandlerFunc {
	return h.RateLimitMiddleware(models.RateLimitWrite)
}

// RateLimitBulk is a convenience wrapper for bulk operations
func (h *Handler) RateLimitBulk() gin.HandlerFunc {
	return h.RateLimitMiddleware(models.RateLimitBulk)
}

// RateLimitUnibox is a convenience wrapper for unibox operations
func (h *Handler) RateLimitUnibox() gin.HandlerFunc {
	return h.RateLimitMiddleware(models.RateLimitUnibox)
}

// RateLimitAnalytics is a convenience wrapper for analytics operations
func (h *Handler) RateLimitAnalytics() gin.HandlerFunc {
	return h.RateLimitMiddleware(models.RateLimitAnalytics)
}
