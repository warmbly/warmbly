package models

import (
	"time"

	"github.com/google/uuid"

	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

// ToolMigrationStatus is where a migration from another outreach tool stands.
type ToolMigrationStatus string

const (
	// ToolMigrationScanning reads the source account to show what it holds.
	ToolMigrationScanning ToolMigrationStatus = "scanning"
	// ToolMigrationReady has a preview and waits for the person to choose.
	ToolMigrationReady ToolMigrationStatus = "ready"
	// ToolMigrationRunning is bringing data over.
	ToolMigrationRunning  ToolMigrationStatus = "running"
	ToolMigrationDone     ToolMigrationStatus = "completed"
	ToolMigrationFailed   ToolMigrationStatus = "failed"
	ToolMigrationCanceled ToolMigrationStatus = "canceled"
)

// Active reports whether the migration still has work in flight.
func (s ToolMigrationStatus) Active() bool {
	return s == ToolMigrationScanning || s == ToolMigrationRunning
}

// ToolMigrationInFlight is what happens to a lead the old tool was part way
// through a sequence with.
type ToolMigrationInFlight string

const (
	// ToolMigrationContinue is refused: picking a lead up mid-sequence would
	// need Warmbly to record sends it never made.
	ToolMigrationContinue ToolMigrationInFlight = "continue"
	// ToolMigrationRestart enrolls the lead from the first step.
	ToolMigrationRestart ToolMigrationInFlight = "restart"
	// ToolMigrationSkip imports the contact without enrolling it.
	ToolMigrationSkip ToolMigrationInFlight = "skip"
)

// ToolMigration is one import from another outreach tool. A workspace's own
// migration has OrganizationID set; an agency's has AgencyID set and writes
// into the client workspaces its targets name.
type ToolMigration struct {
	ID              uuid.UUID           `json:"id"`
	OrganizationID  *uuid.UUID          `json:"organization_id,omitempty"`
	AgencyID        *uuid.UUID          `json:"agency_id,omitempty"`
	CreatedByUserID *uuid.UUID          `json:"created_by_user_id,omitempty"`
	Provider        source.ProviderID   `json:"provider"`
	Label           string              `json:"label"`
	Status          ToolMigrationStatus `json:"status"`
	// Accounts are who each pasted key acts as, in the order they were added.
	Accounts []source.Account `json:"accounts"`
	// Inventory is what the scan found, one entry per source workspace.
	Inventory []ToolMigrationWorkspace `json:"inventory"`
	Options   *ToolMigrationOptions    `json:"options,omitempty"`
	Targets   []ToolMigrationTarget    `json:"targets"`
	Error     string                   `json:"error,omitempty"`
	// CredentialsHeld is false once the keys have been wiped, which happens
	// when the migration finishes, is canceled or is dismissed.
	CredentialsHeld bool       `json:"credentials_held"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// ToolMigrationWorkspace is one source workspace and what it holds.
type ToolMigrationWorkspace struct {
	Workspace source.Workspace `json:"workspace"`
	Inventory source.Inventory `json:"inventory"`
	Error     string           `json:"error,omitempty"`
}

// ToolMigrationOptions are the choices made before a migration starts.
type ToolMigrationOptions struct {
	Kinds []source.Kind `json:"kinds"`
	// CampaignIDs limits campaigns (and their leads) to these source ids;
	// empty brings every campaign.
	CampaignIDs []string              `json:"campaign_ids,omitempty"`
	InFlight    ToolMigrationInFlight `json:"in_flight"`
	// SkipFinished leaves out campaigns the source marks completed or archived.
	SkipFinished bool `json:"skip_finished"`
	// Warmup starts warmup on mailboxes that connect, unless the source said off.
	Warmup bool `json:"warmup"`
}

// ToolMigrationTargetStatus is one target's progress.
type ToolMigrationTargetStatus string

const (
	ToolMigrationTargetPending  ToolMigrationTargetStatus = "pending"
	ToolMigrationTargetRunning  ToolMigrationTargetStatus = "running"
	ToolMigrationTargetDone     ToolMigrationTargetStatus = "completed"
	ToolMigrationTargetFailed   ToolMigrationTargetStatus = "failed"
	ToolMigrationTargetCanceled ToolMigrationTargetStatus = "canceled"
)

// ToolMigrationTarget is one source workspace brought into one Warmbly workspace.
type ToolMigrationTarget struct {
	ID             uuid.UUID                 `json:"id"`
	MigrationID    uuid.UUID                 `json:"migration_id"`
	Workspace      source.Workspace          `json:"workspace"`
	OrganizationID uuid.UUID                 `json:"organization_id"`
	OrgName        string                    `json:"organization_name,omitempty"`
	Status         ToolMigrationTargetStatus `json:"status"`
	// Phase is the kind being imported now, empty before the first.
	Phase    source.Kind           `json:"phase,omitempty"`
	Counts   ToolMigrationCounts   `json:"counts"`
	Followup ToolMigrationFollowup `json:"followup"`
	Error    string                `json:"error,omitempty"`
	// MailboxImportID is the mailbox import that connects mailboxes the
	// source returned passwords for.
	MailboxImportID *uuid.UUID `json:"mailbox_import_id,omitempty"`
	// CategoryID is the contact category every imported contact is filed under.
	CategoryID *uuid.UUID `json:"category_id,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// ToolMigrationCounts is what one target brought over.
type ToolMigrationCounts struct {
	BlocklistImported int `json:"blocklist_imported"`
	MailboxesQueued   int `json:"mailboxes_queued"`
	MailboxesManual   int `json:"mailboxes_manual"`
	CampaignsCreated  int `json:"campaigns_created"`
	CampaignsSkipped  int `json:"campaigns_skipped"`
	StepsCreated      int `json:"steps_created"`
	LeadsRead         int `json:"leads_read"`
	ContactsImported  int `json:"contacts_imported"`
	ContactsUpdated   int `json:"contacts_updated"`
	LeadsEnrolled     int `json:"leads_enrolled"`
	LeadsNotEnrolled  int `json:"leads_not_enrolled"`
	LeadsSuppressed   int `json:"leads_suppressed"`
	LeadsFailed       int `json:"leads_failed"`
}

// ToolMigrationFollowup is what a person still has to do after a target ran.
type ToolMigrationFollowup struct {
	// Reconnect are mailboxes that sign in with Google or Microsoft, or whose
	// password the source does not return, and need connecting by their owner.
	Reconnect []ToolMigrationMailbox `json:"reconnect,omitempty"`
	// Notes are things that did not carry over, such as LinkedIn steps.
	Notes []string `json:"notes,omitempty"`
	// Campaigns are the draft campaigns created, for review before launch.
	Campaigns []ToolMigrationCampaign `json:"campaigns,omitempty"`
}

// ToolMigrationMailbox is a mailbox to reconnect.
type ToolMigrationMailbox struct {
	Email string             `json:"email"`
	Kind  source.MailboxKind `json:"kind"`
}

// ToolMigrationCampaign is one draft campaign a migration created.
type ToolMigrationCampaign struct {
	SourceID   string    `json:"source_id"`
	CampaignID uuid.UUID `json:"campaign_id"`
	Name       string    `json:"name"`
	// MissingSenders are source sender addresses with no mailbox in the
	// workspace yet; linking them is offered once they are connected.
	MissingSenders []string `json:"missing_senders,omitempty"`
}

// NewToolMigration starts a migration: it checks every key and scans.
type NewToolMigration struct {
	Provider source.ProviderID `json:"provider"`
	Label    string            `json:"label,omitempty"`
	// Credentials holds one set per key; most tools need one, an agency on a
	// tool with a key per workspace adds one for each.
	Credentials []source.Credentials `json:"credentials"`
}

// StartToolMigration is the choice of what to bring over, and where.
type StartToolMigration struct {
	Options ToolMigrationOptions `json:"options"`
	// Targets maps source workspaces onto Warmbly workspaces. A workspace's
	// own migration sends none: every source workspace lands in it. An
	// agency names each, either an existing client or a new one to create.
	Targets []ToolMigrationTargetChoice `json:"targets,omitempty"`
}

// ToolMigrationTargetChoice is where one source workspace goes.
type ToolMigrationTargetChoice struct {
	WorkspaceID string `json:"workspace_id"`
	Key         int    `json:"key"`
	// OrganizationID is an existing client workspace.
	OrganizationID *uuid.UUID `json:"organization_id,omitempty"`
	// CreateName creates a new client workspace with this name.
	CreateName string `json:"create_name,omitempty"`
	// Skip leaves this source workspace out.
	Skip bool `json:"skip,omitempty"`
}

// ToolMigrationList is one page of migrations.
type ToolMigrationList struct {
	Data       []ToolMigration `json:"data"`
	Pagination Pagination      `json:"pagination"`
}
