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
	"github.com/warmbly/warmbly/internal/pkg/imagetype"
)

// Brand settings: a workspace's own brand and its agency's, shown on the
// workspace's public pages and to its client members in the dashboard. The
// routes are session-only; PUT replaces the whole brand, so a retry is safe.

var (
	brandLogoLimits = imageLimits{label: "logo", formats: "PNG or JPG", maxBytes: avatarMaxBytes, maxDim: avatarMaxDimension}
	// A tab icon is drawn at 16 to 180px and has to keep its transparency, so it is a small PNG.
	brandIconLimits = imageLimits{label: "icon", formats: "PNG", maxBytes: 1 * 1024 * 1024, maxDim: 512, allowed: []imagetype.Format{imagetype.PNG}}
	// The sign-in image fills a panel or the whole page, so it gets the caps of a form cover.
	brandSignInImageLimits = imageLimits{label: "sign-in image", formats: "PNG or JPG", maxBytes: formCoverMaxBytes, maxDim: formCoverMaxDim}
)

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

// brandAssetKey is unique per upload, so an immutably cached URL never shows a replaced image.
func brandAssetKey(ownerID uuid.UUID, name, ext string) string {
	nonce := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	return fmt.Sprintf("%s%s/%s-%d-%s%s", models.BrandAssetKeyPrefix, ownerID, name, time.Now().UnixMilli(), nonce, ext)
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
	key := brandAssetKey(orgID, "logo", ext)
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

// agencyBrandImage is one uploaded image on an agency's brand: its column, its caps and how its object and audit entry are named.
type agencyBrandImage struct {
	image  models.AgencyBrandImage
	limits imageLimits
	key    string
	audit  string
}

var (
	agencyBrandLogo = agencyBrandImage{image: models.AgencyBrandLogo, limits: brandLogoLimits, key: "logo", audit: "brand_logo"}
	agencyBrandIcon = agencyBrandImage{image: models.AgencyBrandIcon, limits: brandIconLimits, key: "icon", audit: "brand_icon"}
	agencyBrandSign = agencyBrandImage{image: models.AgencyBrandSignInImage, limits: brandSignInImageLimits, key: "sign-in", audit: "brand_sign_in_image"}
)

// uploadAgencyBrandImage stores the multipart "file" as img and answers the agency's brand; owner or admin.
func (h *Handler) uploadAgencyBrandImage(c *gin.Context, img agencyBrandImage) {
	agencyID, userID, ok := agencyBrandScope(c)
	if !ok || !h.brandAvailable(c) {
		return
	}
	ctx := c.Request.Context()
	if xerr := h.BrandService.CanManageAgency(ctx, agencyID, userID); xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	body, mime, ext, xerr := readImageUpload(c, img.limits)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	key := brandAssetKey(agencyID, img.key, ext)
	url, xerr := putPublicObject(ctx, h.Storage, key, body, mime)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	previous, xerr := h.BrandService.SetAgencyImage(ctx, agencyID, userID, img.image, url)
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
	h.auditAgency(c, agencyID, models.AuditActionUpdate, map[string]string{"field": img.audit})
	c.JSON(http.StatusOK, out)
}

// removeAgencyBrandImage clears img, deletes its object and answers the agency's brand; owner or admin.
func (h *Handler) removeAgencyBrandImage(c *gin.Context, img agencyBrandImage) {
	agencyID, userID, ok := agencyBrandScope(c)
	if !ok || !h.brandAvailable(c) {
		return
	}
	ctx := c.Request.Context()
	previous, xerr := h.BrandService.SetAgencyImage(ctx, agencyID, userID, img.image, "")
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
	h.auditAgency(c, agencyID, models.AuditActionUpdate, map[string]string{"field": img.audit, "removed": "true"})
	c.JSON(http.StatusOK, out)
}

// UploadAgencyBrandLogo — POST /agency/:agencyId/brand/logo (multipart, field "file").
func (h *Handler) UploadAgencyBrandLogo(c *gin.Context) { h.uploadAgencyBrandImage(c, agencyBrandLogo) }

// DeleteAgencyBrandLogo — DELETE /agency/:agencyId/brand/logo.
func (h *Handler) DeleteAgencyBrandLogo(c *gin.Context) { h.removeAgencyBrandImage(c, agencyBrandLogo) }

// UploadAgencyBrandIcon — POST /agency/:agencyId/brand/icon (multipart, field "file"): the portal's browser tab icon.
func (h *Handler) UploadAgencyBrandIcon(c *gin.Context) { h.uploadAgencyBrandImage(c, agencyBrandIcon) }

// DeleteAgencyBrandIcon — DELETE /agency/:agencyId/brand/icon.
func (h *Handler) DeleteAgencyBrandIcon(c *gin.Context) { h.removeAgencyBrandImage(c, agencyBrandIcon) }

// UploadAgencyBrandSignInImage — POST /agency/:agencyId/brand/sign-in-image (multipart, field "file").
func (h *Handler) UploadAgencyBrandSignInImage(c *gin.Context) {
	h.uploadAgencyBrandImage(c, agencyBrandSign)
}

// DeleteAgencyBrandSignInImage — DELETE /agency/:agencyId/brand/sign-in-image.
func (h *Handler) DeleteAgencyBrandSignInImage(c *gin.Context) {
	h.removeAgencyBrandImage(c, agencyBrandSign)
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
