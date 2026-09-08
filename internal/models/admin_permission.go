package models

// AdminPermission represents platform-level admin permissions as a bitmask
type AdminPermission uint32

// Bit positions are stored in users.admin_permissions, so a retired bit keeps
// its placeholder here and is never reused.
const (
	// User Management (bits 0-3)
	AdminPermViewUsers AdminPermission = 1 << iota // View user profiles
	AdminPermBanUsers                              // Ban/unban users
	adminPermReserved2                             // Retired (edit users), never reuse
	adminPermReserved3                             // Retired (impersonate users), never reuse

	// Worker Management (bits 4-5)
	AdminPermViewWorkers   // View worker list
	AdminPermManageWorkers // Edit workers, reassign emails

	// Warmup Management (bits 6-8)
	AdminPermViewWarmupPool   // View warmup pool
	AdminPermManageWarmupBans // Block/unblock accounts
	AdminPermReviewAppeals    // Review ban appeals

	// Campaign Management (bits 9-10)
	AdminPermViewCampaigns // View any campaign
	AdminPermStopCampaigns // Force-stop campaigns

	// Analytics (bits 11-12)
	AdminPermViewAnalytics // Platform analytics
	AdminPermViewAuditLogs // Admin audit logs

	// Settings (bits 13-14)
	AdminPermManageRateLimits // User rate limits
	AdminPermManageSettings   // Platform settings

	// Retired enterprise bits (15-18), never reuse
	adminPermReserved15 // Retired (view enterprise inquiries)
	adminPermReserved16 // Retired (manage enterprise inquiries)
	adminPermReserved17 // Retired (manage plans)
	adminPermReserved18 // Retired (manage billing)

	// Super Admin (bit 19)
	AdminPermGrantAdminAccess // Grant/revoke admin permissions

	// Organization (Workspace) Management (bits 20-21)
	AdminPermViewOrganizations   // View workspaces and their plan/usage
	AdminPermManageOrganizations // Set per-org limit overrides, ban scope, etc.
)

// AllAdminPermissions is the full mask, retired bits included: it is what
// existing super admins hold in the database. Bump the shift whenever a new
// bit is added above.
const AllAdminPermissions AdminPermission = (1 << 22) - 1

// LiveAdminPermissions ORs every bit that still gates a route.
const LiveAdminPermissions AdminPermission = AdminPermViewUsers | AdminPermBanUsers |
	AdminPermViewWorkers | AdminPermManageWorkers |
	AdminPermViewWarmupPool | AdminPermManageWarmupBans | AdminPermReviewAppeals |
	AdminPermViewCampaigns | AdminPermStopCampaigns |
	AdminPermViewAnalytics | AdminPermViewAuditLogs |
	AdminPermManageRateLimits | AdminPermManageSettings |
	AdminPermGrantAdminAccess |
	AdminPermViewOrganizations | AdminPermManageOrganizations

// retiredAdminPermissions are the bits that no longer gate anything.
const retiredAdminPermissions = adminPermReserved2 | adminPermReserved3 |
	adminPermReserved15 | adminPermReserved16 | adminPermReserved17 | adminPermReserved18

// Compile-time check: every bit in AllAdminPermissions is live or retired.
var _ [0]struct{} = [AllAdminPermissions ^ (LiveAdminPermissions | retiredAdminPermissions)]struct{}{}

// HasPermission checks if the permission bitmask contains the specified permission
func (p AdminPermission) HasPermission(perm AdminPermission) bool {
	return p&perm == perm
}

// AddPermission adds a permission to the bitmask
func (p AdminPermission) AddPermission(perm AdminPermission) AdminPermission {
	return p | perm
}

// RemovePermission removes a permission from the bitmask
func (p AdminPermission) RemovePermission(perm AdminPermission) AdminPermission {
	return p &^ perm
}

// IsAdmin returns true if the user has any admin permissions
func (p AdminPermission) IsAdmin() bool {
	return p > 0
}

// IsSuperAdmin returns true if the user holds every live admin permission
func (p AdminPermission) IsSuperAdmin() bool {
	return p&LiveAdminPermissions == LiveAdminPermissions
}

// AdminRoleName represents predefined admin role names
type AdminRoleName string

const (
	AdminRoleSuper   AdminRoleName = "super"
	AdminRoleSupport AdminRoleName = "support"
	AdminRoleOps     AdminRoleName = "ops"
	AdminRoleAnalyst AdminRoleName = "analyst"
)

// AdminRolePermissions maps predefined roles to their permissions
var AdminRolePermissions = map[AdminRoleName]AdminPermission{
	AdminRoleSuper: AllAdminPermissions,
	AdminRoleSupport: AdminPermViewUsers | AdminPermViewCampaigns | AdminPermViewWarmupPool |
		AdminPermManageWarmupBans | AdminPermReviewAppeals | AdminPermViewAuditLogs |
		AdminPermViewOrganizations,
	AdminRoleOps: AdminPermViewWorkers | AdminPermManageWorkers | AdminPermViewAnalytics |
		AdminPermViewAuditLogs | AdminPermManageRateLimits | AdminPermViewOrganizations,
	AdminRoleAnalyst: AdminPermViewUsers | AdminPermViewCampaigns | AdminPermViewAnalytics |
		AdminPermViewAuditLogs | AdminPermViewOrganizations,
}

// GetAdminRolePermissions returns the permissions for a predefined admin role
func GetAdminRolePermissions(role AdminRoleName) AdminPermission {
	if perms, ok := AdminRolePermissions[role]; ok {
		return perms
	}
	return 0
}

// PermissionInfo describes an admin permission
type PermissionInfo struct {
	Name        string          `json:"name"`
	Permission  AdminPermission `json:"permission"`
	Description string          `json:"description"`
	Category    string          `json:"category"`
}

// GetAllPermissionInfos returns information about all live admin permissions
func GetAllPermissionInfos() []PermissionInfo {
	return []PermissionInfo{
		// User Management
		{Name: "view_users", Permission: AdminPermViewUsers, Description: "View user profiles and details", Category: "User Management"},
		{Name: "ban_users", Permission: AdminPermBanUsers, Description: "Ban and unban users", Category: "User Management"},

		// Worker Management
		{Name: "view_workers", Permission: AdminPermViewWorkers, Description: "View worker list and status", Category: "Worker Management"},
		{Name: "manage_workers", Permission: AdminPermManageWorkers, Description: "Edit workers and reassign emails", Category: "Worker Management"},

		// Warmup Management
		{Name: "view_warmup_pool", Permission: AdminPermViewWarmupPool, Description: "View warmup pool and participants", Category: "Warmup Management"},
		{Name: "manage_warmup_bans", Permission: AdminPermManageWarmupBans, Description: "Block and unblock warmup accounts", Category: "Warmup Management"},
		{Name: "review_appeals", Permission: AdminPermReviewAppeals, Description: "Review warmup ban appeals", Category: "Warmup Management"},

		// Campaign Management
		{Name: "view_campaigns", Permission: AdminPermViewCampaigns, Description: "View any campaign", Category: "Campaign Management"},
		{Name: "stop_campaigns", Permission: AdminPermStopCampaigns, Description: "Force-stop campaigns", Category: "Campaign Management"},

		// Analytics
		{Name: "view_analytics", Permission: AdminPermViewAnalytics, Description: "View platform analytics", Category: "Analytics"},
		{Name: "view_audit_logs", Permission: AdminPermViewAuditLogs, Description: "View admin audit logs", Category: "Analytics"},

		// Settings
		{Name: "manage_rate_limits", Permission: AdminPermManageRateLimits, Description: "Manage user rate limits", Category: "Settings"},
		{Name: "manage_settings", Permission: AdminPermManageSettings, Description: "Manage platform settings", Category: "Settings"},

		// Super Admin
		{Name: "grant_admin_access", Permission: AdminPermGrantAdminAccess, Description: "Grant or revoke admin permissions", Category: "Super Admin"},

		// Organization Management
		{Name: "view_organizations", Permission: AdminPermViewOrganizations, Description: "View workspaces and their plan/usage", Category: "Organization Management"},
		{Name: "manage_organizations", Permission: AdminPermManageOrganizations, Description: "Set per-org limit overrides and ban scope", Category: "Organization Management"},
	}
}
