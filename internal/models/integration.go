package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// IntegrationProvider identifies one third-party system Warmbly can connect
// to. Adding a new provider here is enough to make it visible in the
// dashboard's catalog. The actual connect/disconnect logic is handled in
// the integration service's per-provider switch.
type IntegrationProvider string

const (
	// CRM
	IntegrationHubSpot    IntegrationProvider = "hubspot"
	IntegrationSalesforce IntegrationProvider = "salesforce"
	IntegrationPipedrive  IntegrationProvider = "pipedrive"
	IntegrationClose      IntegrationProvider = "close"

	// Automation
	IntegrationZapier IntegrationProvider = "zapier"
	IntegrationMake   IntegrationProvider = "make"
	IntegrationN8N    IntegrationProvider = "n8n"

	// Notifications
	IntegrationSlack   IntegrationProvider = "slack"
	IntegrationDiscord IntegrationProvider = "discord"

	// Meetings
	IntegrationCalendly IntegrationProvider = "calendly"
	IntegrationCalCom   IntegrationProvider = "cal_com"

	// Data
	IntegrationGoogleSheets IntegrationProvider = "google_sheets"
)

// AllIntegrationProviders lists every provider the dashboard exposes. The
// order here is the catalog order users see.
var AllIntegrationProviders = []IntegrationProvider{
	IntegrationHubSpot,
	IntegrationSalesforce,
	IntegrationPipedrive,
	IntegrationClose,
	IntegrationZapier,
	IntegrationMake,
	IntegrationN8N,
	IntegrationSlack,
	IntegrationDiscord,
	IntegrationCalendly,
	IntegrationCalCom,
	IntegrationGoogleSheets,
}

func IsValidIntegrationProvider(s string) bool {
	for _, p := range AllIntegrationProviders {
		if string(p) == s {
			return true
		}
	}
	return false
}

// IntegrationStatus is the operational health of a connection.
type IntegrationStatus string

const (
	IntegrationStatusPending      IntegrationStatus = "pending"
	IntegrationStatusConnected    IntegrationStatus = "connected"
	IntegrationStatusDegraded     IntegrationStatus = "degraded"
	IntegrationStatusDisconnected IntegrationStatus = "disconnected"
)

// IntegrationCategory groups providers in the dashboard.
type IntegrationCategory string

const (
	IntegrationCategoryCRM           IntegrationCategory = "crm"
	IntegrationCategoryAutomation    IntegrationCategory = "automation"
	IntegrationCategoryNotifications IntegrationCategory = "notifications"
	IntegrationCategoryMeetings      IntegrationCategory = "meetings"
	IntegrationCategoryData          IntegrationCategory = "data"
)

// IntegrationCatalogEntry is the static metadata for one provider that the
// dashboard renders even when no connection exists yet.
type IntegrationCatalogEntry struct {
	Provider    IntegrationProvider `json:"provider"`
	Name        string              `json:"name"`
	Tagline     string              `json:"tagline"`
	Category    IntegrationCategory `json:"category"`
	DocsURL     string              `json:"docs_url,omitempty"`
	AuthMethod  string              `json:"auth_method"` // 'oauth' | 'api_key' | 'webhook'
	BadgeColor  string              `json:"badge_color,omitempty"`
	BetaFlag    bool                `json:"beta"`
	WebhookHint string              `json:"webhook_hint,omitempty"`
}

// IntegrationConnection is one org's link to one provider.
type IntegrationConnection struct {
	ID             uuid.UUID           `json:"id"`
	OrganizationID uuid.UUID           `json:"organization_id"`
	Provider       IntegrationProvider `json:"provider"`
	Label          string              `json:"label"`
	Status         IntegrationStatus   `json:"status"`
	DisplayFields  json.RawMessage     `json:"display_fields"`
	LastSyncedAt   *time.Time          `json:"last_synced_at,omitempty"`
	LastError      *string             `json:"last_error,omitempty"`
	LastErrorAt    *time.Time          `json:"last_error_at,omitempty"`
	CreatedAt      time.Time           `json:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at"`

	// Returned only at create time for providers that POST inbound
	// (Calendly, Cal.com). The dashboard surfaces the resulting URL once.
	InboundWebhookURL string `json:"inbound_webhook_url,omitempty"`
}

// MeetingBooking represents one booked meeting from Calendly/Cal.com.
type MeetingBooking struct {
	ID              uuid.UUID       `json:"id"`
	OrganizationID  uuid.UUID       `json:"organization_id"`
	Source          string          `json:"source"`
	ExternalEventID string          `json:"external_event_id"`
	InviteeEmail    string          `json:"invitee_email"`
	InviteeName     string          `json:"invitee_name"`
	EventName       string          `json:"event_name"`
	ScheduledFor    *time.Time      `json:"scheduled_for,omitempty"`
	ContactID       *uuid.UUID      `json:"contact_id,omitempty"`
	CampaignID      *uuid.UUID      `json:"campaign_id,omitempty"`
	RawPayload      json.RawMessage `json:"raw_payload,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}
