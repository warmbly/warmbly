package models

import (
	"time"

	"github.com/google/uuid"
)

// OrgTransferFormatVersion is the archive layout version written into every
// manifest. Bump it only for a change an older importer could not read; the
// importer already tolerates unknown tables and unknown columns, so adding
// either is not a format change.
const OrgTransferFormatVersion = 1

// OrgTransferStatus is the lifecycle of one export or import job. Export adds
// "expired": the archive is deleted from blob storage after its retention
// window, and the row survives so the history stays honest about what ran.
type OrgTransferStatus string

const (
	OrgTransferStatusQueued    OrgTransferStatus = "queued"
	OrgTransferStatusRunning   OrgTransferStatus = "running"
	OrgTransferStatusCompleted OrgTransferStatus = "completed"
	OrgTransferStatusFailed    OrgTransferStatus = "failed"
	OrgTransferStatusExpired   OrgTransferStatus = "expired"
)

// IsTerminal reports whether a job has stopped moving.
func (s OrgTransferStatus) IsTerminal() bool {
	switch s {
	case OrgTransferStatusCompleted, OrgTransferStatusFailed, OrgTransferStatusExpired:
		return true
	}
	return false
}

// OrgDataGroup names one slice of a workspace. Groups exist so a migration can
// leave the bulky history behind: "core" plus "contacts" plus "campaigns"
// rebuilds a working workspace, while "inbox" and "events" can multiply the
// archive size by a hundred on a busy org without changing what it does.
type OrgDataGroup string

const (
	// OrgDataGroupCore is the workspace itself: the organization row, members,
	// roles, teams, settings, API keys, webhooks, and the mailbox records.
	// Always included; an archive without it cannot be imported.
	OrgDataGroupCore OrgDataGroup = "core"
	// OrgDataGroupContacts is contacts, categories, notes, activities, and
	// suppression.
	OrgDataGroupContacts OrgDataGroup = "contacts"
	// OrgDataGroupCampaigns is campaigns, sequences, senders, attachments, and
	// per-campaign settings.
	OrgDataGroupCampaigns OrgDataGroup = "campaigns"
	// OrgDataGroupCRM is pipelines, deals, CRM tasks, and meeting bookings.
	OrgDataGroupCRM OrgDataGroup = "crm"
	// OrgDataGroupAutomations is automations, integrations, and lead sync.
	OrgDataGroupAutomations OrgDataGroup = "automations"
	// OrgDataGroupAI is the assistant: sessions, messages, skills, MCP servers,
	// and AI settings.
	OrgDataGroupAI OrgDataGroup = "ai"
	// OrgDataGroupWarmup is warmup participation, statistics, and appeals.
	OrgDataGroupWarmup OrgDataGroup = "warmup"
	// OrgDataGroupInbox is the unified inbox: mailboxes, threads, message
	// bodies, and mailbox sync checkpoints. Usually the largest group.
	OrgDataGroupInbox OrgDataGroup = "inbox"
	// OrgDataGroupSending is the send pipeline: tasks, their payloads, and
	// per-day counters.
	OrgDataGroupSending OrgDataGroup = "sending"
	// OrgDataGroupEvents is delivery, tracking, and placement history.
	OrgDataGroupEvents OrgDataGroup = "events"
	// OrgDataGroupLogs is audit logs, campaign logs, and notifications.
	OrgDataGroupLogs OrgDataGroup = "logs"
	// OrgDataGroupBilling is subscription, credit ledger, and referral state.
	// Never applied verbatim on import: the destination instance owns billing.
	OrgDataGroupBilling OrgDataGroup = "billing"
)

// AllOrgDataGroups is every group in display order. The dashboard renders the
// toggle list from this, so order here is the order the user sees.
var AllOrgDataGroups = []OrgDataGroup{
	OrgDataGroupCore,
	OrgDataGroupContacts,
	OrgDataGroupCampaigns,
	OrgDataGroupCRM,
	OrgDataGroupAutomations,
	OrgDataGroupAI,
	OrgDataGroupWarmup,
	OrgDataGroupInbox,
	OrgDataGroupSending,
	OrgDataGroupEvents,
	OrgDataGroupLogs,
	OrgDataGroupBilling,
}

// OrgDataGroupInfo describes a group for the settings UI.
type OrgDataGroupInfo struct {
	Key         OrgDataGroup `json:"key"`
	Label       string       `json:"label"`
	Description string       `json:"description"`
	// Required groups cannot be switched off.
	Required bool `json:"required"`
	// Heavy marks the groups that dominate archive size on a busy workspace,
	// so the UI can warn before someone exports a decade of inbox history.
	Heavy bool `json:"heavy"`
	// Requires names the groups this one cannot travel without, because its
	// rows hold NOT NULL references into them. Selecting a group selects these
	// too, on the server and in the dashboard, so what the toggles show is
	// what the archive actually gets.
	Requires []OrgDataGroup `json:"requires,omitempty"`
}

// OrgDataGroupCatalog is the user-facing description of every group, and the
// single source of truth for the dependencies between them.
//
// Each Requires entry is a NOT NULL foreign key crossing a group boundary:
// campaign_leads keys on a contact, unibox_thread_labels on a category,
// campaign_logs on a campaign. Leaving the target behind would not degrade an
// import, it would abort it. Nullable crossings need no entry — the importer
// blanks those when their target is not part of the run.
var OrgDataGroupCatalog = []OrgDataGroupInfo{
	{Key: OrgDataGroupCore, Label: "Workspace", Description: "Organization, members, roles, teams, mailboxes, API keys, webhooks, and settings.", Required: true},
	{Key: OrgDataGroupContacts, Label: "Contacts", Description: "Contacts, categories, notes, activities, and the suppression list."},
	{Key: OrgDataGroupCampaigns, Label: "Campaigns", Description: "Campaigns, sequences, senders, attachments, and per-campaign settings.", Requires: []OrgDataGroup{OrgDataGroupContacts}},
	{Key: OrgDataGroupCRM, Label: "CRM", Description: "Pipelines, deals, tasks, and meeting bookings."},
	{Key: OrgDataGroupAutomations, Label: "Automations", Description: "Automations, connected integrations, and lead sync sources."},
	{Key: OrgDataGroupAI, Label: "Assistant", Description: "Assistant sessions and messages, skills, MCP servers, and AI settings."},
	{Key: OrgDataGroupWarmup, Label: "Warmup", Description: "Warmup participation, routing rules, statistics, and appeals."},
	{Key: OrgDataGroupInbox, Label: "Inbox", Description: "Unified inbox threads, message bodies, and mailbox sync state.", Heavy: true, Requires: []OrgDataGroup{OrgDataGroupContacts}},
	{Key: OrgDataGroupSending, Label: "Send history", Description: "Queued and completed send tasks with their payloads.", Heavy: true},
	{Key: OrgDataGroupEvents, Label: "Delivery events", Description: "Bounces, complaints, opens, clicks, and placement tests.", Heavy: true},
	{Key: OrgDataGroupLogs, Label: "Logs", Description: "Audit log, campaign logs, and notifications.", Heavy: true, Requires: []OrgDataGroup{OrgDataGroupCampaigns}},
	{Key: OrgDataGroupBilling, Label: "Billing history", Description: "Subscription, credit ledger, and referral records. Read-only on import: the destination instance owns billing."},
}

// GroupRequirements indexes the catalog's dependencies for closure walks.
func GroupRequirements() map[OrgDataGroup][]OrgDataGroup {
	out := make(map[OrgDataGroup][]OrgDataGroup, len(OrgDataGroupCatalog))
	for _, g := range OrgDataGroupCatalog {
		if len(g.Requires) > 0 {
			out[g.Key] = g.Requires
		}
	}
	return out
}

// OrgImportConflict is what to do when the archive carries a row whose primary
// key already exists in the destination.
type OrgImportConflict string

const (
	// OrgImportConflictSkip keeps the row that is already here. The safe
	// default: an import never destroys data that was not in the archive.
	OrgImportConflictSkip OrgImportConflict = "skip"
	// OrgImportConflictOverwrite replaces the existing row with the archive's.
	OrgImportConflictOverwrite OrgImportConflict = "overwrite"
)

// OrgExportJob is one archive build.
type OrgExportJob struct {
	ID             uuid.UUID         `json:"id"`
	OrganizationID uuid.UUID         `json:"organization_id"`
	RequestedBy    *uuid.UUID        `json:"requested_by,omitempty"`
	Status         OrgTransferStatus `json:"status"`

	Groups         []OrgDataGroup `json:"groups"`
	IncludeSecrets bool           `json:"include_secrets"`
	FormatVersion  int            `json:"format_version"`

	ProgressPercent int    `json:"progress_percent"`
	ProgressStage   string `json:"progress_stage"`

	ArchiveKey    *string          `json:"-"`
	ArchiveBytes  *int64           `json:"archive_bytes,omitempty"`
	ArchiveSHA256 *string          `json:"archive_sha256,omitempty"`
	RowCounts     map[string]int64 `json:"row_counts"`

	ErrorMessage *string    `json:"error_message,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// TotalRows sums every table's row count, for the "12,481 rows" summary line.
func (j *OrgExportJob) TotalRows() int64 {
	var total int64
	for _, n := range j.RowCounts {
		total += n
	}
	return total
}

// OrgImportJob is one archive application.
type OrgImportJob struct {
	ID             uuid.UUID         `json:"id"`
	OrganizationID uuid.UUID         `json:"organization_id"`
	RequestedBy    *uuid.UUID        `json:"requested_by,omitempty"`
	Status         OrgTransferStatus `json:"status"`

	ArchiveKey     *string         `json:"-"`
	ArchiveBytes   *int64          `json:"archive_bytes,omitempty"`
	ArchiveSHA256  *string         `json:"archive_sha256,omitempty"`
	SourceManifest *OrgArchiveInfo `json:"source_manifest,omitempty"`

	Groups           []OrgDataGroup    `json:"groups"`
	ConflictStrategy OrgImportConflict `json:"conflict_strategy"`

	ProgressPercent int    `json:"progress_percent"`
	ProgressStage   string `json:"progress_stage"`

	RowCounts map[string]int64 `json:"row_counts"`
	Warnings  []string         `json:"warnings"`

	ErrorMessage *string    `json:"error_message,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// OrgArchiveInfo is the manifest summary the dashboard shows before an import
// runs and stores on the job afterwards. It is the subset of the on-disk
// manifest that is safe and useful to display.
type OrgArchiveInfo struct {
	FormatVersion    int              `json:"format_version"`
	SourceInstance   string           `json:"source_instance"`
	SourceAppVersion string           `json:"source_app_version"`
	OrganizationID   uuid.UUID        `json:"organization_id"`
	OrganizationName string           `json:"organization_name"`
	ExportedAt       time.Time        `json:"exported_at"`
	Groups           []OrgDataGroup   `json:"groups"`
	HasSecrets       bool             `json:"has_secrets"`
	RowCounts        map[string]int64 `json:"row_counts"`
	BlobCount        int              `json:"blob_count"`
	Members          []OrgArchiveUser `json:"members"`
}

// TotalRows sums the manifest's per-table counts.
func (a *OrgArchiveInfo) TotalRows() int64 {
	var total int64
	for _, n := range a.RowCounts {
		total += n
	}
	return total
}

// OrgArchiveUser is a member carried by the archive. The importer matches
// these to destination accounts by email; there is no password material here,
// so an archive can never mint a usable login on the destination.
type OrgArchiveUser struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	Role      string    `json:"role"`
	IsOwner   bool      `json:"is_owner"`
}

// CreateOrgExportRequest is the export endpoint's body.
type CreateOrgExportRequest struct {
	// Groups to include. Empty means every group.
	Groups []OrgDataGroup `json:"groups,omitempty"`
	// IncludeSecrets re-seals mailbox and integration credentials into the
	// archive under Passphrase so the destination can bring mailboxes back up
	// without every user reconnecting. Requires Passphrase.
	IncludeSecrets bool `json:"include_secrets,omitempty"`
	// Passphrase protects the secrets bundle. Never stored: it is used once to
	// derive the archive's sealing key and then discarded.
	Passphrase string `json:"passphrase,omitempty"`
}

// CreateOrgImportRequest is the import endpoint's options form field. The
// archive itself arrives as the multipart file.
type CreateOrgImportRequest struct {
	// Groups to apply. Empty means every group present in the archive.
	Groups []OrgDataGroup `json:"groups,omitempty"`
	// ConflictStrategy decides what happens to rows that already exist here.
	ConflictStrategy OrgImportConflict `json:"conflict_strategy,omitempty"`
	// Passphrase unseals the archive's secrets bundle. Omit it and the import
	// still runs; mailboxes and integrations just arrive needing a reconnect.
	Passphrase string `json:"passphrase,omitempty"`
}

// OrgImportPreflight is what the dashboard shows before anything is written:
// what the archive holds, and what will happen when it is applied.
type OrgImportPreflight struct {
	Archive *OrgArchiveInfo `json:"archive"`
	// SecretsUnsealed reports whether the supplied passphrase actually opened
	// the secrets bundle, so the UI can say "12 mailboxes will reconnect
	// automatically" rather than promising it blindly.
	SecretsUnsealed bool `json:"secrets_unsealed"`
	// Conflicts is the per-table count of primary keys already present here.
	Conflicts map[string]int64 `json:"conflicts"`
	// UnknownMembers are archive members with no account on this instance.
	// They are imported as pending invitations rather than as new accounts.
	UnknownMembers []OrgArchiveUser `json:"unknown_members"`
	// SkippedTables are tables in the archive this instance does not have,
	// usually because the archive came from a newer release.
	SkippedTables []string `json:"skipped_tables"`
	Warnings      []string `json:"warnings"`
}

// MaxOrgArchiveUploadBytes caps an uploaded archive. A full workspace with
// inbox history can be large, but not this large, and the cap keeps a hostile
// upload from filling the disk before the manifest is ever read.
const MaxOrgArchiveUploadBytes = 8 << 30 // 8 GiB

// OrgExportRetention is how long a finished archive stays downloadable. It is
// a complete copy of a workspace sitting in blob storage, so it does not live
// forever; the operator can always run another export.
const OrgExportRetention = 7 * 24 * time.Hour

// MinOrgExportPassphraseLength is the floor for a secrets passphrase. The
// passphrase is the only thing standing between the archive file and every
// mailbox credential in the workspace, so it is held to a higher bar than a
// login password.
const MinOrgExportPassphraseLength = 12
