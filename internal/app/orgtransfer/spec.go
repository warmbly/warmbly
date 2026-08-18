package orgtransfer

import "github.com/warmbly/warmbly/internal/models"

// KeyDomain names which key a sealed column was encrypted under. Warmbly has
// two, and an archive that confuses them produces mailboxes that authenticate
// against nothing:
//
//   - the instance credential key (CREDENTIALS_ENCRYPTION_KEY) seals mailbox
//     credentials, because the worker reads them without an org context
//   - the per-organization DEK seals everything else, because those values are
//     org assets and the DEK is what the KMS actually wraps
//
// Both are instance-local, so both have to be opened at export and re-sealed
// against the destination's own keys at import.
type KeyDomain uint8

const (
	// KeyDomainNone marks a column that is not ciphertext.
	KeyDomainNone KeyDomain = iota
	// KeyDomainInstance is the CREDENTIALS_ENCRYPTION_KEY AES-GCM encrypter.
	KeyDomainInstance
	// KeyDomainOrgDEK is the per-organization data encryption key.
	KeyDomainOrgDEK
)

// BlobKind says how to get an object key out of a column's value.
type BlobKind uint8

const (
	// BlobKindKey means the column holds the object key verbatim.
	BlobKindKey BlobKind = iota
	// BlobKindPublicURL means the column holds a browser-loadable URL that the
	// key can be recovered from. Avatars are stored as URLs because that is
	// what the dashboard renders, and the two storage backends build them
	// differently, so the key is recovered by locating its known prefix.
	BlobKindPublicURL
)

// BlobColumn is one column whose object travels with the archive.
type BlobColumn struct {
	Column string
	Kind   BlobKind
}

// SecretColumn is one encrypted-at-rest column.
type SecretColumn struct {
	Column string
	Domain KeyDomain
	// Guard, when set, names a boolean column that must be true for this
	// column to hold ciphertext. email_tasks predates unconditional sealing,
	// so its rows carry a flag rather than a format that can be sniffed.
	Guard string
}

// Table is one exported relation and the policy for moving it.
type Table struct {
	Name  string
	Group models.OrgDataGroup

	// Scope is the WHERE fragment selecting this table's rows for one
	// organization. $1 is the organization id.
	Scope string

	// Secrets are columns holding ciphertext that must be re-keyed.
	Secrets []SecretColumn

	// Blobs are columns pointing at an object whose bytes travel alongside
	// the rows.
	Blobs []BlobColumn

	// ResetOnImport are columns set to NULL on the way in because they name
	// something that exists only on the source instance.
	ResetOnImport []string

	// ImportSkip exports the table for the record but never writes it back.
	ImportSkip bool

	// Note explains a non-obvious policy choice; surfaced in the docs table.
	Note string
}

// Scope fragments. Written as subqueries rather than joins so every scope is a
// plain WHERE clause and the reader can stay a single generic SELECT.
const (
	scopeOrg       = `organization_id = $1`
	scopeOrgAlt    = `org_id = $1`
	orgMailboxes   = `(SELECT id FROM email_accounts WHERE organization_id = $1)`
	orgCampaigns   = `(SELECT id FROM campaigns WHERE organization_id = $1)`
	orgContacts    = `(SELECT id FROM contacts WHERE organization_id = $1)`
	orgTasks       = `(SELECT id FROM tasks WHERE email_account_id IN ` + orgMailboxes + `)`
	orgThreads     = `(SELECT DISTINCT thread_id FROM unibox_emails WHERE email_id IN ` + orgMailboxes + `)`
	orgPipelines   = `(SELECT id FROM pipelines WHERE organization_id = $1)`
	orgInvitations = `(SELECT id FROM organization_invitations WHERE organization_id = $1)`
	orgTeams       = `(SELECT id FROM teams WHERE organization_id = $1)`
	orgAPIKeys     = `(SELECT id FROM api_keys WHERE organization_id = $1)`
	orgSessions    = `(SELECT id FROM agent_sessions WHERE org_id = $1)`
	orgPlacements  = `(SELECT id FROM placement_tests WHERE organization_id = $1)`
)

// Tables is every relation an archive carries, in dependency order. Import
// applies them top to bottom, so a table must never appear before something it
// references. Export order does not matter but follows the same list so the
// archive reads in a sensible order when someone unzips it.
//
// The organizations row itself is not here: it is written into the manifest and
// merged onto the destination workspace rather than inserted as a row.
var Tables = []Table{
	// ---------- core: the workspace itself ----------
	{
		Name: "organization_roles", Group: models.OrgDataGroupCore,
		Scope: scopeOrg,
	},
	{
		Name: "organization_members", Group: models.OrgDataGroupCore,
		Scope: scopeOrg,
		Note:  "Members are matched to destination accounts by email; unknown emails become invitations.",
	},
	{
		Name: "organization_member_roles", Group: models.OrgDataGroupCore,
		Scope: scopeOrg,
	},
	{
		Name: "organization_invitations", Group: models.OrgDataGroupCore,
		Scope: scopeOrg,
	},
	{
		Name: "organization_invitation_roles", Group: models.OrgDataGroupCore,
		Scope: `invitation_id IN ` + orgInvitations,
	},
	{
		Name: "teams", Group: models.OrgDataGroupCore,
		Scope: scopeOrg,
	},
	{
		Name: "team_members", Group: models.OrgDataGroupCore,
		Scope: `team_id IN ` + orgTeams,
	},
	{
		Name: "email_accounts", Group: models.OrgDataGroupCore,
		Scope: scopeOrg,
		// Worker placement is a property of the instance the mailbox runs on,
		// never of the mailbox. The destination assigns its own.
		ResetOnImport: []string{"worker_id"},
	},
	{
		Name: "email_accounts_smtp_imap", Group: models.OrgDataGroupCore,
		Scope: `email_account_id IN ` + orgMailboxes,
		Secrets: []SecretColumn{
			{Column: "smtp_host", Domain: KeyDomainInstance},
			{Column: "smtp_user", Domain: KeyDomainInstance},
			{Column: "smtp_password", Domain: KeyDomainInstance},
			{Column: "imap_host", Domain: KeyDomainInstance},
			{Column: "imap_user", Domain: KeyDomainInstance},
			{Column: "imap_password", Domain: KeyDomainInstance},
		},
	},
	{
		Name: "email_accounts_oauth", Group: models.OrgDataGroupCore,
		Scope: `email_account_id IN ` + orgMailboxes,
		Secrets: []SecretColumn{
			{Column: "access_token", Domain: KeyDomainInstance},
			{Column: "refresh_token", Domain: KeyDomainInstance},
		},
	},
	{
		Name: "email_account_behavior", Group: models.OrgDataGroupCore,
		Scope: `email_account_id IN ` + orgMailboxes,
	},
	{
		Name: "tags", Group: models.OrgDataGroupCore,
		// Labels are user-scoped in the schema, so they are collected by what
		// the organization's own rows reference. Scoping them by owning user
		// instead would drag that user's other workspaces into the archive.
		Scope: `id IN (SELECT tag_id FROM email_tags WHERE email_id IN ` + orgMailboxes + `)
		     OR id IN (SELECT tag_id FROM campaign_email_tags WHERE campaign_id IN ` + orgCampaigns + `)`,
		Note: "Only tags this workspace actually uses travel; tags are owned by a user, not an organization.",
	},
	{
		Name: "email_tags", Group: models.OrgDataGroupCore,
		Scope: `email_id IN ` + orgMailboxes,
	},
	{
		Name: "api_keys", Group: models.OrgDataGroupCore,
		Scope:         scopeOrg,
		ResetOnImport: []string{"last_used_at", "last_request_ip"},
		Note:          "Only the key hash travels, so existing keys keep working after the move without the secret ever leaving the source.",
	},
	{
		Name: "webhook_endpoints", Group: models.OrgDataGroupCore,
		Scope:         scopeOrg,
		ResetOnImport: []string{"last_success_at", "last_failure_at", "last_failure_reason", "consecutive_failures", "first_failure_at", "auto_disabled_at", "disabled_reason"},
	},
	{
		Name: "oauth_applications", Group: models.OrgDataGroupCore,
		Scope: scopeOrg,
	},
	{
		Name: "oauth_access_grants", Group: models.OrgDataGroupCore,
		Scope:         scopeOrg,
		ResetOnImport: []string{"last_used_at"},
	},
	{
		Name: "outreach_settings", Group: models.OrgDataGroupCore,
		Scope: scopeOrg,
	},
	{
		Name: "org_ai_settings", Group: models.OrgDataGroupCore,
		Scope: scopeOrgAlt,
	},
	{
		Name: "advisor_settings", Group: models.OrgDataGroupCore,
		Scope: scopeOrg,
	},

	// ---------- contacts ----------
	{
		Name: "categories", Group: models.OrgDataGroupContacts,
		Scope: `id IN (SELECT category_id FROM contact_categories WHERE contact_id IN ` + orgContacts + `)
		     OR id IN (SELECT category_id FROM unibox_thread_labels WHERE thread_id IN ` + orgThreads + `)`,
		Note: "Same user-scoped-label rule as tags.",
	},
	{
		Name: "contacts", Group: models.OrgDataGroupContacts,
		Scope: scopeOrg,
	},
	{
		Name: "contact_categories", Group: models.OrgDataGroupContacts,
		Scope: `contact_id IN ` + orgContacts,
	},
	{
		Name: "contact_notes", Group: models.OrgDataGroupContacts,
		Scope: scopeOrg,
	},
	{
		Name: "contact_activities", Group: models.OrgDataGroupContacts,
		Scope: scopeOrg,
	},
	{
		Name: "suppressed_recipients", Group: models.OrgDataGroupContacts,
		Scope: scopeOrg,
		Note:  "Suppression must travel, or the destination re-mails people who already opted out.",
	},
	{
		Name: "contact_research_runs", Group: models.OrgDataGroupContacts,
		Scope: scopeOrgAlt,
	},

	// ---------- campaigns ----------
	{
		Name: "folders", Group: models.OrgDataGroupCampaigns,
		Scope: `id IN (SELECT folder_id FROM campaign_folders WHERE campaign_id IN ` + orgCampaigns + `)`,
	},
	{
		Name: "campaigns", Group: models.OrgDataGroupCampaigns,
		Scope: scopeOrg,
	},
	{
		Name: "campaign_folders", Group: models.OrgDataGroupCampaigns,
		Scope: `campaign_id IN ` + orgCampaigns,
	},
	{
		Name: "sequences", Group: models.OrgDataGroupCampaigns,
		Scope: scopeOrg,
	},
	{
		Name: "campaign_attachments", Group: models.OrgDataGroupCampaigns,
		Scope: `campaign_id IN ` + orgCampaigns,
		Blobs: []BlobColumn{{Column: "s3_key", Kind: BlobKindKey}},
	},
	{
		Name: "campaign_senders", Group: models.OrgDataGroupCampaigns,
		Scope: `campaign_id IN ` + orgCampaigns,
	},
	{
		Name: "campaign_email_tags", Group: models.OrgDataGroupCampaigns,
		Scope: `campaign_id IN ` + orgCampaigns,
	},
	{
		Name: "campaign_advanced_settings", Group: models.OrgDataGroupCampaigns,
		Scope: `campaign_id IN ` + orgCampaigns,
	},
	{
		Name: "campaign_ab_variants", Group: models.OrgDataGroupCampaigns,
		Scope: `campaign_id IN ` + orgCampaigns,
	},
	{
		Name: "campaign_leads", Group: models.OrgDataGroupCampaigns,
		Scope: `campaign_id IN ` + orgCampaigns,
	},
	{
		Name: "campaign_ab_assignments", Group: models.OrgDataGroupCampaigns,
		Scope: `campaign_id IN ` + orgCampaigns,
	},
	{
		Name: "campaign_contact_progress", Group: models.OrgDataGroupCampaigns,
		Scope: `campaign_id IN ` + orgCampaigns,
		Note:  "Carries per-contact step position, so a running campaign resumes instead of restarting from step one.",
	},
	{
		Name: "campaign_daily_sends", Group: models.OrgDataGroupCampaigns,
		Scope: `campaign_id IN ` + orgCampaigns,
	},
	{
		Name: "preflight_reports", Group: models.OrgDataGroupCampaigns,
		Scope: scopeOrg,
	},

	// ---------- CRM ----------
	{
		Name: "pipelines", Group: models.OrgDataGroupCRM,
		Scope: scopeOrg,
	},
	{
		Name: "pipeline_stages", Group: models.OrgDataGroupCRM,
		Scope: `pipeline_id IN ` + orgPipelines,
	},
	{
		Name: "crm_task_types", Group: models.OrgDataGroupCRM,
		Scope: scopeOrg,
	},
	{
		Name: "deals", Group: models.OrgDataGroupCRM,
		Scope: scopeOrg,
	},
	{
		Name: "crm_tasks", Group: models.OrgDataGroupCRM,
		Scope: scopeOrg,
	},
	{
		Name: "meeting_bookings", Group: models.OrgDataGroupCRM,
		Scope: scopeOrg,
	},

	// ---------- automations and integrations ----------
	{
		Name: "integration_connections", Group: models.OrgDataGroupAutomations,
		Scope: scopeOrg,
		Secrets: []SecretColumn{
			{Column: "config_encrypted", Domain: KeyDomainOrgDEK},
			{Column: "access_token_encrypted", Domain: KeyDomainOrgDEK},
			{Column: "refresh_token_encrypted", Domain: KeyDomainOrgDEK},
		},
		ResetOnImport: []string{"last_synced_at", "last_error", "last_error_at", "health_checked_at"},
	},
	{
		Name: "automations", Group: models.OrgDataGroupAutomations,
		Scope: scopeOrg,
	},
	{
		Name: "integration_event_subscriptions", Group: models.OrgDataGroupAutomations,
		Scope: scopeOrg,
	},
	{
		Name: "integration_field_mappings", Group: models.OrgDataGroupAutomations,
		Scope: scopeOrg,
	},
	{
		Name: "integration_sync_runs", Group: models.OrgDataGroupAutomations,
		Scope: scopeOrg,
	},
	{
		Name: "automation_runs", Group: models.OrgDataGroupAutomations,
		Scope: scopeOrg,
	},
	{
		Name: "lead_sync_sources", Group: models.OrgDataGroupAutomations,
		Scope:         scopeOrg,
		ResetOnImport: []string{"last_synced_at", "last_result", "last_error"},
	},

	// ---------- assistant ----------
	{
		Name: "ai_skills", Group: models.OrgDataGroupAI,
		Scope: scopeOrgAlt,
	},
	{
		Name: "ai_mcp_servers", Group: models.OrgDataGroupAI,
		Scope: scopeOrgAlt,
		Secrets: []SecretColumn{
			{Column: "credentials_encrypted", Domain: KeyDomainOrgDEK},
		},
		ResetOnImport: []string{"last_error"},
	},
	{
		Name: "ai_tool_policies", Group: models.OrgDataGroupAI,
		Scope: scopeOrgAlt,
	},
	{
		Name: "agent_sessions", Group: models.OrgDataGroupAI,
		Scope: scopeOrgAlt,
	},
	{
		Name: "agent_messages", Group: models.OrgDataGroupAI,
		Scope: `session_id IN ` + orgSessions,
	},
	{
		Name: "ai_thread_drafts", Group: models.OrgDataGroupAI,
		Scope: scopeOrg,
	},
	{
		Name: "compose_drafts", Group: models.OrgDataGroupAI,
		Scope: scopeOrg,
	},
	{
		Name: "reply_templates", Group: models.OrgDataGroupAI,
		Scope: scopeOrg,
	},
	{
		Name: "reply_intents", Group: models.OrgDataGroupAI,
		Scope: scopeOrg,
	},

	// ---------- warmup ----------
	{
		Name: "warmup_routing_rules", Group: models.OrgDataGroupWarmup,
		Scope: scopeOrg,
	},
	{
		Name: "warmup_statistics", Group: models.OrgDataGroupWarmup,
		Scope: `email_account_id IN ` + orgMailboxes,
		Note:  "Warmup volume history travels so the destination resumes the ramp instead of restarting at the floor.",
	},
	{
		Name: "warmup_appeals", Group: models.OrgDataGroupWarmup,
		Scope: `email_account_id IN ` + orgMailboxes,
	},
	{
		Name: "warmup_invalid_token_attempts", Group: models.OrgDataGroupWarmup,
		Scope: `email_account_id IN ` + orgMailboxes,
	},
	{
		Name: "warmup_spam_reports", Group: models.OrgDataGroupWarmup,
		Scope: `reporter_account_id IN ` + orgMailboxes,
	},
	{
		Name: "warmup_pool_participants", Group: models.OrgDataGroupWarmup,
		Scope:      `email_account_id IN ` + orgMailboxes,
		ImportSkip: true,
		Note:       "Pool rows are instance-global, so membership is re-earned on the destination rather than asserted by an archive.",
	},
	{
		Name: "warmup_admin_actions", Group: models.OrgDataGroupWarmup,
		Scope:      `email_account_id IN ` + orgMailboxes,
		ImportSkip: true,
		Note:       "Records what a platform admin on the source instance did; meaningless as an assertion about the destination.",
	},

	// ---------- inbox ----------
	{
		Name: "unibox_mailboxes", Group: models.OrgDataGroupInbox,
		Scope: `email_id IN ` + orgMailboxes,
	},
	{
		Name: "unibox_emails", Group: models.OrgDataGroupInbox,
		Scope: `email_id IN ` + orgMailboxes,
	},
	{
		Name: "unibox_thread_labels", Group: models.OrgDataGroupInbox,
		Scope: `thread_id IN ` + orgThreads,
	},
	{
		Name: "unibox_snoozes", Group: models.OrgDataGroupInbox,
		Scope: `thread_id IN ` + orgThreads,
	},
	{
		Name: "email_message_map", Group: models.OrgDataGroupInbox,
		Scope: `email_id IN ` + orgMailboxes,
		Note:  "Maps provider message ids to internal ones, so replies still thread after the move.",
	},
	{
		Name: "email_history_ids", Group: models.OrgDataGroupInbox,
		Scope:      `email_id IN ` + orgMailboxes,
		ImportSkip: true,
		Note:       "A sync checkpoint from the source. Replaying it would make the destination skip everything that arrived between export and import, so it re-syncs from scratch instead.",
	},
	{
		Name: "email_delta_links", Group: models.OrgDataGroupInbox,
		Scope:      `email_id IN ` + orgMailboxes,
		ImportSkip: true,
		Note:       "Same reason as email_history_ids.",
	},

	// ---------- send pipeline ----------
	{
		Name: "tasks", Group: models.OrgDataGroupSending,
		Scope: `email_account_id IN ` + orgMailboxes,
		// The handle belongs to the source instance's queue.
		ResetOnImport: []string{"cloud_task_name"},
	},
	{
		Name: "email_tasks", Group: models.OrgDataGroupSending,
		Scope: `task_id IN ` + orgTasks,
		Secrets: []SecretColumn{
			{Column: "subject", Domain: KeyDomainOrgDEK, Guard: "encrypted"},
			{Column: "body", Domain: KeyDomainOrgDEK, Guard: "encrypted"},
			{Column: "body_html", Domain: KeyDomainOrgDEK, Guard: "encrypted"},
			{Column: "body_plain", Domain: KeyDomainOrgDEK, Guard: "encrypted"},
		},
	},
	{
		Name: "campaign_tasks", Group: models.OrgDataGroupSending,
		Scope: `task_id IN ` + orgTasks,
	},
	{
		Name: "warmup_tasks", Group: models.OrgDataGroupSending,
		Scope: `task_id IN ` + orgTasks,
	},
	{
		Name: "warmup_tokens", Group: models.OrgDataGroupSending,
		Scope: `task_id IN ` + orgTasks,
	},
	{
		Name: "task_failures", Group: models.OrgDataGroupSending,
		Scope: `task_id IN ` + orgTasks,
	},
	{
		Name: "task_dead_letters", Group: models.OrgDataGroupSending,
		Scope: `task_id IN ` + orgTasks,
	},
	{
		Name: "task_execution_keys", Group: models.OrgDataGroupSending,
		Scope: `task_id IN ` + orgTasks,
	},
	{
		Name: "daily_email_counts", Group: models.OrgDataGroupSending,
		Scope: `email_account_id IN ` + orgMailboxes,
		Note:  "Today's counter travels, so a mailbox cannot double its daily volume by being migrated mid-day.",
	},
	{
		Name: "email_account_daily_plan", Group: models.OrgDataGroupSending,
		Scope: `email_account_id IN ` + orgMailboxes,
	},
	{
		Name: "email_account_errors", Group: models.OrgDataGroupSending,
		Scope: `email_account_id IN ` + orgMailboxes,
	},
	{
		Name: "tracked_links", Group: models.OrgDataGroupSending,
		Scope: `campaign_id IN ` + orgCampaigns,
		Note:  "Click tickets already in the wild keep resolving after the move, provided the tracking domain follows.",
	},

	// ---------- delivery events ----------
	{
		Name: "deliverability_events", Group: models.OrgDataGroupEvents,
		Scope: scopeOrg,
	},
	{
		Name: "placement_tests", Group: models.OrgDataGroupEvents,
		Scope: scopeOrg,
	},
	{
		Name: "placement_results", Group: models.OrgDataGroupEvents,
		Scope: `test_id IN ` + orgPlacements,
	},
	{
		Name: "webhook_deliveries", Group: models.OrgDataGroupEvents,
		Scope: scopeOrg,
	},
	{
		Name: "webhook_event_drops", Group: models.OrgDataGroupEvents,
		Scope: scopeOrg,
	},
	{
		Name: "api_key_usage_logs", Group: models.OrgDataGroupEvents,
		Scope: `api_key_id IN ` + orgAPIKeys,
	},

	// ---------- logs ----------
	{
		Name: "audit_logs", Group: models.OrgDataGroupLogs,
		Scope: scopeOrg,
	},
	{
		Name: "campaign_logs", Group: models.OrgDataGroupLogs,
		Scope: `campaign_id IN ` + orgCampaigns,
	},
	{
		Name: "notifications", Group: models.OrgDataGroupLogs,
		Scope:         scopeOrg,
		ResetOnImport: []string{"email_state", "email_due_at", "email_attempts"},
		Note:          "Pending digest state is cleared so an import cannot re-send a month of notification emails.",
	},
	{
		Name: "advisor_runs", Group: models.OrgDataGroupLogs,
		Scope: scopeOrg,
	},
	{
		Name: "advisor_findings", Group: models.OrgDataGroupLogs,
		Scope: scopeOrg,
	},
	{
		Name: "advisor_feedback", Group: models.OrgDataGroupLogs,
		Scope: scopeOrg,
	},
	{
		Name: "advisor_narrations", Group: models.OrgDataGroupLogs,
		Scope: scopeOrg,
	},

	// ---------- billing: exported for the record, never applied ----------
	{
		Name: "subscriptions", Group: models.OrgDataGroupBilling,
		Scope: scopeOrg, ImportSkip: true,
		Note: "Stripe identifiers belong to the source instance's Stripe account; the destination issues its own subscription.",
	},
	{
		Name: "credit_ledger", Group: models.OrgDataGroupBilling,
		Scope: scopeOrgAlt, ImportSkip: true,
		Note: "Importing a balance would mint credits the destination was never paid for.",
	},
	{
		Name: "credit_ledger_transactions", Group: models.OrgDataGroupBilling,
		Scope: scopeOrgAlt, ImportSkip: true,
	},
	{
		Name: "referral_earnings_ledger", Group: models.OrgDataGroupBilling,
		Scope: scopeOrgAlt, ImportSkip: true,
	},
	{
		Name: "referral_earnings_transactions", Group: models.OrgDataGroupBilling,
		Scope: scopeOrgAlt, ImportSkip: true,
	},
	{
		Name: "discount_redemptions", Group: models.OrgDataGroupBilling,
		Scope: scopeOrg, ImportSkip: true,
	},
	{
		Name: "organization_limit_overrides", Group: models.OrgDataGroupBilling,
		Scope: scopeOrg, ImportSkip: true,
		Note: "A plan override is a grant by one platform's operators, not a property the workspace carries with it.",
	},
	{
		Name: "limit_increase_requests", Group: models.OrgDataGroupBilling,
		Scope: scopeOrg, ImportSkip: true,
	},
}

// ExcludedTables are org-owned relations an archive deliberately never carries,
// with the reason. Kept as data so the docs page and the coverage test both
// read from one list instead of restating it.
var ExcludedTables = map[string]string{
	"organization_encrypted_keys":  "The organization's data key, wrapped by the source instance's KMS. The destination cannot unwrap it, and shipping it would put every org secret behind one exported blob.",
	"api_idempotency_keys":         "A short-lived replay cache for in-flight API requests.",
	"realtime_events":              "The websocket outbox. Every row is already delivered or expired.",
	"integration_oauth_states":     "In-flight OAuth handshakes, valid for minutes and bound to the source instance's redirect URL.",
	"oauth_authorization_codes":    "Single-use authorization codes, valid for seconds.",
	"scheduled_deletions":          "Instance lifecycle state. Importing a pending deletion would schedule the destination workspace for destruction.",
	"dedicated_worker_assignments": "Worker topology, which is a property of the instance rather than the workspace.",
	"warmup_pools":                 "Instance-global pool definitions shared by every workspace on the instance.",
	"warmup_conversations":         "The instance's shared warmup content library, not workspace data.",
	"sessions":                     "Live login sessions. They are bound to the source instance's signing key and must not survive a move.",
}

// TableByName indexes Tables for lookup during import.
var TableByName = func() map[string]*Table {
	m := make(map[string]*Table, len(Tables))
	for i := range Tables {
		m[Tables[i].Name] = &Tables[i]
	}
	return m
}()

// GroupTables returns the tables belonging to the selected groups, in
// dependency order. An empty selection means every group.
func GroupTables(groups []models.OrgDataGroup) []*Table {
	want := make(map[models.OrgDataGroup]bool, len(groups))
	for _, g := range groups {
		want[g] = true
	}
	out := make([]*Table, 0, len(Tables))
	for i := range Tables {
		t := &Tables[i]
		if len(want) == 0 || want[t.Group] || t.Group == models.OrgDataGroupCore {
			out = append(out, t)
		}
	}
	return out
}

// NormalizeGroups validates a requested group list, adds core (which every
// archive needs to be importable at all), and closes over the catalog's
// dependencies so a selection can never be one that fails on a foreign key.
func NormalizeGroups(groups []models.OrgDataGroup) []models.OrgDataGroup {
	if len(groups) == 0 {
		return append([]models.OrgDataGroup(nil), models.AllOrgDataGroups...)
	}

	valid := make(map[models.OrgDataGroup]bool, len(models.AllOrgDataGroups))
	for _, g := range models.AllOrgDataGroups {
		valid[g] = true
	}

	requires := models.GroupRequirements()
	seen := map[models.OrgDataGroup]bool{models.OrgDataGroupCore: true}
	queue := append([]models.OrgDataGroup(nil), groups...)
	for len(queue) > 0 {
		g := queue[0]
		queue = queue[1:]
		if seen[g] || !valid[g] {
			continue
		}
		seen[g] = true
		queue = append(queue, requires[g]...)
	}

	// Emit in catalog order so the result is stable whatever order the caller
	// asked in, which keeps the job row and the manifest comparable.
	out := make([]models.OrgDataGroup, 0, len(seen))
	for _, g := range models.AllOrgDataGroups {
		if seen[g] {
			out = append(out, g)
		}
	}
	return out
}
