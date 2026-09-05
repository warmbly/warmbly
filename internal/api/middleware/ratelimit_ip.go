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

	// The CLI sign-in handshake gets its own budget, on its own key.
	//
	// It cannot share the auth one: `warmbly auth login` polls every
	// CLIAuthPollIntervalSeconds for up to CLIAuthCodeTTLMinutes, which is
	// around 200 requests for a single sign-in. On the shared budget that
	// exhausts the allowance in three minutes, and then blocks the person's
	// actual login from the same address for the rest of the window. The
	// allowance below covers two concurrent sign-ins from one NAT with slack.
	cliAuthIPWindow       = 15 * time.Minute
	cliAuthIPDefaultLimit = 500
)

// CLIAuthIPRateLimitMiddleware throttles the public CLI sign-in handshake per
// source IP, on a key of its own so a long poll cannot lock the same address
// out of signing in through the browser.
func (h *Handler) CLIAuthIPRateLimitMiddleware() gin.HandlerFunc {
	return h.ipRateLimiter("cli_auth_ip:", cliAuthIPDefaultLimit, "CLI_AUTH_IP_RATE_LIMIT", cliAuthIPWindow,
		"Too many CLI sign-in requests from this address. Try again later.")
}

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
	return h.ipRateLimiter("auth_ip:", authIPDefaultLimit, "AUTH_IP_RATE_LIMIT", authIPWindow,
		"Too many authentication attempts from this address. Try again later.")
}

// ipRateLimiter is the shared fixed-window limiter behind both. Each caller
// brings its own Redis key prefix, so budgets never bleed into each other.
func (h *Handler) ipRateLimiter(prefix string, defaultLimit int, env string, window time.Duration, message string) gin.HandlerFunc {
	limit := defaultLimit
	if v := os.Getenv(env); v != "" {
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

		key := prefix + ip
		n, err := h.Cache.Incr(c.Request.Context(), key).Result()
		if err != nil {
			c.Next()
			return
		}
		// A counter with no TTL never resets, so the address it belongs to
		// stays blocked forever once it passes the limit. That is a worse
		// outcome than not counting at all, so a failed EXPIRE drops the key
		// and lets the request through, matching how the rest of this
		// middleware handles a cache it cannot trust.
		if n == 1 {
			if err := h.Cache.Expire(c.Request.Context(), key, window).Err(); err != nil {
				_ = h.Cache.Del(c.Request.Context(), key).Err()
				c.Next()
				return
			}
		} else if n > int64(limit) {
			// Repair a key that lost its expiry some other way (an older
			// build, a restore, an eviction between the INCR and the EXPIRE
			// above). Only on the reject path, which is rare, so it costs a
			// round trip nobody feels.
			if ttl, terr := h.Cache.TTL(c.Request.Context(), key).Result(); terr == nil && ttl < 0 {
				_ = h.Cache.Expire(c.Request.Context(), key, window).Err()
			}
		}

		if n > int64(limit) {
			c.Header("Retry-After", fmt.Sprintf("%d", int(window.Seconds())))
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":   "rate_limit_exceeded",
				"message": message,
				"code":    "rate_limit_exceeded",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
