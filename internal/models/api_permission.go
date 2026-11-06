package models

// API Permissions: bitmask of scopes granted to a given API key.
// Distinct from OrganizationPermission (which gates UI users) and
// AdminPermission (which gates platform admins). uint64 leaves room
// for ~50 more scopes before we have to widen the column.
const (
	// Read permissions
	APIPermReadEmails    uint64 = 1 << iota // Read email accounts
	APIPermReadCampaigns                    // Read campaigns + sequences
	APIPermReadContacts                     // Read contacts, segments, notes, activities
	APIPermReadUnibox                       // Read unified inbox
	APIPermReadAnalytics                    // Read analytics + statistics

	// Write permissions
	APIPermWriteEmails    // Modify email accounts
	APIPermWriteCampaigns // Create/modify campaigns + sequences
	APIPermWriteContacts  // Create/modify contacts, segments, notes, activities
	APIPermWriteUnibox    // Mark as seen, reply, etc.

	// Bulk operations (separate so a key can read+write without bulk power)
	APIPermBulkContacts  // Bulk contact import/export/delete
	APIPermBulkCampaigns // Bulk campaign operations

	// Realtime + integrations
	APIPermRealtimeSubscribe // Subscribe to realtime events
	APIPermWebhooks          // Manage webhook endpoints (reserved)

	// Self-service
	APIPermAPIKeys // Create / list / revoke API keys via the API

	// Campaign lifecycle (separated from WriteCampaigns since starting
	// a campaign actually sends mail and is materially different from
	// editing its draft).
	APIPermSendCampaigns

	// Templates + CRM are first-class scopes — they're useful to expose
	// without granting full campaign write.
	APIPermReadTemplates
	APIPermWriteTemplates
	APIPermReadCRM
	APIPermWriteCRM

	// Audit trail
	APIPermReadAuditLogs

	// Organization operations
	APIPermIntegrations  // Connect and manage third-party integrations
	APIPermWarmupRouting // Manage warmup routing rules

	// AI
	APIPermAIAgent    // Run the AI assistant / agent (dashboard agent + MCP)
	APIPermAIResearch // Run AI contact research
)

// AllAPIPermissionsMask is the OR of every defined permission bit.
// Used to reject CreateAPIKey requests that include unknown bits, so
// a future bit can't be granted accidentally by a stale client.
const AllAPIPermissionsMask uint64 = APIPermReadEmails | APIPermReadCampaigns |
	APIPermReadContacts | APIPermReadUnibox | APIPermReadAnalytics |
	APIPermWriteEmails | APIPermWriteCampaigns | APIPermWriteContacts |
	APIPermWriteUnibox |
	APIPermBulkContacts | APIPermBulkCampaigns |
	APIPermRealtimeSubscribe | APIPermWebhooks |
	APIPermAPIKeys |
	APIPermSendCampaigns |
	APIPermReadTemplates | APIPermWriteTemplates |
	APIPermReadCRM | APIPermWriteCRM |
	APIPermReadAuditLogs |
	APIPermIntegrations | APIPermWarmupRouting |
	APIPermAIAgent | APIPermAIResearch

// Preset permission sets surfaced via GET /api-keys/permissions so a
// caller can grant a sane default without picking bits by hand.
var (
	APIPermReadOnly uint64 = APIPermReadEmails | APIPermReadCampaigns |
		APIPermReadContacts | APIPermReadUnibox | APIPermReadAnalytics |
		APIPermReadTemplates | APIPermReadCRM | APIPermReadAuditLogs

	APIPermFullAccess uint64 = APIPermReadOnly |
		APIPermWriteEmails | APIPermWriteCampaigns | APIPermWriteContacts |
		APIPermWriteUnibox |
		APIPermBulkContacts | APIPermBulkCampaigns |
		APIPermSendCampaigns |
		APIPermWriteTemplates | APIPermWriteCRM |
		APIPermRealtimeSubscribe | APIPermWebhooks | APIPermAPIKeys |
		APIPermIntegrations | APIPermWarmupRouting |
		APIPermAIAgent | APIPermAIResearch
)

type APIPermission struct {
	Name        string `json:"name"`
	Value       uint64 `json:"value"`
	Description string `json:"description"`
	Category    string `json:"category"` // read, write, bulk, special
}

var AllAPIPermissions = []APIPermission{
	{"READ_EMAILS", APIPermReadEmails, "View email accounts and settings", "read"},
	{"READ_CAMPAIGNS", APIPermReadCampaigns, "View campaigns and sequences", "read"},
	{"READ_CONTACTS", APIPermReadContacts, "View contact lists, segments, notes, and activities", "read"},
	{"READ_UNIBOX", APIPermReadUnibox, "Access unified inbox", "read"},
	{"READ_ANALYTICS", APIPermReadAnalytics, "View analytics and statistics", "read"},
	{"READ_TEMPLATES", APIPermReadTemplates, "View reply templates", "read"},
	{"READ_CRM", APIPermReadCRM, "View pipelines, deals, and CRM tasks", "read"},
	{"READ_AUDIT_LOGS", APIPermReadAuditLogs, "View organization audit logs", "read"},
	{"WRITE_EMAILS", APIPermWriteEmails, "Modify email account settings", "write"},
	{"WRITE_CAMPAIGNS", APIPermWriteCampaigns, "Create and modify campaigns and sequences", "write"},
	{"WRITE_CONTACTS", APIPermWriteContacts, "Create and modify contacts, segments, notes, and activities", "write"},
	{"WRITE_UNIBOX", APIPermWriteUnibox, "Mark emails as read/unread and send replies", "write"},
	{"WRITE_TEMPLATES", APIPermWriteTemplates, "Create and modify reply templates", "write"},
	{"WRITE_CRM", APIPermWriteCRM, "Create and modify pipelines, deals, and CRM tasks", "write"},
	{"SEND_CAMPAIGNS", APIPermSendCampaigns, "Start and stop campaigns (sends real mail)", "write"},
	{"BULK_CONTACTS", APIPermBulkContacts, "Bulk import/export/delete contacts", "bulk"},
	{"BULK_CAMPAIGNS", APIPermBulkCampaigns, "Bulk campaign operations", "bulk"},
	{"REALTIME_SUBSCRIBE", APIPermRealtimeSubscribe, "Subscribe to realtime events", "special"},
	{"WEBHOOKS", APIPermWebhooks, "Manage webhook endpoints", "special"},
	{"API_KEYS", APIPermAPIKeys, "Create and manage API keys", "special"},
	{"INTEGRATIONS", APIPermIntegrations, "Connect and manage third-party integrations", "special"},
	{"WARMUP_ROUTING", APIPermWarmupRouting, "Manage warmup routing rules", "special"},
	{"AI_AGENT", APIPermAIAgent, "Run the AI assistant and MCP tools", "special"},
	{"AI_RESEARCH", APIPermAIResearch, "Run AI contact research", "special"},
}

// HasAPIPermission reports whether the bitmask grants every bit in `required`.
func HasAPIPermission(permissions uint64, required uint64) bool {
	return permissions&required == required
}

// HasAnyAPIPermission reports whether the bitmask grants at least one bit in `required`.
func HasAnyAPIPermission(permissions uint64, required uint64) bool {
	return permissions&required != 0
}

// apiPermissionRoleNeeds is the member permission each API scope acts under; 0 means every member.
var apiPermissionRoleNeeds = map[uint64]OrganizationPermission{
	APIPermReadEmails:        PermViewCampaigns,
	APIPermReadCampaigns:     PermViewCampaigns,
	APIPermReadContacts:      PermViewContacts,
	APIPermReadUnibox:        PermAccessUnibox,
	APIPermReadAnalytics:     PermViewAnalytics,
	APIPermWriteEmails:       PermManageEmails,
	APIPermWriteCampaigns:    PermManageCampaigns,
	APIPermWriteContacts:     PermManageContacts,
	APIPermWriteUnibox:       PermAccessUnibox,
	APIPermBulkContacts:      PermManageContacts,
	APIPermBulkCampaigns:     PermManageCampaigns,
	APIPermRealtimeSubscribe: 0,
	APIPermWebhooks:          PermManageSettings,
	APIPermAPIKeys:           PermManageAPIKeys,
	APIPermSendCampaigns:     PermSendCampaigns,
	APIPermReadTemplates:     PermViewCampaigns,
	APIPermWriteTemplates:    PermManageCampaigns,
	APIPermReadCRM:           PermViewContacts,
	APIPermWriteCRM:          PermManageContacts,
	APIPermReadAuditLogs:     PermViewAnalytics,
	APIPermIntegrations:      PermUseIntegrations,
	APIPermWarmupRouting:     PermManageSettings,
	APIPermAIAgent:           PermUseAI,
	APIPermAIResearch:        PermViewContacts | PermUseAI,
}

// APIPermissionsForMember is the widest API scope set a member may delegate to a key or an app.
func APIPermissionsForMember(perms OrganizationPermission, owner bool) uint64 {
	if owner {
		return AllAPIPermissionsMask
	}
	var mask uint64
	for bit, need := range apiPermissionRoleNeeds {
		if perms&need == need {
			mask |= bit
		}
	}
	return mask
}

// APIPermissionsFor is APIPermissionsForMember for a loaded membership; nil holds nothing.
func APIPermissionsFor(m *OrganizationMember) uint64 {
	if m == nil {
		return 0
	}
	return APIPermissionsForMember(m.Permissions, m.IsOwner())
}

// AppGrantableScopes is every scope a third-party app may be granted; key management stays with people.
const AppGrantableScopes = AllAPIPermissionsMask &^ APIPermAPIKeys
