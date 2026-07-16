// Billing/credits endpoints: read the org's AI-credit ledger, start a top-up
// checkout, and page the transaction log. All are JWT-gated on manage_billing
// (see routes.go). Fulfillment of a purchase happens only in the Stripe
// webhook, never here.
package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

// GetCreditBalance — GET /subscription/credits
// Returns both credit pools, the plan's monthly allowance, and the next reset.
func (h *Handler) GetCreditBalance(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	if h.CreditService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "credits are not available"))
		return
	}

	ledger, xerr := h.CreditService.GetLedger(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	// Monthly allowance (per-cycle grant) and next reset come from the plan +
	// subscription. Best-effort: an org with no subscription still gets a valid
	// ledger view (allowance 0, no reset date).
	monthlyAllowance := 0
	var nextResetAt *time.Time
	if h.FeatureGateService != nil {
		if status, serr := h.FeatureGateService.GetSubscriptionStatus(c.Request.Context(), *orgID); serr == nil && status != nil && status.Plan != nil {
			monthlyAllowance = status.Plan.MonthlyCredits
		}
	}
	if h.SubscriptionService != nil {
		if sub, serr := h.SubscriptionService.Get(c.Request.Context(), *orgID); serr == nil && sub != nil {
			nextResetAt = sub.CurrentPeriodEnd
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"balance":           ledger.Total(),
		"monthly_balance":   ledger.Balance,
		"purchased_balance": ledger.PurchasedBalance,
		"monthly_allowance": monthlyAllowance,
		"total_purchased":   ledger.TotalPurchased,
		"monthly_reset_at":  ledger.MonthResetAt,
		"next_reset_at":     nextResetAt,
		"packs":             credits.CreditPacks,
	})
}

// CreateCreditCheckoutSession — POST /subscription/credits/checkout {pack}
// Paid orgs only. Starts a one-time Stripe Checkout for a top-up pack.
func (h *Handler) CreateCreditCheckoutSession(c *gin.Context) {
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
		Pack       string `json:"pack" binding:"required"`
		SuccessURL string `json:"success_url" binding:"required"`
		CancelURL  string `json:"cancel_url" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	pack := credits.PackByKey(req.Pack)
	if pack == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "unknown credit pack"))
		return
	}

	// Top-ups are a paid-org feature.
	paid, xerr := h.FeatureGateService.IsPaidOrganization(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	if !paid {
		errx.JSON(c, errx.New(errx.Forbidden, "Credit top-ups require an active paid plan."))
		return
	}

	session, xerr := h.StripeService.CreateCreditCheckoutSession(
		c.Request.Context(), uid, *orgID, pack.Key, pack.Credits, req.SuccessURL, req.CancelURL,
	)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"session_id":   session.ID,
		"checkout_url": session.URL,
	})
}

// ListCreditTransactions — GET /subscription/credits/transactions
// Keyset-paginated (opaque cursor), newest first. Invalid cursor/limit -> 400.
func (h *Handler) ListCreditTransactions(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	if h.CreditService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "credits are not available"))
		return
	}

	limit, xerr := parseCreditLimit(c.Query("limit"), 25, 100)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	beforeAt, beforeID, xerr := paging.DecodeTimeCursor(c.Query("cursor"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	// Fetch one extra row to know whether a next page exists.
	txns, xerr := h.CreditService.ListTransactionsBefore(c.Request.Context(), *orgID, limit+1, beforeAt, beforeID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	var nextCursor *string
	if len(txns) > limit {
		last := txns[limit-1]
		nextCursor = paging.EncodeTime(last.CreatedAt, last.ID)
		txns = txns[:limit]
	}

	c.JSON(http.StatusOK, gin.H{
		"data": txns,
		"pagination": gin.H{
			"next_cursor": nextCursor,
			"has_more":    nextCursor != nil,
		},
	})
}

// parseCreditLimit parses an optional ?limit into [1, max], defaulting to def.
// A non-numeric or out-of-range value is a 400 (never silently clamped from
// garbage), matching the public-API list contract.
func parseCreditLimit(s string, def, max int) (int, *errx.Error) {
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > max {
		return 0, errx.New(errx.BadRequest, "invalid limit")
	}
	return n, nil
}
