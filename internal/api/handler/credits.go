// Balance endpoints: read the workspace balance, add money (one click on the
// saved card, or Stripe Checkout), save a card, automatic reload, spend
// controls, the activity log and invoices. All are JWT-gated on manage_billing
// (see routes.go), and every call that charges a card or changes what is
// charged takes a fresh sign-in. Checkout fulfillment happens only in the
// Stripe webhook.
package handler

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/balance"
	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

// usd rounds a dollar figure to the micro-dollar for JSON.
func usd(m models.Micro) float64 { return math.Round(m.USD()*1e6) / 1e6 }

func usdPtr(m *models.Micro) *float64 {
	if m == nil {
		return nil
	}
	v := usd(*m)
	return &v
}

// limitFromBody reads a spend limit sent in dollars, or in legacy credits by
// a client from before dollars. Nil means no limit.
func limitFromBody(dollars *float64, legacyCredits *int) *models.Micro {
	switch {
	case dollars != nil:
		v := models.USDToMicro(*dollars)
		return &v
	case legacyCredits != nil:
		v := models.Micro(*legacyCredits) * models.LegacyCreditMicro
		return &v
	}
	return nil
}

func (h *Handler) billingOrg(c *gin.Context) (uuid.UUID, bool) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return uuid.Nil, false
	}
	if h.CreditService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "the balance is not available"))
		return uuid.Nil, false
	}
	return *orgID, true
}

// planAllowance is the plan's monthly included usage and the next reset.
// Best-effort: an org with no subscription reads as no allowance.
func (h *Handler) planAllowance(c *gin.Context, orgID uuid.UUID) (models.Micro, *time.Time) {
	var allowance models.Micro
	var next *time.Time
	if h.FeatureGateService != nil {
		if status, err := h.FeatureGateService.GetSubscriptionStatus(c.Request.Context(), orgID); err == nil && status != nil && status.Plan != nil {
			allowance = status.Plan.Allowance()
		}
	}
	if h.SubscriptionService != nil {
		if sub, err := h.SubscriptionService.Get(c.Request.Context(), orgID); err == nil && sub != nil {
			next = sub.CurrentPeriodEnd
		}
	}
	return allowance, next
}

// GetCreditBalance — GET /subscription/credits
// Both pools, the plan's monthly allowance and the next reset, in dollars
// (*_usd) and in legacy credits for clients from before dollars.
func (h *Handler) GetCreditBalance(c *gin.Context) {
	orgID, ok := h.billingOrg(c)
	if !ok {
		return
	}
	if h.CreditService.Unmetered() {
		c.JSON(http.StatusOK, gin.H{
			"unlimited": true, "balance": 0, "monthly_balance": 0, "purchased_balance": 0,
			"monthly_allowance": 0, "total_purchased": 0, "monthly_reset_at": nil, "next_reset_at": nil,
			"balance_usd": 0, "monthly_usd": 0, "purchased_usd": 0, "monthly_allowance_usd": 0,
		})
		return
	}
	ledger, xerr := h.CreditService.GetLedger(c.Request.Context(), orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	allowance, next := h.planAllowance(c, orgID)
	c.JSON(http.StatusOK, gin.H{
		"unlimited":             false,
		"balance":               models.CreditsFromMicro(ledger.Total()),
		"monthly_balance":       models.CreditsFromMicro(ledger.Monthly),
		"purchased_balance":     models.CreditsFromMicro(ledger.Purchased),
		"monthly_allowance":     models.CreditsFromMicro(allowance),
		"total_purchased":       models.CreditsFromMicro(ledger.TotalPurchased),
		"monthly_reset_at":      ledger.MonthResetAt,
		"next_reset_at":         next,
		"balance_usd":           usd(ledger.Total()),
		"monthly_usd":           usd(ledger.Monthly),
		"purchased_usd":         usd(ledger.Purchased),
		"monthly_allowance_usd": usd(allowance),
		"total_purchased_usd":   usd(ledger.TotalPurchased),
	})
}

// GetBalance — GET /subscription/balance
// Everything the balance page shows: what is left, the allowance, the saved
// card, automatic reload and its state, and the top-up bounds.
func (h *Handler) GetBalance(c *gin.Context) {
	orgID, ok := h.billingOrg(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if h.CreditService.Unmetered() {
		c.JSON(http.StatusOK, gin.H{"unlimited": true})
		return
	}
	ledger, xerr := h.CreditService.GetLedger(ctx, orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	allowance, next := h.planAllowance(c, orgID)
	canAdd := false
	if h.FeatureGateService != nil {
		canAdd, _ = h.FeatureGateService.IsPaidOrganization(ctx, orgID)
	}
	out := gin.H{
		"unlimited":             false,
		"can_add_money":         canAdd,
		"balance_usd":           usd(ledger.Total()),
		"monthly_usd":           usd(ledger.Monthly),
		"purchased_usd":         usd(ledger.Purchased),
		"monthly_allowance_usd": usd(allowance),
		"monthly_used_usd":      usd(max(allowance-ledger.Monthly, 0)),
		"next_reset_at":         next,
		"top_up": gin.H{
			"min_usd": usd(credits.TopUpMin), "max_usd": usd(credits.TopUpMax),
			"presets_usd": []int{10, 25, 50, 100, 250}, "default_usd": 25,
		},
	}
	if h.BalanceService != nil {
		st, err := h.BalanceService.State(ctx, orgID)
		if err != nil {
			errx.JSON(c, errx.New(errx.Internal, "failed to read the balance"))
			return
		}
		cfg := st.Settings
		reload := gin.H{
			"enabled":                 cfg.AutoReloadEnabled,
			"threshold_usd":           usd(cfg.AutoReloadThreshold),
			"amount_usd":              usd(cfg.AutoReloadAmount),
			"monthly_limit_usd":       usd(cfg.AutoReloadMonthlyMax),
			"reloaded_this_month_usd": usd(st.ReloadedThisMonth),
			"failure_code":            nilIfEmpty(cfg.AutoReloadFailureCode),
			"failed_at":               cfg.AutoReloadFailedAt,
			"last_reload":             nil,
			"retry_at":                nil,
			"retry_reason":            nil,
		}
		if st.LastReload != nil {
			reload["last_reload"] = gin.H{"amount_usd": usd(st.LastReload.Amount), "at": st.LastReload.CompletedAt}
		}
		if st.Pending != nil && st.Pending.RetryAt != nil {
			reload["retry_at"], reload["retry_reason"] = st.Pending.RetryAt, nilIfEmpty(st.Pending.FailureCode)
		}
		out["auto_reload"] = reload
		out["low_balance_usd"] = usd(cfg.LowBalanceThreshold)
		out["payment_method"] = st.PaymentMethod
		out["charge_in_progress"] = st.Pending != nil && st.Pending.RetryAt == nil
	}
	c.JSON(http.StatusOK, out)
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func readAmount(c *gin.Context) (models.Micro, bool) {
	var body struct {
		AmountUSD float64 `json:"amount_usd" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return 0, false
	}
	// Whole cents: a card is charged in cents.
	amount := models.MicroFromCents(int64(math.Round(body.AmountUSD * 100)))
	if amount < credits.TopUpMin || amount > credits.TopUpMax {
		errx.JSON(c, errx.New(errx.BadRequest, "Enter an amount from $5 to $5,000."))
		return 0, false
	}
	return amount, true
}

func (h *Handler) requirePaidForBalance(c *gin.Context, orgID uuid.UUID) bool {
	paid, xerr := h.FeatureGateService.IsPaidOrganization(c.Request.Context(), orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return false
	}
	if !paid {
		errx.JSON(c, errx.New(errx.Forbidden, "Adding money needs an active paid plan."))
		return false
	}
	return true
}

// TopUpBalance — POST /subscription/balance/top-up {amount_usd}
// Charges the saved card now: 200 paid, or 202 processing while the payment
// is still going through. 402 card_declined or authentication_required tells
// the dashboard to offer Checkout instead; 409 payment_method_required when
// there is no card.
func (h *Handler) TopUpBalance(c *gin.Context) {
	orgID, ok := h.billingOrg(c)
	if !ok || !h.requirePaidForBalance(c, orgID) {
		return
	}
	amount, ok := readAmount(c)
	if !ok {
		return
	}
	if h.BalanceService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "card payments are not available"))
		return
	}
	res, err := h.BalanceService.TopUp(c.Request.Context(), orgID, amount)
	var declined *balance.DeclinedError
	switch {
	case errors.As(err, &declined):
		c.JSON(http.StatusPaymentRequired, gin.H{
			"code": declined.Code, "request_id": c.GetString("request_id"),
			"message": "The card was not charged. Try another way to pay.",
		})
		return
	case errors.Is(err, balance.ErrNoPaymentMethod):
		c.JSON(http.StatusConflict, gin.H{"code": "payment_method_required", "request_id": c.GetString("request_id"), "message": "Add a card first."})
		return
	case errors.Is(err, balance.ErrChargeInFlight):
		c.JSON(http.StatusConflict, gin.H{"code": "charge_in_progress", "request_id": c.GetString("request_id"), "message": "Another payment is still going through. Try again in a minute."})
		return
	case err != nil:
		errx.JSON(c, errx.New(errx.Internal, "failed to charge the card"))
		return
	}
	// Settling the charge already logged it on the audit spine.
	bal, _ := h.CreditService.GetBalance(c.Request.Context(), orgID)
	status := http.StatusOK
	if res.Status != "paid" {
		status = http.StatusAccepted
	}
	c.JSON(status, gin.H{
		"status": res.Status, "amount_usd": usd(res.Amount), "balance_usd": usd(bal),
		"charge_in_progress": res.Status != "paid",
	})
}

// CreateBalanceCheckout — POST /subscription/balance/checkout {amount_usd}
// Stripe Checkout for the amount; it saves the card for next time.
func (h *Handler) CreateBalanceCheckout(c *gin.Context) {
	uid, err := uuid.Parse(c.GetString("user_id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.Unauthorized, "invalid user"))
		return
	}
	orgID, ok := h.billingOrg(c)
	if !ok || !h.requirePaidForBalance(c, orgID) {
		return
	}
	amount, ok := readAmount(c)
	if !ok {
		return
	}
	sess, xerr := h.StripeService.CreateBalanceCheckout(c.Request.Context(), uid, orgID, amount,
		billingReturnURL(c.Request.Context(), "", "/app/settings/billing/balance?top_up=success"),
		billingReturnURL(c.Request.Context(), "", "/app/settings/billing/balance?top_up=canceled"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"session_id": sess.ID, "checkout_url": sess.URL})
}

// CreatePaymentMethodSetup — POST /subscription/balance/payment-method
// Stripe Checkout in setup mode, to save or replace the card.
func (h *Handler) CreatePaymentMethodSetup(c *gin.Context) {
	uid, err := uuid.Parse(c.GetString("user_id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.Unauthorized, "invalid user"))
		return
	}
	orgID, ok := h.billingOrg(c)
	if !ok {
		return
	}
	sess, xerr := h.StripeService.CreateCardSetupCheckout(c.Request.Context(), uid, orgID,
		billingReturnURL(c.Request.Context(), "", "/app/settings/billing/balance?payment_method=success"),
		billingReturnURL(c.Request.Context(), "", "/app/settings/billing/balance?payment_method=canceled"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"session_id": sess.ID, "checkout_url": sess.URL})
}

// UpdateAutoReload — PATCH /subscription/balance/auto-reload
// {enabled, threshold_usd, amount_usd, monthly_limit_usd}. The amount must be
// above the threshold and the monthly limit at least the amount.
func (h *Handler) UpdateAutoReload(c *gin.Context) {
	orgID, ok := h.billingOrg(c)
	if !ok {
		return
	}
	var body struct {
		Enabled         bool    `json:"enabled"`
		ThresholdUSD    float64 `json:"threshold_usd"`
		AmountUSD       float64 `json:"amount_usd"`
		MonthlyLimitUSD float64 `json:"monthly_limit_usd"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	cents := func(v float64) models.Micro { return models.MicroFromCents(int64(math.Round(v * 100))) }
	threshold, amount, limit := cents(body.ThresholdUSD), cents(body.AmountUSD), cents(body.MonthlyLimitUSD)
	switch {
	case threshold < credits.ReloadThresholdMin || threshold > credits.ReloadThresholdMax:
		errx.JSON(c, errx.New(errx.BadRequest, "Reload when the balance falls below $0.01 to $1,000."))
		return
	case amount < credits.TopUpMin || amount > credits.TopUpMax:
		errx.JSON(c, errx.New(errx.BadRequest, "Reload amounts run from $5 to $5,000."))
		return
	case amount <= threshold:
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "recharge_too_small", "request_id": c.GetString("request_id"), "message": "The reload amount has to be more than the balance it reloads at."})
		return
	case limit < amount || limit > credits.ReloadMonthlyLimitMax:
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "monthly_limit_too_small", "request_id": c.GetString("request_id"), "message": "The monthly limit has to be at least one reload and at most $50,000."})
		return
	}
	if h.BalanceService == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "automatic reload is not available"))
		return
	}
	if body.Enabled && !h.requirePaidForBalance(c, orgID) {
		return
	}
	if _, err := h.BalanceService.SetAutoReload(c.Request.Context(), orgID, body.Enabled, threshold, amount, limit); err != nil {
		if errors.Is(err, balance.ErrNoPaymentMethod) {
			c.JSON(http.StatusConflict, gin.H{"code": "payment_method_required", "request_id": c.GetString("request_id"), "message": "Add a card to turn on automatic reload."})
			return
		}
		errx.JSON(c, errx.New(errx.Internal, "failed to save automatic reload"))
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityCreditGrant, nil, nil, map[string]string{
		"kind": "auto_reload", "enabled": strconv.FormatBool(body.Enabled),
	})
	h.GetBalance(c)
}

// ListBalanceInvoices — GET /subscription/balance/invoices
// The latest Stripe invoices, subscription and balance alike.
func (h *Handler) ListBalanceInvoices(c *gin.Context) {
	orgID, ok := h.billingOrg(c)
	if !ok {
		return
	}
	customerID := ""
	if h.SubscriptionService != nil {
		if sub, err := h.SubscriptionService.Get(c.Request.Context(), orgID); err == nil && sub != nil {
			customerID = sub.StripeCustomerID
		}
	}
	invoices, err := h.StripeService.ListInvoices(c.Request.Context(), customerID, 24)
	if err != nil {
		errx.JSON(c, errx.New(errx.Internal, "failed to list invoices"))
		return
	}
	if len(invoices) == 0 {
		c.JSON(http.StatusOK, gin.H{"data": []any{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": invoices})
}

// creditTxnJSON is one activity row: dollars, plus the legacy credit figures.
func creditTxnJSON(t models.CreditTransaction) gin.H {
	return gin.H{
		"id": t.ID, "org_id": t.OrgID, "reason": t.Reason, "model_used": t.ModelUsed,
		"amount_usd": usd(t.Amount), "cost_usd": usd(t.Cost),
		"input_tokens": t.InputTokens, "output_tokens": t.OutputTokens, "quantity": t.Quantity,
		"balance_after_usd":   usd(t.MonthlyAfter + t.PurchasedAfter),
		"monthly_after_usd":   usd(t.MonthlyAfter),
		"purchased_after_usd": usd(t.PurchasedAfter),
		"purchased_delta_usd": usd(t.PurchasedDelta),
		"actor_user_id":       t.ActorUserID, "context": t.Context, "created_at": t.CreatedAt,
		"amount":                  int(math.Round(float64(t.Amount) / float64(models.LegacyCreditMicro))),
		"tokens_used":             t.InputTokens + t.OutputTokens,
		"balance_after":           models.CreditsFromMicro(t.MonthlyAfter),
		"purchased_delta":         int(math.Round(float64(t.PurchasedDelta) / float64(models.LegacyCreditMicro))),
		"purchased_balance_after": models.CreditsFromMicro(t.PurchasedAfter),
	}
}

// ListCreditTransactions — GET /subscription/credits/transactions
// Keyset-paginated (opaque cursor), newest first. Invalid cursor/limit -> 400.
func (h *Handler) ListCreditTransactions(c *gin.Context) {
	orgID, ok := h.billingOrg(c)
	if !ok {
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
	// One extra row tells whether a next page exists.
	txns, xerr := h.CreditService.ListTransactionsBefore(c.Request.Context(), orgID, limit+1, beforeAt, beforeID)
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
	rows := make([]gin.H, 0, len(txns))
	for _, t := range txns {
		rows = append(rows, creditTxnJSON(t))
	}
	c.JSON(http.StatusOK, gin.H{
		"data":       rows,
		"pagination": gin.H{"next_cursor": nextCursor, "has_more": nextCursor != nil},
	})
}

// GetCreditUsage — GET /subscription/credits/usage?days=30
// Spend per window against the limits, a daily series, and breakdowns by
// feature and model over the window (1-90 days).
func (h *Handler) GetCreditUsage(c *gin.Context) {
	orgID, ok := h.billingOrg(c)
	if !ok {
		return
	}
	days := 30
	if raw := c.Query("days"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 90 {
			errx.JSON(c, errx.New(errx.BadRequest, "days must be 1-90"))
			return
		}
		days = n
	}
	o, xerr := h.CreditService.GetUsageOverview(c.Request.Context(), orgID, days)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	series := make([]gin.H, 0, len(o.Series))
	for _, p := range o.Series {
		series = append(series, gin.H{"date": p.Date, "amount_usd": usd(p.Amount), "tokens": p.Tokens, "credits": models.CreditsFromMicro(p.Amount)})
	}
	buckets := func(in []models.CreditUsageBucket) []gin.H {
		out := make([]gin.H, 0, len(in))
		for _, b := range in {
			out = append(out, gin.H{"key": b.Key, "amount_usd": usd(b.Amount), "tokens": b.Tokens, "count": b.Count, "credits": models.CreditsFromMicro(b.Amount)})
		}
		return out
	}
	c.JSON(http.StatusOK, gin.H{
		"spent_today_usd": usd(o.SpentToday), "spent_week_usd": usd(o.SpentWeek), "spent_month_usd": usd(o.SpentMonth),
		"limit_daily_usd": usdPtr(o.LimitDaily), "limit_weekly_usd": usdPtr(o.LimitWeekly), "limit_monthly_usd": usdPtr(o.LimitMonthly),
		"spent_today": models.CreditsFromMicro(o.SpentToday), "spent_week": models.CreditsFromMicro(o.SpentWeek), "spent_month": models.CreditsFromMicro(o.SpentMonth),
		"series": series, "by_reason": buckets(o.ByReason), "by_model": buckets(o.ByModel),
	})
}

func spendSettingsJSON(cfg *models.AISpendSettings) gin.H {
	return gin.H{
		"spend_limit_daily_usd":    usdPtr(cfg.SpendLimitDaily),
		"spend_limit_weekly_usd":   usdPtr(cfg.SpendLimitWeekly),
		"spend_limit_monthly_usd":  usdPtr(cfg.SpendLimitMonthly),
		"member_limit_daily_usd":   usdPtr(cfg.MemberLimitDaily),
		"member_limit_weekly_usd":  usdPtr(cfg.MemberLimitWeekly),
		"member_limit_monthly_usd": usdPtr(cfg.MemberLimitMonthly),
		"low_balance_usd":          usd(cfg.LowBalanceThreshold),
		"auto_reload_enabled":      cfg.AutoReloadEnabled,
	}
}

// GetCreditSettings — GET /subscription/credits/settings
// The workspace's spend limits and low-balance notice, in dollars.
func (h *Handler) GetCreditSettings(c *gin.Context) {
	orgID, ok := h.billingOrg(c)
	if !ok {
		return
	}
	cfg, xerr := h.CreditService.GetSpendSettings(c.Request.Context(), orgID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, spendSettingsJSON(cfg))
}

// UpdateCreditSettings — PATCH /subscription/credits/settings
// Saves the spend limits and the low-balance notice. An absent limit removes
// it. A client from before dollars sends credits, read at the credit's value.
func (h *Handler) UpdateCreditSettings(c *gin.Context) {
	orgID, ok := h.billingOrg(c)
	if !ok {
		return
	}
	var body struct {
		SpendLimitDailyUSD    *float64 `json:"spend_limit_daily_usd"`
		SpendLimitWeeklyUSD   *float64 `json:"spend_limit_weekly_usd"`
		SpendLimitMonthlyUSD  *float64 `json:"spend_limit_monthly_usd"`
		MemberLimitDailyUSD   *float64 `json:"member_limit_daily_usd"`
		MemberLimitWeeklyUSD  *float64 `json:"member_limit_weekly_usd"`
		MemberLimitMonthlyUSD *float64 `json:"member_limit_monthly_usd"`
		LowBalanceUSD         *float64 `json:"low_balance_usd"`

		SpendLimitDaily     *int `json:"spend_limit_daily"`
		SpendLimitWeekly    *int `json:"spend_limit_weekly"`
		SpendLimitMonthly   *int `json:"spend_limit_monthly"`
		MemberLimitDaily    *int `json:"member_limit_daily"`
		MemberLimitWeekly   *int `json:"member_limit_weekly"`
		MemberLimitMonthly  *int `json:"member_limit_monthly"`
		LowBalanceThreshold *int `json:"low_balance_threshold"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		errx.JSON(c, errx.InvalidBody(err))
		return
	}
	low := credits.DefaultLowBalance
	if v := limitFromBody(body.LowBalanceUSD, body.LowBalanceThreshold); v != nil {
		low = *v
	}
	cfg, xerr := h.CreditService.UpdateSpendSettings(c.Request.Context(), orgID, &models.AISpendSettings{
		SpendLimitDaily:     limitFromBody(body.SpendLimitDailyUSD, body.SpendLimitDaily),
		SpendLimitWeekly:    limitFromBody(body.SpendLimitWeeklyUSD, body.SpendLimitWeekly),
		SpendLimitMonthly:   limitFromBody(body.SpendLimitMonthlyUSD, body.SpendLimitMonthly),
		MemberLimitDaily:    limitFromBody(body.MemberLimitDailyUSD, body.MemberLimitDaily),
		MemberLimitWeekly:   limitFromBody(body.MemberLimitWeeklyUSD, body.MemberLimitWeekly),
		MemberLimitMonthly:  limitFromBody(body.MemberLimitMonthlyUSD, body.MemberLimitMonthly),
		LowBalanceThreshold: low,
	})
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	h.auditOrg(c, models.AuditActionUpdate, models.AuditEntityCreditGrant, nil, nil, map[string]string{"kind": "spend_settings"})
	c.JSON(http.StatusOK, spendSettingsJSON(cfg))
}

// parseCreditLimit parses an optional ?limit into [1, max], defaulting to def.
// A non-numeric or out-of-range value is a 400, never silently clamped.
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
