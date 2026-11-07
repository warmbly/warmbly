package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
)

// campaignWarmupPlacementDays is how far back a step's warmup placement is read.
const campaignWarmupPlacementDays = 30

// GetCampaignWarmupPlacement is where each arm of the campaign's opening steps
// landed when warmup carried it over the last 30 days. Only arms warmup carried
// are listed.
func (h *Handler) GetCampaignWarmupPlacement(c *gin.Context) {
	campaignID, xerr := h.ownedCampaign(c)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	if h.WarmupContentRepo == nil {
		c.JSON(http.StatusOK, gin.H{"data": []any{}, "days": campaignWarmupPlacementDays})
		return
	}
	since := time.Now().AddDate(0, 0, -campaignWarmupPlacementDays)
	steps, err := h.WarmupContentRepo.CampaignStepPlacement(c.Request.Context(), *middleware.GetOrganizationID(c), campaignID, since)
	if err != nil {
		errx.JSON(c, errx.InternalError())
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": steps, "days": campaignWarmupPlacementDays})
}
