package admin

import (
	"context"
	"fmt"
	"math/bits"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/repository"
)

// AdminService defines the interface for admin operations
type AdminService interface {
	// Mailbox admin
	SearchMailboxes(ctx context.Context, search *models.AdminMailboxSearch) (*models.AdminMailboxesResult, *errx.Error)
	MailboxesOnDomains(ctx context.Context, domains []string, skip []uuid.UUID, limit int) ([]models.AdminMailboxTarget, int, *errx.Error)

	// User Management
	SearchUsers(ctx context.Context, search *models.AdminUserSearch) (*models.AdminUsersResult, *errx.Error)
	GetUserDetail(ctx context.Context, userID uuid.UUID) (*models.AdminUserDetail, *errx.Error)
	GetUserPreview(ctx context.Context, userID uuid.UUID) (*models.AdminUserPreview, *errx.Error)
	BanUser(ctx context.Context, adminID, userID uuid.UUID, reason string, scope models.BanScope, ipAddress, userAgent string) *errx.Error
	UnbanUser(ctx context.Context, adminID, userID uuid.UUID, reason string, ipAddress, userAgent string) *errx.Error
	GetUserBans(ctx context.Context, userID uuid.UUID) ([]models.UserBan, *errx.Error)
	GetUserCampaigns(ctx context.Context, userID uuid.UUID, offset, limit int) (*models.AdminCampaignsResult, *errx.Error)
	GetUserEmails(ctx context.Context, userID uuid.UUID, offset, limit int) ([]models.AdminWorkerEmail, *models.Pagination, *errx.Error)
	GetUserRateLimits(ctx context.Context, userID uuid.UUID) (*models.AdminUserRateLimits, *errx.Error)
	UpdateUserRateLimits(ctx context.Context, adminID, userID uuid.UUID, update *models.UpdateUserRateLimitsRequest, ipAddress, userAgent string) (*models.AdminUserRateLimits, *errx.Error)

	// Worker Management
	ListWorkers(ctx context.Context, offset, limit int) (*models.AdminWorkersResult, *errx.Error)
	GetWorkerDetail(ctx context.Context, workerID uuid.UUID) (*models.AdminWorkerDetail, *errx.Error)
	UpdateWorker(ctx context.Context, adminID, workerID uuid.UUID, update *models.AdminUpdateWorker, ipAddress, userAgent string) *errx.Error
	GetWorkerEmails(ctx context.Context, workerID uuid.UUID, beforeAt time.Time, beforeID uuid.UUID, limit int) ([]models.AdminWorkerEmail, *models.Pagination, *errx.Error)
	GetWorkerStats(ctx context.Context, workerID uuid.UUID) (*models.WorkerStats, *errx.Error)
	ReassignEmails(ctx context.Context, adminID uuid.UUID, emailIDs []uuid.UUID, newWorkerID uuid.UUID, ipAddress, userAgent string) *errx.Error

	// Warmup Management
	ListWarmupPools(ctx context.Context) ([]models.WarmupPoolInfo, *errx.Error)
	GetPoolParticipants(ctx context.Context, poolType string, offset, limit int) (*models.WarmupPoolParticipantsResult, *errx.Error)
	ListBlockedAccounts(ctx context.Context, offset, limit int) (*models.AdminBlockedAccountsResult, *errx.Error)
	BlockAccount(ctx context.Context, adminID, accountID uuid.UUID, reason string, ipAddress, userAgent string) *errx.Error
	UnblockAccount(ctx context.Context, adminID, accountID uuid.UUID, ipAddress, userAgent string) *errx.Error

	// Appeals
	ListAppeals(ctx context.Context, status string, offset, limit int) (*models.WarmupAppealsResult, *errx.Error)
	GetAppeal(ctx context.Context, appealID uuid.UUID) (*models.WarmupAppeal, *errx.Error)
	ReviewAppeal(ctx context.Context, adminID, appealID uuid.UUID, approved bool, notes string, ipAddress, userAgent string) *errx.Error

	// Campaign Management
	SearchCampaigns(ctx context.Context, search *models.AdminCampaignSearch) (*models.AdminCampaignsResult, *errx.Error)
	GetCampaignDetail(ctx context.Context, campaignID uuid.UUID) (*models.AdminCampaignDetail, *errx.Error)
	StopCampaign(ctx context.Context, adminID, campaignID uuid.UUID, reason, ipAddress, userAgent string) *errx.Error

	// Analytics
	GetPlatformOverview(ctx context.Context) (*models.PlatformOverview, *errx.Error)
	GetAnalyticsTrends(ctx context.Context) (*models.AnalyticsTrends, *errx.Error)
	GetDailyEmailStats(ctx context.Context, startDate, endDate time.Time) ([]models.DailyEmailStats, *errx.Error)
	GetHourlyEmailStats(ctx context.Context, date time.Time) ([]models.HourlyEmailStats, *errx.Error)
	GetUserGrowthStats(ctx context.Context, startDate, endDate time.Time) ([]models.UserGrowthStats, *errx.Error)

	// Admin Management
	ListAdmins(ctx context.Context, offset, limit int) (*models.AdminsResult, *errx.Error)
	GrantAdminPermissions(ctx context.Context, adminID, targetUserID uuid.UUID, permissions models.AdminPermission, ipAddress, userAgent string) *errx.Error
	RevokeAdminPermissions(ctx context.Context, adminID, targetUserID uuid.UUID, ipAddress, userAgent string) *errx.Error

	// Audit Logs
	SearchAuditLogs(ctx context.Context, search *models.AdminAuditLogSearch) (*models.AdminAuditLogsResult, *errx.Error)

	// LogAdminAction writes a row to admin_audit_log. Fire-and-forget; safe
	// to call from any handler that has the admin's identity and a target.
	// Used by the new SSH worker / credentials / release / system handlers.
	LogAdminAction(ctx context.Context, adminID uuid.UUID, action, targetType string, targetID *uuid.UUID, details map[string]any, ipAddress, userAgent string)
}

// SessionRevoker ends a user's live sessions. Implemented by the token
// service, which also clears the Redis session cache: a database-only revoke
// stays invisible to every request served from cache, which is most of them.
type SessionRevoker interface {
	RevokeOtherSessions(ctx context.Context, userID, currentSessionID uuid.UUID) *errx.Error
}

type adminService struct {
	repo repository.AdminRepository
	// The owner-visible campaign activity feed.
	campaignLogRepo repository.CampaignLogRepository
	// sessions ends a banned user's live sessions. Nil-safe: without it a ban
	// still lands, it just does not take effect until the tokens expire.
	sessions SessionRevoker
	// opsNotify tells the operator's channels about admin access changes. Nil-safe.
	opsNotify OperatorNotifier
}

// OperatorNotifier is the slice of opsnotify this package emits through.
type OperatorNotifier interface {
	NotifyOperator(key, title, summary string, fields map[string]string)
}

// WireOperatorNotifier attaches the operator alert channels.
func (s *adminService) WireOperatorNotifier(n OperatorNotifier) { s.opsNotify = n }

// notifyAccessChange raises admin.access_changed when the mask actually moved.
// The key is a literal so this package stays free of opsnotify; it matches
// opsnotify.EventAdminAccess.
func (s *adminService) notifyAccessChange(target, actor *models.AdminUserDetail, perms models.AdminPermission) {
	if s.opsNotify == nil || target == nil || target.AdminPermissions == perms {
		return
	}
	title := "Admin access changed"
	switch {
	case target.AdminPermissions == 0:
		title = "Admin access granted"
	case perms == 0:
		title = "Admin access revoked"
	}
	by := ""
	if actor != nil {
		by = actor.Email
	}
	access := "None"
	switch {
	case perms.IsSuperAdmin():
		access = "Every permission (super admin)"
	case perms != 0:
		access = fmt.Sprintf("%d permissions", bits.OnesCount32(uint32(perms)))
	}
	s.opsNotify.NotifyOperator("admin.access_changed", title, target.Email+": "+title+".", map[string]string{
		"Account":    target.Email,
		"Changed by": by,
		"Access now": access,
	})
}

// NewService creates a new admin service
func NewService(repo repository.AdminRepository, campaignLogRepo repository.CampaignLogRepository) AdminService {
	return &adminService{repo: repo, campaignLogRepo: campaignLogRepo}
}

// WithSessionRevoker wires the session revoker used when a login ban lands.
func (s *adminService) WithSessionRevoker(r SessionRevoker) { s.sessions = r }

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
		errs.CaptureException(err)
	}
}

// LogAdminAction is the public-facing version of logAction. Goroutine-detached
// so handlers can call it without taking the request-cycle hit on the audit
// write. Accepts an optional targetID (uuid.Nil if the action isn't tied to
// a specific entity, e.g. "check releases").
func (s *adminService) LogAdminAction(ctx context.Context, adminID uuid.UUID, action, targetType string, targetID *uuid.UUID, details map[string]any, ipAddress, userAgent string) {
	var tid uuid.UUID
	if targetID != nil {
		tid = *targetID
	}
	// Capture values into the goroutine; ctx is replaced with Background so
	// the audit row still lands even if the request context cancels first.
	go func() {
		s.logAction(context.Background(), adminID, action, targetType, tid, details, ipAddress, userAgent)
	}()
}

// User Management

func (s *adminService) SearchMailboxes(ctx context.Context, search *models.AdminMailboxSearch) (*models.AdminMailboxesResult, *errx.Error) {
	result, err := s.repo.SearchMailboxesForAdmin(ctx, search)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to search mailboxes")
	}
	return result, nil
}

func (s *adminService) MailboxesOnDomains(ctx context.Context, domains []string, skip []uuid.UUID, limit int) ([]models.AdminMailboxTarget, int, *errx.Error) {
	targets, total, err := s.repo.MailboxesOnDomains(ctx, domains, skip, limit)
	if err != nil {
		errs.CaptureException(err)
		return nil, 0, errx.New(errx.Internal, "failed to list mailboxes")
	}
	return targets, total, nil
}

func (s *adminService) SearchUsers(ctx context.Context, search *models.AdminUserSearch) (*models.AdminUsersResult, *errx.Error) {
	result, err := s.repo.SearchUsers(ctx, search)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to search users")
	}
	return result, nil
}

func (s *adminService) GetUserDetail(ctx context.Context, userID uuid.UUID) (*models.AdminUserDetail, *errx.Error) {
	user, err := s.repo.GetUserDetail(ctx, userID)
	if err != nil {
		errs.CaptureException(err)
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
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get user preview")
	}
	if preview == nil {
		return nil, errx.ErrNotFound
	}
	// Same answer as GET /users/:id/rate-limits: no row means the defaults.
	if preview.RateLimits == nil {
		preview.RateLimits = models.DefaultAdminUserRateLimits(userID)
	}
	return preview, nil
}

func (s *adminService) BanUser(ctx context.Context, adminID, userID uuid.UUID, reason string, scope models.BanScope, ipAddress, userAgent string) *errx.Error {
	// Check if user exists
	user, err := s.repo.GetUserDetail(ctx, userID)
	if err != nil {
		errs.CaptureException(err)
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

	// Default to login-only ban when the caller leaves scope empty so
	// the legacy "you can't log in" behaviour is preserved.
	if scope == 0 {
		scope = models.BanScopeLogin
	}

	if err := s.repo.BanUser(ctx, userID, adminID, reason, uint32(scope)); err != nil {
		errs.CaptureException(err)
		return errx.New(errx.Internal, "failed to ban user")
	}

	// A login ban that leaves live sessions alone bans nothing for up to twelve
	// hours: the access token keeps working, the refresh token mints new ones
	// from it, and the websocket keeps streaming. uuid.Nil matches no session,
	// so every one of them is revoked, and a failure is reported rather than
	// letting the admin trust a ban that left sessions running.
	var revokeErr *errx.Error
	if models.BanScope(scope).Has(models.BanScopeLogin) && s.sessions != nil {
		revokeErr = s.sessions.RevokeOtherSessions(ctx, userID, uuid.Nil)
	}

	s.logAction(ctx, adminID, "ban_user", "user", userID, map[string]any{"reason": reason, "scope": uint32(scope)}, ipAddress, userAgent)
	if revokeErr != nil {
		errs.CaptureException(revokeErr)
		return errx.NewPublic(errx.Internal, "The ban is recorded, but some of the user's sessions could not be ended. Unban and ban again to retry.")
	}
	return nil
}

func (s *adminService) UnbanUser(ctx context.Context, adminID, userID uuid.UUID, reason string, ipAddress, userAgent string) *errx.Error {
	// Check if user exists
	user, err := s.repo.GetUserDetail(ctx, userID)
	if err != nil {
		errs.CaptureException(err)
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
		errs.CaptureException(err)
		return errx.New(errx.Internal, "failed to unban user")
	}

	s.logAction(ctx, adminID, "unban_user", "user", userID, map[string]any{"reason": reason}, ipAddress, userAgent)
	return nil
}

func (s *adminService) GetUserBans(ctx context.Context, userID uuid.UUID) ([]models.UserBan, *errx.Error) {
	bans, err := s.repo.GetUserBans(ctx, userID)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get user bans")
	}
	return bans, nil
}

func (s *adminService) GetUserCampaigns(ctx context.Context, userID uuid.UUID, offset, limit int) (*models.AdminCampaignsResult, *errx.Error) {
	search := &models.AdminCampaignSearch{
		UserID: &models.ParamUUID{UUID: userID},
		Offset: offset,
		Limit:  limit,
	}
	result, err := s.repo.SearchCampaigns(ctx, search)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get user campaigns")
	}
	return result, nil
}

func (s *adminService) GetUserEmails(ctx context.Context, userID uuid.UUID, offset, limit int) ([]models.AdminWorkerEmail, *models.Pagination, *errx.Error) {
	emails, pagination, err := s.repo.GetUserEmails(ctx, userID, offset, limit)
	if err != nil {
		errs.CaptureException(err)
		return nil, nil, errx.New(errx.Internal, "failed to get user emails")
	}
	return emails, pagination, nil
}

func (s *adminService) GetUserRateLimits(ctx context.Context, userID uuid.UUID) (*models.AdminUserRateLimits, *errx.Error) {
	limits, err := s.repo.GetUserRateLimits(ctx, userID)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get rate limits")
	}
	// No row means the enforcement path falls back to the product defaults.
	if limits == nil {
		return models.DefaultAdminUserRateLimits(userID), nil
	}
	return limits, nil
}

func (s *adminService) UpdateUserRateLimits(ctx context.Context, adminID, userID uuid.UUID, update *models.UpdateUserRateLimitsRequest, ipAddress, userAgent string) (*models.AdminUserRateLimits, *errx.Error) {
	if err := s.repo.UpdateUserRateLimits(ctx, userID, adminID, update); err != nil {
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to update rate limits")
	}

	s.logAction(ctx, adminID, "update_rate_limits", "user", userID, map[string]any{"limits": update}, ipAddress, userAgent)
	return s.GetUserRateLimits(ctx, userID)
}

// Worker Management

func (s *adminService) ListWorkers(ctx context.Context, offset, limit int) (*models.AdminWorkersResult, *errx.Error) {
	result, err := s.repo.ListWorkers(ctx, offset, limit)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to list workers")
	}
	return result, nil
}

func (s *adminService) GetWorkerDetail(ctx context.Context, workerID uuid.UUID) (*models.AdminWorkerDetail, *errx.Error) {
	worker, err := s.repo.GetWorkerDetail(ctx, workerID)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get worker detail")
	}
	if worker == nil {
		return nil, errx.ErrNotFound
	}
	return worker, nil
}

func (s *adminService) UpdateWorker(ctx context.Context, adminID, workerID uuid.UUID, update *models.AdminUpdateWorker, ipAddress, userAgent string) *errx.Error {
	if err := s.repo.UpdateWorker(ctx, workerID, update); err != nil {
		errs.CaptureException(err)
		return errx.New(errx.Internal, "failed to update worker")
	}

	s.logAction(ctx, adminID, "update_worker", "worker", workerID, map[string]any{"update": update}, ipAddress, userAgent)
	return nil
}

func (s *adminService) GetWorkerEmails(ctx context.Context, workerID uuid.UUID, beforeAt time.Time, beforeID uuid.UUID, limit int) ([]models.AdminWorkerEmail, *models.Pagination, *errx.Error) {
	emails, pagination, err := s.repo.GetWorkerEmails(ctx, workerID, beforeAt, beforeID, limit)
	if err != nil {
		errs.CaptureException(err)
		return nil, nil, errx.New(errx.Internal, "failed to get worker emails")
	}
	return emails, pagination, nil
}

func (s *adminService) GetWorkerStats(ctx context.Context, workerID uuid.UUID) (*models.WorkerStats, *errx.Error) {
	stats, err := s.repo.GetWorkerStats(ctx, workerID)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get worker stats")
	}
	return stats, nil
}

func (s *adminService) ReassignEmails(ctx context.Context, adminID uuid.UUID, emailIDs []uuid.UUID, newWorkerID uuid.UUID, ipAddress, userAgent string) *errx.Error {
	if err := s.repo.ReassignEmails(ctx, emailIDs, newWorkerID); err != nil {
		errs.CaptureException(err)
		return errx.New(errx.Internal, "failed to reassign emails")
	}

	s.logAction(ctx, adminID, "reassign_emails", "worker", newWorkerID, map[string]any{"email_count": len(emailIDs)}, ipAddress, userAgent)
	return nil
}

// Warmup Management

func (s *adminService) ListWarmupPools(ctx context.Context) ([]models.WarmupPoolInfo, *errx.Error) {
	pools, err := s.repo.ListWarmupPools(ctx)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to list warmup pools")
	}
	return pools, nil
}

func (s *adminService) GetPoolParticipants(ctx context.Context, poolType string, offset, limit int) (*models.WarmupPoolParticipantsResult, *errx.Error) {
	result, err := s.repo.GetPoolParticipants(ctx, poolType, offset, limit)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get pool participants")
	}
	return result, nil
}

func (s *adminService) ListBlockedAccounts(ctx context.Context, offset, limit int) (*models.AdminBlockedAccountsResult, *errx.Error) {
	result, err := s.repo.ListBlockedAccounts(ctx, offset, limit)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to list blocked accounts")
	}
	return result, nil
}

func (s *adminService) BlockAccount(ctx context.Context, adminID, accountID uuid.UUID, reason string, ipAddress, userAgent string) *errx.Error {
	if err := s.repo.BlockAccount(ctx, accountID, adminID, reason); err != nil {
		errs.CaptureException(err)
		return errx.New(errx.Internal, "failed to block account")
	}

	s.logAction(ctx, adminID, "block_warmup_account", "email_account", accountID, map[string]any{"reason": reason}, ipAddress, userAgent)
	return nil
}

func (s *adminService) UnblockAccount(ctx context.Context, adminID, accountID uuid.UUID, ipAddress, userAgent string) *errx.Error {
	if err := s.repo.UnblockAccount(ctx, accountID); err != nil {
		errs.CaptureException(err)
		return errx.New(errx.Internal, "failed to unblock account")
	}

	s.logAction(ctx, adminID, "unblock_warmup_account", "email_account", accountID, nil, ipAddress, userAgent)
	return nil
}

// Appeals

func (s *adminService) ListAppeals(ctx context.Context, status string, offset, limit int) (*models.WarmupAppealsResult, *errx.Error) {
	result, err := s.repo.ListAppeals(ctx, status, offset, limit)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to list appeals")
	}
	return result, nil
}

func (s *adminService) GetAppeal(ctx context.Context, appealID uuid.UUID) (*models.WarmupAppeal, *errx.Error) {
	appeal, err := s.repo.GetAppeal(ctx, appealID)
	if err != nil {
		errs.CaptureException(err)
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
		errs.CaptureException(err)
		return errx.New(errx.Internal, "failed to get appeal")
	}
	if appeal == nil {
		return errx.ErrNotFound
	}

	if appeal.Status != models.WarmupAppealStatusPending {
		return errx.New(errx.BadRequest, "appeal has already been reviewed")
	}

	if err := s.repo.ReviewAppeal(ctx, appealID, adminID, approved, notes); err != nil {
		errs.CaptureException(err)
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
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to search campaigns")
	}
	return result, nil
}

func (s *adminService) GetCampaignDetail(ctx context.Context, campaignID uuid.UUID) (*models.AdminCampaignDetail, *errx.Error) {
	campaign, err := s.repo.GetCampaignDetail(ctx, campaignID)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get campaign detail")
	}
	if campaign == nil {
		return nil, errx.ErrNotFound
	}
	return campaign, nil
}

func (s *adminService) StopCampaign(ctx context.Context, adminID, campaignID uuid.UUID, reason, ipAddress, userAgent string) *errx.Error {
	campaign, err := s.repo.GetCampaignDetail(ctx, campaignID)
	if err != nil {
		errs.CaptureException(err)
		return errx.New(errx.Internal, "failed to get campaign")
	}
	if campaign == nil {
		return errx.ErrNotFound
	}

	// The repository decides: a draft has never sent and a completed campaign
	// never will, and either can become true between here and the UPDATE.
	stopped, err := s.repo.StopCampaign(ctx, campaignID)
	if err != nil {
		errs.CaptureException(err)
		return errx.New(errx.Internal, "failed to stop campaign")
	}
	if !stopped {
		return errx.New(errx.BadRequest, "campaign is not running")
	}

	// Without the reason the owner just sees a campaign that went quiet.
	if s.campaignLogRepo != nil {
		if err := s.campaignLogRepo.CreateLog(ctx, &repository.CampaignLogEntry{
			CampaignID: campaignID,
			EventType:  "stopped",
			Message:    "Campaign force-stopped by platform staff: " + reason,
			Metadata:   map[string]interface{}{"reason": reason},
		}); err != nil {
			errs.CaptureException(err)
		}
	}

	s.logAction(ctx, adminID, "force_stop_campaign", "campaign", campaignID, map[string]any{
		"user_id": campaign.UserID,
		"reason":  reason,
	}, ipAddress, userAgent)
	return nil
}

// Analytics

func (s *adminService) GetPlatformOverview(ctx context.Context) (*models.PlatformOverview, *errx.Error) {
	overview, err := s.repo.GetPlatformOverview(ctx)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get platform overview")
	}
	return overview, nil
}

func (s *adminService) GetAnalyticsTrends(ctx context.Context) (*models.AnalyticsTrends, *errx.Error) {
	trends, err := s.repo.GetAnalyticsTrends(ctx)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get analytics trends")
	}
	return trends, nil
}

func (s *adminService) GetDailyEmailStats(ctx context.Context, startDate, endDate time.Time) ([]models.DailyEmailStats, *errx.Error) {
	stats, err := s.repo.GetDailyEmailStats(ctx, startDate, endDate)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get daily email stats")
	}
	return stats, nil
}

func (s *adminService) GetHourlyEmailStats(ctx context.Context, date time.Time) ([]models.HourlyEmailStats, *errx.Error) {
	stats, err := s.repo.GetHourlyEmailStats(ctx, date)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get hourly email stats")
	}
	return stats, nil
}

func (s *adminService) GetUserGrowthStats(ctx context.Context, startDate, endDate time.Time) ([]models.UserGrowthStats, *errx.Error) {
	stats, err := s.repo.GetUserGrowthStats(ctx, startDate, endDate)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to get user growth stats")
	}
	return stats, nil
}

// Admin Management

func (s *adminService) ListAdmins(ctx context.Context, offset, limit int) (*models.AdminsResult, *errx.Error) {
	result, err := s.repo.ListAdmins(ctx, offset, limit)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to list admins")
	}
	return result, nil
}

func (s *adminService) GrantAdminPermissions(ctx context.Context, adminID, targetUserID uuid.UUID, permissions models.AdminPermission, ipAddress, userAgent string) *errx.Error {
	// Cannot modify own permissions
	if adminID == targetUserID {
		return errx.New(errx.BadRequest, "cannot modify your own permissions")
	}

	// An admin may only hand out permissions they hold themselves, so holding
	// grant_admin_access is not by itself a route to every other bit, directly
	// or in two hops through a colleague. A super admin holds every bit, so this
	// never blocks them.
	granter, gerr := s.repo.GetUserDetail(ctx, adminID)
	if gerr != nil {
		errs.CaptureException(gerr)
		return errx.New(errx.Internal, "failed to check admin permissions")
	}
	if granter == nil {
		return errx.ErrForbidden
	}
	if permissions&^granter.AdminPermissions != 0 {
		return errx.New(errx.Forbidden, "cannot grant an admin permission you do not hold yourself")
	}
	// The grant replaces the whole mask, so the bits it removes are checked like the bits it adds.
	target, terr := s.repo.GetUserDetail(ctx, targetUserID)
	if terr != nil {
		errs.CaptureException(terr)
		return errx.New(errx.Internal, "failed to load user")
	}
	if target != nil {
		if (target.AdminPermissions&^permissions)&^granter.AdminPermissions != 0 {
			return errx.New(errx.Forbidden, "cannot remove an admin permission you do not hold yourself")
		}
		if target.AdminPermissions.IsSuperAdmin() && !permissions.IsSuperAdmin() {
			remaining, cerr := s.repo.CountSuperAdmins(ctx)
			if cerr != nil {
				errs.CaptureException(cerr)
				return errx.New(errx.Internal, "failed to count admins")
			}
			if remaining <= 1 {
				return errx.New(errx.BadRequest, "this is the last super admin; grant another one before changing this one")
			}
		}
	}

	if err := s.repo.UpdateUserAdminPermissions(ctx, targetUserID, uint32(permissions), adminID); err != nil {
		errs.CaptureException(err)
		return errx.New(errx.Internal, "failed to grant admin permissions")
	}

	s.logAction(ctx, adminID, "grant_admin", "user", targetUserID, map[string]any{"permissions": permissions}, ipAddress, userAgent)
	s.notifyAccessChange(target, granter, permissions)
	return nil
}

func (s *adminService) RevokeAdminPermissions(ctx context.Context, adminID, targetUserID uuid.UUID, ipAddress, userAgent string) *errx.Error {
	// Cannot modify own permissions
	if adminID == targetUserID {
		return errx.New(errx.BadRequest, "cannot modify your own permissions")
	}

	// Refuse to remove the last super admin. warmblyctl already guards this;
	// the API did not, so the instance could be left with nobody able to grant
	// admin access back, recoverable only with database access.
	target, terr := s.repo.GetUserDetail(ctx, targetUserID)
	if terr != nil {
		errs.CaptureException(terr)
		return errx.New(errx.Internal, "failed to load user")
	}
	// Revoking removes every bit the target holds, so the revoker must hold them all.
	revoker, rerr := s.repo.GetUserDetail(ctx, adminID)
	if rerr != nil {
		errs.CaptureException(rerr)
		return errx.New(errx.Internal, "failed to check admin permissions")
	}
	if revoker == nil || (target != nil && target.AdminPermissions&^revoker.AdminPermissions != 0) {
		return errx.New(errx.Forbidden, "cannot remove an admin permission you do not hold yourself")
	}
	if target != nil && target.AdminPermissions.IsSuperAdmin() {
		remaining, cerr := s.repo.CountSuperAdmins(ctx)
		if cerr != nil {
			errs.CaptureException(cerr)
			return errx.New(errx.Internal, "failed to count admins")
		}
		if remaining <= 1 {
			return errx.New(errx.BadRequest, "this is the last super admin; grant another one before revoking this one")
		}
	}

	if err := s.repo.UpdateUserAdminPermissions(ctx, targetUserID, 0, adminID); err != nil {
		errs.CaptureException(err)
		return errx.New(errx.Internal, "failed to revoke admin permissions")
	}

	s.logAction(ctx, adminID, "revoke_admin", "user", targetUserID, nil, ipAddress, userAgent)
	s.notifyAccessChange(target, revoker, 0)
	return nil
}

// Audit Logs

func (s *adminService) SearchAuditLogs(ctx context.Context, search *models.AdminAuditLogSearch) (*models.AdminAuditLogsResult, *errx.Error) {
	result, err := s.repo.SearchAuditLogs(ctx, search)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.New(errx.Internal, "failed to search audit logs")
	}
	return result, nil
}
