package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
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
		} else {
			errx.Handle(c, errx.New(errx.BadRequest, "email_id must be a UUID"))
			return
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
	if to.Before(from) {
		errx.Handle(c, errx.New(errx.BadRequest, "from must be on or before to"))
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
	if raw := c.Query("to"); raw != "" {
		d, err := time.Parse("2006-01-02", raw)
		if err != nil {
			errx.Handle(c, errx.New(errx.BadRequest, "Invalid to date format (expected YYYY-MM-DD)"))
			return
		}
		to = d
	}
	// The default window is the 30 days ending on to, whichever to is.
	from := to.AddDate(0, 0, -29)
	if raw := c.Query("from"); raw != "" {
		d, err := time.Parse("2006-01-02", raw)
		if err != nil {
			errx.Handle(c, errx.New(errx.BadRequest, "Invalid from date format (expected YYYY-MM-DD)"))
			return
		}
		from = d
	}

	report, xerr := h.AnalyticsService.GetWarmupPlacement(c.Request.Context(), *orgID, emailAccountID, from, to)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	c.JSON(http.StatusOK, report)
}

// GetCampaignAnalytics gets analytics for a specific campaign
// GET /analytics/campaigns/:id[?from=YYYY-MM-DD&to=YYYY-MM-DD]
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

	period, xerr := optionalDayRange(c)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	analytics, xerr := h.AnalyticsService.GetCampaignAnalytics(c.Request.Context(), *orgID, campaignID, period)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, analytics)
}

// optionalDayRange reads from and to as whole days, both or neither; neither
// is nil, which callers read as all time.
func optionalDayRange(c *gin.Context) (*models.DateRange, *errx.Error) {
	period, err := models.ParseDayRange(c.Query("from"), c.Query("to"))
	if err != nil {
		return nil, errx.New(errx.BadRequest, err.Error())
	}
	return period, nil
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

// GetAllAccountStatuses lists email account statuses, one bounded page at a
// time. A page view asks only for the mailbox ids it shows (email_ids);
// otherwise the opaque cursor walks the whole inventory. The per-mailbox reads
// are batched across the page, so a page's cost does not grow with the total
// inventory, and the overflow beyond the old 1000-row cap is reachable through
// next_cursor instead of being silently dropped.
// GET /analytics/accounts
func (h *Handler) GetAllAccountStatuses(c *gin.Context) {
	// Account lookups are org-scoped (emailRepo.Search filters on
	// organization_id), so pass the selected org, not the user id.
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		errx.JSON(c, errx.New(errx.BadRequest, "no organization selected"))
		return
	}

	// email_ids scopes the page to a visible set; invalid or over-limit is a
	// 400, never a silently truncated or ignored filter.
	var emailIDs []uuid.UUID
	if raw := strings.TrimSpace(c.Query("email_ids")); raw != "" {
		parts := strings.Split(raw, ",")
		if len(parts) > config.AccountStatusMaxIDs {
			errx.JSON(c, errx.New(errx.BadRequest, "too many email_ids"))
			return
		}
		emailIDs = make([]uuid.UUID, 0, len(parts))
		for _, p := range parts {
			id, err := uuid.Parse(strings.TrimSpace(p))
			if err != nil {
				errx.JSON(c, errx.New(errx.BadRequest, "invalid email_ids"))
				return
			}
			emailIDs = append(emailIDs, id)
		}
	}

	limit := config.AccountStatusLimitDefault
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > config.AccountStatusLimitMax {
			errx.JSON(c, errx.New(errx.BadRequest, "invalid limit"))
			return
		}
		limit = n
	}

	emailIDs, scopeErr := allowedMailboxFilter(c, emailIDs)
	if scopeErr != nil {
		errx.JSON(c, scopeErr)
		return
	}

	result, xerr := h.AnalyticsService.GetAccountStatusesPage(c.Request.Context(), *orgID, emailIDs, c.Query("cursor"), int32(limit))
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": result.Data, "pagination": result.Pagination})
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

	status, xerr := h.AnalyticsService.GetAccountStatusDetail(c.Request.Context(), *orgID, accountID)
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

// maxDashboardScopeIDs bounds each of campaign_ids and folder_ids; a folder
// stands in for any number of campaigns, so a larger set is never needed.
const maxDashboardScopeIDs = 100

// GetDashboardAnalytics returns main dashboard analytics overview
// GET /analytics/dashboard?period=7d[&campaign_ids=a,b][&folder_ids=c]
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

	var filter models.DashboardFilter
	var xerr *errx.Error
	if filter.CampaignIDs, xerr = uuidListQuery(c, "campaign_ids", maxDashboardScopeIDs); xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	if filter.FolderIDs, xerr = uuidListQuery(c, "folder_ids", maxDashboardScopeIDs); xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	restricted := middleware.IsScopeRestricted(c)
	if restricted {
		// The filter's echo names what it asked for, so it may only ask for granted ones.
		if !idsWithin(filter.CampaignIDs, middleware.AllowedCampaigns(c)) || !idsWithin(filter.FolderIDs, middleware.AllowedFolders(c)) {
			errx.Handle(c, errx.New(errx.NotFound, "campaign or folder not found"))
			return
		}
		filter.AllowedCampaigns = middleware.AllowedCampaigns(c)
		filter.AllowedMailboxes = middleware.AllowedEmailAccounts(c)
	}

	analytics, xerr := h.AnalyticsService.GetDashboardAnalytics(c.Request.Context(), *orgID, period, filter)
	if xerr != nil {
		errx.Handle(c, xerr)
		return
	}
	if restricted {
		// A sender outside the member's mailboxes is not named to them.
		for i := range analytics.RecentActivity {
			if a := &analytics.RecentActivity[i]; a.SenderID != nil && !middleware.EmailAccountAllowed(c, *a.SenderID) {
				a.SenderID, a.SenderEmail = nil, ""
			}
		}
	}
	// The sidebar meter's denominator: the mailboxes' day under the
	// scheduler's own clamps, not their caps added up. Best effort; the
	// dashboard still renders without it. It is the workspace's, so a
	// restricted member is not given it.
	if h.CampaignService != nil && !restricted {
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
	if !idsWithin(campaignIDs, middleware.AllowedCampaigns(c)) {
		errx.Handle(c, errx.New(errx.NotFound, "campaign not found"))
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

// uuidListQuery reads a comma-separated or repeated query parameter of
// UUIDs, deduplicated in order. A malformed id or more than max is a 400.
func uuidListQuery(c *gin.Context, key string, max int) ([]uuid.UUID, *errx.Error) {
	var ids []uuid.UUID
	seen := make(map[uuid.UUID]struct{})
	for _, raw := range c.QueryArray(key) {
		for _, part := range splitAndTrim(raw) {
			id, err := uuid.Parse(part)
			if err != nil {
				return nil, errx.New(errx.BadRequest, key+" must be a comma-separated list of UUIDs")
			}
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	if len(ids) > max {
		return nil, errx.New(errx.BadRequest, fmt.Sprintf("%s accepts at most %d ids", key, max))
	}
	return ids, nil
}

// idsWithin reports whether every id is on allowed; a nil allowed list allows everything.
func idsWithin(ids, allowed []uuid.UUID) bool {
	if allowed == nil {
		return true
	}
	for _, id := range ids {
		ok := false
		for _, a := range allowed {
			if a == id && a != uuid.Nil {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
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
