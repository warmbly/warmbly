package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
)

// GetWarmupAnalytics gets warmup statistics for the selected organization.
// GET /analytics/warmup
func (h *Handler) GetWarmupAnalytics(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	// Parse optional email_id filter
	var emailAccountID *uuid.UUID
	if emailIDStr := c.Query("email_id"); emailIDStr != "" {
		if id, err := uuid.Parse(emailIDStr); err == nil {
			emailAccountID = &id
		}
	}

	// Parse date range (required)
	fromStr := c.Query("from")
	toStr := c.Query("to")

	if fromStr == "" || toStr == "" {
		errx.Handle(c, errx.New(errx.BadRequest, "from and to date parameters are required"))
		return
	}

	from, err := time.Parse("2006-01-02", fromStr)
	if err != nil {
		errx.Handle(c, errx.New(errx.BadRequest, "Invalid from date format (expected YYYY-MM-DD)"))
		return
	}

	to, err := time.Parse("2006-01-02", toStr)
	if err != nil {
		errx.Handle(c, errx.New(errx.BadRequest, "Invalid to date format (expected YYYY-MM-DD)"))
		return
	}

	analytics, xerr := h.AnalyticsService.GetWarmupAnalytics(c.Request.Context(), *orgID, emailAccountID, from, to)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, analytics)
}

// GetWarmupPlacement reports where warmup mail landed (inbox, category tabs,
// spam) per day and per recipient provider, for one mailbox or the workspace.
// GET /analytics/warmup/placement
func (h *Handler) GetWarmupPlacement(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	var emailAccountID *uuid.UUID
	if raw := c.Query("email_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			errx.Handle(c, errx.New(errx.BadRequest, "email_id must be a UUID"))
			return
		}
		emailAccountID = &id
	}

	now := time.Now().UTC()
	to := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	from := to.AddDate(0, 0, -29)
	if raw := c.Query("from"); raw != "" {
		d, err := time.Parse("2006-01-02", raw)
		if err != nil {
			errx.Handle(c, errx.New(errx.BadRequest, "Invalid from date format (expected YYYY-MM-DD)"))
			return
		}
		from = d
	}
	if raw := c.Query("to"); raw != "" {
		d, err := time.Parse("2006-01-02", raw)
		if err != nil {
			errx.Handle(c, errx.New(errx.BadRequest, "Invalid to date format (expected YYYY-MM-DD)"))
			return
		}
		to = d
	}

	report, xerr := h.AnalyticsService.GetWarmupPlacement(c.Request.Context(), *orgID, emailAccountID, from, to)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, report)
}

// GetCampaignAnalytics gets analytics for a specific campaign
// GET /analytics/campaigns/:id
func (h *Handler) GetCampaignAnalytics(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	campaignIDStr := c.Param("id")
	campaignID, err := uuid.Parse(campaignIDStr)
	if err != nil {
		errx.Handle(c, errx.ErrNotFound)
		return
	}

	analytics, xerr := h.AnalyticsService.GetCampaignAnalytics(c.Request.Context(), *orgID, campaignID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, analytics)
}

// GetCampaignDailyStats gets daily statistics for a campaign
// GET /analytics/campaigns/:id/daily
func (h *Handler) GetCampaignDailyStats(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	campaignIDStr := c.Param("id")
	campaignID, err := uuid.Parse(campaignIDStr)
	if err != nil {
		errx.Handle(c, errx.ErrNotFound)
		return
	}

	// Parse date range
	fromStr := c.Query("from")
	toStr := c.Query("to")

	if fromStr == "" || toStr == "" {
		errx.Handle(c, errx.New(errx.BadRequest, "from and to date parameters are required"))
		return
	}

	from, err := time.Parse("2006-01-02", fromStr)
	if err != nil {
		errx.Handle(c, errx.New(errx.BadRequest, "Invalid from date format (expected YYYY-MM-DD)"))
		return
	}

	to, err := time.Parse("2006-01-02", toStr)
	if err != nil {
		errx.Handle(c, errx.New(errx.BadRequest, "Invalid to date format (expected YYYY-MM-DD)"))
		return
	}

	stats, xerr := h.AnalyticsService.GetCampaignDailyStats(c.Request.Context(), *orgID, campaignID, from, to)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": stats})
}

// GetAllAccountStatuses gets status of all email accounts
// GET /analytics/accounts
func (h *Handler) GetAllAccountStatuses(c *gin.Context) {
	// Account lookups are org-scoped (emailRepo.Search filters on
	// organization_id), so pass the selected org, not the user id.
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	statuses, xerr := h.AnalyticsService.GetAllAccountStatuses(c.Request.Context(), *orgID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": statuses})
}

// GetAccountStatus gets status of a specific email account
// GET /analytics/accounts/:id
func (h *Handler) GetAccountStatus(c *gin.Context) {
	// Org-scoped like the list endpoint above.
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	accountIDStr := c.Param("id")
	accountID, err := uuid.Parse(accountIDStr)
	if err != nil {
		errx.Handle(c, errx.ErrNotFound)
		return
	}

	status, xerr := h.AnalyticsService.GetAccountStatus(c.Request.Context(), *orgID, accountID)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, status)
}

// GetUsageOverview gets usage overview for the selected organization.
// GET /analytics/usage
func (h *Handler) GetUsageOverview(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}
	userIDStr := middleware.GetUserID(c)
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		errx.Handle(c, errx.ErrAuth)
		return
	}

	period := c.DefaultQuery("period", "day")
	if period != "day" && period != "week" && period != "month" {
		period = "day"
	}

	overview, xerr := h.AnalyticsService.GetUsageOverview(c.Request.Context(), *orgID, userID, period)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, overview)
}

// GetRealtimeInfo returns WebSocket connection info
// GET /realtime/info
func (h *Handler) GetRealtimeInfo(c *gin.Context) {
	userIDStr := middleware.GetUserID(c)
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		errx.Handle(c, errx.ErrAuth)
		return
	}

	// Get WebSocket host from request or config
	wsHost := c.Request.Header.Get("X-Forwarded-Host")
	if wsHost == "" {
		wsHost = c.Request.Host
	}
	wsHost = "wss://" + wsHost

	c.JSON(http.StatusOK, gin.H{
		"websocket_url": wsHost + "/socket",
		"topics": []string{
			"user:" + userID.String(),
			"campaign:*",
			"account:*",
		},
	})
}

// GetDashboardAnalytics returns main dashboard analytics overview
// GET /analytics/dashboard?period=7d
func (h *Handler) GetDashboardAnalytics(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	// Parse period (7d, 30d, 90d)
	period := c.DefaultQuery("period", "7d")
	if period != "7d" && period != "30d" && period != "90d" {
		period = "7d"
	}

	analytics, xerr := h.AnalyticsService.GetDashboardAnalytics(c.Request.Context(), *orgID, period)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	// The sidebar meter's denominator: the mailboxes' day under the
	// scheduler's own clamps, not their caps added up. Best effort; the
	// dashboard still renders without it.
	if h.CampaignService != nil {
		if capacity, cerr := h.CampaignService.WorkspaceCapacity(c.Request.Context(), *orgID); cerr == nil {
			analytics.CapacityToday = capacity
		}
	}

	c.JSON(http.StatusOK, analytics)
}

// GetCampaignHourlyStats returns hourly statistics for a campaign on a specific date
// GET /analytics/campaigns/:id/hourly?date=2024-01-15
func (h *Handler) GetCampaignHourlyStats(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	campaignIDStr := c.Param("id")
	campaignID, err := uuid.Parse(campaignIDStr)
	if err != nil {
		errx.Handle(c, errx.ErrNotFound)
		return
	}

	// Parse date
	dateStr := c.Query("date")
	if dateStr == "" {
		dateStr = time.Now().Format("2006-01-02")
	}

	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		errx.Handle(c, errx.New(errx.BadRequest, "Invalid date format (expected YYYY-MM-DD)"))
		return
	}

	stats, xerr := h.AnalyticsService.GetCampaignHourlyStats(c.Request.Context(), *orgID, campaignID, date)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": stats, "date": dateStr})
}

// CompareCampaigns returns comparison statistics for multiple campaigns
// GET /analytics/campaigns/compare?ids=uuid1,uuid2&from=2024-01-01&to=2024-01-31
func (h *Handler) CompareCampaigns(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	// Parse campaign IDs
	idsStr := c.Query("ids")
	if idsStr == "" {
		errx.Handle(c, errx.New(errx.BadRequest, "ids parameter is required"))
		return
	}

	var campaignIDs []uuid.UUID
	for _, idStr := range splitAndTrim(idsStr) {
		id, err := uuid.Parse(idStr)
		if err != nil {
			continue
		}
		campaignIDs = append(campaignIDs, id)
	}

	if len(campaignIDs) == 0 {
		errx.Handle(c, errx.New(errx.BadRequest, "at least one valid campaign ID is required"))
		return
	}

	// Limit to 10 campaigns
	if len(campaignIDs) > 10 {
		campaignIDs = campaignIDs[:10]
	}

	// Parse date range
	fromStr := c.Query("from")
	toStr := c.Query("to")

	if fromStr == "" || toStr == "" {
		errx.Handle(c, errx.New(errx.BadRequest, "from and to date parameters are required"))
		return
	}

	from, err := time.Parse("2006-01-02", fromStr)
	if err != nil {
		errx.Handle(c, errx.New(errx.BadRequest, "Invalid from date format (expected YYYY-MM-DD)"))
		return
	}

	to, err := time.Parse("2006-01-02", toStr)
	if err != nil {
		errx.Handle(c, errx.New(errx.BadRequest, "Invalid to date format (expected YYYY-MM-DD)"))
		return
	}

	comparison, xerr := h.AnalyticsService.CompareCampaigns(c.Request.Context(), *orgID, campaignIDs, from, to)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, comparison)
}

// splitAndTrim splits a comma-separated string and trims whitespace
func splitAndTrim(s string) []string {
	if s == "" {
		return nil
	}
	parts := make([]string, 0)
	for _, part := range split(s, ',') {
		trimmed := trim(part)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return parts
}

func split(s string, sep rune) []string {
	var result []string
	current := ""
	for _, c := range s {
		if c == sep {
			result = append(result, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	result = append(result, current)
	return result
}

func trim(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// GetDirectMailAnalytics returns the hand-written-mail overview: volume and
// replies from the synced mailbox, plus opens and clicks for the mailboxes that
// opted into tracking them.
// GET /analytics/direct?period=7d
func (h *Handler) GetDirectMailAnalytics(c *gin.Context) {
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.Handle(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	period := c.DefaultQuery("period", "7d")
	if period != "7d" && period != "30d" && period != "90d" {
		period = "7d"
	}

	analytics, xerr := h.AnalyticsService.GetDirectMailAnalytics(c.Request.Context(), *orgID, period)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, analytics)
}
