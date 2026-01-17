package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (h *Handler) CreateRole(c *gin.Context) {
	adminID := middleware.GetUserID(c)
	aid, err := uuid.Parse(adminID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	var data models.CreateRole

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, err)
		return
	}

	resp, xerr := h.RoleService.Create(c.Request.Context(), aid, &data)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) GetRoles(c *gin.Context) {
	resp, err := h.RoleService.Get(c.Request.Context())
	if err != nil {
		errx.Handle(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) UpdateRole(c *gin.Context) {
	adminID := middleware.GetUserID(c)
	aid, err := uuid.Parse(adminID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	roleID := c.Param("id")
	rid, err := uuid.Parse(roleID)
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	var data models.UpdateRole

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, err)
		return
	}

	resp, xerr := h.RoleService.Update(c.Request.Context(), aid, rid, &data)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) DeleteRole(c *gin.Context) {
	adminID := middleware.GetUserID(c)
	aid, err := uuid.Parse(adminID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	roleID := c.Param("id")
	rid, err := uuid.Parse(roleID)
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	if xerr := h.RoleService.Delete(c.Request.Context(), aid, rid); xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) Add(c *gin.Context) {
	adminID := middleware.GetUserID(c)
	aid, err := uuid.Parse(adminID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	userID := c.Param("user")
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	roleID := c.Param("role")
	rid, err := uuid.Parse(roleID)
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	if xerr := h.RoleService.Add(c.Request.Context(), aid, uid, rid); xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) Remove(c *gin.Context) {
	adminID := middleware.GetUserID(c)
	aid, err := uuid.Parse(adminID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	userID := c.Param("user")
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	roleID := c.Param("role")
	rid, err := uuid.Parse(roleID)
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	if xerr := h.RoleService.Remove(c.Request.Context(), aid, uid, rid); xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.Status(http.StatusNoContent)
}
