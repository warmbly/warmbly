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

func (h *Handler) GetOutreachSettings(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	settings, xerr := h.AdvancedService.GetOrganizationSettings(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, settings)
}

func (h *Handler) UpdateOutreachSettings(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	userIDStr := middleware.GetUserID(c)
	userID, parseErr := uuid.Parse(userIDStr)
	if parseErr != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid user id"))
		return
	}
	var req models.UpsertOutreachSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}
	if xerr := h.AdvancedService.UpdateOrganizationSettings(c.Request.Context(), *orgID, userID, &req.Settings); xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntitySettings, nil, nil, map[string]string{
		"scope": "outreach",
	})

	c.Status(http.StatusNoContent)
}

func (h *Handler) GetCampaignAdvancedSettings(c *gin.Context) {
	campaignID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	settings, xerr := h.AdvancedService.GetCampaignSettings(c.Request.Context(), campaignID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, settings)
}

func (h *Handler) UpdateCampaignAdvancedSettings(c *gin.Context) {
	campaignID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	var req models.UpsertOutreachSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}
	if xerr := h.AdvancedService.UpdateCampaignSettings(c.Request.Context(), campaignID, &req.Settings); xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntitySettings, &campaignID, nil, map[string]string{
		"scope":       "campaign_advanced",
		"campaign_id": campaignID.String(),
	})

	c.Status(http.StatusNoContent)
}

func (h *Handler) ListCampaignABVariants(c *gin.Context) {
	campaignID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	variants, xerr := h.AdvancedService.ListABVariants(c.Request.Context(), campaignID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": variants})
}

func (h *Handler) CreateCampaignABVariant(c *gin.Context) {
	campaignID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	var req models.CreateCampaignABVariantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}
	out, xerr := h.AdvancedService.CreateABVariant(c.Request.Context(), campaignID, &req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	variantID := out.ID
	h.auditOrg(c, models.AuditActionCreate, models.AuditEntitySettings, &variantID, nil, map[string]string{
		"scope":       "ab_variant",
		"campaign_id": campaignID.String(),
		"name":        out.Name,
	})

	c.JSON(http.StatusCreated, out)
}

func (h *Handler) UpdateCampaignABVariant(c *gin.Context) {
	campaignID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	variantID, err := uuid.Parse(c.Param("variantId"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	var req models.UpdateCampaignABVariantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}
	out, xerr := h.AdvancedService.UpdateABVariant(c.Request.Context(), campaignID, variantID, &req)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntitySettings, &variantID, nil, map[string]string{
		"scope":       "ab_variant",
		"campaign_id": campaignID.String(),
	})

	c.JSON(http.StatusOK, out)
}

func (h *Handler) DeleteCampaignABVariant(c *gin.Context) {
	campaignID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	variantID, err := uuid.Parse(c.Param("variantId"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	if xerr := h.AdvancedService.DeleteABVariant(c.Request.Context(), campaignID, variantID); xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionDelete, models.AuditEntitySettings, &variantID, nil, map[string]string{
		"scope":       "ab_variant",
		"campaign_id": campaignID.String(),
	})

	c.Status(http.StatusNoContent)
}

func (h *Handler) GetCampaignABAnalysis(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	campaignID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	analysis, xerr := h.AdvancedService.GetABWinnerAnalysis(c.Request.Context(), *orgID, campaignID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, analysis)
}

func (h *Handler) RunCampaignPreflight(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	campaignID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	report, xerr := h.AdvancedService.RunPreflight(c.Request.Context(), *orgID, campaignID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, report)
}

func (h *Handler) GetDeliverabilityDashboard(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	now := time.Now().UTC()
	from := now.AddDate(0, 0, -7)
	to := now

	if q := c.Query("from"); q != "" {
		if parsed, err := time.Parse(time.RFC3339, q); err == nil {
			from = parsed
		}
	}
	if q := c.Query("to"); q != "" {
		if parsed, err := time.Parse(time.RFC3339, q); err == nil {
			to = parsed
		}
	}

	dashboard, xerr := h.AdvancedService.GetDeliverabilityDashboard(c.Request.Context(), *orgID, from, to)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, dashboard)
}

func (h *Handler) IngestDeliverabilityEvent(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	var req models.IngestDeliverabilityEventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}
	if xerr := h.AdvancedService.IngestDeliverabilityEvent(c.Request.Context(), *orgID, &req); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.Status(http.StatusAccepted)
}

func (h *Handler) ListTaskDeadLetters(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	status := c.Query("status")
	limit := 100
	if v := c.Query("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}
	items, xerr := h.AdvancedService.ListDeadLetters(c.Request.Context(), *orgID, status, limit)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

func (h *Handler) ReplayTaskDeadLetter(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.ErrUuid)
		return
	}
	if xerr := h.AdvancedService.ReplayDeadLetter(c.Request.Context(), *orgID, id); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "replayed"})
}
