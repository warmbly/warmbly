package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (h *Handler) EmailsSearch(c *gin.Context) {
	userID := middleware.GetUserID(c)

	query := c.Query("q")
	cursor := c.Query("cursor")
	tag := c.Query("tag")
	limit := c.Query("limit")

	resp, err := h.EmailService.Search(c.Request.Context(), userID, query, cursor, tag, limit)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) GetEmail(c *gin.Context) {
	userID := middleware.GetUserID(c)

	emailAccountID := c.Param("id")

	resp, err := h.EmailService.Get(c.Request.Context(), userID, emailAccountID)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) UpdateEmail(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)

	emailAccountID := c.Param("id")

	var data models.UpdateEmail

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	resp, err := h.EmailService.Update(c.Request.Context(), userIDStr, emailAccountID, &data)
	if err != nil {
		errx.Handle(c, err)
		return
	}

	// Audit log
	if userID, err := uuid.Parse(userIDStr); err == nil {
		if accountID, err := uuid.Parse(emailAccountID); err == nil {
			h.AuditService.LogAction(c.Request.Context(), userID, models.AuditActionUpdate, models.AuditEntityEmailAccount, &accountID, c.ClientIP(), c.Request.UserAgent(), nil, nil)
		}
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) UpdateEmailTrackingDomain(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)

	emailAccountID := c.Param("id")
	domain := c.Query("domain")

	if err := h.EmailService.UpdateTrackingDomain(c.Request.Context(), userIDStr, emailAccountID, domain); err != nil {
		errx.Handle(c, err)
		return
	}

	// Audit log
	if userID, err := uuid.Parse(userIDStr); err == nil {
		if accountID, err := uuid.Parse(emailAccountID); err == nil {
			h.AuditService.LogAction(c.Request.Context(), userID, models.AuditActionUpdate, models.AuditEntityEmailAccount, &accountID, c.ClientIP(), c.Request.UserAgent(), map[string]string{"tracking_domain": domain}, nil)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"tracking_domain": domain,
	})
}

func (h *Handler) DeleteEmail(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)

	emailAccountID := c.Param("id")

	if err := h.EmailService.Delete(c.Request.Context(), userIDStr, emailAccountID); err != nil {
		errx.Handle(c, err)
		return
	}

	// Audit log
	if userID, err := uuid.Parse(userIDStr); err == nil {
		if accountID, err := uuid.Parse(emailAccountID); err == nil {
			h.AuditService.LogAction(c.Request.Context(), userID, models.AuditActionDelete, models.AuditEntityEmailAccount, &accountID, c.ClientIP(), c.Request.UserAgent(), nil, nil)
		}
	}

	c.Status(http.StatusNoContent)
}
