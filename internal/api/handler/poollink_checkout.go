package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// The self-hosted pool plan is deliberately not public, so it never appears in
// the plans grid: it is not an alternative to Starter or Business, it buys pool
// access for an instance the customer runs themselves. That is why it gets its
// own offer and its own checkout instead of a card alongside the others.

// PoolLinkOffer is what the upgrade prompt renders. Available is false when
// billing is off or no Stripe price has been attached to the plan, and the
// prompt then says so rather than opening a checkout that cannot work.
func (h *Handler) PoolLinkOffer(c *gin.Context) {
	offer := models.PoolLinkOffer{Currency: "usd"}

	if config.BillingProvider() == "none" || h.SubscriptionService == nil {
		c.JSON(http.StatusOK, offer)
		return
	}
	plan, xerr := h.SubscriptionService.GetPlan(c.Request.Context(), uuid.MustParse(config.PoolLinkPlanID))
	if xerr != nil || plan == nil {
		c.JSON(http.StatusOK, offer)
		return
	}

	// Resolved through the same helper the checkout uses, so the prompt can
	// never offer a period the checkout would then refuse.
	offer.MonthlyUSD = plan.Price
	offer.MonthlyAvailable = poolPriceFor(plan, "month") != ""
	// A yearly price with no amount beside it is a half-configured plan, and
	// showing it would put an unpriced option in front of the customer.
	if plan.PriceYearly != nil && poolPriceFor(plan, "year") != "" {
		offer.YearlyUSD = *plan.PriceYearly
		offer.YearlyAvailable = true
	}
	offer.Available = offer.MonthlyAvailable || offer.YearlyAvailable
	c.JSON(http.StatusOK, offer)
}

// PoolLinkCheckout opens a Stripe checkout for the pool plan.
//
// The price is resolved here rather than taken from the caller, because the
// plan is not in any list the dashboard can read and a client-supplied price
// would be the only thing deciding what the customer is charged.
func (h *Handler) PoolLinkCheckout(c *gin.Context) {
	if h.SubscriptionService == nil || h.StripeService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "billing is not enabled on this instance"))
		return
	}
	uid, err := uuid.Parse(c.GetString("user_id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.Unauthorized, "invalid user"))
		return
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var req struct {
		Interval string `json:"interval"`
	}
	_ = c.ShouldBindJSON(&req)
	interval := strings.TrimSpace(strings.ToLower(req.Interval))
	if interval == "" {
		interval = "month"
	}
	if interval != "month" && interval != "year" {
		errx.JSON(c, errx.New(errx.BadRequest, "interval must be month or year"))
		return
	}

	plan, xerr := h.SubscriptionService.GetPlan(c.Request.Context(), uuid.MustParse(config.PoolLinkPlanID))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	if plan == nil {
		errx.JSON(c, errx.New(errx.NotFound, "the self-hosted pool plan is not configured on this instance"))
		return
	}

	priceID := poolPriceFor(plan, interval)
	if priceID == "" {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "the self-hosted pool plan has no price for that billing period yet"))
		return
	}

	// Built here, not accepted from the caller: a checkout return URL is a
	// redirect the customer follows after paying.
	base := config.AppBaseURL()
	session, xerr := h.StripeService.CreateCheckoutSession(c.Request.Context(), uid, *orgID, priceID,
		base+"/app/settings/billing?pool=done", base+"/app/settings/billing?pool=1", "")
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	h.auditOrg(c, models.AuditActionCreate, models.AuditEntitySubscription, nil, nil, map[string]string{
		"action":   "pool_checkout",
		"interval": interval,
		"price_id": priceID,
	})

	c.JSON(http.StatusOK, gin.H{"checkout_url": session.URL})
}

// poolPriceFor picks the Stripe price for a billing period. Empty means the
// plan cannot be sold that way, which is never the same as falling back to the
// other period: that would bill a year at a month's price or the reverse.
func poolPriceFor(plan *models.Plan, interval string) string {
	if plan == nil {
		return ""
	}
	id := plan.StripePriceID
	if interval == "year" {
		id = plan.StripePriceIDYearly
	}
	if id == nil {
		return ""
	}
	return strings.TrimSpace(*id)
}
