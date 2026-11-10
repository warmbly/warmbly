package handler

import (
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/imagetype"
)

// ServePublicObject streams a publicly-readable blob (avatar, org logo, form
// asset, email-body image) from the active storage backend. It exists for the
// filesystem backend, which has no authority to serve objects itself; the S3
// backend returns object-storage URLs from PutPublic and never routes through
// here. Only the fixed public key prefixes are served so this can't be used to
// read arbitrary stored objects.
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

	// The Content-Type is derived from the key's extension, and every writer
	// of a public key forces that extension from the format its bytes decoded
	// as. An extension outside the raster set therefore means the key did not
	// come from one, and the only safe thing to serve is a download.
	if f, ok := imagetype.ByExt(filepath.Ext(key)); ok {
		c.Header("Content-Type", f.MIME)
		c.Header("Content-Disposition", "inline")
	} else {
		c.Header("Content-Type", "application/octet-stream")
		c.Header("Content-Disposition", "attachment")
	}
	// These are user uploads served from our own origin, so the browser must
	// not be free to decide they are something executable.
	c.Header("X-Content-Type-Options", "nosniff")
	// Belt and braces behind the extension allowlist: even if something
	// script-capable reached a public key, this origin holds no session cookie
	// and the sandbox denies it an origin to act in.
	c.Header("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; sandbox; frame-ancestors 'none'")
	// These objects exist to be loaded from somewhere else: an email image is
	// fetched by the recipient's mail client, an avatar by a page on another
	// host. The API-wide same-site policy would block exactly that, so this
	// route opts out. Safe because the objects are public by definition and the
	// origin carries no cookie.
	c.Header("Cross-Origin-Resource-Policy", "cross-origin")
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
	return strings.HasPrefix(key, "avatars/") || strings.HasPrefix(key, "oauth-app-logos/") ||
		strings.HasPrefix(key, "form-assets/") || strings.HasPrefix(key, models.EmailImageKeyPrefix) ||
		strings.HasPrefix(key, models.BrandAssetKeyPrefix)
}
