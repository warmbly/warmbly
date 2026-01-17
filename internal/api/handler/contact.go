package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (h *Handler) AddContacts(c *gin.Context) {
	userID := middleware.GetUserID(c)

	var data []models.AddContact

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	resp, err := h.ContactService.Add(c.Request.Context(), userID, data)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) SearchContacts(c *gin.Context) {
	userID := middleware.GetUserID(c)

	cursor := c.Query("cursor")
	category := c.Query("category")
	limit := c.Query("limit")

	var data models.SearchContacts

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	resp, err := h.ContactService.Search(c.Request.Context(), userID, cursor, category, limit, data)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) UpdateContactBulk(c *gin.Context) {
	userID := middleware.GetUserID(c)

	var data models.BulkEditContactsData

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	resp, err := h.ContactService.BulkUpdate(c.Request.Context(), userID, &data)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) UpdateContact(c *gin.Context) {
	userID := middleware.GetUserID(c)

	id := c.Param("id")

	var data models.UpdateContact

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	resp, err := h.ContactService.Update(c.Request.Context(), userID, id, &data)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) DeleteContactBulk(c *gin.Context) {
	userID := middleware.GetUserID(c)

	var data []string

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	if err := h.ContactService.BulkDelete(c.Request.Context(), userID, data); err != nil {
		errx.Handle(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) DeleteContact(c *gin.Context) {
	userID := middleware.GetUserID(c)

	id := c.Param("id")

	if err := h.ContactService.Delete(c.Request.Context(), userID, id); err != nil {
		errx.Handle(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}
