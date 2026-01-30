package admin

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// AdminService defines the interface for admin operations
type AdminService interface {
	// User Management
	SearchUsers(ctx context.Context, search *models.AdminUserSearch) (*models.AdminUsersResult, *errx.Error)
	GetUserDetail(ctx context.Context, userID uuid.UUID) (*models.AdminUserDetail, *errx.Error)
	GetUserPreview(ctx context.Context, userID uuid.UUID) (*models.AdminUserPreview, *errx.Error)
	BanUser(ctx context.Context, adminID, userID uuid.UUID, reason string, ipAddress, userAgent string) *errx.Error
	UnbanUser(ctx context.Context, adminID, userID uuid.UUID, reason string, ipAddress, userAgent string) *errx.Error
	GetUserBans(ctx context.Context, userID uuid.UUID) ([]models.UserBan, *errx.Error)
	GetUserCampaigns(ctx context.Context, userID uuid.UUID, cursor *uuid.UUID, limit int) (*models.AdminCampaignsResult, *errx.Error)
	GetUserEmails(ctx context.Context, userID uuid.UUID, cursor *uuid.UUID, limit int) ([]models.AdminWorkerEmail, *models.Pagination, *errx.Error)
	GetUserRateLimits(ctx context.Context, userID uuid.UUID) (*models.AdminUserRateLimits, *errx.Error)
	UpdateUserRateLimits(ctx context.Context, adminID, userID uuid.UUID, update *models.UpdateUserRateLimitsRequest, ipAddress, userAgent string) *errx.Error

	// Worker Management
	ListWorkers(ctx context.Context, cursor *uuid.UUID, limit int) (*models.AdminWorkersResult, *errx.Error)
	GetWorkerDetail(ctx context.Context, workerID uuid.UUID) (*models.AdminWorkerDetail, *errx.Error)
	UpdateWorker(ctx context.Context, adminID, workerID uuid.UUID, update *models.AdminUpdateWorker, ipAddress, userAgent string) *errx.Error
	GetWorkerEmails(ctx context.Context, workerID uuid.UUID, cursor *uuid.UUID, limit int) ([]models.AdminWorkerEmail, *models.Pagination, *errx.Error)
	GetWorkerStats(ctx context.Context, workerID uuid.UUID) (*models.WorkerStats, *errx.Error)
	ReassignEmails(ctx context.Context, adminID uuid.UUID, emailIDs []uuid.UUID, newWorkerID uuid.UUID, ipAddress, userAgent string) *errx.Error

	// Warmup Management
	ListWarmupPools(ctx context.Context) ([]models.WarmupPoolInfo, *errx.Error)
	GetPoolParticipants(ctx context.Context, poolType string, cursor *uuid.UUID, limit int) (*models.WarmupPoolParticipantsResult, *errx.Error)
	ListBlockedAccounts(ctx context.Context, cursor *uuid.UUID, limit int) (*models.AdminBlockedAccountsResult, *errx.Error)
	BlockAccount(ctx context.Context, adminID, accountID uuid.UUID, reason string, ipAddress, userAgent string) *errx.Error
	UnblockAccount(ctx context.Context, adminID, accountID uuid.UUID, ipAddress, userAgent string) *errx.Error

	// Appeals
	ListAppeals(ctx context.Context, status string, cursor *uuid.UUID, limit int) (*models.WarmupAppealsResult, *errx.Error)
	GetAppeal(ctx context.Context, appealID uuid.UUID) (*models.WarmupAppeal, *errx.Error)
	ReviewAppeal(ctx context.Context, adminID, appealID uuid.UUID, approved bool, notes string, ipAddress, userAgent string) *errx.Error

	// Campaign Management
	SearchCampaigns(ctx context.Context, search *models.AdminCampaignSearch) (*models.AdminCampaignsResult, *errx.Error)
	GetCampaignDetail(ctx context.Context, campaignID uuid.UUID) (*models.AdminCampaignDetail, *errx.Error)
	StopCampaign(ctx context.Context, adminID, campaignID uuid.UUID, ipAddress, userAgent string) *errx.Error

	// Analytics
	GetPlatformOverview(ctx context.Context) (*models.PlatformOverview, *errx.Error)
	GetAnalyticsTrends(ctx context.Context) (*models.AnalyticsTrends, *errx.Error)
	GetDailyEmailStats(ctx context.Context, startDate, endDate time.Time) ([]models.DailyEmailStats, *errx.Error)
	GetHourlyEmailStats(ctx context.Context, date time.Time) ([]models.HourlyEmailStats, *errx.Error)
	GetWorkerLoadStats(ctx context.Context) ([]models.WorkerLoadStats, *errx.Error)
	GetEmailDistribution(ctx context.Context) ([]models.EmailDistribution, *errx.Error)
	GetUserGrowthStats(ctx context.Context, startDate, endDate time.Time) ([]models.UserGrowthStats, *errx.Error)

	// Plans
	ListPlans(ctx context.Context, includePrivate bool) ([]models.Plan, *errx.Error)
	CreatePlan(ctx context.Context, adminID uuid.UUID, req *models.CreatePlanRequest, ipAddress, userAgent string) (*models.Plan, *errx.Error)
	GetPlan(ctx context.Context, planID uuid.UUID) (*models.Plan, *errx.Error)
	UpdatePlan(ctx context.Context, adminID, planID uuid.UUID, req *models.UpdatePlanRequest, ipAddress, userAgent string) (*models.Plan, *errx.Error)
	DeletePlan(ctx context.Context, adminID, planID uuid.UUID, ipAddress, userAgent string) *errx.Error

	// Enterprise Inquiries
	ListEnterpriseInquiries(ctx context.Context, status string, cursor *uuid.UUID, limit int) (*models.AdminEnterpriseInquiriesResult, *errx.Error)
	GetEnterpriseInquiry(ctx context.Context, id uuid.UUID) (*models.AdminEnterpriseInquiry, *errx.Error)
	UpdateEnterpriseInquiry(ctx context.Context, adminID, inquiryID uuid.UUID, update *models.UpdateEnterpriseInquiryRequest, ipAddress, userAgent string) *errx.Error

	// Admin Management
	ListAdmins(ctx context.Context, cursor *uuid.UUID, limit int) (*models.AdminsResult, *errx.Error)
	GrantAdminPermissions(ctx context.Context, adminID, targetUserID uuid.UUID, permissions models.AdminPermission, ipAddress, userAgent string) *errx.Error
	RevokeAdminPermissions(ctx context.Context, adminID, targetUserID uuid.UUID, ipAddress, userAgent string) *errx.Error

	// Audit Logs
	SearchAuditLogs(ctx context.Context, search *models.AdminAuditLogSearch) (*models.AdminAuditLogsResult, *errx.Error)
}

type adminService struct {
	repo repository.AdminRepository
}

// NewService creates a new admin service
func NewService(repo repository.AdminRepository) AdminService {
	return &adminService{repo: repo}
}

// logAction logs an admin action
func (s *adminService) logAction(ctx context.Context, adminID uuid.UUID, action, targetType string, targetID uuid.UUID, details map[string]any, ipAddress, userAgent string) {
	log := &models.AdminAuditLog{
		ID:          uuid.New(),
		AdminUserID: adminID,
		Action:      action,
		TargetType:  targetType,
		TargetID:    targetID,
		Details:     details,
		IPAddress:   ipAddress,
		UserAgent:   userAgent,
		CreatedAt:   time.Now(),
	}
	if err := s.repo.CreateAuditLog(ctx, log); err != nil {
		sentry.CaptureException(err)
	}
}

// User Management

func (s *adminService) SearchUsers(ctx context.Context, search *models.AdminUserSearch) (*models.AdminUsersResult, *errx.Error) {
	result, err := s.repo.SearchUsers(ctx, search)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to search users")
	}
	return result, nil
}

func (s *adminService) GetUserDetail(ctx context.Context, userID uuid.UUID) (*models.AdminUserDetail, *errx.Error) {
	user, err := s.repo.GetUserDetail(ctx, userID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get user detail")
	}
	if user == nil {
		return nil, errx.ErrNotFound
	}
	return user, nil
}

func (s *adminService) GetUserPreview(ctx context.Context, userID uuid.UUID) (*models.AdminUserPreview, *errx.Error) {
	preview, err := s.repo.GetUserPreview(ctx, userID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get user preview")
	}
	if preview == nil {
		return nil, errx.ErrNotFound
	}
	return preview, nil
}

func (s *adminService) BanUser(ctx context.Context, adminID, userID uuid.UUID, reason string, ipAddress, userAgent string) *errx.Error {
	// Check if user exists
	user, err := s.repo.GetUserDetail(ctx, userID)
	if err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to get user")
	}
	if user == nil {
		return errx.ErrNotFound
	}

	// Check if already banned
	if user.BannedAt != nil {
		return errx.New(errx.BadRequest, "user is already banned")
	}

	// Cannot ban other admins
	if user.AdminPermissions > 0 {
		return errx.New(errx.Forbidden, "cannot ban admin users")
	}

	if err := s.repo.BanUser(ctx, userID, adminID, reason); err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to ban user")
	}

	s.logAction(ctx, adminID, "ban_user", "user", userID, map[string]any{"reason": reason}, ipAddress, userAgent)
	return nil
}

func (s *adminService) UnbanUser(ctx context.Context, adminID, userID uuid.UUID, reason string, ipAddress, userAgent string) *errx.Error {
	// Check if user exists
	user, err := s.repo.GetUserDetail(ctx, userID)
	if err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to get user")
	}
	if user == nil {
		return errx.ErrNotFound
	}

	// Check if not banned
	if user.BannedAt == nil {
		return errx.New(errx.BadRequest, "user is not banned")
	}

	if err := s.repo.UnbanUser(ctx, userID, adminID, reason); err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to unban user")
	}

	s.logAction(ctx, adminID, "unban_user", "user", userID, map[string]any{"reason": reason}, ipAddress, userAgent)
	return nil
}

func (s *adminService) GetUserBans(ctx context.Context, userID uuid.UUID) ([]models.UserBan, *errx.Error) {
	bans, err := s.repo.GetUserBans(ctx, userID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get user bans")
	}
	return bans, nil
}

func (s *adminService) GetUserCampaigns(ctx context.Context, userID uuid.UUID, cursor *uuid.UUID, limit int) (*models.AdminCampaignsResult, *errx.Error) {
	search := &models.AdminCampaignSearch{
		UserID: &userID,
		Cursor: cursor,
		Limit:  limit,
	}
	result, err := s.repo.SearchCampaigns(ctx, search)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get user campaigns")
	}
	return result, nil
}

func (s *adminService) GetUserEmails(ctx context.Context, userID uuid.UUID, cursor *uuid.UUID, limit int) ([]models.AdminWorkerEmail, *models.Pagination, *errx.Error) {
	// This would need a separate query by user ID
	// For now, return empty
	return []models.AdminWorkerEmail{}, &models.Pagination{}, nil
}

func (s *adminService) GetUserRateLimits(ctx context.Context, userID uuid.UUID) (*models.AdminUserRateLimits, *errx.Error) {
	limits, err := s.repo.GetUserRateLimits(ctx, userID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get rate limits")
	}
	return limits, nil
}

func (s *adminService) UpdateUserRateLimits(ctx context.Context, adminID, userID uuid.UUID, update *models.UpdateUserRateLimitsRequest, ipAddress, userAgent string) *errx.Error {
	if err := s.repo.UpdateUserRateLimits(ctx, userID, update); err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to update rate limits")
	}

	s.logAction(ctx, adminID, "update_rate_limits", "user", userID, map[string]any{"limits": update}, ipAddress, userAgent)
	return nil
}

// Worker Management

func (s *adminService) ListWorkers(ctx context.Context, cursor *uuid.UUID, limit int) (*models.AdminWorkersResult, *errx.Error) {
	result, err := s.repo.ListWorkers(ctx, cursor, limit)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to list workers")
	}
	return result, nil
}

func (s *adminService) GetWorkerDetail(ctx context.Context, workerID uuid.UUID) (*models.AdminWorkerDetail, *errx.Error) {
	worker, err := s.repo.GetWorkerDetail(ctx, workerID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get worker detail")
	}
	if worker == nil {
		return nil, errx.ErrNotFound
	}
	return worker, nil
}

func (s *adminService) UpdateWorker(ctx context.Context, adminID, workerID uuid.UUID, update *models.AdminUpdateWorker, ipAddress, userAgent string) *errx.Error {
	if err := s.repo.UpdateWorker(ctx, workerID, update); err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to update worker")
	}

	s.logAction(ctx, adminID, "update_worker", "worker", workerID, map[string]any{"update": update}, ipAddress, userAgent)
	return nil
}

func (s *adminService) GetWorkerEmails(ctx context.Context, workerID uuid.UUID, cursor *uuid.UUID, limit int) ([]models.AdminWorkerEmail, *models.Pagination, *errx.Error) {
	emails, pagination, err := s.repo.GetWorkerEmails(ctx, workerID, cursor, limit)
	if err != nil {
		sentry.CaptureException(err)
		return nil, nil, errx.New(errx.Internal, "failed to get worker emails")
	}
	return emails, pagination, nil
}

func (s *adminService) GetWorkerStats(ctx context.Context, workerID uuid.UUID) (*models.WorkerStats, *errx.Error) {
	stats, err := s.repo.GetWorkerStats(ctx, workerID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get worker stats")
	}
	return stats, nil
}

func (s *adminService) ReassignEmails(ctx context.Context, adminID uuid.UUID, emailIDs []uuid.UUID, newWorkerID uuid.UUID, ipAddress, userAgent string) *errx.Error {
	if err := s.repo.ReassignEmails(ctx, emailIDs, newWorkerID); err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to reassign emails")
	}

	s.logAction(ctx, adminID, "reassign_emails", "worker", newWorkerID, map[string]any{"email_count": len(emailIDs)}, ipAddress, userAgent)
	return nil
}

// Warmup Management

func (s *adminService) ListWarmupPools(ctx context.Context) ([]models.WarmupPoolInfo, *errx.Error) {
	pools, err := s.repo.ListWarmupPools(ctx)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to list warmup pools")
	}
	return pools, nil
}

func (s *adminService) GetPoolParticipants(ctx context.Context, poolType string, cursor *uuid.UUID, limit int) (*models.WarmupPoolParticipantsResult, *errx.Error) {
	result, err := s.repo.GetPoolParticipants(ctx, poolType, cursor, limit)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get pool participants")
	}
	return result, nil
}

func (s *adminService) ListBlockedAccounts(ctx context.Context, cursor *uuid.UUID, limit int) (*models.AdminBlockedAccountsResult, *errx.Error) {
	result, err := s.repo.ListBlockedAccounts(ctx, cursor, limit)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to list blocked accounts")
	}
	return result, nil
}

func (s *adminService) BlockAccount(ctx context.Context, adminID, accountID uuid.UUID, reason string, ipAddress, userAgent string) *errx.Error {
	if err := s.repo.BlockAccount(ctx, accountID, adminID, reason); err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to block account")
	}

	s.logAction(ctx, adminID, "block_warmup_account", "email_account", accountID, map[string]any{"reason": reason}, ipAddress, userAgent)
	return nil
}

func (s *adminService) UnblockAccount(ctx context.Context, adminID, accountID uuid.UUID, ipAddress, userAgent string) *errx.Error {
	if err := s.repo.UnblockAccount(ctx, accountID); err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to unblock account")
	}

	s.logAction(ctx, adminID, "unblock_warmup_account", "email_account", accountID, nil, ipAddress, userAgent)
	return nil
}

// Appeals

func (s *adminService) ListAppeals(ctx context.Context, status string, cursor *uuid.UUID, limit int) (*models.WarmupAppealsResult, *errx.Error) {
	result, err := s.repo.ListAppeals(ctx, status, cursor, limit)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to list appeals")
	}
	return result, nil
}

func (s *adminService) GetAppeal(ctx context.Context, appealID uuid.UUID) (*models.WarmupAppeal, *errx.Error) {
	appeal, err := s.repo.GetAppeal(ctx, appealID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get appeal")
	}
	if appeal == nil {
		return nil, errx.ErrNotFound
	}
	return appeal, nil
}

func (s *adminService) ReviewAppeal(ctx context.Context, adminID, appealID uuid.UUID, approved bool, notes string, ipAddress, userAgent string) *errx.Error {
	appeal, err := s.repo.GetAppeal(ctx, appealID)
	if err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to get appeal")
	}
	if appeal == nil {
		return errx.ErrNotFound
	}

	if appeal.Status != models.WarmupAppealStatusPending {
		return errx.New(errx.BadRequest, "appeal has already been reviewed")
	}

	if err := s.repo.ReviewAppeal(ctx, appealID, adminID, approved, notes); err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to review appeal")
	}

	action := "reject_appeal"
	if approved {
		action = "approve_appeal"
	}
	s.logAction(ctx, adminID, action, "warmup_appeal", appealID, map[string]any{"notes": notes}, ipAddress, userAgent)
	return nil
}

// Campaign Management

func (s *adminService) SearchCampaigns(ctx context.Context, search *models.AdminCampaignSearch) (*models.AdminCampaignsResult, *errx.Error) {
	result, err := s.repo.SearchCampaigns(ctx, search)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to search campaigns")
	}
	return result, nil
}

func (s *adminService) GetCampaignDetail(ctx context.Context, campaignID uuid.UUID) (*models.AdminCampaignDetail, *errx.Error) {
	campaign, err := s.repo.GetCampaignDetail(ctx, campaignID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get campaign detail")
	}
	if campaign == nil {
		return nil, errx.ErrNotFound
	}
	return campaign, nil
}

func (s *adminService) StopCampaign(ctx context.Context, adminID, campaignID uuid.UUID, ipAddress, userAgent string) *errx.Error {
	campaign, err := s.repo.GetCampaignDetail(ctx, campaignID)
	if err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to get campaign")
	}
	if campaign == nil {
		return errx.ErrNotFound
	}

	if err := s.repo.StopCampaign(ctx, campaignID); err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to stop campaign")
	}

	s.logAction(ctx, adminID, "force_stop_campaign", "campaign", campaignID, map[string]any{"user_id": campaign.UserID}, ipAddress, userAgent)
	return nil
}

// Analytics

func (s *adminService) GetPlatformOverview(ctx context.Context) (*models.PlatformOverview, *errx.Error) {
	overview, err := s.repo.GetPlatformOverview(ctx)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get platform overview")
	}
	return overview, nil
}

func (s *adminService) GetAnalyticsTrends(ctx context.Context) (*models.AnalyticsTrends, *errx.Error) {
	trends, err := s.repo.GetAnalyticsTrends(ctx)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get analytics trends")
	}
	return trends, nil
}

func (s *adminService) GetDailyEmailStats(ctx context.Context, startDate, endDate time.Time) ([]models.DailyEmailStats, *errx.Error) {
	stats, err := s.repo.GetDailyEmailStats(ctx, startDate, endDate)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get daily email stats")
	}
	return stats, nil
}

func (s *adminService) GetHourlyEmailStats(ctx context.Context, date time.Time) ([]models.HourlyEmailStats, *errx.Error) {
	stats, err := s.repo.GetHourlyEmailStats(ctx, date)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get hourly email stats")
	}
	return stats, nil
}

func (s *adminService) GetWorkerLoadStats(ctx context.Context) ([]models.WorkerLoadStats, *errx.Error) {
	stats, err := s.repo.GetWorkerLoadStats(ctx)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get worker load stats")
	}
	return stats, nil
}

func (s *adminService) GetEmailDistribution(ctx context.Context) ([]models.EmailDistribution, *errx.Error) {
	dist, err := s.repo.GetEmailDistribution(ctx)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get email distribution")
	}
	return dist, nil
}

func (s *adminService) GetUserGrowthStats(ctx context.Context, startDate, endDate time.Time) ([]models.UserGrowthStats, *errx.Error) {
	stats, err := s.repo.GetUserGrowthStats(ctx, startDate, endDate)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get user growth stats")
	}
	return stats, nil
}

// Plans

func (s *adminService) ListPlans(ctx context.Context, includePrivate bool) ([]models.Plan, *errx.Error) {
	plans, err := s.repo.ListPlans(ctx, includePrivate)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to list plans")
	}
	return plans, nil
}

func (s *adminService) CreatePlan(ctx context.Context, adminID uuid.UUID, req *models.CreatePlanRequest, ipAddress, userAgent string) (*models.Plan, *errx.Error) {
	plan := &models.Plan{
		ID:                 uuid.New(),
		Name:               &req.Name,
		MaxContacts:        req.MaxContacts,
		DailyEmails:        req.DailyEmails,
		AIGeneration:       req.AIGeneration,
		AccountLimit:       req.AccountLimit,
		Price:              req.Price,
		DiscountedPrice:    req.DiscountedPrice,
		Duration:           req.Duration,
		Public:             req.Public,
		DedicatedWorkers:   req.DedicatedWorkers,
		DailyCampaignLimit: req.DailyCampaignLimit,
		MaxCampaigns:       req.MaxCampaigns,
		MaxActiveCampaigns: req.MaxActiveCampaigns,
		MaxTeamMembers:     req.MaxTeamMembers,
		MaxEmailAccounts:   req.MaxEmailAccounts,
	}

	if err := s.repo.CreatePlan(ctx, plan); err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to create plan")
	}

	s.logAction(ctx, adminID, "create_plan", "plan", plan.ID, map[string]any{"name": req.Name}, ipAddress, userAgent)
	return plan, nil
}

func (s *adminService) GetPlan(ctx context.Context, planID uuid.UUID) (*models.Plan, *errx.Error) {
	plan, err := s.repo.GetPlan(ctx, planID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get plan")
	}
	if plan == nil {
		return nil, errx.ErrNotFound
	}
	return plan, nil
}

func (s *adminService) UpdatePlan(ctx context.Context, adminID, planID uuid.UUID, req *models.UpdatePlanRequest, ipAddress, userAgent string) (*models.Plan, *errx.Error) {
	plan, err := s.repo.GetPlan(ctx, planID)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get plan")
	}
	if plan == nil {
		return nil, errx.ErrNotFound
	}

	// Apply updates
	if req.Name != nil {
		plan.Name = req.Name
	}
	if req.MaxContacts != nil {
		plan.MaxContacts = *req.MaxContacts
	}
	if req.DailyEmails != nil {
		plan.DailyEmails = *req.DailyEmails
	}
	if req.AIGeneration != nil {
		plan.AIGeneration = *req.AIGeneration
	}
	if req.AccountLimit != nil {
		plan.AccountLimit = *req.AccountLimit
	}
	if req.Price != nil {
		plan.Price = *req.Price
	}
	if req.DiscountedPrice != nil {
		plan.DiscountedPrice = *req.DiscountedPrice
	}
	if req.Duration != nil {
		plan.Duration = *req.Duration
	}
	if req.Public != nil {
		plan.Public = *req.Public
	}
	if req.DedicatedWorkers != nil {
		plan.DedicatedWorkers = *req.DedicatedWorkers
	}
	if req.DailyCampaignLimit != nil {
		plan.DailyCampaignLimit = req.DailyCampaignLimit
	}
	if req.MaxCampaigns != nil {
		plan.MaxCampaigns = req.MaxCampaigns
	}
	if req.MaxActiveCampaigns != nil {
		plan.MaxActiveCampaigns = req.MaxActiveCampaigns
	}
	if req.MaxTeamMembers != nil {
		plan.MaxTeamMembers = req.MaxTeamMembers
	}
	if req.MaxEmailAccounts != nil {
		plan.MaxEmailAccounts = req.MaxEmailAccounts
	}

	if err := s.repo.UpdatePlan(ctx, plan); err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to update plan")
	}

	s.logAction(ctx, adminID, "update_plan", "plan", planID, nil, ipAddress, userAgent)
	return plan, nil
}

func (s *adminService) DeletePlan(ctx context.Context, adminID, planID uuid.UUID, ipAddress, userAgent string) *errx.Error {
	// Check if plan is in use
	inUse, err := s.repo.IsPlanInUse(ctx, planID)
	if err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to check plan usage")
	}
	if inUse {
		return errx.New(errx.BadRequest, "cannot delete plan that is in use")
	}

	if err := s.repo.DeletePlan(ctx, planID); err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to delete plan")
	}

	s.logAction(ctx, adminID, "delete_plan", "plan", planID, nil, ipAddress, userAgent)
	return nil
}

// Enterprise Inquiries

func (s *adminService) ListEnterpriseInquiries(ctx context.Context, status string, cursor *uuid.UUID, limit int) (*models.AdminEnterpriseInquiriesResult, *errx.Error) {
	result, err := s.repo.ListEnterpriseInquiries(ctx, status, cursor, limit)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to list enterprise inquiries")
	}
	return result, nil
}

func (s *adminService) GetEnterpriseInquiry(ctx context.Context, id uuid.UUID) (*models.AdminEnterpriseInquiry, *errx.Error) {
	inquiry, err := s.repo.GetEnterpriseInquiry(ctx, id)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get enterprise inquiry")
	}
	if inquiry == nil {
		return nil, errx.ErrNotFound
	}
	return inquiry, nil
}

func (s *adminService) UpdateEnterpriseInquiry(ctx context.Context, adminID, inquiryID uuid.UUID, update *models.UpdateEnterpriseInquiryRequest, ipAddress, userAgent string) *errx.Error {
	if err := s.repo.UpdateEnterpriseInquiry(ctx, inquiryID, update); err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to update enterprise inquiry")
	}

	s.logAction(ctx, adminID, "update_enterprise_inquiry", "enterprise_inquiry", inquiryID, map[string]any{"update": update}, ipAddress, userAgent)
	return nil
}

// Admin Management

func (s *adminService) ListAdmins(ctx context.Context, cursor *uuid.UUID, limit int) (*models.AdminsResult, *errx.Error) {
	result, err := s.repo.ListAdmins(ctx, cursor, limit)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to list admins")
	}
	return result, nil
}

func (s *adminService) GrantAdminPermissions(ctx context.Context, adminID, targetUserID uuid.UUID, permissions models.AdminPermission, ipAddress, userAgent string) *errx.Error {
	// Cannot modify own permissions
	if adminID == targetUserID {
		return errx.New(errx.BadRequest, "cannot modify your own permissions")
	}

	if err := s.repo.UpdateUserAdminPermissions(ctx, targetUserID, uint32(permissions), adminID); err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to grant admin permissions")
	}

	s.logAction(ctx, adminID, "grant_admin", "user", targetUserID, map[string]any{"permissions": permissions}, ipAddress, userAgent)
	return nil
}

func (s *adminService) RevokeAdminPermissions(ctx context.Context, adminID, targetUserID uuid.UUID, ipAddress, userAgent string) *errx.Error {
	// Cannot modify own permissions
	if adminID == targetUserID {
		return errx.New(errx.BadRequest, "cannot modify your own permissions")
	}

	if err := s.repo.UpdateUserAdminPermissions(ctx, targetUserID, 0, adminID); err != nil {
		sentry.CaptureException(err)
		return errx.New(errx.Internal, "failed to revoke admin permissions")
	}

	s.logAction(ctx, adminID, "revoke_admin", "user", targetUserID, nil, ipAddress, userAgent)
	return nil
}

// Audit Logs

func (s *adminService) SearchAuditLogs(ctx context.Context, search *models.AdminAuditLogSearch) (*models.AdminAuditLogsResult, *errx.Error) {
	result, err := s.repo.SearchAuditLogs(ctx, search)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to search audit logs")
	}
	return result, nil
}
