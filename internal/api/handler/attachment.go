// Campaign email attachment handlers — upload, list, delete. Binary lives in
// object storage (private; surfaced to the browser via short-lived presigned
// URLs and fetched by the worker at send time). Overall storage is capped per
// organization by the plan-based quota (feature gate).

package handler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

const (
	// Per-file cap. Kept under provider message ceilings (Gmail/Outlook ~25 MB
	// total, and base64 inflates ~37%), so a single attachment always fits.
	attachmentMaxBytes int64 = 15 * 1024 * 1024
	attachmentURLTTL         = 15 * time.Minute
)

// Executable / script types that must never ride an outbound email. Everything
// else (images, PDF, office docs, CSV, archives, …) is allowed.
var blockedAttachmentExt = map[string]bool{
	".exe": true, ".bat": true, ".cmd": true, ".com": true, ".msi": true,
	".scr": true, ".js": true, ".jse": true, ".vbs": true, ".vbe": true,
	".ps1": true, ".sh": true, ".jar": true, ".app": true, ".dll": true,
	".cpl": true, ".hta": true, ".wsf": true, ".pif": true,
}

func sanitizeFilename(name string) string {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	if len(name) > 200 {
		name = name[len(name)-200:]
	}
	return name
}

func mb(b int64) int64 { return b / (1024 * 1024) }

// attachmentCampaign resolves the campaign these attachment routes address and
// proves it belongs to the caller's organization. The route id is a raw path
// parameter, so without this an attachment could be listed, uploaded or deleted
// on another workspace's campaign.
func (h *Handler) attachmentCampaign(c *gin.Context) (campaignID, orgID uuid.UUID, xerr *errx.Error) {
	campaignID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return uuid.Nil, uuid.Nil, errx.ErrUuid
	}
	org := middleware.GetOrganizationID(c)
	if org == nil {
		return uuid.Nil, uuid.Nil, errx.New(errx.BadRequest, "no organization selected")
	}
	if _, xerr := h.CampaignService.Get(c.Request.Context(), org.String(), campaignID.String()); xerr != nil {
		return uuid.Nil, uuid.Nil, xerr
	}
	return campaignID, *org, nil
}

// deleteObjectDetached removes an object whose row was never written, on a
// bounded context that is not cancelled with the request: a client that gives
// up mid-upload must not leave bytes in storage that no quota counts.
func (h *Handler) deleteObjectDetached(ctx context.Context, key string) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if err := h.Storage.Delete(cleanup, key); err != nil {
		sentry.CaptureException(fmt.Errorf("attachment %s: cleanup after refused reservation: %w", key, err))
	}
}

// UploadCampaignAttachment — POST /campaigns/:id/attachments (multipart "file")
func (h *Handler) UploadCampaignAttachment(c *gin.Context) {
	campaignID, orgID, xerr := h.attachmentCampaign(c)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	userID, err := uuid.Parse(middleware.GetUserID(c))
	if err != nil {
		errx.JSON(c, errx.ErrAuth)
		return
	}
	if h.Storage == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "object storage not configured"))
		return
	}

	// Cap the body before anything reads it, so a huge upload can't pin a
	// worker: the first form field read parses the whole multipart body.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, attachmentMaxBytes+(1<<20))

	// Optional step_id scopes the attachment to one step; without it the file
	// rides every step of the campaign. A malformed or foreign step is refused
	// rather than silently widened to the whole campaign.
	var seqID *uuid.UUID
	if raw := strings.TrimSpace(c.PostForm("step_id")); raw != "" {
		id, perr := uuid.Parse(raw)
		if perr != nil {
			errx.JSON(c, errx.New(errx.BadRequest, "step_id must be a uuid"))
			return
		}
		belongs, berr := h.AttachmentRepo.StepBelongsToCampaign(c.Request.Context(), campaignID, id)
		if berr != nil {
			errx.JSON(c, errx.InternalError())
			return
		}
		if !belongs {
			errx.JSON(c, errx.New(errx.NotFound, "step not found in this campaign"))
			return
		}
		seqID = &id
	}

	fh, err := c.FormFile("file")
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "file is required"))
		return
	}
	if fh.Size <= 0 || fh.Size > attachmentMaxBytes {
		errx.JSON(c, errx.New(errx.BadRequest, fmt.Sprintf("file must be between 1 byte and %d MB", mb(attachmentMaxBytes))))
		return
	}
	filename := sanitizeFilename(fh.Filename)
	if blockedAttachmentExt[strings.ToLower(path.Ext(filename))] {
		errx.JSON(c, errx.New(errx.BadRequest, "that file type can't be attached to email"))
		return
	}

	// Plan-based overall storage quota (org-wide). This read is only a fast
	// refusal before the bytes are copied to storage; the check that counts
	// is CreateWithinQuota below, which runs under the org's quota lock.
	limit, xerr := h.FeatureGateService.GetStorageLimitBytes(c.Request.Context(), orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	used, err := h.AttachmentRepo.SumStorageUsedByOrg(c.Request.Context(), orgID)
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	if used+fh.Size > limit {
		errx.JSON(c, errx.StorageLimitReached(used, limit, fh.Size))
		return
	}

	src, err := fh.Open()
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	defer src.Close()
	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, src); err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	body := buf.Bytes()
	mimeType := fh.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = http.DetectContentType(body)
	}

	key := models.AttachmentObjectKey(campaignID, filename)
	if err := h.Storage.Put(c.Request.Context(), key, bytes.NewReader(body), mimeType); err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}

	att := &models.CampaignAttachment{
		CampaignID: campaignID,
		SequenceID: seqID,
		UserID:     userID,
		Filename:   filename,
		Size:       fh.Size,
		MimeType:   mimeType,
		S3Key:      key,
	}
	// The row is the reservation: it is written only if the total still fits
	// once this upload is counted, so concurrent uploads cannot interleave
	// past the limit. The limit is re-read under the lock so a plan change
	// that lands between the pre-check and the insert is honored. A refused
	// file is removed from storage again, on a context that outlives the
	// request so a cancelled upload cannot strand the object.
	limitFn := func(ctx context.Context) (int64, error) {
		l, xerr := h.FeatureGateService.GetStorageLimitBytes(ctx, orgID)
		if xerr != nil {
			return 0, xerr
		}
		return l, nil
	}
	created, used, limit, err := h.AttachmentRepo.CreateWithinQuota(c.Request.Context(), att, orgID, limitFn)
	if err != nil {
		h.deleteObjectDetached(c.Request.Context(), key)
		errx.JSON(c, errx.InternalError())
		return
	}
	if !created {
		h.deleteObjectDetached(c.Request.Context(), key)
		errx.JSON(c, errx.StorageLimitReached(used, limit, fh.Size))
		return
	}

	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityCampaign, &att.ID, nil, map[string]string{
		"scope": "attachment", "campaign_id": campaignID.String(), "filename": filename,
	})

	c.JSON(http.StatusCreated, h.attachmentResponse(c, att))
}

// ListCampaignAttachments — GET /campaigns/:id/attachments
func (h *Handler) ListCampaignAttachments(c *gin.Context) {
	campaignID, _, xerr := h.attachmentCampaign(c)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	atts, err := h.AttachmentRepo.ListByCampaign(c.Request.Context(), campaignID)
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	out := make([]gin.H, 0, len(atts))
	for i := range atts {
		out = append(out, h.attachmentResponse(c, &atts[i]))
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// DeleteCampaignAttachment — DELETE /campaigns/:id/attachments/:attachmentId
func (h *Handler) DeleteCampaignAttachment(c *gin.Context) {
	campaignID, _, xerr := h.attachmentCampaign(c)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	attID, err := uuid.Parse(c.Param("attachmentId"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	att, err := h.AttachmentRepo.GetByID(c.Request.Context(), attID)
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	if att == nil || att.CampaignID != campaignID {
		errx.JSON(c, errx.ErrNotFound)
		return
	}
	if err := h.AttachmentRepo.Delete(c.Request.Context(), attID); err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	if h.Storage != nil {
		_ = h.Storage.Delete(c.Request.Context(), att.S3Key)
	}
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityCampaign, &attID, nil, map[string]string{
		"scope": "attachment", "campaign_id": campaignID.String(),
	})
	c.Status(http.StatusNoContent)
}

func (h *Handler) attachmentResponse(c *gin.Context, att *models.CampaignAttachment) gin.H {
	url := ""
	if h.Storage != nil {
		if u, err := h.Storage.PresignedGetURL(c.Request.Context(), att.S3Key, attachmentURLTTL); err == nil {
			url = u
		}
	}
	return gin.H{
		"id":          att.ID,
		"campaign_id": att.CampaignID,
		"step_id":     att.SequenceID,
		"filename":    att.Filename,
		"size":        att.Size,
		"mime_type":   att.MimeType,
		"url":         url,
		"created_at":  att.CreatedAt,
	}
}
