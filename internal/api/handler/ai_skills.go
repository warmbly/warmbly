// AI skills (org playbooks) CRUD. JWT callers need manage_settings; API-key
// callers use the AI_AGENT scope. Every mutation audits with the ai_skill
// entity so the spine refreshes teammates.
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// ListSkills — GET /ai/skills
func (h *Handler) ListSkills(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil || h.SkillsService == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	list, xerr := h.SkillsService.List(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// CreateSkill — POST /ai/skills
func (h *Handler) CreateSkill(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil || h.SkillsService == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	var req models.CreateAISkill
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	sk, xerr := h.SkillsService.Create(c.Request.Context(), *orgID, &req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityAISkill, &sk.ID, nil, map[string]string{"name": sk.Name})
	c.JSON(http.StatusOK, sk)
}

// UpdateSkill — PATCH /ai/skills/:id
func (h *Handler) UpdateSkill(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil || h.SkillsService == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid skill id"))
		return
	}
	var req models.UpdateAISkill
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	sk, xerr := h.SkillsService.Update(c.Request.Context(), *orgID, id, &req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityAISkill, &sk.ID, nil, map[string]string{"name": sk.Name})
	c.JSON(http.StatusOK, sk)
}

// DeleteSkill — DELETE /ai/skills/:id
func (h *Handler) DeleteSkill(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil || h.SkillsService == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid skill id"))
		return
	}
	if xerr := h.SkillsService.Delete(c.Request.Context(), *orgID, id); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityAISkill, &id, nil, nil)
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}
