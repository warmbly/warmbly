package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (h *Handler) GetUniboxIncoming(c *gin.Context) {
	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	from := c.Query("from")
	cursor := c.Query("cursor")
	limit := c.Query("limit")

	resp, xerr := h.UniboxService.Incoming(c.Request.Context(), uid, limit, cursor, from)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) GetUniboxEmail(c *gin.Context) {
	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	mailID := c.Param("id")
	mid, err := uuid.Parse(mailID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	resp, xerr := h.UniboxService.GetByID(
		c.Request.Context(),
		uid, mid,
	)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) GetUniboxThread(c *gin.Context) {
	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	emailID := c.Query("email")
	eid, err := uuid.Parse(emailID)
	if err != nil {
		errx.Handle(c, errx.ErrUuid)
		return
	}

	threadID := c.Query("id")
	cursor := c.Query("cursor")
	limit := c.Query("limit")

	resp, xerr := h.UniboxService.GetByThread(
		c.Request.Context(),
		uid, eid,
		threadID, limit, cursor,
	)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) UniboxMarkSeen(c *gin.Context) {
	userID := middleware.GetUserID(c)
	uid, err := uuid.Parse(userID)
	if err != nil {
		errx.Handle(c, errx.ErrUser)
		return
	}

	var data models.MarkSeen
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	resp, xerr := h.UniboxService.MarkSeenBulk(c.Request.Context(), uid, &data)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, resp)
}
