package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// orgRiskResponse is what a CUSTOMER sees. Deliberately not the raw signal
// blob: that is operator evidence, and handing an actor the exact detector
// weights that flagged them is a map for evading the next check.
type orgRiskResponse struct {
	State models.OrgRiskState `json:"state"`
	// Restricted and Suspended save every caller re-deriving the effects.
	Restricted bool `json:"restricted"`
	Suspended  bool `json:"suspended"`
	// Reason is the plain sentence shown in the dashboard banner.
	Reason string `json:"reason,omitempty"`
}

// GetOrganizationRisk returns the workspace's sending posture, so a customer
// whose volume is capped can see that it is and why, rather than watching
// sends slow down with no explanation.
func (h *Handler) GetOrganizationRisk(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	if h.OrgRiskService == nil {
		c.JSON(http.StatusOK, orgRiskResponse{State: models.OrgRiskTrusted})
		return
	}

	risk, xerr := h.OrgRiskService.Get(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, orgRiskResponse{
		State:      risk.State,
		Restricted: risk.Restricted(),
		Suspended:  risk.State.BlocksSending(),
		Reason:     risk.Reason,
	})
}
