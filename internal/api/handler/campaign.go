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

func (h *Handler) CreateCampaign(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)
	orgID := middleware.GetOrganizationID(c)

	var data models.CreateCampaign

	if err := c.ShouldBindJSON(&data); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}

	resp, err := h.CampaignService.Create(c.Request.Context(), userIDStr, orgID, &data)
	if err != nil {
		errx.JSON(c, err)
		return
	}

	// Audit log
	h.auditOrg(c, models.AuditActionCreate, models.AuditEntityCampaign, &resp.ID, nil, map[string]string{"name": resp.Name})

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
	if campaignID, err := uuid.Parse(id); err == nil {
		h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityCampaign, &campaignID, nil, nil)
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
	if campaignID, err := uuid.Parse(id); err == nil {
		h.auditOrg(c, models.AuditActionDelete, models.AuditEntityCampaign, &campaignID, nil, nil)
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

	if campaignID, err := uuid.Parse(id); err == nil {
		h.auditOrg(c, models.AuditActionStart, models.AuditEntityCampaign, &campaignID, nil, nil)
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

	if campaignID, err := uuid.Parse(id); err == nil {
		h.auditOrg(c, models.AuditActionStop, models.AuditEntityCampaign, &campaignID, nil, nil)
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

// ListCampaignSenders returns a campaign's explicit sender pool.
// GET /campaigns/:id/senders
func (h *Handler) ListCampaignSenders(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	senders, xerr := h.CampaignService.ListCampaignSenders(c.Request.Context(), *orgID, c.Param("id"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": senders})
}

// ReplaceCampaignSenders atomically replaces a campaign's explicit sender pool.
// PUT /campaigns/:id/senders
func (h *Handler) ReplaceCampaignSenders(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var body struct {
		Senders []models.CampaignSenderInput `json:"senders"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}

	senders, xerr := h.CampaignService.ReplaceCampaignSenders(c.Request.Context(), *orgID, c.Param("id"), body.Senders)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	if campaignID, err := uuid.Parse(c.Param("id")); err == nil {
		h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityCampaign, &campaignID, nil, map[string]string{"scope": "senders"})
	}

	c.JSON(http.StatusOK, gin.H{"data": senders})
}

// VerifyCampaignTrackingDomain resolves the campaign-scoped tracking domain's
// CNAME and flips verified on success.
// POST /campaigns/:id/tracking-domain/verify
func (h *Handler) VerifyCampaignTrackingDomain(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	status, xerr := h.CampaignService.VerifyCampaignTrackingDomain(c.Request.Context(), *orgID, c.Param("id"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	if campaignID, err := uuid.Parse(c.Param("id")); err == nil {
		h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityCampaign, &campaignID, nil, map[string]string{"scope": "tracking_domain_verify"})
	}

	c.JSON(http.StatusOK, status)
}
