package handler

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/imagetype"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

// formScope pulls the org and the :id path param once per handler.
func formScope(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return uuid.Nil, uuid.Nil, false
	}
	raw := c.Param("id")
	if raw == "" {
		return *orgID, uuid.Nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		errx.Handle(c, errx.New(errx.BadRequest, "invalid form id"))
		return uuid.Nil, uuid.Nil, false
	}
	return *orgID, id, true
}

// withFormShareURL stamps the hosted page URL onto the response, on the
// organization's verified custom forms domain when it has one.
func (h *Handler) withFormShareURL(c *gin.Context, orgID uuid.UUID, f *models.Form) *models.Form {
	f.ShareURL = config.FormURLOn(h.FormService.FormsHost(c.Request.Context(), orgID), f.PublicID)
	return f
}

func (h *Handler) ListForms(c *gin.Context) {
	orgID, _, ok := formScope(c)
	if !ok {
		return
	}
	out, xerr := h.FormService.List(c.Request.Context(), orgID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	host := h.FormService.FormsHost(c.Request.Context(), orgID)
	for i := range out {
		out[i].ShareURL = config.FormURLOn(host, out[i].PublicID)
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// GetFormsConfig tells the builder what this instance can do, so the UI
// never shows a captcha toggle that cannot work.
func (h *Handler) GetFormsConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"base_url":          config.FormsBaseURL(),
		"captcha_available": config.CaptchaProvider() != "none" && config.TurnstileSiteKey() != "",
		"triage_available":  config.TypeSafeAPIKey() != "",
	})
}

func (h *Handler) GetForm(c *gin.Context) {
	orgID, id, ok := formScope(c)
	if !ok {
		return
	}
	out, xerr := h.FormService.Get(c.Request.Context(), orgID, id)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, h.withFormShareURL(c, orgID, out))
}

func (h *Handler) CreateForm(c *gin.Context) {
	orgID, _, ok := formScope(c)
	if !ok {
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.Handle(c, errx.InvalidBody(err))
		return
	}
	var createdBy *uuid.UUID
	if uid, err := middleware.GetUserUUID(c); err == nil {
		createdBy = &uid
	}
	out, xerr := h.FormService.Create(c.Request.Context(), orgID, createdBy, in.Name)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityForm, &out.ID, nil, map[string]string{"name": out.Name})
	c.JSON(http.StatusCreated, h.withFormShareURL(c, orgID, out))
}

func (h *Handler) UpdateForm(c *gin.Context) {
	orgID, id, ok := formScope(c)
	if !ok {
		return
	}
	var in models.FormWrite
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.Handle(c, errx.InvalidBody(err))
		return
	}
	out, xerr := h.FormService.Update(c.Request.Context(), orgID, id, &in)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityForm, &out.ID, nil, map[string]string{"name": out.Name, "status": string(out.Status)})
	c.JSON(http.StatusOK, h.withFormShareURL(c, orgID, out))
}

func (h *Handler) DeleteForm(c *gin.Context) {
	orgID, id, ok := formScope(c)
	if !ok {
		return
	}
	if xerr := h.FormService.Delete(c.Request.Context(), orgID, id); xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityForm, &id, nil, nil)
	c.Status(http.StatusNoContent)
}

// GetFormStats serves the analytics tab: funnel totals, daily series,
// per-page drop-off, breakdowns and identified visitors.
func (h *Handler) GetFormStats(c *gin.Context) {
	orgID, id, ok := formScope(c)
	if !ok {
		return
	}
	days := 30
	switch c.Query("range") {
	case "", "30d":
	case "7d":
		days = 7
	case "90d":
		days = 90
	default:
		errx.Handle(c, errx.New(errx.BadRequest, "range must be 7d, 30d or 90d"))
		return
	}
	out, xerr := h.FormService.Stats(c.Request.Context(), orgID, id, days)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}

// GetCampaignForms reports how the forms linked from a campaign performed for
// that campaign's recipients. It hangs off the campaign group, so the id
// param is a campaign, not a form.
func (h *Handler) GetCampaignForms(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	campaignID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.New(errx.BadRequest, "invalid campaign id"))
		return
	}
	out, xerr := h.FormService.CampaignForms(c.Request.Context(), *orgID, campaignID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// GetFormsDomain, SetFormsDomain and VerifyFormsDomain manage the
// organization's custom forms domain. Reading is a stored-state lookup; the
// other two resolve DNS, which is why they are not on the read path.
func (h *Handler) GetFormsDomain(c *gin.Context) {
	orgID, _, ok := formScope(c)
	if !ok {
		return
	}
	out, xerr := h.FormService.FormsDomainStatus(c.Request.Context(), orgID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) SetFormsDomain(c *gin.Context) {
	orgID, _, ok := formScope(c)
	if !ok {
		return
	}
	var in struct {
		FormsDomain string `json:"forms_domain"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.Handle(c, errx.InvalidBody(err))
		return
	}
	out, xerr := h.FormService.SetFormsDomain(c.Request.Context(), orgID, in.FormsDomain)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntitySettings, nil, nil,
		map[string]string{"forms_domain": out.FormsDomain, "verified": strconv.FormatBool(out.FormsDomainVerified)})
	c.JSON(http.StatusOK, out)
}

func (h *Handler) VerifyFormsDomain(c *gin.Context) {
	orgID, _, ok := formScope(c)
	if !ok {
		return
	}
	out, xerr := h.FormService.VerifyFormsDomain(c.Request.Context(), orgID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}

// MintFormLink returns the personalized URL for one contact. GET on top of
// an upsert: retries always land on the same token, so it is naturally safe.
func (h *Handler) MintFormLink(c *gin.Context) {
	orgID, id, ok := formScope(c)
	if !ok {
		return
	}
	contactID, err := uuid.Parse(c.Param("contactID"))
	if err != nil {
		errx.Handle(c, errx.New(errx.BadRequest, "invalid contact id"))
		return
	}
	url, xerr := h.FormService.MintLink(c.Request.Context(), orgID, id, contactID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": url})
}

// Form image caps: logos stay small, covers may be a full panel.
const (
	formLogoMaxBytes  int64 = 1 * 1024 * 1024
	formLogoMaxDim          = 1024
	formCoverMaxBytes int64 = 4 * 1024 * 1024
	formCoverMaxDim         = 2560
)

// readFormImageUpload mirrors readAvatarUpload with per-kind caps and copy;
// the format allowlist (PNG/JPG only) is shared for the same CVE reasons.
func readFormImageUpload(c *gin.Context, kind string) (body []byte, mime, ext string, xerr *errx.Error) {
	maxBytes, maxDim := formLogoMaxBytes, formLogoMaxDim
	// Covers and page backgrounds are both full-bleed art, so they share caps.
	if kind == "cover" || kind == "background" {
		maxBytes, maxDim = formCoverMaxBytes, formCoverMaxDim
	}
	lim := imageLimits{label: kind, formats: "PNG or JPG", maxBytes: maxBytes, maxDim: maxDim}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes+1024)
	fh, err := c.FormFile("file")
	if err != nil {
		return nil, "", "", errx.New(errx.BadRequest, "file is required")
	}
	if fh.Size > maxBytes {
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
	body = buf.Bytes()
	format, cfg, err := imagetype.Detect(body, avatarFormats...)
	if errors.Is(err, imagetype.ErrUnreadable) {
		return nil, "", "", lim.unreadable()
	}
	if err != nil {
		return nil, "", "", lim.wrongFormat()
	}
	mime, ext = format.MIME, format.Ext
	if cfg.Width > maxDim || cfg.Height > maxDim {
		return nil, "", "", lim.tooWide()
	}
	return body, mime, ext, nil
}

func formAssetKind(c *gin.Context) (string, bool) {
	kind := c.Param("kind")
	if kind != "logo" && kind != "cover" && kind != "background" {
		errx.Handle(c, errx.New(errx.BadRequest, "kind must be logo, cover or background"))
		return "", false
	}
	return kind, true
}

// UploadFormAsset — POST /forms/:id/assets/:kind (multipart, field "file").
func (h *Handler) UploadFormAsset(c *gin.Context) {
	orgID, id, ok := formScope(c)
	if !ok {
		return
	}
	kind, ok := formAssetKind(c)
	if !ok {
		return
	}
	body, mime, ext, xerr := readFormImageUpload(c, kind)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	key := fmt.Sprintf("form-assets/%s/%s-%s-%d%s", orgID, id, kind, time.Now().Unix(), ext)
	url, xerr := putPublicObject(c.Request.Context(), h.Storage, key, body, mime)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	out, xerr := h.FormService.SetAsset(c.Request.Context(), orgID, id, kind, url)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityForm, &id, nil, map[string]string{"asset": kind})
	c.JSON(http.StatusOK, h.withFormShareURL(c, orgID, out))
}

// DeleteFormAsset — DELETE /forms/:id/assets/:kind.
func (h *Handler) DeleteFormAsset(c *gin.Context) {
	orgID, id, ok := formScope(c)
	if !ok {
		return
	}
	kind, ok := formAssetKind(c)
	if !ok {
		return
	}
	out, xerr := h.FormService.SetAsset(c.Request.Context(), orgID, id, kind, "")
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityForm, &id, nil, map[string]string{"asset": kind, "removed": "true"})
	c.JSON(http.StatusOK, h.withFormShareURL(c, orgID, out))
}

func (h *Handler) ListFormSubmissions(c *gin.Context) {
	orgID, id, ok := formScope(c)
	if !ok {
		return
	}
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			errx.Handle(c, errx.New(errx.BadRequest, "limit must be between 1 and 100"))
			return
		}
		limit = n
	}
	after, xerr := submissionsPosition(c.Query("cursor"), c.Query("before"))
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	out, hasMore, xerr := h.FormService.ListSubmissions(c.Request.Context(), orgID, id, limit, after)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	page := models.CPagination{HasMore: hasMore}
	if hasMore && len(out) > 0 {
		last := out[len(out)-1]
		page.NextCursor = paging.EncodeTime(last.CreatedAt, last.ID)
	}
	// has_more stays at the top level for clients built before pagination.
	c.JSON(http.StatusOK, gin.H{"data": out, "has_more": hasMore, "pagination": page})
}

// submissionsPosition reads the opaque cursor, or the RFC 3339 `before`
// timestamp earlier clients send.
func submissionsPosition(cursor, before string) (*models.FormSubmissionPosition, *errx.Error) {
	switch {
	case cursor != "" && before != "":
		return nil, errx.New(errx.BadRequest, "send cursor or before, not both")
	case cursor != "":
		at, id, xerr := paging.DecodeTimeCursor(cursor)
		if xerr != nil {
			return nil, xerr
		}
		return &models.FormSubmissionPosition{At: at, ID: id}, nil
	case before != "":
		t, err := time.Parse(time.RFC3339Nano, before)
		if err != nil {
			return nil, errx.New(errx.BadRequest, "before must be an RFC 3339 timestamp")
		}
		return &models.FormSubmissionPosition{At: t}, nil
	}
	return nil, nil
}

func (h *Handler) DeleteFormSubmission(c *gin.Context) {
	orgID, id, ok := formScope(c)
	if !ok {
		return
	}
	subID, err := uuid.Parse(c.Param("sid"))
	if err != nil {
		errx.Handle(c, errx.New(errx.BadRequest, "invalid submission id"))
		return
	}
	if xerr := h.FormService.DeleteSubmission(c.Request.Context(), orgID, id, subID); xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityForm, &id, nil, map[string]string{"submission_id": subID.String()})
	c.Status(http.StatusNoContent)
}
