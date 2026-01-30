package repository

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// AdminRepository defines the interface for admin data access
type AdminRepository interface {
	// User Management
	SearchUsers(ctx context.Context, search *models.AdminUserSearch) (*models.AdminUsersResult, error)
	GetUserDetail(ctx context.Context, userID uuid.UUID) (*models.AdminUserDetail, error)
	GetUserPreview(ctx context.Context, userID uuid.UUID) (*models.AdminUserPreview, error)
	UpdateUserAdminPermissions(ctx context.Context, userID uuid.UUID, permissions uint32, grantedBy uuid.UUID) error
	BanUser(ctx context.Context, userID, bannedBy uuid.UUID, reason string) error
	UnbanUser(ctx context.Context, userID, unbannedBy uuid.UUID, reason string) error
	GetUserBans(ctx context.Context, userID uuid.UUID) ([]models.UserBan, error)
	ListAdmins(ctx context.Context, cursor *uuid.UUID, limit int) (*models.AdminsResult, error)

	// Worker Management
	ListWorkers(ctx context.Context, cursor *uuid.UUID, limit int) (*models.AdminWorkersResult, error)
	GetWorkerDetail(ctx context.Context, workerID uuid.UUID) (*models.AdminWorkerDetail, error)
	UpdateWorker(ctx context.Context, workerID uuid.UUID, update *models.AdminUpdateWorker) error
	GetWorkerEmails(ctx context.Context, workerID uuid.UUID, cursor *uuid.UUID, limit int) ([]models.AdminWorkerEmail, *models.Pagination, error)
	GetWorkerStats(ctx context.Context, workerID uuid.UUID) (*models.WorkerStats, error)
	ReassignEmails(ctx context.Context, emailIDs []uuid.UUID, newWorkerID uuid.UUID) error

	// Warmup Management
	ListWarmupPools(ctx context.Context) ([]models.WarmupPoolInfo, error)
	GetPoolParticipants(ctx context.Context, poolType string, cursor *uuid.UUID, limit int) (*models.WarmupPoolParticipantsResult, error)
	ListBlockedAccounts(ctx context.Context, cursor *uuid.UUID, limit int) (*models.AdminBlockedAccountsResult, error)
	BlockAccount(ctx context.Context, accountID uuid.UUID, blockedBy uuid.UUID, reason string) error
	UnblockAccount(ctx context.Context, accountID uuid.UUID) error

	// Warmup Appeals
	ListAppeals(ctx context.Context, status string, cursor *uuid.UUID, limit int) (*models.WarmupAppealsResult, error)
	GetAppeal(ctx context.Context, appealID uuid.UUID) (*models.WarmupAppeal, error)
	ReviewAppeal(ctx context.Context, appealID uuid.UUID, reviewedBy uuid.UUID, approved bool, notes string) error

	// Campaign Management
	SearchCampaigns(ctx context.Context, search *models.AdminCampaignSearch) (*models.AdminCampaignsResult, error)
	GetCampaignDetail(ctx context.Context, campaignID uuid.UUID) (*models.AdminCampaignDetail, error)
	StopCampaign(ctx context.Context, campaignID uuid.UUID) error

	// Audit Logs
	CreateAuditLog(ctx context.Context, log *models.AdminAuditLog) error
	SearchAuditLogs(ctx context.Context, search *models.AdminAuditLogSearch) (*models.AdminAuditLogsResult, error)

	// Analytics
	GetPlatformOverview(ctx context.Context) (*models.PlatformOverview, error)
	GetDailyEmailStats(ctx context.Context, startDate, endDate time.Time) ([]models.DailyEmailStats, error)
	GetHourlyEmailStats(ctx context.Context, date time.Time) ([]models.HourlyEmailStats, error)
	GetWorkerLoadStats(ctx context.Context) ([]models.WorkerLoadStats, error)
	GetUserGrowthStats(ctx context.Context, startDate, endDate time.Time) ([]models.UserGrowthStats, error)
	GetAnalyticsTrends(ctx context.Context) (*models.AnalyticsTrends, error)
	GetEmailDistribution(ctx context.Context) ([]models.EmailDistribution, error)

	// Plans
	ListPlans(ctx context.Context, includePrivate bool) ([]models.Plan, error)
	CreatePlan(ctx context.Context, plan *models.Plan) error
	GetPlan(ctx context.Context, planID uuid.UUID) (*models.Plan, error)
	UpdatePlan(ctx context.Context, plan *models.Plan) error
	DeletePlan(ctx context.Context, planID uuid.UUID) error
	IsPlanInUse(ctx context.Context, planID uuid.UUID) (bool, error)

	// Enterprise Inquiries
	ListEnterpriseInquiries(ctx context.Context, status string, cursor *uuid.UUID, limit int) (*models.AdminEnterpriseInquiriesResult, error)
	GetEnterpriseInquiry(ctx context.Context, id uuid.UUID) (*models.AdminEnterpriseInquiry, error)
	UpdateEnterpriseInquiry(ctx context.Context, id uuid.UUID, update *models.UpdateEnterpriseInquiryRequest) error

	// User Rate Limits
	GetUserRateLimits(ctx context.Context, userID uuid.UUID) (*models.AdminUserRateLimits, error)
	UpdateUserRateLimits(ctx context.Context, userID uuid.UUID, update *models.UpdateUserRateLimitsRequest) error
}

type adminRepository struct {
	db *pgxpool.Pool
}

// NewAdminRepository creates a new admin repository
func NewAdminRepository(db *pgxpool.Pool) AdminRepository {
	return &adminRepository{db: db}
}

// SearchUsers searches for users with pagination
func (r *adminRepository) SearchUsers(ctx context.Context, search *models.AdminUserSearch) (*models.AdminUsersResult, error) {
	limit := search.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{}
	argNum := 1

	whereClause := "WHERE 1=1"
	if search.Query != "" {
		whereClause += ` AND (u.email ILIKE $` + itoa(argNum) + ` OR u.first_name ILIKE $` + itoa(argNum) + ` OR u.last_name ILIKE $` + itoa(argNum) + `)`
		args = append(args, "%"+search.Query+"%")
		argNum++
	}

	if search.Status == "banned" {
		whereClause += ` AND u.banned_at IS NOT NULL`
	} else if search.Status == "active" {
		whereClause += ` AND u.banned_at IS NULL`
	}

	if search.IsAdmin != nil && *search.IsAdmin {
		whereClause += ` AND u.admin_permissions > 0`
	}

	if search.Cursor != nil {
		whereClause += ` AND u.id < $` + itoa(argNum)
		args = append(args, *search.Cursor)
		argNum++
	}

	orderBy := "ORDER BY u.created_at DESC"
	if search.SortBy != "" {
		switch search.SortBy {
		case "email":
			orderBy = "ORDER BY u.email"
		case "name":
			orderBy = "ORDER BY u.first_name, u.last_name"
		}
		if search.SortDesc {
			orderBy += " DESC"
		}
	}

	args = append(args, limit+1)

	query := `
		SELECT
			u.id, u.first_name, u.last_name, u.email, u.max_organizations, u.free_trial_used,
			u.admin_permissions, u.admin_granted_at, u.admin_granted_by, u.banned_at,
			u.created_at, u.updated_at,
			(SELECT COUNT(*) FROM organization_members om WHERE om.user_id = u.id) as org_count,
			(SELECT COUNT(*) FROM email_accounts ea WHERE ea.user_id = u.id::text) as email_count,
			(SELECT COUNT(*) FROM campaigns c WHERE c.user_id = u.id) as campaign_count
		FROM users u
		` + whereClause + `
		` + orderBy + `
		LIMIT $` + itoa(argNum)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.AdminUserDetail
	for rows.Next() {
		var u models.AdminUserDetail
		err := rows.Scan(
			&u.ID, &u.FirstName, &u.LastName, &u.Email, &u.MaxOrganizations, &u.FreeTrialUsed,
			&u.AdminPermissions, &u.AdminGrantedAt, &u.AdminGrantedBy, &u.BannedAt,
			&u.CreatedAt, &u.UpdatedAt,
			&u.OrganizationCount, &u.EmailAccountCount, &u.CampaignCount,
		)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}

	result := &models.AdminUsersResult{
		Data: users,
		Pagination: models.Pagination{
			HasMore: len(users) > limit,
		},
	}

	if len(users) > limit {
		result.Data = users[:limit]
		lastID := users[limit-1].ID
		result.Pagination.NextCursor = &lastID
	}

	// Get total count
	countQuery := `SELECT COUNT(*) FROM users u ` + whereClause
	var total int64
	if err := r.db.QueryRow(ctx, countQuery, args[:len(args)-1]...).Scan(&total); err == nil {
		result.Pagination.Total = &total
	}

	return result, nil
}

// GetUserDetail gets detailed user information
func (r *adminRepository) GetUserDetail(ctx context.Context, userID uuid.UUID) (*models.AdminUserDetail, error) {
	query := `
		SELECT
			u.id, u.first_name, u.last_name, u.email, u.max_organizations, u.free_trial_used,
			u.admin_permissions, u.admin_granted_at, u.admin_granted_by, u.banned_at,
			u.created_at, u.updated_at,
			(SELECT COUNT(*) FROM organization_members om WHERE om.user_id = u.id) as org_count,
			(SELECT COUNT(*) FROM email_accounts ea WHERE ea.user_id = u.id::text) as email_count,
			(SELECT COUNT(*) FROM campaigns c WHERE c.user_id = u.id) as campaign_count
		FROM users u
		WHERE u.id = $1
	`

	var u models.AdminUserDetail
	err := r.db.QueryRow(ctx, query, userID).Scan(
		&u.ID, &u.FirstName, &u.LastName, &u.Email, &u.MaxOrganizations, &u.FreeTrialUsed,
		&u.AdminPermissions, &u.AdminGrantedAt, &u.AdminGrantedBy, &u.BannedAt,
		&u.CreatedAt, &u.UpdatedAt,
		&u.OrganizationCount, &u.EmailAccountCount, &u.CampaignCount,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUserPreview gets a complete preview of a user's account
func (r *adminRepository) GetUserPreview(ctx context.Context, userID uuid.UUID) (*models.AdminUserPreview, error) {
	user, err := r.GetUserDetail(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, nil
	}

	preview := &models.AdminUserPreview{
		User: *user,
	}

	// Get organizations
	orgQuery := `
		SELECT o.id, o.name, o.slug, o.owner_user_id, o.created_at, o.updated_at
		FROM organizations o
		JOIN organization_members om ON om.organization_id = o.id
		WHERE om.user_id = $1
	`
	orgRows, err := r.db.Query(ctx, orgQuery, userID)
	if err == nil {
		defer orgRows.Close()
		for orgRows.Next() {
			var org models.Organization
			if err := orgRows.Scan(&org.ID, &org.Name, &org.Slug, &org.OwnerUserID, &org.CreatedAt, &org.UpdatedAt); err == nil {
				preview.Organizations = append(preview.Organizations, org)
			}
		}
	}

	// Get subscriptions
	subQuery := `
		SELECT s.id, s.user_id, s.organization_id, s.plan_id, s.stripe_customer_id,
			s.stripe_subscription_id, s.stripe_price_id, s.status,
			s.current_period_start, s.current_period_end, s.cancel_at_period_end,
			s.canceled_at, s.trial_start, s.trial_end,
			s.free_trial_started_at, s.free_trial_ends_at, s.is_enterprise,
			s.created_at, s.updated_at
		FROM subscriptions s
		WHERE s.user_id = $1
	`
	subRows, err := r.db.Query(ctx, subQuery, userID)
	if err == nil {
		defer subRows.Close()
		for subRows.Next() {
			var sub models.Subscription
			if err := subRows.Scan(
				&sub.ID, &sub.UserID, &sub.OrganizationID, &sub.PlanID, &sub.StripeCustomerID,
				&sub.StripeSubscriptionID, &sub.StripePriceID, &sub.Status,
				&sub.CurrentPeriodStart, &sub.CurrentPeriodEnd, &sub.CancelAtPeriodEnd,
				&sub.CanceledAt, &sub.TrialStart, &sub.TrialEnd,
				&sub.FreeTrialStartedAt, &sub.FreeTrialEndsAt, &sub.IsEnterprise,
				&sub.CreatedAt, &sub.UpdatedAt,
			); err == nil {
				preview.Subscriptions = append(preview.Subscriptions, sub)
			}
		}
	}

	// Get email accounts
	emailQuery := `
		SELECT id, email, user_id, organization_id, status, provider,
			warmup IS NOT NULL as warmup_enabled, last_synced_at
		FROM email_accounts
		WHERE user_id = $1::text
	`
	emailRows, err := r.db.Query(ctx, emailQuery, userID)
	if err == nil {
		defer emailRows.Close()
		for emailRows.Next() {
			var email models.AdminWorkerEmail
			if err := emailRows.Scan(
				&email.ID, &email.Email, &email.UserID, &email.OrganizationID,
				&email.Status, &email.Provider, &email.WarmupEnabled, &email.LastSyncedAt,
			); err == nil {
				preview.EmailAccounts = append(preview.EmailAccounts, email)
			}
		}
	}

	// Get recent bans
	bans, _ := r.GetUserBans(ctx, userID)
	if len(bans) > 5 {
		bans = bans[:5]
	}
	preview.RecentBans = bans

	// Get rate limits
	preview.RateLimits, _ = r.GetUserRateLimits(ctx, userID)

	return preview, nil
}

// UpdateUserAdminPermissions updates a user's admin permissions
func (r *adminRepository) UpdateUserAdminPermissions(ctx context.Context, userID uuid.UUID, permissions uint32, grantedBy uuid.UUID) error {
	var grantedAt *time.Time
	var grantedByPtr *uuid.UUID
	if permissions > 0 {
		now := time.Now()
		grantedAt = &now
		grantedByPtr = &grantedBy
	}

	query := `
		UPDATE users SET
			admin_permissions = $2,
			admin_granted_at = $3,
			admin_granted_by = $4,
			updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.db.Exec(ctx, query, userID, permissions, grantedAt, grantedByPtr)
	return err
}

// BanUser bans a user
func (r *adminRepository) BanUser(ctx context.Context, userID, bannedBy uuid.UUID, reason string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Update user
	_, err = tx.Exec(ctx, `UPDATE users SET banned_at = NOW(), updated_at = NOW() WHERE id = $1`, userID)
	if err != nil {
		return err
	}

	// Create ban record
	_, err = tx.Exec(ctx, `
		INSERT INTO user_bans (user_id, banned_by, reason, banned_at)
		VALUES ($1, $2, $3, NOW())
	`, userID, bannedBy, reason)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// UnbanUser unbans a user
func (r *adminRepository) UnbanUser(ctx context.Context, userID, unbannedBy uuid.UUID, reason string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Update user
	_, err = tx.Exec(ctx, `UPDATE users SET banned_at = NULL, updated_at = NOW() WHERE id = $1`, userID)
	if err != nil {
		return err
	}

	// Update most recent ban record
	_, err = tx.Exec(ctx, `
		UPDATE user_bans SET unbanned_at = NOW(), unbanned_by = $2, unban_reason = $3
		WHERE user_id = $1 AND unbanned_at IS NULL
	`, userID, unbannedBy, reason)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// GetUserBans gets the ban history for a user
func (r *adminRepository) GetUserBans(ctx context.Context, userID uuid.UUID) ([]models.UserBan, error) {
	query := `
		SELECT ub.id, ub.user_id, ub.banned_by, ub.reason, ub.banned_at,
			ub.unbanned_at, ub.unbanned_by, ub.unban_reason,
			bu.id, bu.first_name, bu.last_name, bu.email,
			uu.id, uu.first_name, uu.last_name, uu.email
		FROM user_bans ub
		JOIN users bu ON bu.id = ub.banned_by
		LEFT JOIN users uu ON uu.id = ub.unbanned_by
		WHERE ub.user_id = $1
		ORDER BY ub.banned_at DESC
	`
	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bans []models.UserBan
	for rows.Next() {
		var ban models.UserBan
		var bannedByUser models.AdminUserSummary
		var unbannedByID, unbannedByFirstName, unbannedByLastName, unbannedByEmail *string

		err := rows.Scan(
			&ban.ID, &ban.UserID, &ban.BannedBy, &ban.Reason, &ban.BannedAt,
			&ban.UnbannedAt, &ban.UnbannedBy, &ban.UnbanReason,
			&bannedByUser.ID, &bannedByUser.FirstName, &bannedByUser.LastName, &bannedByUser.Email,
			&unbannedByID, &unbannedByFirstName, &unbannedByLastName, &unbannedByEmail,
		)
		if err != nil {
			return nil, err
		}

		ban.BannedByUser = &bannedByUser
		if unbannedByID != nil {
			id, _ := uuid.Parse(*unbannedByID)
			ban.UnbannedByUser = &models.AdminUserSummary{
				ID:        id,
				FirstName: *unbannedByFirstName,
				LastName:  *unbannedByLastName,
				Email:     *unbannedByEmail,
			}
		}

		bans = append(bans, ban)
	}

	return bans, nil
}

// ListAdmins lists all admin users
func (r *adminRepository) ListAdmins(ctx context.Context, cursor *uuid.UUID, limit int) (*models.AdminsResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{limit + 1}
	whereClause := "WHERE u.admin_permissions > 0"
	if cursor != nil {
		whereClause += " AND u.id < $2"
		args = append(args, *cursor)
	}

	query := `
		SELECT u.id, u.first_name, u.last_name, u.email, u.admin_permissions,
			u.admin_granted_at, u.admin_granted_by,
			gu.id, gu.first_name, gu.last_name, gu.email
		FROM users u
		LEFT JOIN users gu ON gu.id = u.admin_granted_by
		` + whereClause + `
		ORDER BY u.admin_granted_at DESC
		LIMIT $1
	`

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var admins []models.AdminInfo
	for rows.Next() {
		var admin models.AdminInfo
		var grantedByID *uuid.UUID
		var grantedByFirstName, grantedByLastName, grantedByEmail *string

		err := rows.Scan(
			&admin.ID, &admin.FirstName, &admin.LastName, &admin.Email, &admin.AdminPermissions,
			&admin.AdminGrantedAt, &admin.AdminGrantedBy,
			&grantedByID, &grantedByFirstName, &grantedByLastName, &grantedByEmail,
		)
		if err != nil {
			return nil, err
		}

		if grantedByID != nil {
			admin.GrantedByUser = &models.AdminUserSummary{
				ID:        *grantedByID,
				FirstName: *grantedByFirstName,
				LastName:  *grantedByLastName,
				Email:     *grantedByEmail,
			}
		}

		admins = append(admins, admin)
	}

	result := &models.AdminsResult{
		Data: admins,
		Pagination: models.Pagination{
			HasMore: len(admins) > limit,
		},
	}

	if len(admins) > limit {
		result.Data = admins[:limit]
		lastID := admins[limit-1].ID
		result.Pagination.NextCursor = &lastID
	}

	return result, nil
}

// ListWorkers lists all workers with details
func (r *adminRepository) ListWorkers(ctx context.Context, cursor *uuid.UUID, limit int) (*models.AdminWorkersResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{limit + 1}
	whereClause := ""
	if cursor != nil {
		whereClause = "WHERE w.id < $2"
		args = append(args, *cursor)
	}

	query := `
		SELECT w.id, w.name, COALESCE(w.notes, ''), w.ip_addr, w.active,
			COALESCE(w.free_tier, false), COALESCE(w.worker_type, 'shared'),
			w.created_at, w.updated_at,
			(SELECT COUNT(*) FROM email_accounts ea WHERE ea.worker_id = w.id) as connected_emails
		FROM workers w
		` + whereClause + `
		ORDER BY w.created_at DESC
		LIMIT $1
	`

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workers []models.AdminWorkerDetail
	for rows.Next() {
		var w models.AdminWorkerDetail
		err := rows.Scan(
			&w.ID, &w.Name, &w.Notes, &w.IPAddr, &w.Active,
			&w.FreeTier, &w.WorkerType,
			&w.CreatedAt, &w.UpdatedAt,
			&w.ConnectedEmails,
		)
		if err != nil {
			return nil, err
		}
		workers = append(workers, w)
	}

	result := &models.AdminWorkersResult{
		Data: workers,
		Pagination: models.Pagination{
			HasMore: len(workers) > limit,
		},
	}

	if len(workers) > limit {
		result.Data = workers[:limit]
		lastID := workers[limit-1].ID
		result.Pagination.NextCursor = &lastID
	}

	return result, nil
}

// GetWorkerDetail gets detailed worker information
func (r *adminRepository) GetWorkerDetail(ctx context.Context, workerID uuid.UUID) (*models.AdminWorkerDetail, error) {
	query := `
		SELECT w.id, w.name, COALESCE(w.notes, ''), w.ip_addr, w.active,
			COALESCE(w.free_tier, false), COALESCE(w.worker_type, 'shared'),
			w.created_at, w.updated_at,
			(SELECT COUNT(*) FROM email_accounts ea WHERE ea.worker_id = w.id) as connected_emails,
			(SELECT COUNT(*) FROM email_accounts ea WHERE ea.worker_id = w.id AND ea.warmup IS NOT NULL) as warmup_emails
		FROM workers w
		WHERE w.id = $1
	`

	var w models.AdminWorkerDetail
	err := r.db.QueryRow(ctx, query, workerID).Scan(
		&w.ID, &w.Name, &w.Notes, &w.IPAddr, &w.Active,
		&w.FreeTier, &w.WorkerType,
		&w.CreatedAt, &w.UpdatedAt,
		&w.ConnectedEmails, &w.WarmupEmails,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// UpdateWorker updates a worker
func (r *adminRepository) UpdateWorker(ctx context.Context, workerID uuid.UUID, update *models.AdminUpdateWorker) error {
	setClauses := []string{"updated_at = NOW()"}
	args := []interface{}{workerID}
	argNum := 2

	if update.Name != nil {
		setClauses = append(setClauses, "name = $"+itoa(argNum))
		args = append(args, *update.Name)
		argNum++
	}
	if update.Notes != nil {
		setClauses = append(setClauses, "notes = $"+itoa(argNum))
		args = append(args, *update.Notes)
		argNum++
	}
	if update.Active != nil {
		setClauses = append(setClauses, "active = $"+itoa(argNum))
		args = append(args, *update.Active)
		argNum++
	}
	if update.WorkerType != nil {
		setClauses = append(setClauses, "worker_type = $"+itoa(argNum))
		args = append(args, *update.WorkerType)
		argNum++
	}

	query := "UPDATE workers SET " + joinStrings(setClauses, ", ") + " WHERE id = $1"
	_, err := r.db.Exec(ctx, query, args...)
	return err
}

// GetWorkerEmails gets emails connected to a worker
func (r *adminRepository) GetWorkerEmails(ctx context.Context, workerID uuid.UUID, cursor *uuid.UUID, limit int) ([]models.AdminWorkerEmail, *models.Pagination, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{workerID, limit + 1}
	whereClause := "WHERE ea.worker_id = $1"
	if cursor != nil {
		whereClause += " AND ea.id < $3"
		args = append(args, *cursor)
	}

	query := `
		SELECT ea.id, ea.email, ea.user_id::uuid, ea.organization_id,
			ea.status, ea.provider, ea.warmup IS NOT NULL, ea.last_synced_at
		FROM email_accounts ea
		` + whereClause + `
		ORDER BY ea.created_at DESC
		LIMIT $2
	`

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var emails []models.AdminWorkerEmail
	for rows.Next() {
		var e models.AdminWorkerEmail
		err := rows.Scan(
			&e.ID, &e.Email, &e.UserID, &e.OrganizationID,
			&e.Status, &e.Provider, &e.WarmupEnabled, &e.LastSyncedAt,
		)
		if err != nil {
			return nil, nil, err
		}
		emails = append(emails, e)
	}

	pagination := &models.Pagination{
		HasMore: len(emails) > limit,
	}

	if len(emails) > limit {
		emails = emails[:limit]
		pagination.NextCursor = &emails[limit-1].ID
	}

	return emails, pagination, nil
}

// GetWorkerStats gets statistics for a worker
func (r *adminRepository) GetWorkerStats(ctx context.Context, workerID uuid.UUID) (*models.WorkerStats, error) {
	// This would need to query actual task/email statistics tables
	// For now, return a basic implementation
	stats := &models.WorkerStats{
		WorkerID: workerID,
	}

	// Get emails sent today
	r.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM tasks t
		WHERE t.worker_id = $1 AND t.created_at >= CURRENT_DATE
	`, workerID).Scan(&stats.EmailsSentToday)

	return stats, nil
}

// ReassignEmails reassigns emails to a new worker
func (r *adminRepository) ReassignEmails(ctx context.Context, emailIDs []uuid.UUID, newWorkerID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE email_accounts SET worker_id = $1, updated_at = NOW()
		WHERE id = ANY($2)
	`, newWorkerID, emailIDs)
	return err
}

// ListWarmupPools lists all warmup pools
func (r *adminRepository) ListWarmupPools(ctx context.Context) ([]models.WarmupPoolInfo, error) {
	// This would query the warmup pools table
	// Implementation depends on the actual warmup pool structure
	return []models.WarmupPoolInfo{
		{Type: "standard", TotalParticipants: 0, ActiveParticipants: 0, BlockedCount: 0},
		{Type: "premium", TotalParticipants: 0, ActiveParticipants: 0, BlockedCount: 0},
	}, nil
}

// GetPoolParticipants gets participants in a warmup pool
func (r *adminRepository) GetPoolParticipants(ctx context.Context, poolType string, cursor *uuid.UUID, limit int) (*models.WarmupPoolParticipantsResult, error) {
	// Implementation depends on actual warmup pool structure
	return &models.WarmupPoolParticipantsResult{
		Data:       []models.WarmupPoolParticipant{},
		Pagination: models.Pagination{HasMore: false},
	}, nil
}

// ListBlockedAccounts lists blocked warmup accounts
func (r *adminRepository) ListBlockedAccounts(ctx context.Context, cursor *uuid.UUID, limit int) (*models.AdminBlockedAccountsResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	// Query blocked accounts from email_accounts where warmup_blocked = true or similar
	// This is a placeholder - actual implementation depends on how blocking is tracked
	return &models.AdminBlockedAccountsResult{
		Data:       []models.AdminBlockedAccount{},
		Pagination: models.Pagination{HasMore: false},
	}, nil
}

// BlockAccount blocks an account from warmup
func (r *adminRepository) BlockAccount(ctx context.Context, accountID uuid.UUID, blockedBy uuid.UUID, reason string) error {
	// Implementation depends on how blocking is tracked
	// Could be a column on email_accounts or a separate table
	return nil
}

// UnblockAccount unblocks an account from warmup
func (r *adminRepository) UnblockAccount(ctx context.Context, accountID uuid.UUID) error {
	// Implementation depends on how blocking is tracked
	return nil
}

// ListAppeals lists warmup appeals
func (r *adminRepository) ListAppeals(ctx context.Context, status string, cursor *uuid.UUID, limit int) (*models.WarmupAppealsResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{limit + 1}
	whereClause := "WHERE 1=1"
	argNum := 2

	if status != "" {
		whereClause += " AND wa.status = $" + itoa(argNum)
		args = append(args, status)
		argNum++
	}

	if cursor != nil {
		whereClause += " AND wa.id < $" + itoa(argNum)
		args = append(args, *cursor)
	}

	query := `
		SELECT wa.id, wa.email_account_id, wa.user_id, wa.reason, wa.status,
			wa.reviewed_by, wa.reviewed_at, wa.review_notes, wa.created_at,
			u.id, u.first_name, u.last_name, u.email
		FROM warmup_appeals wa
		JOIN users u ON u.id = wa.user_id
		` + whereClause + `
		ORDER BY wa.created_at DESC
		LIMIT $1
	`

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var appeals []models.WarmupAppeal
	for rows.Next() {
		var a models.WarmupAppeal
		var user models.AdminUserSummary

		err := rows.Scan(
			&a.ID, &a.EmailAccountID, &a.UserID, &a.Reason, &a.Status,
			&a.ReviewedBy, &a.ReviewedAt, &a.ReviewNotes, &a.CreatedAt,
			&user.ID, &user.FirstName, &user.LastName, &user.Email,
		)
		if err != nil {
			return nil, err
		}

		a.User = &user
		appeals = append(appeals, a)
	}

	result := &models.WarmupAppealsResult{
		Data: appeals,
		Pagination: models.Pagination{
			HasMore: len(appeals) > limit,
		},
	}

	if len(appeals) > limit {
		result.Data = appeals[:limit]
		lastID := appeals[limit-1].ID
		result.Pagination.NextCursor = &lastID
	}

	return result, nil
}

// GetAppeal gets a specific appeal
func (r *adminRepository) GetAppeal(ctx context.Context, appealID uuid.UUID) (*models.WarmupAppeal, error) {
	query := `
		SELECT wa.id, wa.email_account_id, wa.user_id, wa.reason, wa.status,
			wa.reviewed_by, wa.reviewed_at, wa.review_notes, wa.created_at,
			u.id, u.first_name, u.last_name, u.email
		FROM warmup_appeals wa
		JOIN users u ON u.id = wa.user_id
		WHERE wa.id = $1
	`

	var a models.WarmupAppeal
	var user models.AdminUserSummary

	err := r.db.QueryRow(ctx, query, appealID).Scan(
		&a.ID, &a.EmailAccountID, &a.UserID, &a.Reason, &a.Status,
		&a.ReviewedBy, &a.ReviewedAt, &a.ReviewNotes, &a.CreatedAt,
		&user.ID, &user.FirstName, &user.LastName, &user.Email,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	a.User = &user
	return &a, nil
}

// ReviewAppeal reviews a warmup appeal
func (r *adminRepository) ReviewAppeal(ctx context.Context, appealID uuid.UUID, reviewedBy uuid.UUID, approved bool, notes string) error {
	status := models.WarmupAppealStatusRejected
	if approved {
		status = models.WarmupAppealStatusApproved
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Update appeal
	_, err = tx.Exec(ctx, `
		UPDATE warmup_appeals SET
			status = $2, reviewed_by = $3, reviewed_at = NOW(), review_notes = $4
		WHERE id = $1
	`, appealID, status, reviewedBy, notes)
	if err != nil {
		return err
	}

	// If approved, unblock the account
	if approved {
		var accountID uuid.UUID
		err = tx.QueryRow(ctx, `SELECT email_account_id FROM warmup_appeals WHERE id = $1`, appealID).Scan(&accountID)
		if err == nil {
			// Unblock the account (implementation depends on how blocking is tracked)
			// This is a placeholder
		}
	}

	return tx.Commit(ctx)
}

// SearchCampaigns searches for campaigns with pagination
func (r *adminRepository) SearchCampaigns(ctx context.Context, search *models.AdminCampaignSearch) (*models.AdminCampaignsResult, error) {
	limit := search.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{}
	argNum := 1
	whereClause := "WHERE 1=1"

	if search.Query != "" {
		whereClause += " AND c.name ILIKE $" + itoa(argNum)
		args = append(args, "%"+search.Query+"%")
		argNum++
	}

	if search.UserID != nil {
		whereClause += " AND c.user_id = $" + itoa(argNum)
		args = append(args, *search.UserID)
		argNum++
	}

	if search.OrgID != nil {
		whereClause += " AND c.organization_id = $" + itoa(argNum)
		args = append(args, *search.OrgID)
		argNum++
	}

	if search.Status != "" && search.Status != "all" {
		whereClause += " AND c.status = $" + itoa(argNum)
		args = append(args, search.Status)
		argNum++
	}

	if search.Cursor != nil {
		whereClause += " AND c.id < $" + itoa(argNum)
		args = append(args, *search.Cursor)
		argNum++
	}

	args = append(args, limit+1)

	query := `
		SELECT c.id, c.name, c.user_id, c.organization_id, c.status, c.created_at,
			c.started_at, c.stopped_at,
			u.id, u.first_name, u.last_name, u.email
		FROM campaigns c
		JOIN users u ON u.id = c.user_id
		` + whereClause + `
		ORDER BY c.created_at DESC
		LIMIT $` + itoa(argNum)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var campaigns []models.AdminCampaignDetail
	for rows.Next() {
		var c models.AdminCampaignDetail
		var user models.AdminUserSummary

		err := rows.Scan(
			&c.ID, &c.Name, &c.UserID, &c.OrganizationID, &c.Status, &c.CreatedAt,
			&c.StartedAt, &c.StoppedAt,
			&user.ID, &user.FirstName, &user.LastName, &user.Email,
		)
		if err != nil {
			return nil, err
		}

		c.User = &user
		campaigns = append(campaigns, c)
	}

	result := &models.AdminCampaignsResult{
		Data: campaigns,
		Pagination: models.Pagination{
			HasMore: len(campaigns) > limit,
		},
	}

	if len(campaigns) > limit {
		result.Data = campaigns[:limit]
		lastID := campaigns[limit-1].ID
		result.Pagination.NextCursor = &lastID
	}

	return result, nil
}

// GetCampaignDetail gets detailed campaign information
func (r *adminRepository) GetCampaignDetail(ctx context.Context, campaignID uuid.UUID) (*models.AdminCampaignDetail, error) {
	query := `
		SELECT c.id, c.name, c.user_id, c.organization_id, c.status, c.created_at,
			c.started_at, c.stopped_at,
			u.id, u.first_name, u.last_name, u.email
		FROM campaigns c
		JOIN users u ON u.id = c.user_id
		WHERE c.id = $1
	`

	var c models.AdminCampaignDetail
	var user models.AdminUserSummary

	err := r.db.QueryRow(ctx, query, campaignID).Scan(
		&c.ID, &c.Name, &c.UserID, &c.OrganizationID, &c.Status, &c.CreatedAt,
		&c.StartedAt, &c.StoppedAt,
		&user.ID, &user.FirstName, &user.LastName, &user.Email,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	c.User = &user
	return &c, nil
}

// StopCampaign force-stops a campaign
func (r *adminRepository) StopCampaign(ctx context.Context, campaignID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE campaigns SET status = 'stopped', stopped_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`, campaignID)
	return err
}

// CreateAuditLog creates an audit log entry
func (r *adminRepository) CreateAuditLog(ctx context.Context, log *models.AdminAuditLog) error {
	detailsJSON, _ := json.Marshal(log.Details)

	query := `
		INSERT INTO admin_audit_logs (id, admin_user_id, action, target_type, target_id, details, ip_address, user_agent, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := r.db.Exec(ctx, query,
		log.ID, log.AdminUserID, log.Action, log.TargetType, log.TargetID,
		detailsJSON, log.IPAddress, log.UserAgent, log.CreatedAt,
	)
	return err
}

// SearchAuditLogs searches audit logs with filters
func (r *adminRepository) SearchAuditLogs(ctx context.Context, search *models.AdminAuditLogSearch) (*models.AdminAuditLogsResult, error) {
	limit := search.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{}
	argNum := 1
	whereClause := "WHERE 1=1"

	if search.AdminUserID != nil {
		whereClause += " AND al.admin_user_id = $" + itoa(argNum)
		args = append(args, *search.AdminUserID)
		argNum++
	}

	if search.Action != "" {
		whereClause += " AND al.action = $" + itoa(argNum)
		args = append(args, search.Action)
		argNum++
	}

	if search.TargetType != "" {
		whereClause += " AND al.target_type = $" + itoa(argNum)
		args = append(args, search.TargetType)
		argNum++
	}

	if search.TargetID != nil {
		whereClause += " AND al.target_id = $" + itoa(argNum)
		args = append(args, *search.TargetID)
		argNum++
	}

	if search.StartDate != nil {
		whereClause += " AND al.created_at >= $" + itoa(argNum)
		args = append(args, *search.StartDate)
		argNum++
	}

	if search.EndDate != nil {
		whereClause += " AND al.created_at <= $" + itoa(argNum)
		args = append(args, *search.EndDate)
		argNum++
	}

	if search.Cursor != nil {
		whereClause += " AND al.id < $" + itoa(argNum)
		args = append(args, *search.Cursor)
		argNum++
	}

	args = append(args, limit+1)

	query := `
		SELECT al.id, al.admin_user_id, al.action, al.target_type, al.target_id,
			al.details, al.ip_address, al.user_agent, al.created_at,
			u.id, u.first_name, u.last_name, u.email
		FROM admin_audit_logs al
		JOIN users u ON u.id = al.admin_user_id
		` + whereClause + `
		ORDER BY al.created_at DESC
		LIMIT $` + itoa(argNum)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []models.AdminAuditLog
	for rows.Next() {
		var log models.AdminAuditLog
		var user models.AdminUserSummary
		var detailsJSON []byte

		err := rows.Scan(
			&log.ID, &log.AdminUserID, &log.Action, &log.TargetType, &log.TargetID,
			&detailsJSON, &log.IPAddress, &log.UserAgent, &log.CreatedAt,
			&user.ID, &user.FirstName, &user.LastName, &user.Email,
		)
		if err != nil {
			return nil, err
		}

		if len(detailsJSON) > 0 {
			json.Unmarshal(detailsJSON, &log.Details)
		}

		log.AdminUser = &user
		logs = append(logs, log)
	}

	result := &models.AdminAuditLogsResult{
		Data: logs,
		Pagination: models.Pagination{
			HasMore: len(logs) > limit,
		},
	}

	if len(logs) > limit {
		result.Data = logs[:limit]
		lastID := logs[limit-1].ID
		result.Pagination.NextCursor = &lastID
	}

	return result, nil
}

// GetPlatformOverview gets high-level platform statistics
func (r *adminRepository) GetPlatformOverview(ctx context.Context) (*models.PlatformOverview, error) {
	overview := &models.PlatformOverview{}

	// Total users
	r.db.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&overview.TotalUsers)

	// Active users (last 30 days)
	r.db.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE updated_at > NOW() - INTERVAL '30 days'`).Scan(&overview.ActiveUsers)

	// New users today
	r.db.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE created_at >= CURRENT_DATE`).Scan(&overview.NewUsersToday)

	// New users this week
	r.db.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE created_at >= CURRENT_DATE - INTERVAL '7 days'`).Scan(&overview.NewUsersThisWeek)

	// Total campaigns
	r.db.QueryRow(ctx, `SELECT COUNT(*) FROM campaigns`).Scan(&overview.TotalCampaigns)

	// Active campaigns
	r.db.QueryRow(ctx, `SELECT COUNT(*) FROM campaigns WHERE status = 'active'`).Scan(&overview.ActiveCampaigns)

	// Total workers
	r.db.QueryRow(ctx, `SELECT COUNT(*) FROM workers`).Scan(&overview.TotalWorkers)

	// Active workers
	r.db.QueryRow(ctx, `SELECT COUNT(*) FROM workers WHERE active = true`).Scan(&overview.ActiveWorkers)

	// Pending appeals
	r.db.QueryRow(ctx, `SELECT COUNT(*) FROM warmup_appeals WHERE status = 'pending'`).Scan(&overview.PendingAppeals)

	// Active subscriptions
	r.db.QueryRow(ctx, `SELECT COUNT(*) FROM subscriptions WHERE status = 'active'`).Scan(&overview.ActiveSubscriptions)

	// Trialing users
	r.db.QueryRow(ctx, `SELECT COUNT(*) FROM subscriptions WHERE status = 'trialing'`).Scan(&overview.TrialingUsers)

	return overview, nil
}

// GetDailyEmailStats gets daily email statistics
func (r *adminRepository) GetDailyEmailStats(ctx context.Context, startDate, endDate time.Time) ([]models.DailyEmailStats, error) {
	// This would query actual email statistics tables
	// Placeholder implementation
	return []models.DailyEmailStats{}, nil
}

// GetHourlyEmailStats gets hourly email statistics
func (r *adminRepository) GetHourlyEmailStats(ctx context.Context, date time.Time) ([]models.HourlyEmailStats, error) {
	// This would query actual email statistics tables
	// Placeholder implementation
	return []models.HourlyEmailStats{}, nil
}

// GetWorkerLoadStats gets worker load statistics
func (r *adminRepository) GetWorkerLoadStats(ctx context.Context) ([]models.WorkerLoadStats, error) {
	query := `
		SELECT w.id, w.name,
			(SELECT COUNT(*) FROM email_accounts ea WHERE ea.worker_id = w.id) as connected_emails
		FROM workers w
		WHERE w.active = true
	`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []models.WorkerLoadStats
	for rows.Next() {
		var s models.WorkerLoadStats
		err := rows.Scan(&s.WorkerID, &s.WorkerName, &s.ConnectedEmails)
		if err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}

	return stats, nil
}

// GetUserGrowthStats gets user growth statistics
func (r *adminRepository) GetUserGrowthStats(ctx context.Context, startDate, endDate time.Time) ([]models.UserGrowthStats, error) {
	query := `
		SELECT date_trunc('day', created_at)::date as date, COUNT(*) as new_users
		FROM users
		WHERE created_at BETWEEN $1 AND $2
		GROUP BY date_trunc('day', created_at)
		ORDER BY date
	`

	rows, err := r.db.Query(ctx, query, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []models.UserGrowthStats
	for rows.Next() {
		var s models.UserGrowthStats
		err := rows.Scan(&s.Date, &s.NewUsers)
		if err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}

	return stats, nil
}

// GetAnalyticsTrends gets trend data
func (r *adminRepository) GetAnalyticsTrends(ctx context.Context) (*models.AnalyticsTrends, error) {
	trends := &models.AnalyticsTrends{}

	// Calculate week-over-week growth
	var usersThisWeek, usersLastWeek int64
	r.db.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE created_at >= CURRENT_DATE - INTERVAL '7 days'`).Scan(&usersThisWeek)
	r.db.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE created_at >= CURRENT_DATE - INTERVAL '14 days' AND created_at < CURRENT_DATE - INTERVAL '7 days'`).Scan(&usersLastWeek)

	if usersLastWeek > 0 {
		trends.UsersGrowthPercent = float64(usersThisWeek-usersLastWeek) / float64(usersLastWeek) * 100
	}

	return trends, nil
}

// GetEmailDistribution gets email distribution across workers
func (r *adminRepository) GetEmailDistribution(ctx context.Context) ([]models.EmailDistribution, error) {
	query := `
		SELECT w.id, w.name, COUNT(ea.id) as email_count
		FROM workers w
		LEFT JOIN email_accounts ea ON ea.worker_id = w.id
		GROUP BY w.id, w.name
		ORDER BY email_count DESC
	`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var total int64
	var distributions []models.EmailDistribution
	for rows.Next() {
		var d models.EmailDistribution
		err := rows.Scan(&d.WorkerID, &d.WorkerName, &d.EmailCount)
		if err != nil {
			return nil, err
		}
		total += d.EmailCount
		distributions = append(distributions, d)
	}

	// Calculate percentages
	for i := range distributions {
		if total > 0 {
			distributions[i].Percentage = float64(distributions[i].EmailCount) / float64(total) * 100
		}
	}

	return distributions, nil
}

// ListPlans lists all plans
func (r *adminRepository) ListPlans(ctx context.Context, includePrivate bool) ([]models.Plan, error) {
	whereClause := ""
	if !includePrivate {
		whereClause = "WHERE public = true"
	}

	query := `
		SELECT id, name, max_contacts, daily_emails, ai_generation, account_limit,
			price, discounted_price, duration, savings, public,
			stripe_price_id, stripe_product_id, dedicated_workers, daily_campaign_limit,
			max_campaigns, max_active_campaigns, max_team_members, max_email_accounts,
			updated_at, created_at
		FROM plans
		` + whereClause + `
		ORDER BY price ASC
	`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var plans []models.Plan
	for rows.Next() {
		var p models.Plan
		err := rows.Scan(
			&p.ID, &p.Name, &p.MaxContacts, &p.DailyEmails, &p.AIGeneration, &p.AccountLimit,
			&p.Price, &p.DiscountedPrice, &p.Duration, &p.Savings, &p.Public,
			&p.StripePriceID, &p.StripeProductID, &p.DedicatedWorkers, &p.DailyCampaignLimit,
			&p.MaxCampaigns, &p.MaxActiveCampaigns, &p.MaxTeamMembers, &p.MaxEmailAccounts,
			&p.UpdatedAt, &p.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		plans = append(plans, p)
	}

	return plans, nil
}

// CreatePlan creates a new plan
func (r *adminRepository) CreatePlan(ctx context.Context, plan *models.Plan) error {
	query := `
		INSERT INTO plans (id, name, max_contacts, daily_emails, ai_generation, account_limit,
			price, discounted_price, duration, savings, public, dedicated_workers, daily_campaign_limit,
			max_campaigns, max_active_campaigns, max_team_members, max_email_accounts,
			created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
	`
	now := time.Now()
	_, err := r.db.Exec(ctx, query,
		plan.ID, plan.Name, plan.MaxContacts, plan.DailyEmails, plan.AIGeneration, plan.AccountLimit,
		plan.Price, plan.DiscountedPrice, plan.Duration, plan.Savings, plan.Public,
		plan.DedicatedWorkers, plan.DailyCampaignLimit,
		plan.MaxCampaigns, plan.MaxActiveCampaigns, plan.MaxTeamMembers, plan.MaxEmailAccounts,
		now, now,
	)
	return err
}

// GetPlan gets a plan by ID
func (r *adminRepository) GetPlan(ctx context.Context, planID uuid.UUID) (*models.Plan, error) {
	query := `
		SELECT id, name, max_contacts, daily_emails, ai_generation, account_limit,
			price, discounted_price, duration, savings, public,
			stripe_price_id, stripe_product_id, dedicated_workers, daily_campaign_limit,
			max_campaigns, max_active_campaigns, max_team_members, max_email_accounts,
			updated_at, created_at
		FROM plans WHERE id = $1
	`

	var p models.Plan
	err := r.db.QueryRow(ctx, query, planID).Scan(
		&p.ID, &p.Name, &p.MaxContacts, &p.DailyEmails, &p.AIGeneration, &p.AccountLimit,
		&p.Price, &p.DiscountedPrice, &p.Duration, &p.Savings, &p.Public,
		&p.StripePriceID, &p.StripeProductID, &p.DedicatedWorkers, &p.DailyCampaignLimit,
		&p.MaxCampaigns, &p.MaxActiveCampaigns, &p.MaxTeamMembers, &p.MaxEmailAccounts,
		&p.UpdatedAt, &p.CreatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdatePlan updates a plan
func (r *adminRepository) UpdatePlan(ctx context.Context, plan *models.Plan) error {
	query := `
		UPDATE plans SET name = $2, max_contacts = $3, daily_emails = $4, ai_generation = $5,
			account_limit = $6, price = $7, discounted_price = $8, duration = $9, public = $10,
			dedicated_workers = $11, daily_campaign_limit = $12, max_campaigns = $13,
			max_active_campaigns = $14, max_team_members = $15, max_email_accounts = $16,
			updated_at = $17
		WHERE id = $1
	`
	_, err := r.db.Exec(ctx, query,
		plan.ID, plan.Name, plan.MaxContacts, plan.DailyEmails, plan.AIGeneration,
		plan.AccountLimit, plan.Price, plan.DiscountedPrice, plan.Duration, plan.Public,
		plan.DedicatedWorkers, plan.DailyCampaignLimit, plan.MaxCampaigns,
		plan.MaxActiveCampaigns, plan.MaxTeamMembers, plan.MaxEmailAccounts,
		time.Now(),
	)
	return err
}

// DeletePlan deletes a plan
func (r *adminRepository) DeletePlan(ctx context.Context, planID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM plans WHERE id = $1`, planID)
	return err
}

// IsPlanInUse checks if a plan is being used by any subscription
func (r *adminRepository) IsPlanInUse(ctx context.Context, planID uuid.UUID) (bool, error) {
	var count int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM subscriptions WHERE plan_id = $1`, planID).Scan(&count)
	return count > 0, err
}

// ListEnterpriseInquiries lists enterprise inquiries
func (r *adminRepository) ListEnterpriseInquiries(ctx context.Context, status string, cursor *uuid.UUID, limit int) (*models.AdminEnterpriseInquiriesResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{limit + 1}
	argNum := 2
	whereClause := "WHERE 1=1"

	if status != "" && status != "all" {
		whereClause += " AND ei.status = $" + itoa(argNum)
		args = append(args, status)
		argNum++
	}

	if cursor != nil {
		whereClause += " AND ei.id < $" + itoa(argNum)
		args = append(args, *cursor)
	}

	query := `
		SELECT ei.id, ei.user_id, ei.company_name, ei.contact_name, ei.contact_email,
			ei.phone, ei.team_size, ei.notes, ei.status, ei.assigned_to,
			ei.created_at, COALESCE(ei.updated_at, ei.created_at) as updated_at,
			u.id, u.first_name, u.last_name, u.email,
			au.id, au.first_name, au.last_name, au.email
		FROM enterprise_inquiries ei
		LEFT JOIN users u ON u.id = ei.user_id
		LEFT JOIN users au ON au.id = ei.assigned_to
		` + whereClause + `
		ORDER BY ei.created_at DESC
		LIMIT $1
	`

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var inquiries []models.AdminEnterpriseInquiry
	for rows.Next() {
		var inq models.AdminEnterpriseInquiry
		var userID, assignedUserID *uuid.UUID
		var userFirstName, userLastName, userEmail *string
		var assignedFirstName, assignedLastName, assignedEmail *string

		err := rows.Scan(
			&inq.ID, &inq.UserID, &inq.CompanyName, &inq.ContactName, &inq.ContactEmail,
			&inq.Phone, &inq.TeamSize, &inq.Notes, &inq.Status, &inq.AssignedTo,
			&inq.CreatedAt, &inq.UpdatedAt,
			&userID, &userFirstName, &userLastName, &userEmail,
			&assignedUserID, &assignedFirstName, &assignedLastName, &assignedEmail,
		)
		if err != nil {
			return nil, err
		}

		if userID != nil {
			inq.User = &models.AdminUserSummary{
				ID:        *userID,
				FirstName: *userFirstName,
				LastName:  *userLastName,
				Email:     *userEmail,
			}
		}

		if assignedUserID != nil {
			inq.AssignedAdmin = &models.AdminUserSummary{
				ID:        *assignedUserID,
				FirstName: *assignedFirstName,
				LastName:  *assignedLastName,
				Email:     *assignedEmail,
			}
		}

		inquiries = append(inquiries, inq)
	}

	result := &models.AdminEnterpriseInquiriesResult{
		Data: inquiries,
		Pagination: models.Pagination{
			HasMore: len(inquiries) > limit,
		},
	}

	if len(inquiries) > limit {
		result.Data = inquiries[:limit]
		lastID := inquiries[limit-1].ID
		result.Pagination.NextCursor = &lastID
	}

	return result, nil
}

// GetEnterpriseInquiry gets a specific enterprise inquiry
func (r *adminRepository) GetEnterpriseInquiry(ctx context.Context, id uuid.UUID) (*models.AdminEnterpriseInquiry, error) {
	query := `
		SELECT ei.id, ei.user_id, ei.company_name, ei.contact_name, ei.contact_email,
			ei.phone, ei.team_size, ei.notes, ei.status, ei.assigned_to,
			ei.created_at, COALESCE(ei.updated_at, ei.created_at) as updated_at
		FROM enterprise_inquiries ei
		WHERE ei.id = $1
	`

	var inq models.AdminEnterpriseInquiry
	err := r.db.QueryRow(ctx, query, id).Scan(
		&inq.ID, &inq.UserID, &inq.CompanyName, &inq.ContactName, &inq.ContactEmail,
		&inq.Phone, &inq.TeamSize, &inq.Notes, &inq.Status, &inq.AssignedTo,
		&inq.CreatedAt, &inq.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &inq, nil
}

// UpdateEnterpriseInquiry updates an enterprise inquiry
func (r *adminRepository) UpdateEnterpriseInquiry(ctx context.Context, id uuid.UUID, update *models.UpdateEnterpriseInquiryRequest) error {
	setClauses := []string{"updated_at = NOW()"}
	args := []interface{}{id}
	argNum := 2

	if update.Status != nil {
		setClauses = append(setClauses, "status = $"+itoa(argNum))
		args = append(args, *update.Status)
		argNum++
	}
	if update.AssignedTo != nil {
		setClauses = append(setClauses, "assigned_to = $"+itoa(argNum))
		args = append(args, *update.AssignedTo)
		argNum++
	}
	if update.Notes != nil {
		setClauses = append(setClauses, "notes = $"+itoa(argNum))
		args = append(args, *update.Notes)
		argNum++
	}

	query := "UPDATE enterprise_inquiries SET " + joinStrings(setClauses, ", ") + " WHERE id = $1"
	_, err := r.db.Exec(ctx, query, args...)
	return err
}

// GetUserRateLimits gets rate limits for a user
func (r *adminRepository) GetUserRateLimits(ctx context.Context, userID uuid.UUID) (*models.AdminUserRateLimits, error) {
	query := `
		SELECT user_id, limit_ws_message_pm, limit_ws_join_pm, limit_ws_event_pm,
			max_connections, daily_email_limit, updated_at
		FROM user_rate_limits
		WHERE user_id = $1
	`

	var limits models.AdminUserRateLimits
	err := r.db.QueryRow(ctx, query, userID).Scan(
		&limits.UserID, &limits.LimitWSMessagePM, &limits.LimitWSJoinPM, &limits.LimitWSEventPM,
		&limits.MaxConnections, &limits.DailyEmailLimit, &limits.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &limits, nil
}

// UpdateUserRateLimits updates rate limits for a user
func (r *adminRepository) UpdateUserRateLimits(ctx context.Context, userID uuid.UUID, update *models.UpdateUserRateLimitsRequest) error {
	query := `
		INSERT INTO user_rate_limits (user_id, limit_ws_message_pm, limit_ws_join_pm, limit_ws_event_pm,
			max_connections, daily_email_limit, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			limit_ws_message_pm = COALESCE($2, user_rate_limits.limit_ws_message_pm),
			limit_ws_join_pm = COALESCE($3, user_rate_limits.limit_ws_join_pm),
			limit_ws_event_pm = COALESCE($4, user_rate_limits.limit_ws_event_pm),
			max_connections = COALESCE($5, user_rate_limits.max_connections),
			daily_email_limit = COALESCE($6, user_rate_limits.daily_email_limit),
			updated_at = NOW()
	`
	_, err := r.db.Exec(ctx, query, userID,
		update.LimitWSMessagePM, update.LimitWSJoinPM, update.LimitWSEventPM,
		update.MaxConnections, update.DailyEmailLimit,
	)
	return err
}

// Helper functions
func itoa(i int) string {
	return strconv.Itoa(i)
}

func joinStrings(strs []string, sep string) string {
	return strings.Join(strs, sep)
}
