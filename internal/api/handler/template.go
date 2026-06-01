package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// ListTemplates lists all reply templates for the organization, optionally
// filtered by `?q=` against name and subject.
// GET /templates
func (h *Handler) ListTemplates(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var q models.ListReplyTemplatesQuery
	_ = c.ShouldBindQuery(&q)

	templates, xerr := h.TemplateService.List(c.Request.Context(), *orgID, q.Search)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, models.ReplyTemplatesResult{Data: templates})
}

// CreateTemplate creates a new reply template
// POST /templates
func (h *Handler) CreateTemplate(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	var data models.CreateReplyTemplate
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	tmpl, xerr := h.TemplateService.Create(c.Request.Context(), *orgID, userID, &data)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityTemplate, &tmpl.ID, nil, map[string]string{"name": tmpl.Name})

	c.JSON(http.StatusOK, tmpl)
}

// GetTemplate retrieves a reply template by ID
// GET /templates/:id
func (h *Handler) GetTemplate(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	templateID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	tmpl, xerr := h.TemplateService.GetByID(c.Request.Context(), *orgID, templateID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, tmpl)
}

// UpdateTemplate updates a reply template
// PATCH /templates/:id
func (h *Handler) UpdateTemplate(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	templateID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	var data models.UpdateReplyTemplate
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	tmpl, xerr := h.TemplateService.Update(c.Request.Context(), *orgID, templateID, &data)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityTemplate, &templateID, nil, map[string]string{"name": tmpl.Name})

	c.JSON(http.StatusOK, tmpl)
}

// DeleteTemplate deletes a reply template
// DELETE /templates/:id
func (h *Handler) DeleteTemplate(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	templateID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	if xerr := h.TemplateService.Delete(c.Request.Context(), *orgID, templateID); xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityTemplate, &templateID, nil, nil)

	c.Status(http.StatusNoContent)
}

// DuplicateTemplate clones a template, appending " (copy)" to the name
// and placing it at the end of the org's list.
// POST /templates/:id/duplicate
func (h *Handler) DuplicateTemplate(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	templateID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	tmpl, xerr := h.TemplateService.Duplicate(c.Request.Context(), *orgID, userID, templateID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionDuplicate, models.AuditEntityTemplate, &tmpl.ID, nil, map[string]string{"source": templateID.String(), "name": tmpl.Name})

	c.JSON(http.StatusOK, tmpl)
}

// ReorderTemplates reassigns positions across the org's templates so they
// line up with the order in the request body.
// PATCH /templates/reorder
func (h *Handler) ReorderTemplates(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var data models.ReorderReplyTemplates
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	if xerr := h.TemplateService.Reorder(c.Request.Context(), *orgID, data.IDs); xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	templates, xerr := h.TemplateService.List(c.Request.Context(), *orgID, "")
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, models.ReplyTemplatesResult{Data: templates})
}

// RenderTemplate expands {{.Key}} placeholders in subject + body fields
// using a caller-supplied variable map. Used by Unibox to preview a reply
// before scheduling the send.
// POST /templates/:id/render
func (h *Handler) RenderTemplate(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	templateID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	var data models.RenderReplyTemplateRequest
	// Body is optional: missing/empty body just renders all placeholders empty.
	_ = c.ShouldBindJSON(&data)

	rendered, xerr := h.TemplateService.Render(c.Request.Context(), *orgID, templateID, data.Variables)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, rendered)
}
