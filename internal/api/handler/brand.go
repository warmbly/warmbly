package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/brandkit"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Brand settings: a workspace's own brand and its agency's, shown on the
// workspace's public pages and to its client members in the dashboard. The
// routes are session-only; PUT replaces the whole brand, so a retry is safe.

var brandLogoLimits = imageLimits{label: "logo", formats: "PNG or JPG", maxBytes: avatarMaxBytes, maxDim: avatarMaxDimension}

// brandScope resolves the caller and the workspace for a brand route.
func brandScope(c *gin.Context) (orgID, userID uuid.UUID, ok bool) {
	org := middleware.GetOrganizationID(c)
	if org == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return uuid.Nil, uuid.Nil, false
	}
	user, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrAuth)
		return uuid.Nil, uuid.Nil, false
	}
	return *org, user, true
}

func (h *Handler) brandAvailable(c *gin.Context) bool {
	if h.BrandService == nil {
		errx.Handle(c, errx.New(errx.ServiceUnavailable, "branding is not available on this instance"))
		return false
	}
	return true
}

// brandLogoKey is unique per upload, so an immutably cached URL never shows a replaced logo.
func brandLogoKey(ownerID uuid.UUID, ext string) string {
	nonce := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	return fmt.Sprintf("%s%s/logo-%d-%s%s", models.BrandAssetKeyPrefix, ownerID, time.Now().UnixMilli(), nonce, ext)
}

// GetWorkspaceBrand — GET /organization/brand. Any member reads it: the dashboard chrome draws from it.
func (h *Handler) GetWorkspaceBrand(c *gin.Context) {
	orgID, _, ok := brandScope(c)
	if !ok || !h.brandAvailable(c) {
		return
	}
	out, xerr := h.BrandService.Workspace(c.Request.Context(), orgID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}

// UpdateWorkspaceBrand — PUT /organization/brand.
func (h *Handler) UpdateWorkspaceBrand(c *gin.Context) {
	orgID, userID, ok := brandScope(c)
	if !ok || !h.brandAvailable(c) {
		return
	}
	var in models.BrandInput
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.Handle(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	out, xerr := h.BrandService.UpdateWorkspace(c.Request.Context(), orgID, userID, in)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityWorkspaceBrand, &orgID, nil, nil)
	c.JSON(http.StatusOK, out)
}

// ResetWorkspaceBrand — DELETE /organization/brand: back to the agency's or the deployment's brand.
func (h *Handler) ResetWorkspaceBrand(c *gin.Context) {
	orgID, _, ok := brandScope(c)
	if !ok || !h.brandAvailable(c) {
		return
	}
	ctx := c.Request.Context()
	previous, xerr := h.BrandService.ResetWorkspace(ctx, orgID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.deleteAvatarObject(ctx, previous, models.BrandAssetKeyPrefix, "")
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityWorkspaceBrand, &orgID, nil, nil)
	out, xerr := h.BrandService.Workspace(ctx, orgID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}

// UploadWorkspaceBrandLogo — POST /organization/brand/logo (multipart, field "file").
func (h *Handler) UploadWorkspaceBrandLogo(c *gin.Context) {
	orgID, userID, ok := brandScope(c)
	if !ok || !h.brandAvailable(c) {
		return
	}
	body, mime, ext, xerr := readImageUpload(c, brandLogoLimits)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	ctx := c.Request.Context()
	key := brandLogoKey(orgID, ext)
	url, xerr := putPublicObject(ctx, h.Storage, key, body, mime)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	previous, xerr := h.BrandService.SetWorkspaceLogo(ctx, orgID, userID, url)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.deleteAvatarObject(ctx, previous, models.BrandAssetKeyPrefix, key)
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityWorkspaceBrand, &orgID, nil, map[string]string{"field": "logo"})
	out, xerr := h.BrandService.Workspace(ctx, orgID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}

// DeleteWorkspaceBrandLogo — DELETE /organization/brand/logo.
func (h *Handler) DeleteWorkspaceBrandLogo(c *gin.Context) {
	orgID, userID, ok := brandScope(c)
	if !ok || !h.brandAvailable(c) {
		return
	}
	ctx := c.Request.Context()
	previous, xerr := h.BrandService.SetWorkspaceLogo(ctx, orgID, userID, "")
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.deleteAvatarObject(ctx, previous, models.BrandAssetKeyPrefix, "")
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityWorkspaceBrand, &orgID, nil, map[string]string{"field": "logo", "removed": "true"})
	out, xerr := h.BrandService.Workspace(ctx, orgID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}

// agencyBrandScope resolves the caller and the :agencyId agency; membership is the service's check.
func agencyBrandScope(c *gin.Context) (agencyID, userID uuid.UUID, ok bool) {
	user, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrAuth)
		return uuid.Nil, uuid.Nil, false
	}
	agencyID, err = uuid.Parse(c.Param("agencyId"))
	if err != nil {
		errx.Handle(c, errx.NewWithIdentifier(errx.NotFound, "agency_not_found", "Agency not found."))
		return uuid.Nil, uuid.Nil, false
	}
	return agencyID, user, true
}

// GetAgencyBrand — GET /agency/:agencyId/brand, for any of the agency's staff.
func (h *Handler) GetAgencyBrand(c *gin.Context) {
	agencyID, userID, ok := agencyBrandScope(c)
	if !ok || !h.brandAvailable(c) {
		return
	}
	out, xerr := h.BrandService.Agency(c.Request.Context(), agencyID, userID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}

// UpdateAgencyBrand — PUT /agency/:agencyId/brand, owner or admin.
func (h *Handler) UpdateAgencyBrand(c *gin.Context) {
	agencyID, userID, ok := agencyBrandScope(c)
	if !ok || !h.brandAvailable(c) {
		return
	}
	var in models.BrandInput
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.Handle(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	out, xerr := h.BrandService.UpdateAgency(c.Request.Context(), agencyID, userID, in)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditAgency(c, agencyID, models.AuditActionUpdate, map[string]string{"field": "brand"})
	c.JSON(http.StatusOK, out)
}

// UploadAgencyBrandLogo — POST /agency/:agencyId/brand/logo (multipart, field "file").
func (h *Handler) UploadAgencyBrandLogo(c *gin.Context) {
	agencyID, userID, ok := agencyBrandScope(c)
	if !ok || !h.brandAvailable(c) {
		return
	}
	ctx := c.Request.Context()
	if xerr := h.BrandService.CanManageAgency(ctx, agencyID, userID); xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	body, mime, ext, xerr := readImageUpload(c, brandLogoLimits)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	key := brandLogoKey(agencyID, ext)
	url, xerr := putPublicObject(ctx, h.Storage, key, body, mime)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	previous, xerr := h.BrandService.SetAgencyLogo(ctx, agencyID, userID, url)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.deleteAvatarObject(ctx, previous, models.BrandAssetKeyPrefix, key)
	out, xerr := h.BrandService.Agency(ctx, agencyID, userID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditAgency(c, agencyID, models.AuditActionUpdate, map[string]string{"field": "brand_logo"})
	c.JSON(http.StatusOK, out)
}

// DeleteAgencyBrandLogo — DELETE /agency/:agencyId/brand/logo.
func (h *Handler) DeleteAgencyBrandLogo(c *gin.Context) {
	agencyID, userID, ok := agencyBrandScope(c)
	if !ok || !h.brandAvailable(c) {
		return
	}
	ctx := c.Request.Context()
	previous, xerr := h.BrandService.SetAgencyLogo(ctx, agencyID, userID, "")
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.deleteAvatarObject(ctx, previous, models.BrandAssetKeyPrefix, "")
	out, xerr := h.BrandService.Agency(ctx, agencyID, userID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditAgency(c, agencyID, models.AuditActionUpdate, map[string]string{"field": "brand_logo", "removed": "true"})
	c.JSON(http.StatusOK, out)
}

// SetAgencyPortalDomain — PUT /agency/:agencyId/brand/domain, body {"domain": "portal.example.com"}; empty clears it.
func (h *Handler) SetAgencyPortalDomain(c *gin.Context) {
	agencyID, userID, ok := agencyBrandScope(c)
	if !ok || !h.brandAvailable(c) {
		return
	}
	var in struct {
		Domain string `json:"domain"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		errx.Handle(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	out, xerr := h.BrandService.SetPortalDomain(c.Request.Context(), agencyID, userID, in.Domain)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	h.auditAgency(c, agencyID, models.AuditActionUpdate, map[string]string{"field": "portal_domain"})
	c.JSON(http.StatusOK, out)
}

// VerifyAgencyPortalDomain — POST /agency/:agencyId/brand/domain/verify; a DNS re-check, safe to retry.
func (h *Handler) VerifyAgencyPortalDomain(c *gin.Context) {
	agencyID, userID, ok := agencyBrandScope(c)
	if !ok || !h.brandAvailable(c) {
		return
	}
	out, xerr := h.BrandService.VerifyPortalDomain(c.Request.Context(), agencyID, userID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, out)
}

// publicBrand is what a workspace's public pages show; a failed read shows the deployment's brand.
func (h *Handler) publicBrand(ctx context.Context, orgID uuid.UUID) brandkit.Resolution {
	if h.BrandService == nil {
		return brandkit.Resolution{Brand: brandkit.Default(), Source: brandkit.SourceDeployment}
	}
	res, _ := h.BrandService.Resolver().Resolve(ctx, orgID)
	return res
}
