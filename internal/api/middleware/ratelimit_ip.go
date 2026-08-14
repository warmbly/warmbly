package middleware

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	// authIPWindow and authIPLimit bound unauthenticated auth traffic from one
	// source. The limit is generous enough for a shared office NAT retyping a
	// password, and far below what credential stuffing needs.
	authIPWindow       = 15 * time.Minute
	authIPDefaultLimit = 60
)

// AuthIPRateLimitMiddleware throttles the public /auth group per source IP.
//
// This is the only limiter those routes have. RateLimitMiddleware keys on the
// authenticated user id and calls c.Next() when there is none, so before this
// existed every pre-login endpoint was unbounded: password guessing was free,
// and each guess cost a 64 MiB Argon2 hash, which is a memory-exhaustion lever
// on the small VPS most self-hosters run.
//
// Fails open on a cache error, deliberately: a Redis blip must not lock every
// user out of their own instance.
func (h *Handler) AuthIPRateLimitMiddleware() gin.HandlerFunc {
	limit := authIPDefaultLimit
	if v := os.Getenv("AUTH_IP_RATE_LIMIT"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	return func(c *gin.Context) {
		if h.Cache == nil {
			c.Next()
			return
		}
		// Reads are cheap and the login screen makes one on every load.
		if c.Request.Method == http.MethodGet {
			c.Next()
			return
		}

		ip := c.ClientIP()
		if ip == "" {
			c.Next()
			return
		}

		key := "auth_ip:" + ip
		n, err := h.Cache.Incr(c.Request.Context(), key).Result()
		if err != nil {
			c.Next()
			return
		}
		if n == 1 {
			_ = h.Cache.Expire(c.Request.Context(), key, authIPWindow).Err()
		}

		if n > int64(limit) {
			c.Header("Retry-After", fmt.Sprintf("%d", int(authIPWindow.Seconds())))
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":   "rate_limit_exceeded",
				"message": "Too many authentication attempts from this address. Try again later.",
				"code":    "rate_limit_exceeded",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
