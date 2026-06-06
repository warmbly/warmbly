// Campaign email attachment handlers — upload, list, delete. Binary lives in
// object storage (private; surfaced to the browser via short-lived presigned
// URLs and fetched by the worker at send time). Overall storage is capped per
// organization by the plan-based quota (feature gate).

package handler

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

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

// UploadCampaignAttachment — POST /campaigns/:id/attachments (multipart "file")
func (h *Handler) UploadCampaignAttachment(c *gin.Context) {
	campaignID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
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

	// Optional sequence_id form field scopes the attachment to one step.
	var seqID *uuid.UUID
	if s := strings.TrimSpace(c.PostForm("sequence_id")); s != "" {
		if id, perr := uuid.Parse(s); perr == nil {
			seqID = &id
		}
	}

	// Cap the body before parsing so a huge upload can't pin a worker.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, attachmentMaxBytes+(1<<20))
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

	// Plan-based overall storage quota (org-wide).
	limit, xerr := h.FeatureGateService.GetStorageLimitBytes(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	used, err := h.AttachmentRepo.SumStorageUsedByOrg(c.Request.Context(), *orgID)
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	if used+fh.Size > limit {
		errx.JSON(c, errx.New(errx.BadRequest, fmt.Sprintf(
			"storage limit reached (%d MB of %d MB used) — remove attachments or upgrade your plan", mb(used), mb(limit))))
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

	key := fmt.Sprintf("attachments/%s/%s-%s", campaignID.String(), uuid.NewString(), filename)
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
	if err := h.AttachmentRepo.Create(c.Request.Context(), att); err != nil {
		_ = h.Storage.Delete(c.Request.Context(), key) // best-effort cleanup
		errx.JSON(c, errx.InternalError())
		return
	}

	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityCampaign, &att.ID, nil, map[string]string{
		"scope": "attachment", "campaign_id": campaignID.String(), "filename": filename,
	})

	c.JSON(http.StatusCreated, h.attachmentResponse(c, att))
}

// ListCampaignAttachments — GET /campaigns/:id/attachments
func (h *Handler) ListCampaignAttachments(c *gin.Context) {
	campaignID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
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
	campaignID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
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
		"sequence_id": att.SequenceID,
		"filename":    att.Filename,
		"size":        att.Size,
		"mime_type":   att.MimeType,
		"url":         url,
		"created_at":  att.CreatedAt,
	}
}
