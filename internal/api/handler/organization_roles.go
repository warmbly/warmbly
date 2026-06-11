package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Custom org roles: named permission sets managed by anyone with
// PermManageTeam (mutations). Listing is open to every member so role names
// render on the roster for non-admins too.

func (h *Handler) ListOrganizationRoles(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	roles, xerr := h.OrganizationService.ListRoles(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": roles})
}

func (h *Handler) CreateOrganizationRole(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var req models.CreateOrganizationRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}

	actorID, uerr := middleware.GetUserUUID(c)
	if uerr != nil {
		errx.JSON(c, errx.New(errx.Unauthorized, "invalid user"))
		return
	}

	role, xerr := h.OrganizationService.CreateRole(c.Request.Context(), *orgID, actorID, &req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityRole, &role.ID, nil, map[string]string{"name": role.Name})

	c.JSON(http.StatusCreated, role)
}

func (h *Handler) UpdateOrganizationRole(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	roleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}

	var req models.UpdateOrganizationRoleRequest
	if berr := c.ShouldBindJSON(&req); berr != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}

	actorID, uerr := middleware.GetUserUUID(c)
	if uerr != nil {
		errx.JSON(c, errx.New(errx.Unauthorized, "invalid user"))
		return
	}

	role, xerr := h.OrganizationService.UpdateRole(c.Request.Context(), *orgID, actorID, roleID, &req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityRole, &roleID, nil, map[string]string{"name": role.Name})

	c.JSON(http.StatusOK, role)
}

func (h *Handler) DeleteOrganizationRole(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	roleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}

	actorID, uerr := middleware.GetUserUUID(c)
	if uerr != nil {
		errx.JSON(c, errx.New(errx.Unauthorized, "invalid user"))
		return
	}

	if xerr := h.OrganizationService.DeleteRole(c.Request.Context(), *orgID, actorID, roleID); xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityRole, &roleID, nil, nil)

	c.Status(http.StatusNoContent)
}
