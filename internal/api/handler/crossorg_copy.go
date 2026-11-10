package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/crossorgcopy"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// copyCaller resolves who is copying, refusing a key that lacks the write
// scopes the copy needs in its own workspace.
func (h *Handler) copyCaller(c *gin.Context, writeScopes ...uint64) (crossorgcopy.Caller, *errx.Error) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		return crossorgcopy.Caller{}, errx.ErrNoOrganization
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		return crossorgcopy.Caller{}, errx.ErrUser
	}
	authType := c.GetString(middleware.AuthTypeKey)
	keyBound := authType == middleware.AuthTypeAPIKey || authType == middleware.AuthTypeOAuth
	if keyBound {
		perms := middleware.GetAPIKeyPermissions(c)
		for _, p := range writeScopes {
			if !models.HasAPIPermission(perms, p) {
				return crossorgcopy.Caller{}, errx.New(errx.Forbidden, "insufficient API key permissions")
			}
		}
	}
	return crossorgcopy.Caller{
		UserID:           userID,
		SourceOrgID:      *orgID,
		KeyBound:         keyBound,
		AllowedCampaigns: middleware.AllowedCampaigns(c),
	}, nil
}

// CopyCampaignToWorkspaces copies a campaign's configuration, and optionally
// some templates, into other workspaces the caller belongs to as drafts.
// POST /campaigns/:id/copy-to
func (h *Handler) CopyCampaignToWorkspaces(c *gin.Context) {
	if h.CrossOrgCopy == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "copying between workspaces is not available"))
		return
	}
	campaignID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	var req models.CopyCampaignToWorkspacesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	scopes := []uint64{models.APIPermWriteCampaigns}
	if len(req.TemplateIDs) > 0 {
		scopes = append(scopes, models.APIPermReadTemplates, models.APIPermWriteTemplates)
	}
	caller, xerr := h.copyCaller(c, scopes...)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	if xerr := middleware.RefuseOutsidePortal(c, req.OrganizationIDs...); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	resp, xerr := h.CrossOrgCopy.CopyCampaign(c.Request.Context(), caller, campaignID, req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditCopies(c, &campaignID, resp)
	c.JSON(http.StatusOK, resp)
}

// CopyTemplatesToWorkspaces copies reply templates into other workspaces the
// caller belongs to.
// POST /templates/copy-to
func (h *Handler) CopyTemplatesToWorkspaces(c *gin.Context) {
	if h.CrossOrgCopy == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "copying between workspaces is not available"))
		return
	}
	var req models.CopyTemplatesToWorkspacesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	caller, xerr := h.copyCaller(c, models.APIPermWriteTemplates)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	if xerr := middleware.RefuseOutsidePortal(c, req.OrganizationIDs...); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	resp, xerr := h.CrossOrgCopy.CopyTemplates(c.Request.Context(), caller, req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditCopies(c, nil, resp)
	c.JSON(http.StatusOK, resp)
}

// auditCopies records each copy in the workspace it landed in, which also
// refreshes that workspace's lists, and in the source workspace.
func (h *Handler) auditCopies(c *gin.Context, sourceCampaign *uuid.UUID, resp *models.WorkspaceCopyResponse) {
	for _, r := range resp.Data {
		dest := r.OrganizationID.String()
		if r.Campaign != nil {
			h.auditInOrg(c, r.OrganizationID, models.AuditActionDuplicate, models.AuditEntityCampaign, &r.Campaign.ID, nil,
				map[string]string{"name": r.Campaign.Name, "origin": "workspace_copy"})
			h.auditOrg(c, models.AuditActionDuplicate, models.AuditEntityCampaign, sourceCampaign, nil,
				map[string]string{"destination_organization_id": dest, "copy_id": r.Campaign.ID.String()})
		}
		for _, t := range r.Templates {
			h.auditInOrg(c, r.OrganizationID, models.AuditActionDuplicate, models.AuditEntityTemplate, &t.ID, nil,
				map[string]string{"name": t.Name, "origin": "workspace_copy"})
		}
		if len(r.Templates) > 0 {
			h.auditOrg(c, models.AuditActionDuplicate, models.AuditEntityTemplate, nil, nil,
				map[string]string{"destination_organization_id": dest, "templates": strconv.Itoa(len(r.Templates))})
		}
	}
}
