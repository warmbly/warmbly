package handler

import (
	"net/http"
	"strconv"

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

