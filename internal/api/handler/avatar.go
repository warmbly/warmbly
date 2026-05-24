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
//   - accepted MIME: image/png, image/jpeg, image/webp, image/gif.
//   - object key: avatars/{kind}/{id}-{epoch}.{ext}
//
// The epoch suffix forces cache busting on replacement so the
// browser doesn't keep showing the old avatar at the same URL.

package handler

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	avatarMaxBytes        int64 = 2 * 1024 * 1024
	avatarMaxDimension          = 1024 // px — reject anything bigger so a phone-camera dump doesn't sneak through
	avatarPublicURLFormat       = "https://%s.s3.amazonaws.com/%s"
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
var allowedAvatarMIME = map[string]string{
	"image/png":  ".png",
	"image/jpg":  ".jpg",
	"image/jpeg": ".jpg",
}

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

	key := fmt.Sprintf("avatars/users/%s-%d%s", userID.String(), time.Now().Unix(), ext)
	url, xerr := putPublicObject(c.Request.Context(), h.Storage, key, bytesRead, mime)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	if err := h.UserRepo.UpdateAvatar(c.Request.Context(), userID, &url); err != nil {
		errx.Handle(c, errx.InternalError())
		return
	}

	h.AuditService.LogAction(c.Request.Context(), userID, models.AuditActionUpdate, models.AuditEntityUser, &userID, c.ClientIP(), c.Request.UserAgent(), nil, map[string]string{"field": "avatar_url"})

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

	if err := h.UserRepo.UpdateAvatar(c.Request.Context(), userID, nil); err != nil {
		errx.Handle(c, errx.InternalError())
		return
	}

	h.AuditService.LogAction(c.Request.Context(), userID, models.AuditActionUpdate, models.AuditEntityUser, &userID, c.ClientIP(), c.Request.UserAgent(), nil, map[string]string{"field": "avatar_url", "value": "cleared"})

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

	key := fmt.Sprintf("avatars/organizations/%s-%d%s", orgID.String(), time.Now().Unix(), ext)
	url, xerr := putPublicObject(c.Request.Context(), h.Storage, key, bytesRead, mime)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	if err := h.OrgRepo.UpdateAvatar(c.Request.Context(), *orgID, &url); err != nil {
		errx.Handle(c, errx.InternalError())
		return
	}

	h.AuditService.LogAction(c.Request.Context(), userID, models.AuditActionUpdate, models.AuditEntityUser, orgID, c.ClientIP(), c.Request.UserAgent(), nil, map[string]string{"field": "org_avatar_url"})

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

	if err := h.OrgRepo.UpdateAvatar(c.Request.Context(), *orgID, nil); err != nil {
		errx.Handle(c, errx.InternalError())
		return
	}

	c.Status(http.StatusNoContent)
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
// the size cap, sniffs the mime, and returns the bytes ready for S3.
func readAvatarUpload(c *gin.Context) ([]byte, string, string, *errx.Error) {
	// Cap the request body before parsing so a 50MB upload doesn't
	// pin a gin worker.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, avatarMaxBytes+1024)

	fh, err := c.FormFile("file")
	if err != nil {
		return nil, "", "", errx.New(errx.BadRequest, "file is required")
	}
	if fh.Size > avatarMaxBytes {
		return nil, "", "", errx.New(errx.BadRequest, "avatar must be smaller than 2 MB")
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

	// Trust the client-declared content type only if it's on the
	// allowlist. http.DetectContentType is a stronger signal, so
	// prefer that if it disagrees.
	declared := strings.ToLower(fh.Header.Get("Content-Type"))
	sniffed := http.DetectContentType(body)
	mime := declared
	if _, ok := allowedAvatarMIME[sniffed]; ok {
		mime = sniffed
	}
	ext, ok := allowedAvatarMIME[mime]
	if !ok {
		return nil, "", "", errx.New(errx.BadRequest, "avatar must be a PNG or JPG")
	}
	// Fallback ext from the filename when mime sniff is ambiguous.
	if ext == "" {
		ext = strings.ToLower(path.Ext(fh.Filename))
		if ext == "" {
			ext = ".png"
		}
	}

	// Dimension cap. We expect the client to resize before upload —
	// this is a backstop against a raw camera dump or a bypass of the
	// JS resizer.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return nil, "", "", errx.New(errx.BadRequest, "image could not be parsed")
	}
	if cfg.Width > avatarMaxDimension || cfg.Height > avatarMaxDimension {
		return nil, "", "", errx.New(
			errx.BadRequest,
			fmt.Sprintf("avatar dimensions must be %dpx or smaller — please resize before uploading", avatarMaxDimension),
		)
	}
	return body, mime, ext, nil
}

func putPublicObject(ctx context.Context, store *storage.Client, key string, body []byte, mime string) (string, *errx.Error) {
	if store == nil {
		return "", errx.New(errx.ServiceUnavailable, "object storage not configured")
	}
	_, err := store.PutObject(ctx, &s3.PutObjectInput{
		Bucket:       aws.String(store.Bucket),
		Key:          aws.String(key),
		Body:         bytes.NewReader(body),
		ContentType:  aws.String(mime),
		CacheControl: aws.String("public, max-age=31536000, immutable"),
		ACL:          s3types.ObjectCannedACLPublicRead,
	})
	if err != nil {
		return "", errx.InternalError()
	}
	return fmt.Sprintf(avatarPublicURLFormat, store.Bucket, key), nil
}

// Imports below are referenced so the file compiles cleanly even if
// future refactors remove specific dependencies above.
var _ repository.UserRepository = (repository.UserRepository)(nil)
