package grouph

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func GetGroupID(c *gin.Context) string {
	return c.Param("gid")
}

// entityType maps the group's name ("folders"/"tags"/"categories") to the
// matching audit entity type.
func (h *Handler) entityType() models.AuditEntityType {
	switch h.name {
	case "tags":
		return models.AuditEntityTag
	case "categories":
		return models.AuditEntityCategory
	default:
		return models.AuditEntityFolder
	}
}

// logAudit records a group mutation in the organization-wide audit trail. It
// pulls actor/org/IP/user-agent from the gin context; no-op without org context.
func (h *Handler) logAudit(c *gin.Context, action models.AuditAction, entityID *uuid.UUID, metadata map[string]string) {
	if h.audit == nil {
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		return
	}
	actorID, err := middleware.GetUserUUID(c)
	if err != nil {
		return
	}
	h.audit.LogAction(c.Request.Context(), *orgID, actorID, action, h.entityType(), entityID, c.ClientIP(), c.Request.UserAgent(), nil, metadata)
}

func (h *Handler) Create(c *gin.Context) {
	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	var data models.GroupCreate

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	group, xerr := h.service.Create(c.Request.Context(), uid, &data)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	h.logAudit(c, models.AuditActionCreate, &group.ID, map[string]string{"title": group.Title})

	c.JSON(http.StatusOK, group)
}

func (h *Handler) Update(c *gin.Context) {
	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}
	groupID := GetGroupID(c)
	gid, err := uuid.Parse(groupID)
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	var data models.GroupUpdate

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	group, xerr := h.service.Update(c.Request.Context(), uid, gid, &data)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	h.logAudit(c, models.AuditActionUpdate, &gid, map[string]string{"title": group.Title})

	c.JSON(http.StatusOK, group)
}

func (h *Handler) Move(c *gin.Context) {
	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}
	groupID := GetGroupID(c)
	gid, err := uuid.Parse(groupID)
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	var data models.Move

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	orders, xerr := h.service.Move(c.Request.Context(), uid, gid, data.Position)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	h.logAudit(c, models.AuditActionUpdate, &gid, map[string]string{"moved": "true"})

	c.JSON(http.StatusOK, orders)
}

func (h *Handler) Delete(c *gin.Context) {
	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}
	groupID := GetGroupID(c)
	gid, err := uuid.Parse(groupID)
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	if xerr := h.service.Delete(c.Request.Context(), uid, gid); xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	h.logAudit(c, models.AuditActionDelete, &gid, nil)

	c.Status(http.StatusNoContent)
}
