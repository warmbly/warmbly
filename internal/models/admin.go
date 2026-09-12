package models

import (
	"time"

	"github.com/google/uuid"
)

// AdminUserSearch represents search parameters for admin user listing
type AdminUserSearch struct {
	Query         string `form:"q"`
	Status        string `form:"status"` // active, banned, all
	IsAdmin       *bool  `form:"is_admin"`
	HasOverrides  bool   `form:"has_overrides"`   // has custom rate limits
	FreeTrialUsed bool   `form:"free_trial_used"` // has consumed the free trial
	CreatedWithin int    `form:"created_within"`  // signup window, in days

	// Plan / subscription
	PlanID                *uuid.UUID `form:"plan_id"`             // subscribed to THIS plan
	SubscriptionStatus    string     `form:"subscription_status"` // trialing, active, past_due, ...
	IsEnterprise          bool       `form:"is_enterprise"`
	HasSubscription       bool       `form:"has_subscription"`
	HasActiveSubscription bool       `form:"has_active_subscription"`

	// Account state
	OnboardingCompleted bool `form:"onboarding_completed"`
	DeletionScheduled   bool `form:"deletion_scheduled"`
	HasAvatar           bool `form:"has_avatar"`
	HasActiveCampaign   bool `form:"has_active_campaign"`
	HasBanRecord        bool `form:"has_ban_record"`
	HasDedicatedWorker  bool `form:"has_dedicated_worker"`

	// Count / numeric ranges
	OrgCountMin          *int `form:"org_count_min"`
	OrgCountMax          *int `form:"org_count_max"`
	EmailAccountCountMin *int `form:"email_account_count_min"`
	EmailAccountCountMax *int `form:"email_account_count_max"`
	CampaignCountMin     *int `form:"campaign_count_min"`
	CampaignCountMax     *int `form:"campaign_count_max"`
	MaxOrganizationsMin  *int `form:"max_organizations_min"`
	MaxOrganizationsMax  *int `form:"max_organizations_max"`

	// Date ranges (YYYY-MM-DD, parsed as UTC)
	CreatedAfter       *time.Time `form:"created_after" time_format:"2006-01-02" time_utc:"true"`
	CreatedBefore      *time.Time `form:"created_before" time_format:"2006-01-02" time_utc:"true"`
	AdminGrantedAfter  *time.Time `form:"admin_granted_after" time_format:"2006-01-02" time_utc:"true"`
	AdminGrantedBefore *time.Time `form:"admin_granted_before" time_format:"2006-01-02" time_utc:"true"`
	BannedAfter        *time.Time `form:"banned_after" time_format:"2006-01-02" time_utc:"true"`
	BannedBefore       *time.Time `form:"banned_before" time_format:"2006-01-02" time_utc:"true"`
	UpdatedAfter       *time.Time `form:"updated_after" time_format:"2006-01-02" time_utc:"true"`
	UpdatedBefore      *time.Time `form:"updated_before" time_format:"2006-01-02" time_utc:"true"`

	Cursor   *uuid.UUID `form:"cursor"`
	Limit    int        `form:"limit"`
	SortBy   string     `form:"sort_by"` // created_at, email, name
	SortDesc bool       `form:"sort_desc"`
}

// AdminUserDetail represents a user with admin-relevant statistics
type AdminUserDetail struct {
	ID               uuid.UUID       `json:"id"`
	FirstName        string          `json:"first_name"`
	LastName         string          `json:"last_name"`
	Email            string          `json:"email"`
	MaxOrganizations int             `json:"max_organizations"`
	FreeTrialUsed    bool            `json:"free_trial_used"`
	AdminPermissions AdminPermission `json:"admin_permissions"`
	AdminGrantedAt   *time.Time      `json:"admin_granted_at,omitempty"`
	AdminGrantedBy   *uuid.UUID      `json:"admin_granted_by,omitempty"`
	BannedAt         *time.Time      `json:"banned_at,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`

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

// BanScope is a bitmask describing which actions are blocked while a
// user is banned. The existing banned_at column still marks "banned",
// but the scope decides what concretely stops working. Kept in sync
// with the values in 000045_ban_scope.up.sql.
type BanScope uint32

const (
	BanScopeLogin     BanScope = 1 << iota // 1 — block authentication
	BanScopeOrgCreate                      // 2 — refuse new workspace creation
	BanScopeSend                           // 4 — block outbound campaign sends
)

// BanScopeAll is the legacy "everything" mask applied at migration to
// preserve the pre-bitmask "fully banned" semantics. New bans should
// pick a more specific scope where possible.
const BanScopeAll = BanScopeLogin | BanScopeOrgCreate | BanScopeSend

func (s BanScope) Has(flag BanScope) bool { return s&flag == flag }

// BanUserRequest represents the request to ban a user
type BanUserRequest struct {
	Reason string `json:"reason" binding:"required"`
	// Scope is a BanScope bitmask. Zero / missing falls back to
	// BanScopeLogin so the historic "you can't log in" behaviour
	// stays the default for ambiguous calls.
	Scope BanScope `json:"scope,omitempty"`
}

// UnbanUserRequest represents the request to unban a user
type UnbanUserRequest struct {
	Reason string `json:"reason" binding:"required"`
}

// AdminWorkerDetail represents a worker with admin-relevant details
type AdminWorkerDetail struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Notes        string    `json:"notes"`
	IPAddr       string    `json:"ip_addr"`
	Active       bool      `json:"active"`
	Region       string    `json:"region"`
	AccountCount int       `json:"account_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	// Statistics
	EmailsSentToday int `json:"emails_sent_today"`
	EmailsSentTotal int `json:"emails_sent_total"`
	ActiveCampaigns int `json:"active_campaigns"`
	ConnectedEmails int `json:"connected_emails"`
	WarmupEmails    int `json:"warmup_emails"`
}

// AdminWorkersResult represents paginated worker listing
type AdminWorkersResult struct {
	Data       []AdminWorkerDetail `json:"data"`
	Pagination Pagination          `json:"pagination"`
}

// AdminUpdateWorker represents the request to update a worker
type AdminUpdateWorker struct {
	Name   *string `json:"name,omitempty"`
	Notes  *string `json:"notes,omitempty"`
	Active *bool   `json:"active,omitempty"`
	// Region is a sign-in geography hint the placer scores on. It is the only
	// worker attribute an operator sets, and leaving it empty is fine.
	Region *string `json:"region,omitempty"`
}

// AdminWorkerEmail represents an email account connected to a worker, including
// the per-mailbox health signals (risk band + worst warmup health state) so the
// admin worker view can show how healthy the inboxes on a worker are.
type AdminWorkerEmail struct {
	ID             uuid.UUID  `json:"id"`
	Email          string     `json:"email"`
	UserID         uuid.UUID  `json:"user_id"`
	OrganizationID *uuid.UUID `json:"organization_id,omitempty"`
	Status         string     `json:"status"`
	Provider       string     `json:"provider"`
	WarmupEnabled  bool       `json:"warmup_enabled"`
	// NULL until the mailbox syncs for the first time.
	LastSyncedAt    *time.Time `json:"last_synced_at"`
	RiskBand        string     `json:"risk_band"` // clean | risky | quarantine
	RiskEvaluatedAt *time.Time `json:"risk_evaluated_at,omitempty"`
	WarmupHealth    string     `json:"warmup_health,omitempty"` // worst warmup health_state, "" if not in a pool
	SpamScore       *int       `json:"spam_score,omitempty"`
	BlockedUntil    *time.Time `json:"blocked_until,omitempty"`
}

// ReassignEmailsRequest represents the request to reassign emails
type ReassignEmailsRequest struct {
	EmailIDs    []uuid.UUID `json:"email_ids" binding:"required"`
	NewWorkerID uuid.UUID   `json:"new_worker_id" binding:"required"`
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
	User           *AdminUserSummary `json:"user,omitempty"`
	EmailAccount   *AdminWorkerEmail `json:"email_account,omitempty"`
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
	ID           uuid.UUID           `json:"id"`
	Email        string              `json:"email"`
	UserID       uuid.UUID           `json:"user_id"`
	BlockedAt    time.Time           `json:"blocked_at"`
	BlockedBy    *uuid.UUID          `json:"blocked_by,omitempty"`
	BlockReason  string              `json:"block_reason"`
	HasAppeal    bool                `json:"has_appeal"`
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

// AdminStopCampaignRequest carries the reason the audit trail and the owner's
// campaign feed show, so it is required.
type AdminStopCampaignRequest struct {
	Reason string `json:"reason" binding:"required"`
}

// AdminCampaignsResult represents paginated campaign listing
type AdminCampaignsResult struct {
	Data       []AdminCampaignDetail `json:"data"`
	Pagination Pagination            `json:"pagination"`
}

// AdminCampaignSearch represents search parameters for campaigns. form tags
// must stay byte-identical to the TS AdminCampaignSearch keys; date facets are
// *time.Time with time_format/time_utc, mirroring AdminOrgSearch.
type AdminCampaignSearch struct {
	Query  string     `form:"q"`
	UserID *uuid.UUID `form:"user_id"`
	OrgID  *uuid.UUID `form:"org_id"`
	Status string     `form:"status"` // draft, active, paused, completed, paused_trial_expired, paused_no_accounts, paused_guardrail, paused_undeliverable

	// Boolean flags
	OpenTracking      bool `form:"open_tracking"`
	LinkTracking      bool `form:"link_tracking"`
	StopOnReply       bool `form:"stop_on_reply"`
	TextOnly          bool `form:"text_only"`
	UnsubscribeHeader bool `form:"unsubscribe_header"`

	// Relationship existence
	HasContacts bool `form:"has_contacts"`
	HasBounces  bool `form:"has_bounces"`

	// Count ranges
	DailyLimitMin   *int `form:"daily_limit_min"`
	DailyLimitMax   *int `form:"daily_limit_max"`
	ContactCountMin *int `form:"contact_count_min"`
	ContactCountMax *int `form:"contact_count_max"`
	SentCountMin    *int `form:"sent_count_min"`
	SentCountMax    *int `form:"sent_count_max"`

	// Date ranges (YYYY-MM-DD, UTC)
	CreatedWithin   int        `form:"created_within"` // days; 0 = any
	CreatedAfter    *time.Time `form:"created_after" time_format:"2006-01-02" time_utc:"true"`
	CreatedBefore   *time.Time `form:"created_before" time_format:"2006-01-02" time_utc:"true"`
	StartDateAfter  *time.Time `form:"start_date_after" time_format:"2006-01-02" time_utc:"true"`
	StartDateBefore *time.Time `form:"start_date_before" time_format:"2006-01-02" time_utc:"true"`
	UpdatedAfter    *time.Time `form:"updated_after" time_format:"2006-01-02" time_utc:"true"`
	UpdatedBefore   *time.Time `form:"updated_before" time_format:"2006-01-02" time_utc:"true"`

	Cursor   *uuid.UUID `form:"cursor"`
	Limit    int        `form:"limit"`
	SortBy   string     `form:"sort_by"`
	SortDesc bool       `form:"sort_desc"`
}

// AdminAuditLog represents an audit log entry for admin actions
type AdminAuditLog struct {
	ID          uuid.UUID      `json:"id"`
	AdminUserID uuid.UUID      `json:"admin_user_id"`
	Action      string         `json:"action"`
	TargetType  string         `json:"target_type"`
	TargetID    uuid.UUID      `json:"target_id"`
	Details     map[string]any `json:"details,omitempty"`
	IPAddress   string         `json:"ip_address"`
	UserAgent   string         `json:"user_agent"`
	CreatedAt   time.Time      `json:"created_at"`

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
	TotalUsers       int64 `json:"total_users"`
	ActiveUsers      int64 `json:"active_users"` // Active in last 30 days
	NewUsersToday    int64 `json:"new_users_today"`
	NewUsersThisWeek int64 `json:"new_users_this_week"`

	TotalCampaigns  int64 `json:"total_campaigns"`
	ActiveCampaigns int64 `json:"active_campaigns"`

	TotalEmailsSent int64 `json:"total_emails_sent"`
	EmailsSentToday int64 `json:"emails_sent_today"`

	TotalWorkers  int64 `json:"total_workers"`
	ActiveWorkers int64 `json:"active_workers"`

	WarmupBlockedCount int64 `json:"warmup_blocked_count"`
	PendingAppeals     int64 `json:"pending_appeals"`

	ActiveSubscriptions int64 `json:"active_subscriptions"`
	TrialingUsers       int64 `json:"trialing_users"`
}

// AdminMailboxRow is one row in the platform-wide mailbox admin list.
// Joins the connected mailbox with owner + workspace so the table can
// answer "whose mailbox is this and where does it live" without
// fan-out fetches.
type AdminMailboxRow struct {
	ID             uuid.UUID  `json:"id"`
	Email          string     `json:"email"`
	Provider       string     `json:"provider"`
	Status         string     `json:"status"`
	UserID         uuid.UUID  `json:"user_id"`
	OwnerEmail     string     `json:"owner_email"`
	OrganizationID *uuid.UUID `json:"organization_id,omitempty"`
	OrgName        *string    `json:"org_name,omitempty"`
	WorkerID       *uuid.UUID `json:"worker_id,omitempty"`
	WarmupEnabled  bool       `json:"warmup_enabled"`
	RiskBand       string     `json:"risk_band"`
	WarmupPoolType *string    `json:"warmup_pool_type,omitempty"`
	CampaignLimit  int        `json:"campaign_limit"`
	LastSyncedAt   *time.Time `json:"last_synced_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// AdminMailboxesResult is the paginated wrapper.
type AdminMailboxesResult struct {
	Data       []AdminMailboxRow `json:"data"`
	Pagination Pagination        `json:"pagination"`
}

// AdminMailboxSearch covers the query knobs the admin table needs.
// All fields optional; empty status = active, "all" returns every
// status. provider lets ops filter to "all gmail mailboxes" quickly.
type AdminMailboxSearch struct {
	Query         string     `form:"q"`
	Status        string     `form:"status"`
	Provider      string     `form:"provider"`
	Warmup        string     `form:"warmup"`         // "on", "off", or "" for any
	CreatedWithin int        `form:"created_within"` // connected window, in days
	OrgID         *uuid.UUID `form:"org_id"`         // browse a single org's mailboxes

	// Ownership / placement
	UserID   *uuid.UUID `form:"user_id"`   // mailboxes owned by THIS user
	WorkerID *uuid.UUID `form:"worker_id"` // mailboxes on THIS worker

	// Classification
	RiskBand       string `form:"risk_band"`        // clean, risky, quarantine
	WarmupPoolType string `form:"warmup_pool_type"` // free, premium
	SyncedStatus   string `form:"synced_status"`    // never, stale, recent

	// Flags
	WarmupPaused           bool `form:"warmup_paused"`
	TrackingDomainVerified bool `form:"tracking_domain_verified"`
	HasTrackingDomain      bool `form:"has_tracking_domain"`
	HasOrganization        bool `form:"has_organization"`
	SignatureSync          bool `form:"signature_sync"`
	HasOAuth               bool `form:"has_oauth"`
	HasSMTPImap            bool `form:"has_smtp_imap"`

	// Numeric ranges
	CampaignLimitMin *int `form:"campaign_limit_min"`
	CampaignLimitMax *int `form:"campaign_limit_max"`
	MinWaitTimeMin   *int `form:"min_wait_time_min"`
	MinWaitTimeMax   *int `form:"min_wait_time_max"`

	// Date ranges (YYYY-MM-DD, UTC)
	CreatedAfter     *time.Time `form:"created_after" time_format:"2006-01-02" time_utc:"true"`
	CreatedBefore    *time.Time `form:"created_before" time_format:"2006-01-02" time_utc:"true"`
	LastSyncedAfter  *time.Time `form:"last_synced_after" time_format:"2006-01-02" time_utc:"true"`
	LastSyncedBefore *time.Time `form:"last_synced_before" time_format:"2006-01-02" time_utc:"true"`

	Cursor   *uuid.UUID `form:"cursor"`
	Limit    int        `form:"limit"`
	SortBy   string     `form:"sort_by"` // email, created_at, last_synced_at, campaign_limit
	SortDesc bool       `form:"sort_desc"`
}

// DailyEmailStats represents daily email statistics for graphs
type DailyEmailStats struct {
	Date           time.Time `json:"date"`
	TotalSent      int64     `json:"total_sent"`
	TotalDelivered int64     `json:"total_delivered"`
	TotalBounced   int64     `json:"total_bounced"`
	TotalOpened    int64     `json:"total_opened"`
	TotalClicked   int64     `json:"total_clicked"`
	TotalReplied   int64     `json:"total_replied"`
}

// HourlyEmailStats represents hourly email statistics
type HourlyEmailStats struct {
	Hour      int   `json:"hour"`
	TotalSent int64 `json:"total_sent"`
}

// UserGrowthStats represents user growth statistics
type UserGrowthStats struct {
	Date       time.Time `json:"date"`
	NewUsers   int64     `json:"new_users"`
	TotalUsers int64     `json:"total_users"`
}

// AnalyticsTrends represents trend data for analytics
type AnalyticsTrends struct {
	UsersGrowthPercent     float64 `json:"users_growth_percent"`
	EmailsGrowthPercent    float64 `json:"emails_growth_percent"`
	CampaignsGrowthPercent float64 `json:"campaigns_growth_percent"`
	RevenueGrowthPercent   float64 `json:"revenue_growth_percent"`
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

// AdminUserRateLimits is one user's row in user_rate_limits: the API and
// realtime throughput they are allowed. Mail volume is not a rate limit.
type AdminUserRateLimits struct {
	UserID uuid.UUID `json:"user_id"`

	LimitReadPM      int `json:"limit_read_pm"`
	LimitWritePM     int `json:"limit_write_pm"`
	LimitBulkPM      int `json:"limit_bulk_pm"`
	LimitUniboxPM    int `json:"limit_unibox_pm"`
	LimitAnalyticsPM int `json:"limit_analytics_pm"`

	LimitAPICallsDaily int `json:"limit_api_calls_daily"`
	LimitBulkOpsDaily  int `json:"limit_bulk_ops_daily"`

	LimitWSMessagePM int `json:"limit_ws_message_pm"`
	LimitWSJoinPM    int `json:"limit_ws_join_pm"`
	LimitWSEventPM   int `json:"limit_ws_event_pm"`
	MaxConnections   int `json:"max_connections"`

	Notes     *string    `json:"notes,omitempty"`
	UpdatedBy *uuid.UUID `json:"updated_by,omitempty"`
	// Absent when the user has no override row and these are the defaults.
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// UpdateUserRateLimitsRequest patches user_rate_limits. An omitted field is left
// alone; there is no null state, so clearing means typing the default back in.
type UpdateUserRateLimitsRequest struct {
	LimitReadPM      *int `json:"limit_read_pm,omitempty"`
	LimitWritePM     *int `json:"limit_write_pm,omitempty"`
	LimitBulkPM      *int `json:"limit_bulk_pm,omitempty"`
	LimitUniboxPM    *int `json:"limit_unibox_pm,omitempty"`
	LimitAnalyticsPM *int `json:"limit_analytics_pm,omitempty"`

	LimitAPICallsDaily *int `json:"limit_api_calls_daily,omitempty"`
	LimitBulkOpsDaily  *int `json:"limit_bulk_ops_daily,omitempty"`

	LimitWSMessagePM *int `json:"limit_ws_message_pm,omitempty"`
	LimitWSJoinPM    *int `json:"limit_ws_join_pm,omitempty"`
	LimitWSEventPM   *int `json:"limit_ws_event_pm,omitempty"`
	MaxConnections   *int `json:"max_connections,omitempty"`

	Notes *string `json:"notes,omitempty"`
}

// DefaultAdminUserRateLimits is what the enforcement path applies to a user with
// no override row, so the editor shows the limits actually in force.
func DefaultAdminUserRateLimits(userID uuid.UUID) *AdminUserRateLimits {
	d := DefaultRateLimits()
	return &AdminUserRateLimits{
		UserID:             userID,
		LimitReadPM:        d.LimitReadPM,
		LimitWritePM:       d.LimitWritePM,
		LimitBulkPM:        d.LimitBulkPM,
		LimitUniboxPM:      d.LimitUniboxPM,
		LimitAnalyticsPM:   d.LimitAnalyticsPM,
		LimitAPICallsDaily: d.LimitAPICallsDaily,
		LimitBulkOpsDaily:  d.LimitBulkOpsDaily,
		LimitWSMessagePM:   d.LimitWSMessagePM,
		LimitWSJoinPM:      d.LimitWSJoinPM,
		LimitWSEventPM:     d.LimitWSEventPM,
		MaxConnections:     d.MaxConnections,
	}
}

// AdminUserPreview represents a full preview of a user's account
type AdminUserPreview struct {
	User          AdminUserDetail      `json:"user"`
	Organizations []Organization       `json:"organizations"`
	Subscriptions []Subscription       `json:"subscriptions"`
	EmailAccounts []AdminWorkerEmail   `json:"email_accounts"`
	RecentBans    []UserBan            `json:"recent_bans"`
	RateLimits    *AdminUserRateLimits `json:"rate_limits,omitempty"`
}

// WarmupPoolInfo represents information about a warmup pool
type WarmupPoolInfo struct {
	Type               string `json:"type"`
	TotalParticipants  int64  `json:"total_participants"`
	ActiveParticipants int64  `json:"active_participants"`
	BlockedCount       int64  `json:"blocked_count"`
}

// WarmupPoolParticipant represents a participant in a warmup pool
type WarmupPoolParticipant struct {
	ID              uuid.UUID  `json:"id"`
	Email           string     `json:"email"`
	UserID          uuid.UUID  `json:"user_id"`
	JoinedAt        time.Time  `json:"joined_at"`
	EmailsSent      int64      `json:"emails_sent"`
	EmailsReceived  int64      `json:"emails_received"`
	ReputationScore float64    `json:"reputation_score"`
	IsBlocked       bool       `json:"is_blocked"`
	BlockedAt       *time.Time `json:"blocked_at,omitempty"`

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
	WorkerID            uuid.UUID `json:"worker_id"`
	TotalEmailsSent     int64     `json:"total_emails_sent"`
	EmailsSentToday     int64     `json:"emails_sent_today"`
	EmailsSentThisWeek  int64     `json:"emails_sent_this_week"`
	AverageDeliveryTime float64   `json:"average_delivery_time_ms"`
	SuccessRate         float64   `json:"success_rate"`
	QueueDepth          int64     `json:"queue_depth"`
}

// AdminOrgSearch are the query params for the admin organization listing.
type AdminOrgSearch struct {
	Query          string     `form:"q"`
	Status         string     `form:"status"` // active, pending_deletion, all
	PlanID         *uuid.UUID `form:"plan_id"`
	PlanVisibility string     `form:"plan_visibility"` // public, private, none
	CreatedWithin  int        `form:"created_within"`  // days; 0 = any
	HasOverrides   bool       `form:"has_overrides"`   // has organization_limit_overrides
	RiskState      string     `form:"risk_state"`      // exact posture: trusted|watch|restricted|suspended
	RiskFlagged    bool       `form:"risk_flagged"`    // any posture other than trusted
	Enterprise     bool       `form:"enterprise"`      // has an enterprise subscription
	ManagedPlan    bool       `form:"managed_plan"`    // plan granted by an operator, not Stripe

	// Subscription state
	SubscriptionStatus    string `form:"subscription_status"`
	CancelAtPeriodEnd     bool   `form:"cancel_at_period_end"`
	HasActiveSubscription bool   `form:"has_active_subscription"`
	NoSubscription        bool   `form:"no_subscription"`
	OwnerBanned           bool   `form:"owner_banned"`

	// Relationship existence
	HasActiveCampaigns bool `form:"has_active_campaigns"`
	HasEmailAccounts   bool `form:"has_email_accounts"`

	// Count ranges
	MemberCountMin       *int `form:"member_count_min"`
	MemberCountMax       *int `form:"member_count_max"`
	EmailAccountCountMin *int `form:"email_account_count_min"`
	EmailAccountCountMax *int `form:"email_account_count_max"`
	CampaignCountMin     *int `form:"campaign_count_min"`
	CampaignCountMax     *int `form:"campaign_count_max"`

	// Date ranges (YYYY-MM-DD, UTC)
	CreatedAfter           *time.Time `form:"created_after" time_format:"2006-01-02" time_utc:"true"`
	CreatedBefore          *time.Time `form:"created_before" time_format:"2006-01-02" time_utc:"true"`
	TrialEndAfter          *time.Time `form:"trial_end_after" time_format:"2006-01-02" time_utc:"true"`
	TrialEndBefore         *time.Time `form:"trial_end_before" time_format:"2006-01-02" time_utc:"true"`
	CurrentPeriodEndAfter  *time.Time `form:"current_period_end_after" time_format:"2006-01-02" time_utc:"true"`
	CurrentPeriodEndBefore *time.Time `form:"current_period_end_before" time_format:"2006-01-02" time_utc:"true"`
	UpdatedAfter           *time.Time `form:"updated_after" time_format:"2006-01-02" time_utc:"true"`
	UpdatedBefore          *time.Time `form:"updated_before" time_format:"2006-01-02" time_utc:"true"`

	// Acquisition channel. UTMSource/UTMMedium/UTMCampaign match exactly.
	//
	// HasAcquisition and NoAcquisition select on the presence of an
	// acquisition record, not on a UTM tag: a signup that came from a
	// marketing page with no campaign parameters has a landing path and
	// counts as having acquisition data. "Direct" therefore means "arrived
	// with nothing at all", which is what the admin list's Channel column
	// shows too. They are mutually exclusive; setting both applies only
	// HasAcquisition.
	UTMSource      string `form:"utm_source"`
	UTMMedium      string `form:"utm_medium"`
	UTMCampaign    string `form:"utm_campaign"`
	HasAcquisition bool   `form:"has_acquisition"`
	NoAcquisition  bool   `form:"no_acquisition"`

	Cursor   *uuid.UUID `form:"cursor"`
	Limit    int        `form:"limit"`
	SortBy   string     `form:"sort_by"` // created_at, name, owner_email, member_count, campaign_count, email_account_count
	SortDesc bool       `form:"sort_desc"`
}

// AdminOrgListItem is one row in the admin org list. It carries enough
// summary state for the table (owner, counts, deletion status) without
// joining to plans or subscriptions — those land on the detail endpoint.
type AdminOrgListItem struct {
	ID                   uuid.UUID  `json:"id"`
	Name                 string     `json:"name"`
	Slug                 *string    `json:"slug,omitempty"`
	OwnerUserID          uuid.UUID  `json:"owner_user_id"`
	OwnerEmail           string     `json:"owner_email"`
	OwnerFirstName       string     `json:"owner_first_name"`
	OwnerLastName        string     `json:"owner_last_name"`
	OwnerBannedAt        *time.Time `json:"owner_banned_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	DeletionScheduledFor *time.Time `json:"deletion_scheduled_for,omitempty"`

	// Resource counts. Cheap enough to inline on the list query so the
	// table can show usage at a glance without an extra round-trip.
	MemberCount       int `json:"member_count"`
	EmailAccountCount int `json:"email_account_count"`
	CampaignCount     int `json:"campaign_count"`
	ActiveCampaigns   int `json:"active_campaigns"`

	// RiskState is the fused abuse posture, inlined so the table can show
	// which workspaces a detector has acted on without a call per row.
	RiskState OrgRiskState `json:"risk_state,omitempty"`

	// Acquisition channel recorded at signup. Inlined so revenue by channel is
	// readable in the table rather than a separate report. Absent for a direct
	// signup, which is most of them, and always absent on a self-host.
	UTMSource   *string `json:"utm_source,omitempty"`
	UTMMedium   *string `json:"utm_medium,omitempty"`
	UTMCampaign *string `json:"utm_campaign,omitempty"`
	LandingPath *string `json:"landing_path,omitempty"`

	// Plan summary (LEFT JOINed via the org's single active subscription).
	PlanName     *string `json:"plan_name,omitempty"`
	PlanPublic   *bool   `json:"plan_public,omitempty"`
	IsEnterprise bool    `json:"is_enterprise"`

	// ManagedPlan marks a plan an operator granted rather than Stripe, so the
	// table can answer "which workspaces are paid because we said so" without
	// a call per row. ManagedPlanExpired is a grant that lapsed, which is
	// deliberately distinct: the workspace is back on free and the reason is
	// still on file.
	ManagedPlan        bool       `json:"managed_plan"`
	ManagedPlanExpired bool       `json:"managed_plan_expired"`
	ManagedPlanReason  *string    `json:"managed_plan_reason,omitempty"`
	ManagedPlanUntil   *time.Time `json:"managed_plan_until,omitempty"`
}

// AdminOrgsResult is the paginated response for the admin org listing.
type AdminOrgsResult struct {
	Data       []AdminOrgListItem `json:"data"`
	Pagination Pagination         `json:"pagination"`
}

// AdminLimitRequestSearch are the query params for the admin limit-request
// queue. Mirrors AdminOrgSearch: *time.Time date facets, *int range bounds,
// *uuid.UUID for cursor/org/submitter. The form tags must stay byte-identical
// to the frontend param keys and the TS AdminLimitRequestSearch interface.
type AdminLimitRequestSearch struct {
	Query  string `form:"q"`
	Status string `form:"status"` // pending, approved, rejected, cancelled, all
	Field  string `form:"field"`  // app-validated: max_email_accounts ... daily_campaign_limit

	OrgID       *uuid.UUID `form:"org_id"`
	SubmittedBy *uuid.UUID `form:"submitted_by"`

	// Flags
	Reviewed   bool `form:"reviewed"`   // reviewed_at IS NOT NULL
	Unreviewed bool `form:"unreviewed"` // reviewed_at IS NULL

	// Numeric ranges
	RequestedMin        *int `form:"requested_min"`
	RequestedMax        *int `form:"requested_max"`
	CurrentEffectiveMin *int `form:"current_effective_min"`
	CurrentEffectiveMax *int `form:"current_effective_max"`

	// Date ranges
	SubmittedWithin int        `form:"submitted_within"` // days; 0 = any
	SubmittedAfter  *time.Time `form:"submitted_after" time_format:"2006-01-02" time_utc:"true"`
	SubmittedBefore *time.Time `form:"submitted_before" time_format:"2006-01-02" time_utc:"true"`
	ReviewedAfter   *time.Time `form:"reviewed_after" time_format:"2006-01-02" time_utc:"true"`
	ReviewedBefore  *time.Time `form:"reviewed_before" time_format:"2006-01-02" time_utc:"true"`

	Cursor   *uuid.UUID `form:"cursor"`
	Limit    int        `form:"limit"`
	SortBy   string     `form:"sort_by"` // submitted_at, requested, current_effective, reviewed_at, status, field, org_name
	SortDesc bool       `form:"sort_desc"`
}

// AdminLimitRequestsResult is the paginated admin limit-request response.
type AdminLimitRequestsResult struct {
	Data       []LimitIncreaseRequest `json:"data"`
	Pagination Pagination             `json:"pagination"`
}

// AdminOrgDetail is the full payload for the org detail page. Carries
// three limit blocks side-by-side so the UI can explain *why* each
// effective number is what it is:
//
//   - Limits          — plan defaults (nil = unlimited)
//   - Overrides       — raw override row (0 per field = inherit)
//   - EffectiveLimits — what the runtime actually enforces
type AdminOrgDetail struct {
	AdminOrgListItem

	UpdatedAt           time.Time                   `json:"updated_at"`
	DeletionScheduledAt *time.Time                  `json:"deletion_scheduled_at,omitempty"`
	Limits              *OrganizationLimits         `json:"limits,omitempty"`
	Overrides           *OrganizationLimitOverrides `json:"overrides,omitempty"`
	EffectiveLimits     *OrganizationLimits         `json:"effective_limits,omitempty"`
	Counts              *OrganizationCounts         `json:"counts,omitempty"`

	// Plan / subscription context. Either may be nil (org with no active
	// subscription — e.g. trial or freshly created).
	PlanName           *string    `json:"plan_name,omitempty"`
	SubscriptionStatus *string    `json:"subscription_status,omitempty"`
	IsEnterprise       bool       `json:"is_enterprise"`
	CurrentPeriodEnd   *time.Time `json:"current_period_end,omitempty"`
	TrialEnd           *time.Time `json:"trial_end,omitempty"`
}

// AdminOrgMember is a member row enriched with the joined user.
type AdminOrgMember struct {
	OrganizationMember
	User *AdminUserSummary `json:"user,omitempty"`
}

// AdminOrgMembersResult is the response for /admin/organizations/:id/members.
type AdminOrgMembersResult struct {
	Data []AdminOrgMember `json:"data"`
}
