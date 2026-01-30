package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// GetAuditLogs gets audit logs for the current user
// GET /audit-logs
func (h *Handler) GetAuditLogs(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		errx.Handle(c, errx.ErrAuth)
		return
	}

	params := &models.AuditLogSearch{
		Limit:  50,
		Cursor: c.Query("cursor"),
	}

	// Parse optional filters
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			params.Limit = l
		}
	}

	if dateStr := c.Query("date"); dateStr != "" {
		if date, err := time.Parse("2006-01-02", dateStr); err == nil {
			params.Since = &date
		}
	}

	if entityType := c.Query("entity_type"); entityType != "" {
		et := models.AuditEntityType(entityType)
		params.EntityType = &et
	}

	if action := c.Query("action"); action != "" {
		a := models.AuditAction(action)
		params.Action = &a
	}

	result, xerr := h.AuditService.GetUserLogs(c.Request.Context(), userID, params)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetAdminAuditLogs gets audit logs for any user (admin only)
// GET /admin/audit-logs
func (h *Handler) GetAdminAuditLogs(c *gin.Context) {
	// Note: This endpoint should be protected by admin role middleware

	params := &models.AuditLogSearch{
		Limit:  50,
		Cursor: c.Query("cursor"),
	}

	// Parse user_id filter (required for admin)
	userIDStr := c.Query("user_id")
	if userIDStr == "" {
		errx.Handle(c, errx.New(errx.BadRequest, "user_id parameter is required"))
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		errx.Handle(c, errx.New(errx.BadRequest, "Invalid user_id"))
		return
	}
	params.UserID = &userID

	// Parse optional filters
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			params.Limit = l
		}
	}

	if dateStr := c.Query("date"); dateStr != "" {
		if date, err := time.Parse("2006-01-02", dateStr); err == nil {
			params.Since = &date
		}
	}

	if entityType := c.Query("entity_type"); entityType != "" {
		et := models.AuditEntityType(entityType)
		params.EntityType = &et
	}

	if entityIDStr := c.Query("entity_id"); entityIDStr != "" {
		if entityID, err := uuid.Parse(entityIDStr); err == nil {
			params.EntityID = &entityID
		}
	}

	if action := c.Query("action"); action != "" {
		a := models.AuditAction(action)
		params.Action = &a
	}

	result, xerr := h.AuditService.GetUserLogs(c.Request.Context(), userID, params)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetUserRateLimits gets rate limits for a user (admin only)
// GET /admin/users/:id/rate-limits
func (h *Handler) GetUserRateLimits(c *gin.Context) {
	userIDStr := c.Param("id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		errx.Handle(c, errx.ErrNotFound)
		return
	}

	limits, xerr := h.RateLimitService.GetUserLimits(c.Request.Context(), userID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, limits)
}

// UpdateUserRateLimits updates rate limits for a user (admin only)
// PATCH /admin/users/:id/rate-limits
func (h *Handler) UpdateUserRateLimits(c *gin.Context) {
	adminIDStr := middleware.GetUserID(c)
	adminID, err := uuid.Parse(adminIDStr)
	if err != nil {
		errx.Handle(c, errx.ErrAuth)
		return
	}

	userIDStr := c.Param("id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		errx.Handle(c, errx.ErrNotFound)
		return
	}

	var data models.UpdateUserRateLimits
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.Handle(c, errx.New(errx.BadRequest, "Invalid request body"))
		return
	}

	limits, xerr := h.RateLimitService.UpdateUserLimits(c.Request.Context(), userID, &data, adminID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, limits)
}
