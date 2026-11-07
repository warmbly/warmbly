package middleware

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/fleetnode"
	"github.com/warmbly/warmbly/internal/pkg/nodeauth"
)

// NodeCredentialVerifier checks that secret is the credential join issued to an enrolled node.
type NodeCredentialVerifier interface {
	VerifyNodeCredential(ctx context.Context, id uuid.UUID, secret string) error
}

const authenticatedNodeKey = "warmbly_authenticated_node"

// NodeCredentialRequired reports whether node-only routes admit only a node's own credential.
func NodeCredentialRequired() bool {
	v, err := strconv.ParseBool(strings.TrimSpace(os.Getenv("NODE_REQUIRE_NODE_CREDENTIAL")))
	return err == nil && v
}

// AuthenticatedNodeID is the node whose own credential admitted this request, if any.
func AuthenticatedNodeID(c *gin.Context) (uuid.UUID, bool) {
	id, ok := c.Get(authenticatedNodeKey)
	if !ok {
		return uuid.Nil, false
	}
	nodeID, ok := id.(uuid.UUID)
	return nodeID, ok
}

// mismatchedNodes remembers up to maxMismatchedNodes presented ids already warned about, so the log says it once per node.
var (
	mismatchedNodes     sync.Map
	mismatchedNodeCount atomic.Int64
)

const maxMismatchedNodes = 1024

// authenticateNode admits a caller presenting its own node credential. It returns true once the
// request is answered either way; false leaves it to the fleet token, which only happens while not required.
func (h *Handler) authenticateNode(c *gin.Context) bool {
	required := NodeCredentialRequired()
	rawID := strings.TrimSpace(c.GetHeader(nodeauth.IDHeader))
	if rawID == "" {
		// The host updater names its node in the query and has always done so.
		rawID = strings.TrimSpace(c.Query("node_id"))
	}
	secret := strings.TrimSpace(c.GetHeader(nodeauth.CredentialHeader))
	if rawID == "" || secret == "" {
		if required {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "node credential required; join this machine again to receive one"})
			return true
		}
		return false
	}

	reason := "node credential does not match the node"
	id, err := uuid.Parse(rawID)
	switch {
	case err != nil:
		reason = "node id is not a uuid"
	case h.NodeCredentials == nil:
		reason = "node credentials are not available on this instance"
	default:
		verr := h.NodeCredentials.VerifyNodeCredential(c.Request.Context(), id, secret)
		switch {
		case verr == nil:
			c.Set(authenticatedNodeKey, id)
			c.Next()
			return true
		case errors.Is(verr, fleetnode.ErrNoNodeCredential), errors.Is(verr, fleetnode.ErrBadNodeCredential):
		default:
			if required {
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "node credential could not be checked"})
				return true
			}
			reason = "node credential could not be checked"
		}
	}
	if required {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "node credential does not match this node; join this machine again"})
		return true
	}
	if _, seen := mismatchedNodes.Load(rawID); !seen && mismatchedNodeCount.Add(1) <= maxMismatchedNodes {
		mismatchedNodes.Store(rawID, struct{}{})
		log.Warn().Str("node_id", rawID).Str("reason", reason).Msg("fleet: node credential refused; the request falls back to the fleet token, which stops working once NODE_REQUIRE_NODE_CREDENTIAL is on. Join this machine again")
	}
	return false
}
