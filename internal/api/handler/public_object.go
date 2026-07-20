package handler

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/infrastructure/storage"
)

// ServePublicObject streams a publicly-readable blob (avatar, org logo) from the
// active storage backend. It exists for the filesystem backend, which has no
// authority to serve objects itself; the S3 backend returns object-storage URLs
// from PutPublic and never routes through here. Only the fixed public key
// prefixes are served so this can't be used to read arbitrary stored objects.
func (h *Handler) ServePublicObject(c *gin.Context) {
	key := strings.TrimPrefix(c.Param("key"), "/")
	if key == "" || !isPublicKey(key) {
		c.Status(http.StatusNotFound)
		return
	}
	if h.Storage == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}

	body, err := h.Storage.Get(c.Request.Context(), key)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			c.Status(http.StatusNotFound)
			return
		}
		c.Status(http.StatusInternalServerError)
		return
	}
	defer body.Close()

	if ct := mime.TypeByExtension(filepath.Ext(key)); ct != "" {
		c.Header("Content-Type", ct)
	}
	// Keys are content-addressed (they carry an epoch suffix), so they're safe
	// to cache immutably.
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, body)
}

// isPublicKey guards the /public route to the key prefixes PutPublic writes, so
// it can't be turned into a reader for arbitrary blob keys.
func isPublicKey(key string) bool {
	if strings.Contains(key, "..") {
		return false
	}
	return strings.HasPrefix(key, "avatars/") || strings.HasPrefix(key, "oauth-app-logos/")
}
