package handler

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
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
		PriceID    string `json:"price_id" binding:"required"`
		SuccessURL string `json:"success_url" binding:"required"`
		CancelURL  string `json:"cancel_url" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	session, errX := h.StripeService.CreateCheckoutSession(c.Request.Context(), uid, *orgID, req.PriceID, req.SuccessURL, req.CancelURL)
	if errX != nil {
		errx.JSON(c, errX)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"session_id":  session.ID,
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
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	sub, errX := h.SubscriptionService.Get(c.Request.Context(), *orgID)
	if errX != nil {
		errx.JSON(c, errX)
		return
	}

	portalURL, errX := h.StripeService.CreatePortalSession(c.Request.Context(), sub.StripeCustomerID, req.ReturnURL)
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
	c.ShouldBindJSON(&req)

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
		// Log but don't fail - Stripe will retry
		c.JSON(http.StatusOK, gin.H{"received": true, "error": errX.Message})
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
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	updated, errX := h.StripeService.ChangePlan(c.Request.Context(), *orgID, req.PlanID, req.ProrationBehavior)
	if errX != nil {
		errx.JSON(c, errX)
		return
	}

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

// SubmitEnterpriseInquiry submits an enterprise pricing inquiry
func (h *Handler) SubmitEnterpriseInquiry(c *gin.Context) {
	var req EnterpriseInquiryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	inquiry := &models.EnterpriseInquiry{
		CompanyName:     req.CompanyName,
		ContactName:     req.ContactName,
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
