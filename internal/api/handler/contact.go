package handler

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (h *Handler) AddContacts(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)

	var data []models.AddContact

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	resp, err := h.ContactService.Add(c.Request.Context(), userIDStr, data)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	// Audit log - bulk import
	if userID, err := uuid.Parse(userIDStr); err == nil {
		h.AuditService.LogAction(c.Request.Context(), userID, models.AuditActionImport, models.AuditEntityContact, nil, c.ClientIP(), c.Request.UserAgent(), nil, map[string]string{"count": fmt.Sprintf("%d", len(data))})
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
	userIDStr := middleware.GetUserID(c)

	var data models.BulkEditContactsData

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	resp, err := h.ContactService.BulkUpdate(c.Request.Context(), userIDStr, &data)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	// Audit log - bulk update
	if userID, err := uuid.Parse(userIDStr); err == nil {
		h.AuditService.LogAction(c.Request.Context(), userID, models.AuditActionUpdate, models.AuditEntityContact, nil, c.ClientIP(), c.Request.UserAgent(), nil, map[string]string{"bulk": "true", "count": fmt.Sprintf("%d", len(data.Contacts))})
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) UpdateContact(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)

	id := c.Param("id")

	var data models.UpdateContact

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	resp, err := h.ContactService.Update(c.Request.Context(), userIDStr, id, &data)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	// Audit log
	if userID, err := uuid.Parse(userIDStr); err == nil {
		if contactID, err := uuid.Parse(id); err == nil {
			h.AuditService.LogAction(c.Request.Context(), userID, models.AuditActionUpdate, models.AuditEntityContact, &contactID, c.ClientIP(), c.Request.UserAgent(), nil, nil)
		}
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) DeleteContactBulk(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)

	var data []string

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	if err := h.ContactService.BulkDelete(c.Request.Context(), userIDStr, data); err != nil {
		errx.Handle(c, err)
		return
	}

	// Audit log - bulk delete
	if userID, err := uuid.Parse(userIDStr); err == nil {
		h.AuditService.LogAction(c.Request.Context(), userID, models.AuditActionDelete, models.AuditEntityContact, nil, c.ClientIP(), c.Request.UserAgent(), nil, map[string]string{"bulk": "true", "count": fmt.Sprintf("%d", len(data))})
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) DeleteContact(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)

	id := c.Param("id")

	if err := h.ContactService.Delete(c.Request.Context(), userIDStr, id); err != nil {
		errx.Handle(c, err)
		return
	}

	// Audit log
	if userID, err := uuid.Parse(userIDStr); err == nil {
		if contactID, err := uuid.Parse(id); err == nil {
			h.AuditService.LogAction(c.Request.Context(), userID, models.AuditActionDelete, models.AuditEntityContact, &contactID, c.ClientIP(), c.Request.UserAgent(), nil, nil)
		}
	}

	c.Status(http.StatusNoContent)
}
