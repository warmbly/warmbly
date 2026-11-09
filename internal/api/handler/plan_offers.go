package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/planoffer"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// adminGrantRequest is an offer grant; the reason keeps it answerable later.
type adminGrantRequest struct {
	Reason    string     `json:"reason"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type adminPlanOfferView struct {
	*models.PlanOffer
	Active bool `json:"active"`
}

func (h *Handler) planOffersReady(c *gin.Context) bool {
	if h.PlanOfferService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "plan offers are not available on this instance"))
		return false
	}
	return true
}

func adminPlanParam(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("planId"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid plan ID"))
		return uuid.Nil, false
	}
	return id, true
}

// AdminListOrgPlanOffers lists every private plan offered to a workspace, lapsed ones included.
func (h *Handler) AdminListOrgPlanOffers(c *gin.Context) {
	orgID, ok := adminOrgParam(c)
	if !ok || !h.planOffersReady(c) {
		return
	}
	offers, xerr := h.PlanOfferService.ListOffers(c.Request.Context(), orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	now := time.Now()
	views := make([]adminPlanOfferView, 0, len(offers))
	for _, o := range offers {
		views = append(views, adminPlanOfferView{PlanOffer: o, Active: o.Active(now)})
	}
	c.JSON(http.StatusOK, gin.H{"offers": views})
}

// AdminPutOrgPlanOffer offers a private plan to a workspace, or renews the offer.
func (h *Handler) AdminPutOrgPlanOffer(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	orgID, ok := adminOrgParam(c)
	if !ok {
		return
	}
	planID, ok := adminPlanParam(c)
	if !ok || !h.planOffersReady(c) {
		return
	}
	var req adminGrantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "a reason is required"))
		return
	}
	offer, xerr := h.PlanOfferService.GrantOffer(c.Request.Context(), orgID, planID, *adminID, req.Reason, req.ExpiresAt)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	details := map[string]any{"plan_id": planID.String(), "reason": offer.Reason}
	if req.ExpiresAt != nil {
		details["expires_at"] = req.ExpiresAt.UTC().Format(time.RFC3339)
	}
	h.logOrgPlanAction(c, *adminID, orgID, "grant_plan_offer", details)
	c.JSON(http.StatusOK, adminPlanOfferView{PlanOffer: offer, Active: offer.Active(time.Now())})
}

// AdminDeleteOrgPlanOffer withdraws an offer; a subscription already bought is untouched.
func (h *Handler) AdminDeleteOrgPlanOffer(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}
	orgID, ok := adminOrgParam(c)
	if !ok {
		return
	}
	planID, ok := adminPlanParam(c)
	if !ok || !h.planOffersReady(c) {
		return
	}
	if xerr := h.PlanOfferService.WithdrawOffer(c.Request.Context(), orgID, planID); xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.logOrgPlanAction(c, *adminID, orgID, "withdraw_plan_offer", map[string]any{"plan_id": planID.String()})
	c.Status(http.StatusNoContent)
}

// customerPlanOffer is what the billing page renders for one offered plan.
type customerPlanOffer struct {
	PlanID           uuid.UUID  `json:"plan_id"`
	Name             string     `json:"name"`
	Currency         string     `json:"currency"`
	MonthlyUSD       float32    `json:"monthly_usd"`
	YearlyUSD        float32    `json:"yearly_usd,omitempty"`
	MonthlyAvailable bool       `json:"monthly_available"`
	YearlyAvailable  bool       `json:"yearly_available"`
	Available        bool       `json:"available"`
	WarmupOnly       bool       `json:"warmup_only"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
}

// ListPlanOffers returns the private plans this workspace has an open offer for.
func (h *Handler) ListPlanOffers(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	out := []customerPlanOffer{}
	if h.PlanOfferService == nil {
		c.JSON(http.StatusOK, gin.H{"offers": out})
		return
	}
	offers, xerr := h.PlanOfferService.ActiveOffers(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	billing := config.BillingProvider() != "none" && h.StripeService != nil
	for _, o := range offers {
		view := customerPlanOffer{PlanID: o.PlanID, Currency: "usd", ExpiresAt: o.ExpiresAt, WarmupOnly: models.IsWarmupPlan(o.PlanID)}
		if o.Plan.Name != nil {
			view.Name = *o.Plan.Name
		}
		view.MonthlyUSD = o.Plan.Price
		if billing {
			view.MonthlyAvailable = offerPriceFor(o.Plan, "month") != ""
			if offerPriceFor(o.Plan, "year") != "" {
				view.YearlyUSD = *o.Plan.PriceYearly
				view.YearlyAvailable = true
			}
		}
		view.Available = view.MonthlyAvailable || view.YearlyAvailable
		out = append(out, view)
	}
	c.JSON(http.StatusOK, gin.H{"offers": out})
}

// PlanOfferCheckout buys an offered plan. The price is resolved here from the
// offered plan, never taken from the caller.
func (h *Handler) PlanOfferCheckout(c *gin.Context) {
	if h.SubscriptionService == nil || h.StripeService == nil || h.PlanOfferService == nil {
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
	planID, err := uuid.Parse(c.Param("planId"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid plan ID"))
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

	ctx := c.Request.Context()
	offer, xerr := h.PlanOfferService.ActiveOffer(ctx, *orgID, planID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	if offer == nil || offer.Plan == nil {
		errx.JSON(c, planoffer.ErrOfferRequired)
		return
	}
	priceID := offerPriceFor(offer.Plan, interval)
	if priceID == "" {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "this plan has no price for that billing period yet"))
		return
	}

	// A workspace already paying Stripe switches plans on its subscription rather than opening a second one.
	sub, xerr := h.SubscriptionService.Get(ctx, *orgID)
	if xerr == nil && sub != nil && sub.StripeSubscriptionID != nil && strings.TrimSpace(*sub.StripeSubscriptionID) != "" && sub.Status.IsActive() {
		if _, xerr := h.StripeService.ChangePlan(ctx, *orgID, planID, "", "", interval); xerr != nil {
			errx.JSON(c, xerr)
			return
		}
		h.auditOrg(c, models.AuditActionUpdate, models.AuditEntitySubscription, &planID, nil, map[string]string{
			"action":   "offer_change_plan",
			"plan":     planID.String(),
			"interval": interval,
		})
		c.JSON(http.StatusOK, gin.H{"changed": true})
		return
	}

	base := config.DashboardBaseURL(ctx)
	session, xerr := h.StripeService.CreateCheckoutSessionUntil(ctx, uid, *orgID, priceID,
		base+"/app/settings/billing?offer=done", base+"/app/settings/billing", "", offerCheckoutExpiry(offer, time.Now()))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionCreate, models.AuditEntitySubscription, &planID, nil, map[string]string{
		"action":   "offer_checkout",
		"plan":     planID.String(),
		"interval": interval,
		"price_id": priceID,
	})
	c.JSON(http.StatusOK, gin.H{"checkout_url": session.URL})
}

// offerPriceFor is the Stripe price an offer sells for a period; yearly also needs the yearly amount shown beside it.
func offerPriceFor(plan *models.Plan, interval string) string {
	if interval == "year" && (plan == nil || plan.PriceYearly == nil) {
		return ""
	}
	return stripePriceFor(plan, interval)
}

// stripePriceFor picks the Stripe price for a billing period; empty means none, never the other period's.
func stripePriceFor(plan *models.Plan, interval string) string {
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

// Stripe accepts a checkout session lifetime between 30 minutes and 24 hours; a minute of margin each side.
const (
	offerCheckoutMinLife = 31 * time.Minute
	offerCheckoutMaxLife = 24*time.Hour - time.Minute
)

// offerCheckoutExpiry ends an offer's checkout session at the offer's own end, within Stripe's allowed lifetime.
func offerCheckoutExpiry(offer *models.PlanOffer, now time.Time) time.Time {
	latest := now.Add(offerCheckoutMaxLife)
	if offer.ExpiresAt == nil || offer.ExpiresAt.After(latest) {
		return latest
	}
	if earliest := now.Add(offerCheckoutMinLife); offer.ExpiresAt.Before(earliest) {
		return earliest
	}
	return *offer.ExpiresAt
}
