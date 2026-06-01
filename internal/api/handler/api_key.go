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

// CreateAPIKey creates a new API key for the organization
// POST /api-keys
func (h *Handler) CreateAPIKey(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	var data models.CreateAPIKey
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	keyWithSecret, xerr := h.APIKeyService.Create(c.Request.Context(), *orgID, userID, &data)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	keyID := keyWithSecret.ID
	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityAPIKey, &keyID, nil, map[string]string{"name": keyWithSecret.Name})

	c.JSON(http.StatusCreated, keyWithSecret)
}

// ListAPIKeys lists all API keys for the organization
// GET /api-keys
func (h *Handler) ListAPIKeys(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var cursor *uuid.UUID
	if cursorStr := c.Query("cursor"); cursorStr != "" {
		if id, err := uuid.Parse(cursorStr); err == nil {
			cursor = &id
		}
	}

	limit := 50
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	result, xerr := h.APIKeyService.List(c.Request.Context(), *orgID, limit, cursor)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetAPIKey gets a specific API key
// GET /api-keys/:id
func (h *Handler) GetAPIKey(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	keyIDStr := c.Param("id")
	keyID, err := uuid.Parse(keyIDStr)
	if err != nil {
		errx.JSON(c, errx.ErrNotFound)
		return
	}

	key, xerr := h.APIKeyService.Get(c.Request.Context(), *orgID, keyID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, key)
}

// UpdateAPIKey updates an API key
// PATCH /api-keys/:id
func (h *Handler) UpdateAPIKey(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	keyIDStr := c.Param("id")
	keyID, err := uuid.Parse(keyIDStr)
	if err != nil {
		errx.JSON(c, errx.ErrNotFound)
		return
	}

	var data models.UpdateAPIKey
	if err := c.ShouldBindJSON(&data); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	key, xerr := h.APIKeyService.Update(c.Request.Context(), *orgID, keyID, &data)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityAPIKey, &keyID, nil, nil)

	c.JSON(http.StatusOK, key)
}

// RevokeAPIKey revokes an API key
// DELETE /api-keys/:id
func (h *Handler) RevokeAPIKey(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	keyIDStr := c.Param("id")
	keyID, err := uuid.Parse(keyIDStr)
	if err != nil {
		errx.JSON(c, errx.ErrNotFound)
		return
	}

	reason := c.Query("reason")
	if reason == "" {
		reason = "Revoked by user"
	}

	xerr := h.APIKeyService.Revoke(c.Request.Context(), *orgID, keyID, reason)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionRevoke, models.AuditEntityAPIKey, &keyID, nil, nil)

	c.JSON(http.StatusOK, gin.H{"status": "revoked"})
}

// ListAPIPermissions lists all available API permissions
// GET /api-keys/permissions
func (h *Handler) ListAPIPermissions(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"permissions": models.AllAPIPermissions,
		"presets": gin.H{
			"read_only":   models.APIPermReadOnly,
			"full_access": models.APIPermFullAccess,
		},
	})
}

// GetAPIKeyUsageSummary returns the org-level usage strip (active key
// count, 24h request total, last call timestamp, error rate, avg latency).
// GET /api-keys/usage/summary
func (h *Handler) GetAPIKeyUsageSummary(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	summary, xerr := h.APIKeyService.GetUsageSummary(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, summary)
}

// GetAPIKeyAnalytics returns the request timeseries + endpoint breakdown
// for a single key (when :id is set) or the whole org (when :id is "all").
// GET /api-keys/:id/analytics?from=...&to=...&interval=hour|day|minute
func (h *Handler) GetAPIKeyAnalytics(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var keyID *uuid.UUID
	if id := c.Param("id"); id != "" && id != "all" {
		parsed, err := uuid.Parse(id)
		if err != nil {
			errx.JSON(c, errx.ErrNotFound)
			return
		}
		keyID = &parsed
	}

	from, to := parseAnalyticsRange(c)
	interval := c.Query("interval")

	analytics, xerr := h.APIKeyService.GetAnalytics(c.Request.Context(), *orgID, keyID, from, to, interval)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, analytics)
}

// ListAPIKeyUsageLogs returns the recent raw request entries for a single key.
// GET /api-keys/:id/logs?cursor=...&limit=...
func (h *Handler) ListAPIKeyUsageLogs(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	keyID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrNotFound)
		return
	}

	var cursor *uuid.UUID
	if cursorStr := c.Query("cursor"); cursorStr != "" {
		if id, err := uuid.Parse(cursorStr); err == nil {
			cursor = &id
		}
	}

	limit := 50
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}

	result, xerr := h.APIKeyService.ListUsageLogs(c.Request.Context(), *orgID, keyID, limit, cursor)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, result)
}

// parseAnalyticsRange pulls ?from / ?to off the query string, defaulting
// to "last 24 hours" when either is missing. Accepts RFC3339 timestamps.
func parseAnalyticsRange(c *gin.Context) (from, to time.Time) {
	if f := c.Query("from"); f != "" {
		if parsed, err := time.Parse(time.RFC3339, f); err == nil {
			from = parsed
		}
	}
	if t := c.Query("to"); t != "" {
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			to = parsed
		}
	}
	if to.IsZero() {
		to = time.Now()
	}
	if from.IsZero() {
		from = to.Add(-24 * time.Hour)
	}
	return from, to
}
