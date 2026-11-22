package models

import (
	"time"

	"github.com/google/uuid"
)

// ApolloList is one saved Apollo list of contacts, as the import picker shows it.
type ApolloList struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// ApolloImportSource is a saved Apollo list that feeds contacts into Warmbly.
type ApolloImportSource struct {
	ID              uuid.UUID        `json:"id"`
	OrganizationID  uuid.UUID        `json:"organization_id"`
	ConnectionID    uuid.UUID        `json:"connection_id"`
	CreatedByUserID *uuid.UUID       `json:"created_by_user_id,omitempty"`
	Name            string           `json:"name"`
	ListID          string           `json:"list_id"`
	ListName        string           `json:"list_name"`
	CampaignID      *uuid.UUID       `json:"campaign_id,omitempty"`
	CategoryIDs     []uuid.UUID      `json:"category_ids"`
	SegmentIDs      []uuid.UUID      `json:"segment_ids"`
	VerifiedOnly    bool             `json:"verified_only"`
	Recurring       bool             `json:"recurring"`
	Enabled         bool             `json:"enabled"`
	Status          string           `json:"status"`
	LastRunAt       *time.Time       `json:"last_run_at,omitempty"`
	LastResult      *ApolloRunResult `json:"last_result,omitempty"`
	LastError       string           `json:"last_error,omitempty"`
	TotalImported   int              `json:"total_imported"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

// ApolloRunResult is what one Apollo list import run did.
type ApolloRunResult struct {
	Read        int  `json:"read"`
	Imported    int  `json:"imported"`
	Updated     int  `json:"updated"`
	Skipped     int  `json:"skipped"`
	Failed      int  `json:"failed"`
	NoEmail     int  `json:"no_email"`
	Unverified  int  `json:"unverified"`
	Suppressed  int  `json:"suppressed"`
	Truncated   bool `json:"truncated,omitempty"`
	AlreadySeen int  `json:"already_seen"`
	// NextPage is where the next run resumes reading a list too long for one.
	NextPage int `json:"next_page,omitempty"`
}

// ApolloPreviewRow is one sample person of a list preview.
type ApolloPreviewRow struct {
	ApolloID     string `json:"apollo_id"`
	Name         string `json:"name"`
	Email        string `json:"email"`
	EmailStatus  string `json:"email_status"`
	Title        string `json:"title"`
	Company      string `json:"company"`
	AlreadyInWB  bool   `json:"already_in_warmbly"`
	WillBeImport bool   `json:"will_import"`
}

// ApolloPreview is the first people of a list and how many it holds.
type ApolloPreview struct {
	Total  int                `json:"total"`
	Sample []ApolloPreviewRow `json:"sample"`
}

// Apollo enrichment run lifecycle.
const (
	ApolloEnrichmentQueued  = "queued"
	ApolloEnrichmentRunning = "running"
	ApolloEnrichmentDone    = "done"
	ApolloEnrichmentError   = "error"
)

// ApolloEnrichmentRun is one background request to enrich Warmbly contacts
// through Apollo's people match.
type ApolloEnrichmentRun struct {
	ID                uuid.UUID   `json:"id"`
	OrganizationID    uuid.UUID   `json:"organization_id"`
	ConnectionID      uuid.UUID   `json:"connection_id"`
	RequestedByUserID *uuid.UUID  `json:"requested_by_user_id,omitempty"`
	ContactIDs        []uuid.UUID `json:"-"`
	Overwrite         bool        `json:"overwrite"`
	Status            string      `json:"status"`
	Total             int         `json:"total"`
	Processed         int         `json:"processed"`
	Enriched          int         `json:"enriched"`
	NotFound          int         `json:"not_found"`
	Failed            int         `json:"failed"`
	CreditsConsumed   int         `json:"credits_consumed"`
	LastError         string      `json:"last_error,omitempty"`
	StartedAt         *time.Time  `json:"started_at,omitempty"`
	FinishedAt        *time.Time  `json:"finished_at,omitempty"`
	CreatedAt         time.Time   `json:"created_at"`
	UpdatedAt         time.Time   `json:"updated_at"`
}

// ApolloOverview is the Apollo page header: the account a key acts as and
// what the connection has done.
type ApolloOverview struct {
	ConnectionID  uuid.UUID         `json:"connection_id"`
	Label         string            `json:"label"`
	Status        IntegrationStatus `json:"status"`
	Health        string            `json:"health"`
	HealthDetail  string            `json:"health_detail,omitempty"`
	AccountName   string            `json:"account_name,omitempty"`
	AccountEmail  string            `json:"account_email,omitempty"`
	Sources       int               `json:"sources"`
	TotalImported int               `json:"total_imported"`
	LastRunAt     *time.Time        `json:"last_run_at,omitempty"`
}
