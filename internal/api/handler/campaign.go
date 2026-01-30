package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/audit"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (h *Handler) CreateCampaign(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)

	var data models.CreateCampaign

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}

	resp, err := h.CampaignService.Create(c.Request.Context(), userIDStr, &data)
	if err != nil {
		errx.JSON(c, err)
		return
	}

	// Audit log
	if userID, err := uuid.Parse(userIDStr); err == nil {
		audit.LogCreate(h.AuditService, c.Request.Context(), userID, models.AuditEntityCampaign, resp.ID, c.ClientIP(), c.Request.UserAgent(), map[string]string{"name": resp.Name})
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) GetCampaign(c *gin.Context) {
	userID := middleware.GetUserID(c)

	id := c.Param("id")

	resp, err := h.CampaignService.Get(c.Request.Context(), userID, id)
	if err != nil {
		errx.JSON(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) SearchCampaigns(c *gin.Context) {
	userID := middleware.GetUserID(c)

	query := c.Query("q")
	cursor := c.Query("cursor")
	folder := c.Query("folder")
	limit := c.Query("limit")

	resp, err := h.CampaignService.Search(c.Request.Context(), userID, query, cursor, folder, limit)
	if err != nil {
		errx.JSON(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) UpdateCampaign(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)

	id := c.Param("id")

	var data models.UpdateCampaign

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}

	resp, err := h.CampaignService.Update(c.Request.Context(), userIDStr, id, &data)
	if err != nil {
		errx.JSON(c, err)
		return
	}

	// Audit log
	if userID, err := uuid.Parse(userIDStr); err == nil {
		if campaignID, err := uuid.Parse(id); err == nil {
			audit.LogUpdate(h.AuditService, c.Request.Context(), userID, models.AuditEntityCampaign, campaignID, c.ClientIP(), c.Request.UserAgent(), nil)
		}
	}

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) DeleteCampaign(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)

	id := c.Param("id")

	if err := h.CampaignService.Delete(c.Request.Context(), userIDStr, id); err != nil {
		errx.JSON(c, err)
		return
	}

	// Audit log
	if userID, err := uuid.Parse(userIDStr); err == nil {
		if campaignID, err := uuid.Parse(id); err == nil {
			audit.LogDelete(h.AuditService, c.Request.Context(), userID, models.AuditEntityCampaign, campaignID, c.ClientIP(), c.Request.UserAgent())
		}
	}

	c.Status(http.StatusNoContent)
}

// StartCampaign starts a campaign
// POST /campaigns/:id/start
func (h *Handler) StartCampaign(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	id := c.Param("id")

	if xerr := h.CampaignService.StartCampaign(c.Request.Context(), *orgID, id); xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "started"})
}

// StopCampaign stops a campaign
// POST /campaigns/:id/stop
func (h *Handler) StopCampaign(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	id := c.Param("id")

	if xerr := h.CampaignService.StopCampaign(c.Request.Context(), *orgID, id); xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "stopped"})
}

// GetCampaignLogs returns campaign activity logs
// GET /campaigns/:id/logs
func (h *Handler) GetCampaignLogs(c *gin.Context) {
	userID := middleware.GetUserID(c)
	id := c.Param("id")

	cursorStr := c.Query("cursor")
	var cursor *string
	if cursorStr != "" {
		cursor = &cursorStr
	}

	limit := 50
	if limitStr := c.DefaultQuery("limit", "50"); limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	result, xerr := h.CampaignService.GetLogs(c.Request.Context(), userID, id, limit, cursor)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}
