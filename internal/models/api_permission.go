package models

// API Permissions - separate from admin Role permissions
// These control what API keys can access (uint64 for growth room)
const (
	// Read permissions
	APIPermReadEmails    uint64 = 1 << iota // Read email accounts
	APIPermReadCampaigns                    // Read campaigns
	APIPermReadContacts                     // Read contacts
	APIPermReadUnibox                       // Read unified inbox
	APIPermReadAnalytics                    // Read analytics/stats

	// Write permissions
	APIPermWriteEmails    // Modify email accounts
	APIPermWriteCampaigns // Create/modify campaigns
	APIPermWriteContacts  // Create/modify contacts
	APIPermWriteUnibox    // Modify inbox (mark seen, etc.)

	// Bulk operations
	APIPermBulkContacts  // Bulk contact operations
	APIPermBulkCampaigns // Bulk campaign operations

	// Realtime
	APIPermRealtimeSubscribe // Subscribe to realtime events

	// Special
	APIPermWebhooks // Manage webhooks
	APIPermAPIKeys  // Manage API keys (self-service)
)

// Preset permission sets
var (
	APIPermReadOnly uint64 = APIPermReadEmails | APIPermReadCampaigns |
		APIPermReadContacts | APIPermReadUnibox |
		APIPermReadAnalytics

	APIPermFullAccess uint64 = APIPermReadOnly | APIPermWriteEmails |
		APIPermWriteCampaigns | APIPermWriteContacts |
		APIPermWriteUnibox | APIPermBulkContacts |
		APIPermBulkCampaigns | APIPermRealtimeSubscribe |
		APIPermWebhooks | APIPermAPIKeys
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
	{"READ_CONTACTS", APIPermReadContacts, "View contact lists", "read"},
	{"READ_UNIBOX", APIPermReadUnibox, "Access unified inbox", "read"},
	{"READ_ANALYTICS", APIPermReadAnalytics, "View analytics and statistics", "read"},
	{"WRITE_EMAILS", APIPermWriteEmails, "Modify email account settings", "write"},
	{"WRITE_CAMPAIGNS", APIPermWriteCampaigns, "Create and modify campaigns", "write"},
	{"WRITE_CONTACTS", APIPermWriteContacts, "Create and modify contacts", "write"},
	{"WRITE_UNIBOX", APIPermWriteUnibox, "Mark emails as read/unread", "write"},
	{"BULK_CONTACTS", APIPermBulkContacts, "Bulk import/export contacts", "bulk"},
	{"BULK_CAMPAIGNS", APIPermBulkCampaigns, "Bulk campaign operations", "bulk"},
	{"REALTIME_SUBSCRIBE", APIPermRealtimeSubscribe, "Subscribe to real-time events", "special"},
	{"WEBHOOKS", APIPermWebhooks, "Manage webhook endpoints", "special"},
	{"API_KEYS", APIPermAPIKeys, "Create and manage API keys", "special"},
}

// HasPermission checks if permissions bitmask has the required permission
func HasAPIPermission(permissions uint64, required uint64) bool {
	return permissions&required == required
}

// HasAnyPermission checks if permissions bitmask has any of the required permissions
func HasAnyAPIPermission(permissions uint64, required uint64) bool {
	return permissions&required != 0
}
