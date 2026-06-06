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

// IntegrationAuthMethod describes how a connection is authenticated.
type IntegrationAuthMethod string

const (
	// IntegrationAuthOAuth is a real OAuth 2.0 authorization-code handshake.
	// The user clicks "Connect", authorizes in the provider's popup, and we
	// store an encrypted access/refresh token pair — no pasting credentials.
	IntegrationAuthOAuth IntegrationAuthMethod = "oauth"
	// IntegrationAuthAPIKey is a provider-issued API token the user pastes
	// (used only where the provider offers no OAuth app, e.g. Close).
	IntegrationAuthAPIKey IntegrationAuthMethod = "api_key"
	// IntegrationAuthWebhook is an inbound URL Warmbly mints (Calendly, Cal.com)
	// or an outbound URL the user pastes (Discord).
	IntegrationAuthWebhook IntegrationAuthMethod = "webhook"
)

// IntegrationStatus is the lifecycle state of a connection.
type IntegrationStatus string

const (
	// IntegrationStatusPending — row created, not yet usable.
	IntegrationStatusPending IntegrationStatus = "pending"
	// IntegrationStatusAuthorizing — OAuth handshake is mid-flight.
	IntegrationStatusAuthorizing IntegrationStatus = "authorizing"
	// IntegrationStatusConnected — healthy, last interaction succeeded.
	IntegrationStatusConnected IntegrationStatus = "connected"
	// IntegrationStatusDegraded — connected but the last call errored.
	IntegrationStatusDegraded IntegrationStatus = "degraded"
	// IntegrationStatusReauthRequired — token revoked/expired; user must reconnect.
	IntegrationStatusReauthRequired IntegrationStatus = "reauth_required"
	// IntegrationStatusDisconnected — intentionally removed.
	IntegrationStatusDisconnected IntegrationStatus = "disconnected"
)

// IntegrationHealth is the rolling health signal surfaced on the connection
// card, independent of the lifecycle status (a connection can be 'connected'
// but 'degraded' health after a transient provider error).
type IntegrationHealth string

const (
	IntegrationHealthUnknown  IntegrationHealth = "unknown"
	IntegrationHealthHealthy  IntegrationHealth = "healthy"
	IntegrationHealthDegraded IntegrationHealth = "degraded"
	IntegrationHealthDown     IntegrationHealth = "down"
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
	Provider   IntegrationProvider `json:"provider"`
	Name       string              `json:"name"`
	Tagline    string              `json:"tagline"`
	Category   IntegrationCategory `json:"category"`
	DocsURL    string              `json:"docs_url,omitempty"`
	AuthMethod string              `json:"auth_method"` // 'oauth' | 'api_key' | 'webhook'
	BadgeColor string              `json:"badge_color,omitempty"`
	BetaFlag   bool                `json:"beta"`

	// WebhookHint is shown to webhook-URL providers (Discord) and inbound
	// providers (Calendly, Cal.com).
	WebhookHint string `json:"webhook_hint,omitempty"`

	// Highlights are short "what you get" bullets shown on the provider's
	// detail screen during onboarding.
	Highlights []string `json:"highlights,omitempty"`

	// Scopes is the set of OAuth scopes requested at authorize time. Empty
	// for non-OAuth providers. Surfaced so the consent screen is honest.
	Scopes []string `json:"scopes,omitempty"`

	// Events lists the Warmbly events this provider can react to (so the UI
	// can offer "notify on positive reply", etc).
	Events []string `json:"events,omitempty"`

	// Configured reports whether the server has OAuth client credentials wired
	// for this provider. OAuth providers without credentials render as
	// "coming soon" instead of a dead Connect button.
	Configured bool `json:"configured"`
}

// IntegrationConnection is one org's link to one provider. Secrets
// (access/refresh tokens, pasted API keys) are never serialized here — only
// the encrypted columns in the DB hold them.
type IntegrationConnection struct {
	ID             uuid.UUID           `json:"id"`
	OrganizationID uuid.UUID           `json:"organization_id"`
	Provider       IntegrationProvider `json:"provider"`
	Label          string              `json:"label"`
	Status         IntegrationStatus   `json:"status"`
	AuthMethod     string              `json:"auth_method"`
	DisplayFields  json.RawMessage     `json:"display_fields"`

	ConnectedByUserID   *uuid.UUID `json:"connected_by_user_id,omitempty"`
	ExternalAccountID   string     `json:"external_account_id,omitempty"`
	ExternalAccountName string     `json:"external_account_name,omitempty"`
	GrantedScopes       []string   `json:"granted_scopes,omitempty"`
	TokenExpiresAt      *time.Time `json:"token_expires_at,omitempty"`

	Health          string     `json:"health"`
	HealthDetail    *string    `json:"health_detail,omitempty"`
	HealthCheckedAt *time.Time `json:"health_checked_at,omitempty"`

	LastSyncedAt *time.Time `json:"last_synced_at,omitempty"`
	LastError    *string    `json:"last_error,omitempty"`
	LastErrorAt  *time.Time `json:"last_error_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`

	// Returned only at create time for providers that POST inbound
	// (Calendly, Cal.com). The dashboard surfaces the resulting URL once.
	InboundWebhookURL string `json:"inbound_webhook_url,omitempty"`
}

// IntegrationTokens carries the freshly-exchanged OAuth material an
// implementation persists. Plaintext lives only in memory.
type IntegrationTokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    *time.Time
	Scopes       []string
}

// IntegrationOAuthState is the short-lived CSRF/PKCE record minted at the
// start of an OAuth handshake and consumed on callback.
type IntegrationOAuthState struct {
	ID              uuid.UUID
	OrganizationID  uuid.UUID
	UserID          uuid.UUID
	Provider        IntegrationProvider
	State           string
	CodeVerifier    string
	Label           string
	RequestedScopes []string
	UsedAt          *time.Time
	ExpiresAt       time.Time
	CreatedAt       time.Time
}

// IntegrationOAuthStartResponse is returned to the SPA so it can open the
// provider authorization popup.
type IntegrationOAuthStartResponse struct {
	URL   string `json:"url"`
	State string `json:"state"`
}

// IntegrationAction is a provider-specific side effect fired by an event
// subscription.
type IntegrationAction string

const (
	IntegrationActionSlackNotify        IntegrationAction = "slack.notify"
	IntegrationActionDiscordNotify      IntegrationAction = "discord.notify"
	IntegrationActionHubSpotUpsert      IntegrationAction = "hubspot.upsert_contact"
	IntegrationActionPipedriveUpsert    IntegrationAction = "pipedrive.upsert_person"
	IntegrationActionGenericWebhookPing IntegrationAction = "webhook.ping"
)

// IntegrationEventSubscription routes a Warmbly event to a provider action.
type IntegrationEventSubscription struct {
	ID             uuid.UUID         `json:"id"`
	ConnectionID   uuid.UUID         `json:"connection_id"`
	OrganizationID uuid.UUID         `json:"organization_id"`
	EventType      string            `json:"event_type"`
	Action         IntegrationAction `json:"action"`
	Config         json.RawMessage   `json:"config"`
	Enabled        bool              `json:"enabled"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// IntegrationSyncRun is one observability record of work done against a
// connection (connect, token refresh, event dispatch, manual sync).
type IntegrationSyncRun struct {
	ID               uuid.UUID  `json:"id"`
	ConnectionID     uuid.UUID  `json:"connection_id"`
	OrganizationID   uuid.UUID  `json:"organization_id"`
	Kind             string     `json:"kind"`
	Status           string     `json:"status"`
	Detail           string     `json:"detail"`
	RecordsProcessed int        `json:"records_processed"`
	StartedAt        time.Time  `json:"started_at"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
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
