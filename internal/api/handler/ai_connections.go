// Connected MCP servers (client direction). An org admin connects an external
// MCP server; Warmbly discovers its tools and, once enabled, exposes them to the
// assistant as approval-gated tools. JWT + manage_settings only; bearer tokens
// are sealed server-side and never returned.
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (h *Handler) mcpOrgUser(c *gin.Context) (uuid.UUID, uuid.UUID, *errx.Error) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil || h.MCPService == nil {
		return uuid.Nil, uuid.Nil, errx.New(errx.BadRequest, "no organization selected")
	}
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		return uuid.Nil, uuid.Nil, errx.New(errx.Unauthorized, "invalid user")
	}
	return *orgID, userID, nil
}

// ListMCPServers — GET /ai/connections
func (h *Handler) ListMCPServers(c *gin.Context) {
	orgID, _, xerr := h.mcpOrgUser(c)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	list, lerr := h.MCPService.List(c.Request.Context(), orgID)
	if lerr != nil {
		errx.JSON(c, lerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// CreateMCPServer — POST /ai/connections
func (h *Handler) CreateMCPServer(c *gin.Context) {
	orgID, userID, xerr := h.mcpOrgUser(c)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	var req models.CreateMCPServer
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	sv, cerr := h.MCPService.Create(c.Request.Context(), orgID, userID, &req)
	if cerr != nil {
		errx.JSON(c, cerr)
		return
	}
	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityMCPServer, &sv.ID, nil, map[string]string{"name": sv.Name})
	c.JSON(http.StatusOK, sv)
}

// UpdateMCPServer — PATCH /ai/connections/:id (rename, enable/disable, re-token)
func (h *Handler) UpdateMCPServer(c *gin.Context) {
	orgID, _, xerr := h.mcpOrgUser(c)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid connection id"))
		return
	}
	var req models.UpdateMCPServer
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}
	sv, uerr := h.MCPService.Update(c.Request.Context(), orgID, id, &req)
	if uerr != nil {
		errx.JSON(c, uerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityMCPServer, &sv.ID, nil, map[string]string{"name": sv.Name})
	c.JSON(http.StatusOK, sv)
}

// DeleteMCPServer — DELETE /ai/connections/:id
func (h *Handler) DeleteMCPServer(c *gin.Context) {
	orgID, _, xerr := h.mcpOrgUser(c)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid connection id"))
		return
	}
	if derr := h.MCPService.Delete(c.Request.Context(), orgID, id); derr != nil {
		errx.JSON(c, derr)
		return
	}
	h.auditOrg(c, models.AuditActionDelete, models.AuditEntityMCPServer, &id, nil, nil)
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// RefreshMCPServer — POST /ai/connections/:id/refresh (re-discover tools)
func (h *Handler) RefreshMCPServer(c *gin.Context) {
	orgID, _, xerr := h.mcpOrgUser(c)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid connection id"))
		return
	}
	sv, rerr := h.MCPService.Refresh(c.Request.Context(), orgID, id)
	if rerr != nil {
		errx.JSON(c, rerr)
		return
	}
	c.JSON(http.StatusOK, sv)
}
