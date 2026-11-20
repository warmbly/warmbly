package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// SalesforceRecordLink ties a Warmbly contact to the Lead or Contact it is in
// one connected Salesforce org.
type SalesforceRecordLink struct {
	ID               uuid.UUID       `json:"id"`
	OrganizationID   uuid.UUID       `json:"organization_id"`
	ConnectionID     uuid.UUID       `json:"connection_id"`
	ContactID        uuid.UUID       `json:"contact_id"`
	SObject          string          `json:"object"`
	RecordID         string          `json:"record_id"`
	AccountID        string          `json:"account_id,omitempty"`
	AccountName      string          `json:"account_name,omitempty"`
	OwnerID          string          `json:"owner_id,omitempty"`
	OwnerName        string          `json:"owner_name,omitempty"`
	LeadStatus       string          `json:"lead_status,omitempty"`
	IsConverted      bool            `json:"is_converted"`
	OptedOut         bool            `json:"opted_out"`
	Snapshot         json.RawMessage `json:"snapshot,omitempty"`
	LinkedBy         string          `json:"linked_by"`
	RecordModifiedAt *time.Time      `json:"record_modified_at,omitempty"`
	LastPushedAt     *time.Time      `json:"last_pushed_at,omitempty"`
	LastPulledAt     *time.Time      `json:"last_pulled_at,omitempty"`
	LastError        *string         `json:"last_error,omitempty"`
	LastErrorAt      *time.Time      `json:"last_error_at,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

// SalesforceActivity is one queued or processed activity: a campaign event to
// log as a Task, and what became of it.
type SalesforceActivity struct {
	ID             uuid.UUID      `json:"id"`
	OrganizationID uuid.UUID      `json:"organization_id"`
	ConnectionID   uuid.UUID      `json:"connection_id"`
	ContactID      *uuid.UUID     `json:"contact_id,omitempty"`
	ContactEmail   string         `json:"contact_email"`
	Kind           string         `json:"kind"`
	DedupeKey      string         `json:"-"`
	Payload        map[string]any `json:"payload,omitempty"`
	// ContentEncrypted is subject and body text sealed with the org DEK.
	ContentEncrypted string     `json:"-"`
	Status           string     `json:"status"`
	Attempts         int        `json:"attempts"`
	LeaseID          *uuid.UUID `json:"-"`
	NextAttemptAt    time.Time  `json:"next_attempt_at"`
	RecordID         string     `json:"record_id,omitempty"`
	TaskID           string     `json:"task_id,omitempty"`
	Detail           string     `json:"detail,omitempty"`
	OccurredAt       time.Time  `json:"occurred_at"`
	CreatedAt        time.Time  `json:"created_at"`
	ProcessedAt      *time.Time `json:"processed_at,omitempty"`
}

// Activity statuses; mirror the queue's CHECK.
const (
	SalesforceActivityPending = "pending"
	SalesforceActivitySynced  = "synced"
	SalesforceActivitySkipped = "skipped"
	SalesforceActivityFailed  = "failed"
)

// SalesforceActivityPage is a page of the activity log.
type SalesforceActivityPage struct {
	Data       []SalesforceActivity `json:"data"`
	Pagination Pagination           `json:"pagination"`
}

// SalesforceImportSource is a saved list view or Salesforce Campaign that feeds
// contacts into Warmbly.
type SalesforceImportSource struct {
	ID              uuid.UUID            `json:"id"`
	OrganizationID  uuid.UUID            `json:"organization_id"`
	ConnectionID    uuid.UUID            `json:"connection_id"`
	CreatedByUserID *uuid.UUID           `json:"created_by_user_id,omitempty"`
	Name            string               `json:"name"`
	SourceKind      string               `json:"source_kind"`
	SObject         string               `json:"object"`
	SourceID        string               `json:"source_id"`
	SourceLabel     string               `json:"source_label"`
	CampaignID      *uuid.UUID           `json:"campaign_id,omitempty"`
	CategoryIDs     []uuid.UUID          `json:"category_ids"`
	SegmentIDs      []uuid.UUID          `json:"segment_ids"`
	Recurring       bool                 `json:"recurring"`
	Enabled         bool                 `json:"enabled"`
	Status          string               `json:"status"`
	LastRunAt       *time.Time           `json:"last_run_at,omitempty"`
	LastResult      *SalesforceRunResult `json:"last_result,omitempty"`
	LastError       string               `json:"last_error,omitempty"`
	TotalImported   int                  `json:"total_imported"`
	CreatedAt       time.Time            `json:"created_at"`
	UpdatedAt       time.Time            `json:"updated_at"`
}

// SalesforceRunResult is what one import run did.
type SalesforceRunResult struct {
	Read     int `json:"read"`
	Imported int `json:"imported"`
	Updated  int `json:"updated"`
	Linked   int `json:"linked"`
	Skipped  int `json:"skipped"`
	Failed   int `json:"failed"`
	// NoEmail counts records with no address, which cannot be contacts.
	NoEmail int `json:"no_email"`
	// OptedOut counts people with Email Opt Out set, suppressed rather than
	// imported.
	OptedOut int `json:"opted_out"`
	// Truncated is set when the source held more than one run reads.
	Truncated bool `json:"truncated,omitempty"`
}

// SalesforceSyncState is where the pull loop got to and the budget it saw.
type SalesforceSyncState struct {
	ConnectionID   uuid.UUID  `json:"connection_id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	LeadCursor     *time.Time `json:"lead_cursor,omitempty"`
	ContactCursor  *time.Time `json:"contact_cursor,omitempty"`
	OppCursor      *time.Time `json:"opportunity_cursor,omitempty"`
	LastPullAt     *time.Time `json:"last_pull_at,omitempty"`
	LastPullError  string     `json:"last_pull_error,omitempty"`
	APIUsed        int        `json:"api_used"`
	APIMax         int        `json:"api_max"`
	APISeenAt      *time.Time `json:"api_seen_at,omitempty"`
	CallsToday     int        `json:"calls_today"`
}

// SalesforceActivityCounts summarises the outbox for the overview.
type SalesforceActivityCounts struct {
	Pending     int `json:"pending"`
	Synced24h   int `json:"synced_24h"`
	Failed      int `json:"failed"`
	Skipped24h  int `json:"skipped_24h"`
	LinkedTotal int `json:"linked_records"`
}
