package middleware

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/pkg/secretset"
)

// InternalAuthMiddleware protects the internal routes the edge services
// (tracking, forms) call, with the static INTERNAL_API_TOKEN bearer. It fails
// closed when the token is unset; the compare is constant-time.
func (h *Handler) InternalAuthMiddleware() gin.HandlerFunc {
	return internalAuth
}

var (
	internalTokenOnce sync.Once
	internalToken     []byte
)

func loadInternalToken() {
	if v := os.Getenv("INTERNAL_API_TOKEN"); v != "" {
		internalToken = []byte(v)
	}
}

func internalAuth(c *gin.Context) {
	internalTokenOnce.Do(loadInternalToken)
	if len(internalToken) == 0 {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "internal auth not configured"})
		return
	}
	header := c.GetHeader("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
		return
	}
	provided := []byte(strings.TrimPrefix(header, "Bearer "))
	if subtle.ConstantTimeCompare(provided, internalToken) != 1 && !previousTokenMatches(provided, "INTERNAL_API") {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid bearer token"})
		return
	}
	c.Next()
}

// NodeBrokerAuthMiddleware protects the internal routes only fleet nodes
// call: key material, blob signing, the cloud-link token and the node env.
//
// A node is admitted by its own NODE_CREDENTIAL and is then named on the
// request. Unless NODE_REQUIRE_NODE_CREDENTIAL is on, NODE_BROKER_TOKEN (else
// INTERNAL_API_TOKEN) is also accepted, kept apart from the edge services' token.
func (h *Handler) NodeBrokerAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.authenticateNode(c) {
			return
		}
		nodeBrokerAuth(c)
	}
}

var (
	brokerTokenOnce sync.Once
	brokerToken     []byte
)

func loadBrokerToken() {
	if v := os.Getenv("NODE_BROKER_TOKEN"); v != "" {
		brokerToken = []byte(v)
		return
	}
	brokerToken = []byte(os.Getenv("INTERNAL_API_TOKEN"))
}

func nodeBrokerAuth(c *gin.Context) {
	brokerTokenOnce.Do(loadBrokerToken)
	checkNodeToken(c, brokerToken, nil)
}

func previousTokenMatches(provided []byte, prefix string) bool {
	until, err := time.Parse(time.RFC3339, os.Getenv(prefix+"_PREVIOUS_UNTIL"))
	if err != nil || !time.Now().Before(until) {
		return false
	}
	tokens, err := secretset.Parse(os.Getenv(prefix + "_PREVIOUS_TOKENS"))
	if err != nil {
		return false
	}
	matched := false
	for _, token := range tokens {
		matched = subtle.ConstantTimeCompare(provided, []byte(token)) == 1 || matched
	}
	return matched
}

// NodeAuthMiddleware guards node-only routes on the node's own credential, or NODE_BROKER_TOKEN while it is not required; NODE_ACCEPT_INTERNAL_TOKEN=true also admits INTERNAL_API_TOKEN while older nodes upgrade.
func (h *Handler) NodeAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.authenticateNode(c) {
			return
		}
		brokerTokenOnce.Do(loadBrokerToken)
		var legacy []byte
		if os.Getenv("NODE_ACCEPT_INTERNAL_TOKEN") == "true" {
			legacy = []byte(os.Getenv("INTERNAL_API_TOKEN"))
		}
		checkNodeToken(c, brokerToken, legacy)
	}
}

// nodeUpdateOnlyKey marks a heartbeat that authenticated with INTERNAL_API_TOKEN where NODE_BROKER_TOKEN is required.
const nodeUpdateOnlyKey = "node_update_only"

// NodeHeartbeatAuthMiddleware guards the heartbeat like NodeAuthMiddleware, but
// admits INTERNAL_API_TOKEN as update-only so a node on an older release can
// always learn the version that sends NODE_BROKER_TOKEN.
func (h *Handler) NodeHeartbeatAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.authenticateNode(c) {
			return
		}
		brokerTokenOnce.Do(loadBrokerToken)
		shared := []byte(os.Getenv("INTERNAL_API_TOKEN"))
		if os.Getenv("NODE_ACCEPT_INTERNAL_TOKEN") == "true" {
			checkNodeToken(c, brokerToken, shared)
			return
		}
		if len(shared) > 0 && subtle.ConstantTimeCompare(shared, brokerToken) != 1 && bearerIs(c, shared) {
			c.Set(nodeUpdateOnlyKey, true)
			c.Next()
			return
		}
		checkNodeToken(c, brokerToken, nil)
	}
}

// IsNodeUpdateOnly reports whether the heartbeat may only be told its version, not recorded.
func IsNodeUpdateOnly(c *gin.Context) bool {
	return c.GetBool(nodeUpdateOnlyKey)
}

func bearerIs(c *gin.Context, token []byte) bool {
	header := c.GetHeader("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(header, "Bearer ")), token) == 1
}

func checkNodeToken(c *gin.Context, token, legacy []byte) {
	if len(token) == 0 {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "internal auth not configured"})
		return
	}
	header := c.GetHeader("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
		return
	}
	provided := []byte(strings.TrimPrefix(header, "Bearer "))
	ok := subtle.ConstantTimeCompare(provided, token) == 1
	if !ok && len(legacy) > 0 {
		ok = subtle.ConstantTimeCompare(provided, legacy) == 1
	}
	if !ok {
		ok = previousTokenMatches(provided, "NODE_BROKER")
	}
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid bearer token"})
		return
	}
	c.Next()
}
