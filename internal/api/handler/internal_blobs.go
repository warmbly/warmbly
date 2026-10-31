package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
)

// Internal blob brokering, used by nodes that hold no credential for the
// object store. The control plane signs one operation on one key; the node
// then talks to the store directly, so message bodies and attachments never
// pass through the backend and it pays no bandwidth for a mailbox sync.
//
// Auth is middleware.NodeBrokerAuthMiddleware, the node token every other
// node-only internal route uses.
//
//	POST /api/v1/internal/blobs/presign
//	  body {"op":"get|put|head|delete","key":"...","content_type":"..."}
//	  200  {"url":"...","method":"GET","expires_in":300}
//	  501  the instance's blob backend cannot sign (filesystem)

// nodeKeyPrefixes are the object prefixes a node may have signed. A signed URL
// is the whole authorisation, so without this the broker turns one token into
// read, write and delete over every object in the bucket, including avatars,
// form assets and workspace export archives that no node has any business
// touching.
//
// The list is what a node actually reaches:
//
//	emails/       transport bodies the backend wrote for a send
//	attachments/  campaign attachments the send pipeline references
//	users/        mailbox bodies a sync stores
//
// It has to grow when a node starts touching a new prefix. A miss is a refused
// operation that names the key, not a mystery, which is the tradeoff being
// bought here.
var nodeKeyPrefixes = []string{"emails/", "attachments/", "users/"}

// keyAllowedForNode reports whether key sits under a prefix a node may reach.
// Traversal is rejected outright rather than cleaned: no legitimate key
// contains "..", so the only caller producing one is probing.
func keyAllowedForNode(key string) bool {
	if strings.Contains(key, "..") {
		return false
	}
	for _, p := range nodeKeyPrefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

// blobPresignTTL is how long a signed URL lives. Long enough for a large
// attachment over a slow link, short enough that one captured in a log is
// already dead.
const blobPresignTTL = 5 * time.Minute

type blobPresignRequest struct {
	Op          string `json:"op"`
	Key         string `json:"key"`
	ContentType string `json:"content_type"`
}

type blobPresignResponse struct {
	URL       string `json:"url"`
	Method    string `json:"method"`
	ExpiresIn int    `json:"expires_in"`
}

// InternalPresignBlob signs one blob operation for a node.
func (h *Handler) InternalPresignBlob(c *gin.Context) {
	if h.Storage == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "no blob store configured"})
		return
	}
	var req blobPresignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "decode body"})
		return
	}
	op := storage.PresignOp(req.Op)
	if !op.Valid() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "op must be one of get, put, head, delete"})
		return
	}
	if req.Key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "key required"})
		return
	}
	if !keyAllowedForNode(req.Key) {
		// The clearest probe signal the instance produces: a real node only
		// ever asks for prefixes it uses.
		logBrokerAccess(c, "blob.presign."+string(op), req.Key, false, "key outside the node prefixes")
		c.JSON(http.StatusForbidden, gin.H{
			"error": "key " + req.Key + " is outside the prefixes a node may reach",
		})
		return
	}
	if !h.nodeInScope(c, "blob.presign", http.StatusForbidden, func(ctx context.Context, node uuid.UUID) (bool, error) {
		return h.NodeScope.BlobKey(ctx, node, req.Key)
	}) {
		logBrokerAccess(c, "blob.presign."+string(op), req.Key, false, "key outside the node's assignment")
		return
	}
	logBrokerAccess(c, "blob.presign."+string(op), req.Key, true, "")

	url, err := h.Storage.PresignedURL(c.Request.Context(), op, req.Key, req.ContentType, blobPresignTTL)
	if err != nil {
		// The filesystem backend cannot sign anything, and a node on another
		// machine could not reach those bytes even if it could: they are on a
		// disk it does not have. Say which it is, because the fix is a
		// different BLOB_PROVIDER rather than a retry.
		if errors.Is(err, storage.ErrUnsupported) {
			c.JSON(http.StatusNotImplemented, gin.H{
				"error": "this instance's blob backend cannot sign URLs; a fleet needs BLOB_PROVIDER=s3",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not sign blob url"})
		return
	}

	c.JSON(http.StatusOK, blobPresignResponse{
		URL:       url,
		Method:    op.Method(),
		ExpiresIn: int(blobPresignTTL.Seconds()),
	})
}
