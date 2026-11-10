// Avatar handlers — upload, replace and clear avatars for users and
// organizations.
//
// Strategy: server receives a multipart upload, validates size + mime,
// stores the image in S3 under a deterministic key, marks the object
// public-readable and saves the public URL on the user/org row.
//
// Constants:
//
//   - max size: 2 MiB. Anything larger gets a 400.
//   - accepted formats: PNG, JPEG, decided from the bytes (see avatarFormats).
//   - object key: avatars/{kind}/{id}-{epoch_ms}-{nonce}.{ext}
//
// The epoch suffix forces cache busting on replacement so the
// browser doesn't keep showing the old avatar at the same URL (the
// objects are served immutable, so a reused key would never refresh).
// The previous object is deleted best-effort once the row points at
// the new one, so replacing or removing an avatar doesn't leak blobs.

package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/imagetype"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	avatarMaxBytes     int64 = 2 * 1024 * 1024
	avatarMaxDimension       = 1024 // px — reject anything bigger so a phone-camera dump doesn't sneak through

	userAvatarKeyPrefix = "avatars/users/"
	orgAvatarKeyPrefix  = "avatars/organizations/"
)

// Intentionally narrow allowlist: only PNG and JPEG. WebP, GIF and
// SVG are excluded because:
//
//   - SVG can carry script payloads served from the same origin.
//   - GIF historically has decoder CVEs and we don't need motion in
//     an avatar.
//   - WebP has had several Chrome decoder CVEs (2023's heap overflow
//     being the loudest) and it's not worth the surface area when
//     PNG/JPEG cover the same use cases.
//
// The client-side resizer always re-encodes to JPEG anyway, so this
// list constrains what survives a bypass of the JS path.
var avatarFormats = []imagetype.Format{imagetype.PNG, imagetype.JPEG}

// UploadUserAvatar — POST /me/avatar (multipart, field "file")
func (h *Handler) UploadUserAvatar(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		errx.Handle(c, errx.ErrAuth)
		return
	}

	bytesRead, mime, ext, xerr := readAvatarUpload(c)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	ctx := c.Request.Context()
	previous := h.currentUserAvatarURL(ctx, userID)

	key := avatarObjectKey(userAvatarKeyPrefix, userID, ext)
	url, xerr := putPublicObject(ctx, h.Storage, key, bytesRead, mime)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	// Through the service, not the repo: /auth/me is served from a cached
	// copy, and only the service drops it.
	if xerr := h.UserService.UpdateAvatar(ctx, userID, &url); xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.deleteAvatarObject(ctx, previous, userAvatarKeyPrefix, key)

	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityUser, &userID, nil, map[string]string{"field": "avatar_url"})

	c.JSON(http.StatusOK, gin.H{"avatar_url": url})
}

// DeleteUserAvatar — DELETE /me/avatar
func (h *Handler) DeleteUserAvatar(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		errx.Handle(c, errx.ErrAuth)
		return
	}

	ctx := c.Request.Context()
	previous := h.currentUserAvatarURL(ctx, userID)

	if xerr := h.UserService.UpdateAvatar(ctx, userID, nil); xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.deleteAvatarObject(ctx, previous, userAvatarKeyPrefix, "")

	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityUser, &userID, nil, map[string]string{"field": "avatar_url", "value": "cleared"})

	c.Status(http.StatusNoContent)
}

// UploadOrganizationAvatar — POST /organization/avatar (owner only)
func (h *Handler) UploadOrganizationAvatar(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		errx.Handle(c, errx.ErrAuth)
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	// Only the owner can change the workspace's avatar — covers the
	// same trust boundary as billing.
	if xerr := h.requireOrgOwner(c, *orgID, userID); xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	bytesRead, mime, ext, xerr := readAvatarUpload(c)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	ctx := c.Request.Context()
	previous := h.currentOrgAvatarURL(ctx, *orgID)

	key := avatarObjectKey(orgAvatarKeyPrefix, *orgID, ext)
	url, xerr := putPublicObject(ctx, h.Storage, key, bytesRead, mime)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	if err := h.OrgRepo.UpdateAvatar(ctx, *orgID, &url); err != nil {
		errx.Handle(c, errx.InternalError())
		return
	}
	h.deleteAvatarObject(ctx, previous, orgAvatarKeyPrefix, key)

	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityOrganization, orgID, nil, map[string]string{"field": "avatar_url"})

	c.JSON(http.StatusOK, gin.H{"avatar_url": url})
}

// DeleteOrganizationAvatar — DELETE /organization/avatar (owner only)
func (h *Handler) DeleteOrganizationAvatar(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		errx.Handle(c, errx.ErrAuth)
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	if xerr := h.requireOrgOwner(c, *orgID, userID); xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	ctx := c.Request.Context()
	previous := h.currentOrgAvatarURL(ctx, *orgID)

	if err := h.OrgRepo.UpdateAvatar(ctx, *orgID, nil); err != nil {
		errx.Handle(c, errx.InternalError())
		return
	}
	h.deleteAvatarObject(ctx, previous, orgAvatarKeyPrefix, "")

	// Audited like the upload so teammates' org switcher refreshes live.
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityOrganization, orgID, nil, map[string]string{"field": "avatar_url", "value": "cleared"})

	c.Status(http.StatusNoContent)
}

// avatarObjectKey builds a key that is unique per upload: the epoch keeps keys
// sortable, the random nonce keeps two uploads in the same millisecond from
// sharing an immutably cached URL.
func avatarObjectKey(prefix string, id uuid.UUID, ext string) string {
	nonce := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	return fmt.Sprintf("%s%s-%d-%s%s", prefix, id.String(), time.Now().UnixMilli(), nonce, ext)
}

// currentUserAvatarURL returns the avatar URL stored on the user row, or ""
// when there is none or the lookup fails (cleanup is best-effort).
func (h *Handler) currentUserAvatarURL(ctx context.Context, userID uuid.UUID) string {
	u, xerr := h.UserService.GetUser(ctx, userID)
	if xerr != nil || u == nil || u.AvatarURL == nil {
		return ""
	}
	return *u.AvatarURL
}

// currentOrgAvatarURL is the organization counterpart of currentUserAvatarURL.
func (h *Handler) currentOrgAvatarURL(ctx context.Context, orgID uuid.UUID) string {
	org, xerr := h.OrganizationService.Get(ctx, orgID)
	if xerr != nil || org == nil || org.AvatarURL == nil {
		return ""
	}
	return *org.AvatarURL
}

// deleteAvatarObject removes the object behind a previous avatar URL once the
// row no longer points at it. Only keys under our own prefix are touched, so
// an external URL (an OAuth profile picture, say) is left alone, and keepKey
// guards the freshly written object. Failures are ignored: an orphaned blob
// is harmless, a failed request after a successful update is not.
func (h *Handler) deleteAvatarObject(ctx context.Context, previousURL, prefix, keepKey string) {
	if h.Storage == nil || previousURL == "" {
		return
	}
	key := avatarKeyFromURL(previousURL, prefix)
	if key == "" || key == keepKey {
		return
	}
	_ = h.Storage.Delete(ctx, key)
}

// avatarKeyFromURL recovers the object key from a public avatar URL. Both
// storage backends build the URL differently, but the key always starts with
// the known prefix, so that is what is looked for.
func avatarKeyFromURL(url, prefix string) string {
	idx := strings.Index(url, prefix)
	if idx < 0 {
		return ""
	}
	key := url[idx:]
	if q := strings.IndexAny(key, "?#"); q >= 0 {
		key = key[:q]
	}
	if key == prefix || strings.Contains(key, "..") {
		return ""
	}
	return key
}

func (h *Handler) requireOrgOwner(c *gin.Context, orgID, userID uuid.UUID) *errx.Error {
	m, err := h.OrgRepo.GetMember(c.Request.Context(), orgID, userID)
	if err != nil || m == nil {
		return errx.ErrForbidden
	}
	if !strings.EqualFold(m.Role, string(models.RoleOwner)) {
		return errx.ErrForbidden
	}
	return nil
}

// readAvatarUpload pulls the "file" field out of the request, enforces
// the size cap, decides the format from the bytes, and returns them ready for S3.
func readAvatarUpload(c *gin.Context) ([]byte, string, string, *errx.Error) {
	return readImageUpload(c, avatarLimits)
}

// readImageUpload is readAvatarUpload with the refusals worded for lim.
func readImageUpload(c *gin.Context, lim imageLimits) ([]byte, string, string, *errx.Error) {
	// Cap the request body before parsing so a 50MB upload doesn't
	// pin a gin worker.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, lim.maxBytes+1024)

	fh, err := c.FormFile("file")
	if err != nil {
		return nil, "", "", errx.New(errx.BadRequest, "file is required")
	}
	if fh.Size > lim.maxBytes {
		return nil, "", "", lim.tooLarge()
	}

	src, err := fh.Open()
	if err != nil {
		return nil, "", "", errx.InternalError()
	}
	defer src.Close()

	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, src); err != nil {
		return nil, "", "", errx.InternalError()
	}
	body := buf.Bytes()

	// The declared type and the filename take no part: the sniff and the
	// format's own decoder have to agree.
	format, cfg, err := imagetype.Detect(body, lim.accepts()...)
	if errors.Is(err, imagetype.ErrUnreadable) {
		return nil, "", "", lim.unreadable()
	}
	if err != nil {
		return nil, "", "", lim.wrongFormat()
	}
	mime, ext := format.MIME, format.Ext

	// Dimension cap. We expect the client to resize before upload —
	// this is a backstop against a raw camera dump or a bypass of the
	// JS resizer.
	if cfg.Width > lim.maxDim || cfg.Height > lim.maxDim {
		return nil, "", "", lim.tooWide()
	}
	return body, mime, ext, nil
}

func putPublicObject(ctx context.Context, store storage.Store, key string, body []byte, mime string) (string, *errx.Error) {
	if store == nil {
		return "", errx.New(errx.ServiceUnavailable, "object storage not configured")
	}
	// Public-read URL with long-lived cache. The backend chooses the right
	// semantics per store (S3 sets an ACL + s3 URL; filesystem writes and
	// serves via /public), so callers stay backend-agnostic.
	url, err := store.PutPublic(ctx, key, bytes.NewReader(body), mime)
	if err != nil {
		// Without this the object store's own refusal is dropped on the floor
		// and the upload is a 500 with nothing behind it anywhere: no log
		// line, no issue, and a person told only that it failed.
		errs.CaptureException(err)
		return "", errx.InternalError()
	}
	return url, nil
}

// Imports below are referenced so the file compiles cleanly even if
// future refactors remove specific dependencies above.
var _ repository.UserRepository = (repository.UserRepository)(nil)
