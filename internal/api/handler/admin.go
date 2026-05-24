package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// User Management Handlers

// AdminSearchUsers searches for users with pagination
func (h *Handler) AdminSearchUsers(c *gin.Context) {
	var search models.AdminUserSearch
	if err := c.ShouldBindQuery(&search); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid query parameters"))
		return
	}

	result, xerr := h.AdminService.SearchUsers(c.Request.Context(), &search)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}

// AdminGetUser gets a user's details
func (h *Handler) AdminGetUser(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid user ID"))
		return
	}

	user, xerr := h.AdminService.GetUserDetail(c.Request.Context(), userID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, user)
}

// AdminGetUserPreview gets a full preview of a user's account
func (h *Handler) AdminGetUserPreview(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid user ID"))
		return
	}

	preview, xerr := h.AdminService.GetUserPreview(c.Request.Context(), userID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, preview)
}

// AdminBanUser bans a user
func (h *Handler) AdminBanUser(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid user ID"))
		return
	}

	var req models.BanUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	xerr := h.AdminService.BanUser(c.Request.Context(), *adminID, userID, req.Reason, c.ClientIP(), c.GetHeader("User-Agent"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "user banned successfully"})
}

// AdminUnbanUser unbans a user
func (h *Handler) AdminUnbanUser(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid user ID"))
		return
	}

	var req models.UnbanUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	xerr := h.AdminService.UnbanUser(c.Request.Context(), *adminID, userID, req.Reason, c.ClientIP(), c.GetHeader("User-Agent"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "user unbanned successfully"})
}

// AdminGetUserBans gets the ban history for a user
func (h *Handler) AdminGetUserBans(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid user ID"))
		return
	}

	bans, xerr := h.AdminService.GetUserBans(c.Request.Context(), userID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"bans": bans})
}

// AdminGetUserCampaigns gets a user's campaigns
func (h *Handler) AdminGetUserCampaigns(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid user ID"))
		return
	}

	cursor := parseCursor(c.Query("cursor"))
	limit := parseLimit(c.Query("limit"), 50)

	result, xerr := h.AdminService.GetUserCampaigns(c.Request.Context(), userID, cursor, limit)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}

// AdminGetUserEmails gets a user's email accounts
func (h *Handler) AdminGetUserEmails(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid user ID"))
		return
	}

	cursor := parseCursor(c.Query("cursor"))
	limit := parseLimit(c.Query("limit"), 50)

	emails, pagination, xerr := h.AdminService.GetUserEmails(c.Request.Context(), userID, cursor, limit)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": emails, "pagination": pagination})
}

// AdminGetUserRateLimits gets a user's rate limits
func (h *Handler) AdminGetUserRateLimits(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid user ID"))
		return
	}

	limits, xerr := h.AdminService.GetUserRateLimits(c.Request.Context(), userID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, limits)
}

// AdminUpdateUserRateLimits updates a user's rate limits
func (h *Handler) AdminUpdateUserRateLimits(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid user ID"))
		return
	}

	var req models.UpdateUserRateLimitsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	xerr := h.AdminService.UpdateUserRateLimits(c.Request.Context(), *adminID, userID, &req, c.ClientIP(), c.GetHeader("User-Agent"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "rate limits updated successfully"})
}

// Worker Management Handlers

// AdminListWorkers lists all workers
func (h *Handler) AdminListWorkers(c *gin.Context) {
	cursor := parseCursor(c.Query("cursor"))
	limit := parseLimit(c.Query("limit"), 50)

	result, xerr := h.AdminService.ListWorkers(c.Request.Context(), cursor, limit)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}

// AdminGetWorker gets a worker's details
func (h *Handler) AdminGetWorker(c *gin.Context) {
	workerID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid worker ID"))
		return
	}

	worker, xerr := h.AdminService.GetWorkerDetail(c.Request.Context(), workerID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, worker)
}

// AdminUpdateWorker updates a worker
func (h *Handler) AdminUpdateWorker(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	workerID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid worker ID"))
		return
	}

	var req models.AdminUpdateWorker
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	xerr := h.AdminService.UpdateWorker(c.Request.Context(), *adminID, workerID, &req, c.ClientIP(), c.GetHeader("User-Agent"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "worker updated successfully"})
}

// AdminGetWorkerEmails gets emails connected to a worker
func (h *Handler) AdminGetWorkerEmails(c *gin.Context) {
	workerID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid worker ID"))
		return
	}

	cursor := parseCursor(c.Query("cursor"))
	limit := parseLimit(c.Query("limit"), 50)

	emails, pagination, xerr := h.AdminService.GetWorkerEmails(c.Request.Context(), workerID, cursor, limit)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": emails, "pagination": pagination})
}

// AdminGetWorkerStats gets statistics for a worker
func (h *Handler) AdminGetWorkerStats(c *gin.Context) {
	workerID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid worker ID"))
		return
	}

	stats, xerr := h.AdminService.GetWorkerStats(c.Request.Context(), workerID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, stats)
}

// AdminReassignEmails reassigns emails to a new worker
func (h *Handler) AdminReassignEmails(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	workerID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid worker ID"))
		return
	}

	var req models.ReassignEmailsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	// Use the workerID from the URL as the target
	xerr := h.AdminService.ReassignEmails(c.Request.Context(), *adminID, req.EmailIDs, workerID, c.ClientIP(), c.GetHeader("User-Agent"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "emails reassigned successfully"})
}

// Warmup Management Handlers

// AdminListWarmupPools lists all warmup pools
func (h *Handler) AdminListWarmupPools(c *gin.Context) {
	pools, xerr := h.AdminService.ListWarmupPools(c.Request.Context())
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"pools": pools})
}

// AdminGetWarmupHealthSummary returns an aggregate health overview of all warmup pools
func (h *Handler) AdminGetWarmupHealthSummary(c *gin.Context) {
	if h.WarmupService == nil {
		errx.JSON(c, errx.New(errx.Internal, "warmup service not available"))
		return
	}
	summary, xerr := h.WarmupService.GetPoolHealthSummary(c.Request.Context())
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}
	c.JSON(http.StatusOK, summary)
}

// AdminGetPoolParticipants gets participants in a warmup pool
func (h *Handler) AdminGetPoolParticipants(c *gin.Context) {
	poolType := c.Param("type")
	cursor := parseCursor(c.Query("cursor"))
	limit := parseLimit(c.Query("limit"), 50)

	result, xerr := h.AdminService.GetPoolParticipants(c.Request.Context(), poolType, cursor, limit)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}

// AdminListBlockedAccounts lists blocked warmup accounts
func (h *Handler) AdminListBlockedAccounts(c *gin.Context) {
	cursor := parseCursor(c.Query("cursor"))
	limit := parseLimit(c.Query("limit"), 50)

	result, xerr := h.AdminService.ListBlockedAccounts(c.Request.Context(), cursor, limit)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}

// AdminBlockAccount blocks an account from warmup
func (h *Handler) AdminBlockAccount(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	accountID, err := uuid.Parse(c.Param("accountId"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid account ID"))
		return
	}

	var req models.BlockAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	xerr := h.AdminService.BlockAccount(c.Request.Context(), *adminID, accountID, req.Reason, c.ClientIP(), c.GetHeader("User-Agent"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "account blocked successfully"})
}

// AdminUnblockAccount unblocks an account from warmup
func (h *Handler) AdminUnblockAccount(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	accountID, err := uuid.Parse(c.Param("accountId"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid account ID"))
		return
	}

	xerr := h.AdminService.UnblockAccount(c.Request.Context(), *adminID, accountID, c.ClientIP(), c.GetHeader("User-Agent"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "account unblocked successfully"})
}

// Appeal Handlers

// AdminListAppeals lists warmup appeals
func (h *Handler) AdminListAppeals(c *gin.Context) {
	status := c.Query("status")
	cursor := parseCursor(c.Query("cursor"))
	limit := parseLimit(c.Query("limit"), 50)

	result, xerr := h.AdminService.ListAppeals(c.Request.Context(), status, cursor, limit)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}

// AdminGetAppeal gets a specific appeal
func (h *Handler) AdminGetAppeal(c *gin.Context) {
	appealID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid appeal ID"))
		return
	}

	appeal, xerr := h.AdminService.GetAppeal(c.Request.Context(), appealID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, appeal)
}

// AdminApproveAppeal approves a warmup appeal
func (h *Handler) AdminApproveAppeal(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	appealID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid appeal ID"))
		return
	}

	var req models.ReviewAppealRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}

	xerr := h.AdminService.ReviewAppeal(c.Request.Context(), *adminID, appealID, true, req.Notes, c.ClientIP(), c.GetHeader("User-Agent"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "appeal approved successfully"})
}

// AdminRejectAppeal rejects a warmup appeal
func (h *Handler) AdminRejectAppeal(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	appealID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid appeal ID"))
		return
	}

	var req models.ReviewAppealRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.ErrInvalid)
		return
	}

	xerr := h.AdminService.ReviewAppeal(c.Request.Context(), *adminID, appealID, false, req.Notes, c.ClientIP(), c.GetHeader("User-Agent"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "appeal rejected"})
}

// Campaign Management Handlers

// AdminSearchCampaigns searches for campaigns
func (h *Handler) AdminSearchCampaigns(c *gin.Context) {
	var search models.AdminCampaignSearch
	if err := c.ShouldBindQuery(&search); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid query parameters"))
		return
	}

	result, xerr := h.AdminService.SearchCampaigns(c.Request.Context(), &search)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}

// AdminGetCampaign gets a campaign's details
func (h *Handler) AdminGetCampaign(c *gin.Context) {
	campaignID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid campaign ID"))
		return
	}

	campaign, xerr := h.AdminService.GetCampaignDetail(c.Request.Context(), campaignID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, campaign)
}

// AdminStopCampaign force-stops a campaign
func (h *Handler) AdminStopCampaign(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	campaignID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid campaign ID"))
		return
	}

	xerr := h.AdminService.StopCampaign(c.Request.Context(), *adminID, campaignID, c.ClientIP(), c.GetHeader("User-Agent"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "campaign stopped successfully"})
}

// Analytics Handlers

// AdminGetPlatformOverview gets platform overview statistics
func (h *Handler) AdminGetPlatformOverview(c *gin.Context) {
	overview, xerr := h.AdminService.GetPlatformOverview(c.Request.Context())
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, overview)
}

// AdminGetAnalyticsTrends gets analytics trend data
func (h *Handler) AdminGetAnalyticsTrends(c *gin.Context) {
	trends, xerr := h.AdminService.GetAnalyticsTrends(c.Request.Context())
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, trends)
}

// AdminGetDailyEmailStats gets daily email statistics
func (h *Handler) AdminGetDailyEmailStats(c *gin.Context) {
	startDate, endDate := parseDateRange(c)

	stats, xerr := h.AdminService.GetDailyEmailStats(c.Request.Context(), startDate, endDate)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"stats": stats})
}

// AdminGetHourlyEmailStats gets hourly email statistics
func (h *Handler) AdminGetHourlyEmailStats(c *gin.Context) {
	dateStr := c.Query("date")
	date := time.Now()
	if dateStr != "" {
		if parsed, err := time.Parse("2006-01-02", dateStr); err == nil {
			date = parsed
		}
	}

	stats, xerr := h.AdminService.GetHourlyEmailStats(c.Request.Context(), date)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"stats": stats})
}

// AdminGetWorkerLoadStats gets worker load statistics
func (h *Handler) AdminGetWorkerLoadStats(c *gin.Context) {
	stats, xerr := h.AdminService.GetWorkerLoadStats(c.Request.Context())
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"stats": stats})
}

// AdminGetEmailDistribution gets email distribution across workers
func (h *Handler) AdminGetEmailDistribution(c *gin.Context) {
	dist, xerr := h.AdminService.GetEmailDistribution(c.Request.Context())
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"distribution": dist})
}

// AdminGetUserGrowthStats gets user growth statistics
func (h *Handler) AdminGetUserGrowthStats(c *gin.Context) {
	startDate, endDate := parseDateRange(c)

	stats, xerr := h.AdminService.GetUserGrowthStats(c.Request.Context(), startDate, endDate)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"stats": stats})
}

// Plan Management Handlers

// AdminListPlans lists all plans
func (h *Handler) AdminListPlans(c *gin.Context) {
	plans, xerr := h.AdminService.ListPlans(c.Request.Context(), true)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"plans": plans})
}

// AdminCreatePlan creates a new plan
func (h *Handler) AdminCreatePlan(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	var req models.CreatePlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	plan, xerr := h.AdminService.CreatePlan(c.Request.Context(), *adminID, &req, c.ClientIP(), c.GetHeader("User-Agent"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusCreated, plan)
}

// AdminGetPlan gets a plan by ID
func (h *Handler) AdminGetPlan(c *gin.Context) {
	planID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid plan ID"))
		return
	}

	plan, xerr := h.AdminService.GetPlan(c.Request.Context(), planID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, plan)
}

// AdminUpdatePlan updates a plan
func (h *Handler) AdminUpdatePlan(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	planID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid plan ID"))
		return
	}

	var req models.UpdatePlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	plan, xerr := h.AdminService.UpdatePlan(c.Request.Context(), *adminID, planID, &req, c.ClientIP(), c.GetHeader("User-Agent"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, plan)
}

// AdminDeletePlan deletes a plan
func (h *Handler) AdminDeletePlan(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	planID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid plan ID"))
		return
	}

	xerr := h.AdminService.DeletePlan(c.Request.Context(), *adminID, planID, c.ClientIP(), c.GetHeader("User-Agent"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "plan deleted successfully"})
}

// Enterprise Inquiry Handlers

// AdminListEnterpriseInquiries lists enterprise inquiries
func (h *Handler) AdminListEnterpriseInquiries(c *gin.Context) {
	status := c.Query("status")
	cursor := parseCursor(c.Query("cursor"))
	limit := parseLimit(c.Query("limit"), 50)

	result, xerr := h.AdminService.ListEnterpriseInquiries(c.Request.Context(), status, cursor, limit)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}

// AdminGetEnterpriseInquiry gets a specific enterprise inquiry
func (h *Handler) AdminGetEnterpriseInquiry(c *gin.Context) {
	inquiryID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid inquiry ID"))
		return
	}

	inquiry, xerr := h.AdminService.GetEnterpriseInquiry(c.Request.Context(), inquiryID)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, inquiry)
}

// AdminUpdateEnterpriseInquiry updates an enterprise inquiry
func (h *Handler) AdminUpdateEnterpriseInquiry(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	inquiryID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid inquiry ID"))
		return
	}

	var req models.UpdateEnterpriseInquiryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	xerr := h.AdminService.UpdateEnterpriseInquiry(c.Request.Context(), *adminID, inquiryID, &req, c.ClientIP(), c.GetHeader("User-Agent"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "inquiry updated successfully"})
}

// Admin Management Handlers

// AdminListAdmins lists all admin users
func (h *Handler) AdminListAdmins(c *gin.Context) {
	cursor := parseCursor(c.Query("cursor"))
	limit := parseLimit(c.Query("limit"), 50)

	result, xerr := h.AdminService.ListAdmins(c.Request.Context(), cursor, limit)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}

// AdminGrantPermissions grants admin permissions to a user
func (h *Handler) AdminGrantPermissions(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	userID, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid user ID"))
		return
	}

	var req models.GrantAdminRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid request body"))
		return
	}

	xerr := h.AdminService.GrantAdminPermissions(c.Request.Context(), *adminID, userID, req.Permissions, c.ClientIP(), c.GetHeader("User-Agent"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "admin permissions granted successfully"})
}

// AdminRevokePermissions revokes admin permissions from a user
func (h *Handler) AdminRevokePermissions(c *gin.Context) {
	adminID := middleware.GetAdminUserID(c)
	if adminID == nil {
		errx.JSON(c, errx.ErrUnauthorized)
		return
	}

	userID, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid user ID"))
		return
	}

	xerr := h.AdminService.RevokeAdminPermissions(c.Request.Context(), *adminID, userID, c.ClientIP(), c.GetHeader("User-Agent"))
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "admin permissions revoked successfully"})
}

// AdminSearchAuditLogs searches audit logs
func (h *Handler) AdminSearchAuditLogs(c *gin.Context) {
	var search models.AdminAuditLogSearch
	if err := c.ShouldBindQuery(&search); err != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "invalid query parameters"))
		return
	}

	result, xerr := h.AdminService.SearchAuditLogs(c.Request.Context(), &search)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	c.JSON(http.StatusOK, result)
}

// AdminGetPermissionList returns the list of available admin permissions
func (h *Handler) AdminGetPermissionList(c *gin.Context) {
	permissions := models.GetAllPermissionInfos()
	c.JSON(http.StatusOK, gin.H{"permissions": permissions})
}

// Helper functions

func parseCursor(s string) *uuid.UUID {
	if s == "" {
		return nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return nil
	}
	return &id
}

func parseLimit(s string, defaultLimit int) int {
	if s == "" {
		return defaultLimit
	}
	limit, err := strconv.Atoi(s)
	if err != nil || limit <= 0 {
		return defaultLimit
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func parseDateRange(c *gin.Context) (time.Time, time.Time) {
	now := time.Now()
	endDate := now
	startDate := now.AddDate(0, 0, -30) // Default to last 30 days

	if s := c.Query("start_date"); s != "" {
		if t, err := time.Parse("2006-01-02", s); err == nil {
			startDate = t
		}
	}
	if s := c.Query("end_date"); s != "" {
		if t, err := time.Parse("2006-01-02", s); err == nil {
			endDate = t
		}
	}

	return startDate, endDate
}
