package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// The Warmup plan is retired: nobody new can buy it, and a workspace already on it keeps it while it pays.
var errWarmupPlanRetired = errx.NewWithIdentifier(errx.Conflict, "warmup_plan_retired", "The Warmup plan is no longer sold. Choose a plan on the billing page.")

// PoolLinkOffer always reports the retired plan unavailable, which older dashboards render as such.
func (h *Handler) PoolLinkOffer(c *gin.Context) {
	c.JSON(http.StatusOK, models.PoolLinkOffer{Currency: "usd"})
}

// PoolLinkCheckout refuses: the route stays so an older dashboard gets a reason, not a 404.
func (h *Handler) PoolLinkCheckout(c *gin.Context) {
	errx.JSON(c, errWarmupPlanRetired)
}
