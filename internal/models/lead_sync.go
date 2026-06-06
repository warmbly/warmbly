package models

import (
	"time"

	"github.com/google/uuid"
)

// LeadSyncStatus is the lifecycle state of a saved lead-sync source. It is
// not a queue state — syncs are on-demand — only a record of how the last
// "Sync now" went.
type LeadSyncStatus string

const (
	// LeadSyncStatusIdle — never synced, or the last sync succeeded.
	LeadSyncStatusIdle LeadSyncStatus = "idle"
	// LeadSyncStatusSyncing — a sync is in flight (set for the duration of
	// the synchronous SyncNow call).
	LeadSyncStatusSyncing LeadSyncStatus = "syncing"
	// LeadSyncStatusError — the last sync failed; LastError carries why.
	LeadSyncStatusError LeadSyncStatus = "error"
)

// LeadSyncSource is a saved, re-runnable binding between a Google Sheet (read
// through an existing google_sheets OAuth connection) and Warmbly's contact
// importer. New rows create contacts; rows matching an existing contact by
// email are updated. A source optionally enrols new/updated leads into a
// campaign and/or tags them with categories.
//
// Secrets never live here — the sheet is read using the linked connection's
// envelope-encrypted Google token, resolved at sync time.
type LeadSyncSource struct {
	ID              uuid.UUID `json:"id"`
	OrganizationID  uuid.UUID `json:"organization_id"`
	CreatedByUserID uuid.UUID `json:"created_by_user_id"`
	Provider        string    `json:"provider"`
	ConnectionID    uuid.UUID `json:"connection_id"`
	SheetID         string    `json:"sheet_id"`
	SheetTitle      string    `json:"sheet_title,omitempty"`
	TabTitle        string    `json:"tab_title,omitempty"`
	A1Range         string    `json:"a1_range,omitempty"`
	HasHeader       bool      `json:"has_header"`

	// ColumnMapping reuses the exact contact-import mapping shape so the sheet
	// rows flow through the same /contacts/import/commit code path.
	ColumnMapping []ContactImportColumnMapping `json:"column_mapping"`
	Dedup         ContactImportDedupStrategy   `json:"dedup"`

	// TargetCampaignID, when set, enrols every new/updated lead into that
	// campaign on each sync.
	TargetCampaignID  *uuid.UUID `json:"target_campaign_id,omitempty"`
	CategoryIDs       []string   `json:"category_ids"`
	SubscribedDefault bool       `json:"subscribed_default"`

	Label  string         `json:"label,omitempty"`
	Status LeadSyncStatus `json:"status"`

	LastSyncedAt *time.Time           `json:"last_synced_at,omitempty"`
	LastResult   *ContactImportResult `json:"last_result,omitempty"`
	LastError    string               `json:"last_error,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreateLeadSyncSource is the input for creating a saved source. The connection
// must be the org's google_sheets OAuth connection.
type CreateLeadSyncSource struct {
	ConnectionID      uuid.UUID                    `json:"connection_id"`
	SheetID           string                       `json:"sheet_id"`
	SheetTitle        string                       `json:"sheet_title"`
	TabTitle          string                       `json:"tab_title"`
	HasHeader         bool                         `json:"has_header"`
	ColumnMapping     []ContactImportColumnMapping `json:"column_mapping"`
	Dedup             ContactImportDedupStrategy   `json:"dedup"`
	TargetCampaignID  *uuid.UUID                   `json:"target_campaign_id"`
	CategoryIDs       []string                     `json:"category_ids"`
	SubscribedDefault *bool                        `json:"subscribed_default"`
	Label             string                       `json:"label"`
}

// UpdateLeadSyncSource is the input for editing a saved source. All fields are
// optional pointers so a PATCH can touch a single setting; nil leaves the
// stored value untouched.
type UpdateLeadSyncSource struct {
	SheetID           *string                       `json:"sheet_id"`
	SheetTitle        *string                       `json:"sheet_title"`
	TabTitle          *string                       `json:"tab_title"`
	HasHeader         *bool                         `json:"has_header"`
	ColumnMapping     *[]ContactImportColumnMapping `json:"column_mapping"`
	Dedup             *ContactImportDedupStrategy   `json:"dedup"`
	TargetCampaignID  *uuid.UUID                    `json:"target_campaign_id"`
	ClearCampaign     bool                          `json:"clear_campaign"`
	CategoryIDs       *[]string                     `json:"category_ids"`
	SubscribedDefault *bool                         `json:"subscribed_default"`
	Label             *string                       `json:"label"`
}

// LeadSyncResult is what "Sync now" returns: the underlying contact-import
// counts plus the source id so the dashboard can attribute the run.
type LeadSyncResult struct {
	SourceID uuid.UUID            `json:"source_id"`
	Result   *ContactImportResult `json:"result"`
}
