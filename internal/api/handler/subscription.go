package handler

import (
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
)

// GetSubscription returns the current organization's subscription
func (h *Handler) GetSubscription(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	sub, errX := h.SubscriptionService.Get(c.Request.Context(), *orgID)
	if errX != nil {
		errx.JSON(c, errX)
		return
	}

	c.JSON(http.StatusOK, sub)
}

// GetSubscriptionLimits returns the current organization's subscription with rate limits
func (h *Handler) GetSubscriptionLimits(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	sub, errX := h.SubscriptionService.GetWithLimits(c.Request.Context(), *orgID)
	if errX != nil {
		errx.JSON(c, errX)
		return
	}

	c.JSON(http.StatusOK, sub)
}

// ListPlans returns available subscription plans
func (h *Handler) ListPlans(c *gin.Context) {
	plans, errX := h.SubscriptionService.ListPlans(c.Request.Context(), true)
	if errX != nil {
		errx.JSON(c, errX)
		return
	}

	c.JSON(http.StatusOK, gin.H{"plans": plans})
}

// CreateCheckoutSession creates a Stripe checkout session
func (h *Handler) CreateCheckoutSession(c *gin.Context) {
	userID := c.GetString("user_id")
	uid, err := uuid.Parse(userID)
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
		PriceID      string `json:"price_id" binding:"required"`
		SuccessURL   string `json:"success_url" binding:"required"`
		CancelURL    string `json:"cancel_url" binding:"required"`
		DiscountCode string `json:"discount_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}

	// An agency client's plan is the agency's; buying one here would bill twice.
	if h.AgencyService != nil {
		if ref := h.AgencyService.AgencyOf(c.Request.Context(), *orgID); ref != nil {
			errx.JSON(c, errx.NewWithIdentifier(errx.Conflict, "agency_billed", "This workspace is billed through its agency. Manage the plan from the agency's billing settings."))
			return
		}
	}

	// Sells public plans; a private one only to a workspace holding an open offer for it.
	if xerr := h.authorizePlanPurchase(c, *orgID, req.PriceID, uuid.Nil); xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	// Pinned to this instance's dashboard: Stripe will redirect the customer
	// to whatever is set here.
	successURL := billingReturnURL(c.Request.Context(), req.SuccessURL, "/app/settings/billing?checkout=done")
	cancelURL := billingReturnURL(c.Request.Context(), req.CancelURL, "/app/settings/billing")

	session, errX := h.StripeService.CreateCheckoutSession(c.Request.Context(), uid, *orgID, req.PriceID, successURL, cancelURL, req.DiscountCode)
	if errX != nil {
		errx.JSON(c, errX)
		return
	}

	h.auditOrg(c, models.AuditActionCreate, models.AuditEntitySubscription, nil, nil, map[string]string{
		"action":   "checkout",
		"price_id": req.PriceID,
	})

	c.JSON(http.StatusOK, gin.H{
		"session_id":   session.ID,
		"checkout_url": session.URL,
	})
}

// CreateBillingPortalSession creates a Stripe billing portal session
func (h *Handler) CreateBillingPortalSession(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var req struct {
		ReturnURL string `json:"return_url" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}

	sub, errX := h.SubscriptionService.Get(c.Request.Context(), *orgID)
	if errX != nil {
		errx.JSON(c, errX)
		return
	}

	portalURL, errX := h.StripeService.CreatePortalSession(c.Request.Context(), sub.StripeCustomerID,
		billingReturnURL(c.Request.Context(), req.ReturnURL, "/app/settings/billing"))
	if errX != nil {
		errx.JSON(c, errX)
		return
	}

	c.JSON(http.StatusOK, gin.H{"portal_url": portalURL})
}

// CancelSubscription cancels the current organization's subscription
func (h *Handler) CancelSubscription(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var req struct {
		CancelAtPeriodEnd bool `json:"cancel_at_period_end"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}

	sub, errX := h.SubscriptionService.Get(c.Request.Context(), *orgID)
	if errX != nil {
		errx.JSON(c, errX)
		return
	}

	if sub.StripeSubscriptionID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no active subscription"))
		return
	}

	errX = h.StripeService.CancelSubscription(c.Request.Context(), *sub.StripeSubscriptionID, req.CancelAtPeriodEnd)
	if errX != nil {
		errx.JSON(c, errX)
		return
	}

	subID := sub.ID
	action := models.AuditActionResume
	if req.CancelAtPeriodEnd {
		action = models.AuditActionStop
	}
	h.auditOrg(c, action, models.AuditEntitySubscription, &subID, nil, map[string]string{
		"plan":                 sub.PlanID.String(),
		"cancel_at_period_end": strconv.FormatBool(req.CancelAtPeriodEnd),
	})

	c.JSON(http.StatusOK, gin.H{"message": "subscription cancelled"})
}

// HandleStripeWebhook processes Stripe webhook events
func (h *Handler) HandleStripeWebhook(c *gin.Context) {
	payload, err := io.ReadAll(c.Request.Body)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "failed to read request body"))
		return
	}

	signature := c.GetHeader("Stripe-Signature")
	if signature == "" {
		errx.JSON(c, errx.New(errx.BadRequest, "missing stripe signature"))
		return
	}

	event, errX := h.StripeService.VerifyWebhook(payload, signature)
	if errX != nil {
		errx.JSON(c, errX)
		return
	}

	errX = h.StripeService.ProcessWebhookEvent(c.Request.Context(), event)
	if errX != nil {
		// Return a retryable 5xx so Stripe re-delivers. The event was NOT
		// recorded as processed (see ProcessWebhookEvent), and every handler is
		// idempotent on the event id, so a retry safely re-runs without
		// double-applying. Silently 200-ing here would strand paid-for credits.
		log.Error().Str("request_id", c.GetString("request_id")).Str("event_id", event.ID).Str("detail", errX.Message).Msg("stripe webhook processing failed")
		c.JSON(http.StatusInternalServerError, gin.H{"received": false, "error": "event could not be processed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"received": true})
}

// GetTrialStatus returns the current organization's free trial status
func (h *Handler) GetTrialStatus(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	if h.TrialService == nil {
		errx.JSON(c, errx.New(errx.Internal, "trial service not available"))
		return
	}

	status, errX := h.TrialService.GetTrialStatus(c.Request.Context(), *orgID)
	if errX != nil {
		errx.JSON(c, errX)
		return
	}

	c.JSON(http.StatusOK, status)
}

// GetFeatureStatus returns the current organization's feature access status
func (h *Handler) GetFeatureStatus(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	if h.FeatureGateService == nil {
		errx.JSON(c, errx.New(errx.Internal, "feature gate service not available"))
		return
	}

	status, errX := h.FeatureGateService.GetSubscriptionStatus(c.Request.Context(), *orgID)
	if errX != nil {
		errx.JSON(c, errX)
		return
	}

	// Add additional feature flags
	canSend, _ := h.FeatureGateService.CanSendCampaignEmail(c.Request.Context(), *orgID)
	canWarmup, _ := h.FeatureGateService.CanUseWarmup(c.Request.Context(), *orgID)
	canUnibox, _ := h.FeatureGateService.CanUseUnibox(c.Request.Context(), *orgID)

	c.JSON(http.StatusOK, gin.H{
		"subscription":       status,
		"can_send_campaigns": canSend,
		"can_use_warmup":     canWarmup,
		"can_use_unibox":     canUnibox,
	})
}

// ChangePlan changes the organization's subscription plan with proration
func (h *Handler) ChangePlan(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var req struct {
		PlanID            uuid.UUID `json:"plan_id" binding:"required"`
		ProrationBehavior string    `json:"proration_behavior"` // "create_prorations", "always_invoice", "none"
		DiscountCode      string    `json:"discount_code"`
		Interval          string    `json:"interval"` // "month" or "year"; defaults to monthly
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}

	if xerr := h.authorizePlanPurchase(c, *orgID, "", req.PlanID); xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	updated, errX := h.StripeService.ChangePlan(c.Request.Context(), *orgID, req.PlanID, req.ProrationBehavior, req.DiscountCode, req.Interval)
	if errX != nil {
		errx.JSON(c, errX)
		return
	}

	planID := req.PlanID
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntitySubscription, &planID, nil, map[string]string{
		"action": "change_plan",
		"plan":   req.PlanID.String(),
	})

	c.JSON(http.StatusOK, gin.H{
		"message":      "plan changed successfully",
		"subscription": updated,
	})
}

// PreviewPlanChange previews the proration for a plan change
func (h *Handler) PreviewPlanChange(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	newPlanIDStr := c.Query("new_plan_id")
	if newPlanIDStr == "" {
		errx.JSON(c, errx.New(errx.BadRequest, "new_plan_id is required"))
		return
	}

	newPlanID, err := uuid.Parse(newPlanIDStr)
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid plan ID"))
		return
	}

	preview, errX := h.StripeService.PreviewPlanChange(c.Request.Context(), *orgID, newPlanID)
	if errX != nil {
		errx.JSON(c, errX)
		return
	}

	c.JSON(http.StatusOK, preview)
}

// EnterpriseInquiryRequest represents a request for enterprise pricing
type EnterpriseInquiryRequest struct {
	CompanyName     string `json:"company_name" binding:"required"`
	ContactName     string `json:"contact_name" binding:"required"`
	ContactEmail    string `json:"contact_email" binding:"required,email"`
	EstimatedVolume *int   `json:"estimated_volume,omitempty"`
	TeamSize        *int   `json:"team_size,omitempty"`
	Notes           string `json:"notes,omitempty"`
}

const enterpriseNotesMaxLength = 2000

// SubmitEnterpriseInquiry submits an enterprise pricing inquiry
func (h *Handler) SubmitEnterpriseInquiry(c *gin.Context) {
	var req EnterpriseInquiryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}

	company, xerr := displayname.Validate("Company name", req.CompanyName, displayname.Workspace, false)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	contact, xerr := displayname.Validate("Contact name", req.ContactName, displayname.Person, false)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	if len([]rune(req.Notes)) > enterpriseNotesMaxLength {
		errx.JSON(c, errx.New(errx.BadRequest, fmt.Sprintf("Notes must be %d characters or less.", enterpriseNotesMaxLength)))
		return
	}

	inquiry := &models.EnterpriseInquiry{
		CompanyName:     company,
		ContactName:     contact,
		ContactEmail:    req.ContactEmail,
		EstimatedVolume: req.EstimatedVolume,
		TeamSize:        req.TeamSize,
		Notes:           req.Notes,
	}

	// Get organization ID if available (user might be authenticated)
	orgID := middleware.GetOrganizationID(c)
	if orgID != nil {
		// Could track which organization made the inquiry
		inquiry.Notes = inquiry.Notes + " [Organization: " + orgID.String() + "]"
	}

	created, errX := h.OrganizationService.CreateEnterpriseInquiry(c.Request.Context(), inquiry)
	if errX != nil {
		errx.JSON(c, errX)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":    "Thank you! Our team will contact you within 24 hours.",
		"inquiry_id": created.ID,
	})
}

// authorizePlanPurchase applies the plan offer rule to a price or, when priceID is empty, a plan id.
func (h *Handler) authorizePlanPurchase(c *gin.Context, orgID uuid.UUID, priceID string, planID uuid.UUID) *errx.Error {
	if h.PlanOfferService == nil {
		return errx.New(errx.ServiceUnavailable, "billing is not enabled on this instance")
	}
	if priceID != "" {
		return h.PlanOfferService.AuthorizePrice(c.Request.Context(), orgID, priceID)
	}
	return h.PlanOfferService.AuthorizePlanID(c.Request.Context(), orgID, planID)
}
