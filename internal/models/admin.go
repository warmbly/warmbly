package models

import (
	"time"

	"github.com/google/uuid"
)

// AdminUserSearch represents search parameters for admin user listing
type AdminUserSearch struct {
	Query    string     `form:"q"`
	Status   string     `form:"status"` // active, banned, all
	IsAdmin  *bool      `form:"is_admin"`
	Cursor   *uuid.UUID `form:"cursor"`
	Limit    int        `form:"limit"`
	SortBy   string     `form:"sort_by"`   // created_at, email, name
	SortDesc bool       `form:"sort_desc"`
}

// AdminUserDetail represents a user with admin-relevant statistics
type AdminUserDetail struct {
	ID               uuid.UUID        `json:"id"`
	FirstName        string           `json:"first_name"`
	LastName         string           `json:"last_name"`
	Email            string           `json:"email"`
	MaxOrganizations int              `json:"max_organizations"`
	FreeTrialUsed    bool             `json:"free_trial_used"`
	AdminPermissions AdminPermission  `json:"admin_permissions"`
	AdminGrantedAt   *time.Time       `json:"admin_granted_at,omitempty"`
	AdminGrantedBy   *uuid.UUID       `json:"admin_granted_by,omitempty"`
	BannedAt         *time.Time       `json:"banned_at,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`

	// Statistics
	OrganizationCount int `json:"organization_count"`
	EmailAccountCount int `json:"email_account_count"`
	CampaignCount     int `json:"campaign_count"`
}

// AdminUsersResult represents paginated user listing
type AdminUsersResult struct {
	Data       []AdminUserDetail `json:"data"`
	Pagination Pagination        `json:"pagination"`
}

// UserBan represents a ban record for a user
type UserBan struct {
	ID          uuid.UUID  `json:"id"`
	UserID      uuid.UUID  `json:"user_id"`
	BannedBy    uuid.UUID  `json:"banned_by"`
	Reason      string     `json:"reason"`
	BannedAt    time.Time  `json:"banned_at"`
	UnbannedAt  *time.Time `json:"unbanned_at,omitempty"`
	UnbannedBy  *uuid.UUID `json:"unbanned_by,omitempty"`
	UnbanReason *string    `json:"unban_reason,omitempty"`

	// Joined data
	BannedByUser   *AdminUserSummary `json:"banned_by_user,omitempty"`
	UnbannedByUser *AdminUserSummary `json:"unbanned_by_user,omitempty"`
}

// AdminUserSummary is a minimal user representation for joined data
type AdminUserSummary struct {
	ID        uuid.UUID `json:"id"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	Email     string    `json:"email"`
}

// BanUserRequest represents the request to ban a user
type BanUserRequest struct {
	Reason string `json:"reason" binding:"required"`
}

// UnbanUserRequest represents the request to unban a user
type UnbanUserRequest struct {
	Reason string `json:"reason" binding:"required"`
}

// AdminWorkerDetail represents a worker with admin-relevant details
type AdminWorkerDetail struct {
	ID           uuid.UUID  `json:"id"`
	Name         string     `json:"name"`
	Notes        string     `json:"notes"`
	IPAddr       string     `json:"ip_addr"`
	Active       bool       `json:"active"`
	FreeTier     bool       `json:"free_tier"`
	WorkerType   WorkerType `json:"worker_type"`
	AccountCount int        `json:"account_count"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`

	// Statistics
	EmailsSentToday  int `json:"emails_sent_today"`
	EmailsSentTotal  int `json:"emails_sent_total"`
	ActiveCampaigns  int `json:"active_campaigns"`
	ConnectedEmails  int `json:"connected_emails"`
	WarmupEmails     int `json:"warmup_emails"`
}

// AdminWorkersResult represents paginated worker listing
type AdminWorkersResult struct {
	Data       []AdminWorkerDetail `json:"data"`
	Pagination Pagination          `json:"pagination"`
}

// AdminUpdateWorker represents the request to update a worker
type AdminUpdateWorker struct {
	Name       *string     `json:"name,omitempty"`
	Notes      *string     `json:"notes,omitempty"`
	Active     *bool       `json:"active,omitempty"`
	WorkerType *WorkerType `json:"worker_type,omitempty"`
}

// AdminWorkerEmail represents an email account connected to a worker
type AdminWorkerEmail struct {
	ID             uuid.UUID  `json:"id"`
	Email          string     `json:"email"`
	UserID         uuid.UUID  `json:"user_id"`
	OrganizationID *uuid.UUID `json:"organization_id,omitempty"`
	Status         string     `json:"status"`
	Provider       string     `json:"provider"`
	WarmupEnabled  bool       `json:"warmup_enabled"`
	LastSyncedAt   time.Time  `json:"last_synced_at"`
}

// ReassignEmailsRequest represents the request to reassign emails
type ReassignEmailsRequest struct {
	EmailIDs     []uuid.UUID `json:"email_ids" binding:"required"`
	NewWorkerID  uuid.UUID   `json:"new_worker_id" binding:"required"`
}

// WarmupAppealStatus represents the status of a warmup appeal
type WarmupAppealStatus string

const (
	WarmupAppealStatusPending  WarmupAppealStatus = "pending"
	WarmupAppealStatusApproved WarmupAppealStatus = "approved"
	WarmupAppealStatusRejected WarmupAppealStatus = "rejected"
)

// WarmupAppeal represents an appeal for a blocked warmup account
type WarmupAppeal struct {
	ID             uuid.UUID          `json:"id"`
	EmailAccountID uuid.UUID          `json:"email_account_id"`
	UserID         uuid.UUID          `json:"user_id"`
	Reason         string             `json:"reason"`
	Status         WarmupAppealStatus `json:"status"`
	ReviewedBy     *uuid.UUID         `json:"reviewed_by,omitempty"`
	ReviewedAt     *time.Time         `json:"reviewed_at,omitempty"`
	ReviewNotes    *string            `json:"review_notes,omitempty"`
	CreatedAt      time.Time          `json:"created_at"`

	// Joined data
	User         *AdminUserSummary `json:"user,omitempty"`
	EmailAccount *AdminWorkerEmail `json:"email_account,omitempty"`
	ReviewedByUser *AdminUserSummary `json:"reviewed_by_user,omitempty"`
}

// WarmupAppealsResult represents paginated appeal listing
type WarmupAppealsResult struct {
	Data       []WarmupAppeal `json:"data"`
	Pagination Pagination     `json:"pagination"`
}

// ReviewAppealRequest represents the request to review an appeal
type ReviewAppealRequest struct {
	Approved bool   `json:"approved"`
	Notes    string `json:"notes"`
}

// AdminBlockedAccount represents a blocked warmup account
type AdminBlockedAccount struct {
	ID           uuid.UUID        `json:"id"`
	Email        string           `json:"email"`
	UserID       uuid.UUID        `json:"user_id"`
	BlockedAt    time.Time        `json:"blocked_at"`
	BlockedBy    *uuid.UUID       `json:"blocked_by,omitempty"`
	BlockReason  string           `json:"block_reason"`
	HasAppeal    bool             `json:"has_appeal"`
	AppealStatus *WarmupAppealStatus `json:"appeal_status,omitempty"`

	// Joined data
	User *AdminUserSummary `json:"user,omitempty"`
}

// AdminBlockedAccountsResult represents paginated blocked accounts
type AdminBlockedAccountsResult struct {
	Data       []AdminBlockedAccount `json:"data"`
	Pagination Pagination            `json:"pagination"`
}

// BlockAccountRequest represents the request to block an account
type BlockAccountRequest struct {
	Reason string `json:"reason" binding:"required"`
}

// AdminCampaignDetail represents a campaign with admin-relevant details
type AdminCampaignDetail struct {
	ID             uuid.UUID  `json:"id"`
	Name           string     `json:"name"`
	UserID         uuid.UUID  `json:"user_id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	StoppedAt      *time.Time `json:"stopped_at,omitempty"`

	// Statistics
	TotalContacts int `json:"total_contacts"`
	EmailsSent    int `json:"emails_sent"`
	EmailsOpened  int `json:"emails_opened"`
	EmailsClicked int `json:"emails_clicked"`
	EmailsReplied int `json:"emails_replied"`
	EmailsBounced int `json:"emails_bounced"`

	// Joined data
	User         *AdminUserSummary `json:"user,omitempty"`
	Organization *Organization     `json:"organization,omitempty"`
}

// AdminCampaignsResult represents paginated campaign listing
type AdminCampaignsResult struct {
	Data       []AdminCampaignDetail `json:"data"`
	Pagination Pagination            `json:"pagination"`
}

// AdminCampaignSearch represents search parameters for campaigns
type AdminCampaignSearch struct {
	Query    string     `form:"q"`
	UserID   *uuid.UUID `form:"user_id"`
	OrgID    *uuid.UUID `form:"org_id"`
	Status   string     `form:"status"` // active, paused, completed, all
	Cursor   *uuid.UUID `form:"cursor"`
	Limit    int        `form:"limit"`
	SortBy   string     `form:"sort_by"`
	SortDesc bool       `form:"sort_desc"`
}

// AdminAuditLog represents an audit log entry for admin actions
type AdminAuditLog struct {
	ID          uuid.UUID         `json:"id"`
	AdminUserID uuid.UUID         `json:"admin_user_id"`
	Action      string            `json:"action"`
	TargetType  string            `json:"target_type"`
	TargetID    uuid.UUID         `json:"target_id"`
	Details     map[string]any    `json:"details,omitempty"`
	IPAddress   string            `json:"ip_address"`
	UserAgent   string            `json:"user_agent"`
	CreatedAt   time.Time         `json:"created_at"`

	// Joined data
	AdminUser *AdminUserSummary `json:"admin_user,omitempty"`
}

// AdminAuditLogsResult represents paginated audit logs
type AdminAuditLogsResult struct {
	Data       []AdminAuditLog `json:"data"`
	Pagination Pagination      `json:"pagination"`
}

// AdminAuditLogSearch represents search parameters for audit logs
type AdminAuditLogSearch struct {
	AdminUserID *uuid.UUID `form:"admin_user_id"`
	Action      string     `form:"action"`
	TargetType  string     `form:"target_type"`
	TargetID    *uuid.UUID `form:"target_id"`
	StartDate   *time.Time `form:"start_date"`
	EndDate     *time.Time `form:"end_date"`
	Cursor      *uuid.UUID `form:"cursor"`
	Limit       int        `form:"limit"`
}

// PlatformOverview represents high-level platform statistics
type PlatformOverview struct {
	TotalUsers        int64   `json:"total_users"`
	ActiveUsers       int64   `json:"active_users"`      // Active in last 30 days
	NewUsersToday     int64   `json:"new_users_today"`
	NewUsersThisWeek  int64   `json:"new_users_this_week"`

	TotalCampaigns    int64   `json:"total_campaigns"`
	ActiveCampaigns   int64   `json:"active_campaigns"`

	TotalEmailsSent   int64   `json:"total_emails_sent"`
	EmailsSentToday   int64   `json:"emails_sent_today"`

	TotalWorkers      int64   `json:"total_workers"`
	ActiveWorkers     int64   `json:"active_workers"`

	WarmupBlockedCount int64  `json:"warmup_blocked_count"`
	PendingAppeals    int64   `json:"pending_appeals"`

	ActiveSubscriptions int64 `json:"active_subscriptions"`
	TrialingUsers       int64 `json:"trialing_users"`
}

// DailyEmailStats represents daily email statistics for graphs
type DailyEmailStats struct {
	Date          time.Time `json:"date"`
	TotalSent     int64     `json:"total_sent"`
	TotalDelivered int64    `json:"total_delivered"`
	TotalBounced  int64     `json:"total_bounced"`
	TotalOpened   int64     `json:"total_opened"`
	TotalClicked  int64     `json:"total_clicked"`
	TotalReplied  int64     `json:"total_replied"`
}

// HourlyEmailStats represents hourly email statistics
type HourlyEmailStats struct {
	Hour      int   `json:"hour"`
	TotalSent int64 `json:"total_sent"`
}

// WorkerLoadStats represents worker load statistics
type WorkerLoadStats struct {
	WorkerID        uuid.UUID `json:"worker_id"`
	WorkerName      string    `json:"worker_name"`
	EmailsSentToday int64     `json:"emails_sent_today"`
	QueuedEmails    int64     `json:"queued_emails"`
	ConnectedEmails int64     `json:"connected_emails"`
	CPUUsage        float64   `json:"cpu_usage,omitempty"`
	MemoryUsage     float64   `json:"memory_usage,omitempty"`
}

// UserGrowthStats represents user growth statistics
type UserGrowthStats struct {
	Date      time.Time `json:"date"`
	NewUsers  int64     `json:"new_users"`
	TotalUsers int64    `json:"total_users"`
}

// AnalyticsTrends represents trend data for analytics
type AnalyticsTrends struct {
	UsersGrowthPercent     float64 `json:"users_growth_percent"`
	EmailsGrowthPercent    float64 `json:"emails_growth_percent"`
	CampaignsGrowthPercent float64 `json:"campaigns_growth_percent"`
	RevenueGrowthPercent   float64 `json:"revenue_growth_percent"`
}

// CreatePlanRequest represents the request to create a custom plan
type CreatePlanRequest struct {
	Name               string   `json:"name" binding:"required"`
	MaxContacts        uint     `json:"max_contacts"`
	DailyEmails        uint     `json:"daily_emails"`
	AIGeneration       bool     `json:"ai_generation"`
	AccountLimit       uint     `json:"account_limit"`
	Price              float32  `json:"price"`
	DiscountedPrice    float32  `json:"discounted_price"`
	Duration           Duration `json:"duration"` // month/year
	DedicatedWorkers   int      `json:"dedicated_workers"`
	DailyCampaignLimit *int     `json:"daily_campaign_limit,omitempty"`
	MaxCampaigns       *int     `json:"max_campaigns,omitempty"`
	MaxActiveCampaigns *int     `json:"max_active_campaigns,omitempty"`
	MaxTeamMembers     *int     `json:"max_team_members,omitempty"`
	MaxEmailAccounts   *int     `json:"max_email_accounts,omitempty"`
	Public             bool     `json:"public"` // false for enterprise-only
}

// UpdatePlanRequest represents the request to update a plan
type UpdatePlanRequest struct {
	Name               *string   `json:"name,omitempty"`
	MaxContacts        *uint     `json:"max_contacts,omitempty"`
	DailyEmails        *uint     `json:"daily_emails,omitempty"`
	AIGeneration       *bool     `json:"ai_generation,omitempty"`
	AccountLimit       *uint     `json:"account_limit,omitempty"`
	Price              *float32  `json:"price,omitempty"`
	DiscountedPrice    *float32  `json:"discounted_price,omitempty"`
	Duration           *Duration `json:"duration,omitempty"`
	DedicatedWorkers   *int      `json:"dedicated_workers,omitempty"`
	DailyCampaignLimit *int      `json:"daily_campaign_limit,omitempty"`
	MaxCampaigns       *int      `json:"max_campaigns,omitempty"`
	MaxActiveCampaigns *int      `json:"max_active_campaigns,omitempty"`
	MaxTeamMembers     *int      `json:"max_team_members,omitempty"`
	MaxEmailAccounts   *int      `json:"max_email_accounts,omitempty"`
	Public             *bool     `json:"public,omitempty"`
}

// AdminEnterpriseInquiry represents an enterprise inquiry with admin details
type AdminEnterpriseInquiry struct {
	ID                 uuid.UUID  `json:"id"`
	UserID             *uuid.UUID `json:"user_id,omitempty"`
	CompanyName        string     `json:"company_name"`
	ContactName        string     `json:"contact_name"`
	ContactEmail       string     `json:"contact_email"`
	Phone              *string    `json:"phone,omitempty"`
	TeamSize           *string    `json:"team_size,omitempty"`
	MonthlyEmailVolume *string    `json:"monthly_email_volume,omitempty"`
	Message            *string    `json:"message,omitempty"`
	Status             string     `json:"status"`
	AssignedTo         *uuid.UUID `json:"assigned_to,omitempty"`
	Notes              *string    `json:"notes,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`

	// Joined data
	User       *AdminUserSummary `json:"user,omitempty"`
	AssignedAdmin *AdminUserSummary `json:"assigned_admin,omitempty"`
}

// AdminEnterpriseInquiriesResult represents paginated inquiries
type AdminEnterpriseInquiriesResult struct {
	Data       []AdminEnterpriseInquiry `json:"data"`
	Pagination Pagination               `json:"pagination"`
}

// UpdateEnterpriseInquiryRequest represents the request to update an inquiry
type UpdateEnterpriseInquiryRequest struct {
	Status     *string    `json:"status,omitempty"`
	AssignedTo *uuid.UUID `json:"assigned_to,omitempty"`
	Notes      *string    `json:"notes,omitempty"`
}

// AdminInfo represents an admin user for listing
type AdminInfo struct {
	ID               uuid.UUID       `json:"id"`
	FirstName        string          `json:"first_name"`
	LastName         string          `json:"last_name"`
	Email            string          `json:"email"`
	AdminPermissions AdminPermission `json:"admin_permissions"`
	AdminGrantedAt   *time.Time      `json:"admin_granted_at,omitempty"`
	AdminGrantedBy   *uuid.UUID      `json:"admin_granted_by,omitempty"`

	// Joined
	GrantedByUser *AdminUserSummary `json:"granted_by_user,omitempty"`
}

// AdminsResult represents paginated admin listing
type AdminsResult struct {
	Data       []AdminInfo `json:"data"`
	Pagination Pagination  `json:"pagination"`
}

// GrantAdminRequest represents the request to grant admin permissions
type GrantAdminRequest struct {
	Permissions AdminPermission `json:"permissions" binding:"required"`
}

// AdminUserRateLimits represents rate limits for a specific user
type AdminUserRateLimits struct {
	UserID           uuid.UUID `json:"user_id"`
	LimitWSMessagePM *int      `json:"limit_ws_message_pm,omitempty"`
	LimitWSJoinPM    *int      `json:"limit_ws_join_pm,omitempty"`
	LimitWSEventPM   *int      `json:"limit_ws_event_pm,omitempty"`
	MaxConnections   *int      `json:"max_connections,omitempty"`
	DailyEmailLimit  *int      `json:"daily_email_limit,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// UpdateUserRateLimitsRequest represents the request to update user rate limits
type UpdateUserRateLimitsRequest struct {
	LimitWSMessagePM *int `json:"limit_ws_message_pm,omitempty"`
	LimitWSJoinPM    *int `json:"limit_ws_join_pm,omitempty"`
	LimitWSEventPM   *int `json:"limit_ws_event_pm,omitempty"`
	MaxConnections   *int `json:"max_connections,omitempty"`
	DailyEmailLimit  *int `json:"daily_email_limit,omitempty"`
}

// AdminUserPreview represents a full preview of a user's account
type AdminUserPreview struct {
	User          AdminUserDetail       `json:"user"`
	Organizations []Organization        `json:"organizations"`
	Subscriptions []Subscription        `json:"subscriptions"`
	EmailAccounts []AdminWorkerEmail    `json:"email_accounts"`
	RecentBans    []UserBan             `json:"recent_bans"`
	RateLimits    *AdminUserRateLimits  `json:"rate_limits,omitempty"`
}

// WarmupPoolInfo represents information about a warmup pool
type WarmupPoolInfo struct {
	Type             string `json:"type"`
	TotalParticipants int64 `json:"total_participants"`
	ActiveParticipants int64 `json:"active_participants"`
	BlockedCount      int64 `json:"blocked_count"`
}

// WarmupPoolParticipant represents a participant in a warmup pool
type WarmupPoolParticipant struct {
	ID            uuid.UUID  `json:"id"`
	Email         string     `json:"email"`
	UserID        uuid.UUID  `json:"user_id"`
	JoinedAt      time.Time  `json:"joined_at"`
	EmailsSent    int64      `json:"emails_sent"`
	EmailsReceived int64     `json:"emails_received"`
	ReputationScore float64  `json:"reputation_score"`
	IsBlocked     bool       `json:"is_blocked"`
	BlockedAt     *time.Time `json:"blocked_at,omitempty"`

	// Joined data
	User *AdminUserSummary `json:"user,omitempty"`
}

// WarmupPoolParticipantsResult represents paginated pool participants
type WarmupPoolParticipantsResult struct {
	Data       []WarmupPoolParticipant `json:"data"`
	Pagination Pagination              `json:"pagination"`
}

// WorkerStats represents statistics for a specific worker
type WorkerStats struct {
	WorkerID          uuid.UUID `json:"worker_id"`
	TotalEmailsSent   int64     `json:"total_emails_sent"`
	EmailsSentToday   int64     `json:"emails_sent_today"`
	EmailsSentThisWeek int64    `json:"emails_sent_this_week"`
	AverageDeliveryTime float64 `json:"average_delivery_time_ms"`
	SuccessRate       float64   `json:"success_rate"`
	QueueDepth        int64     `json:"queue_depth"`
}

// EmailDistribution represents email distribution across workers
type EmailDistribution struct {
	WorkerID       uuid.UUID `json:"worker_id"`
	WorkerName     string    `json:"worker_name"`
	EmailCount     int64     `json:"email_count"`
	Percentage     float64   `json:"percentage"`
}
