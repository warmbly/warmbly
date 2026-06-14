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

	// ActionTypes lists the provider action identifiers that have a real
	// backend handler. The dashboard reads this so the automation builder only
	// offers actions Warmbly can actually execute (a provider with no action
	// types should not surface an automation/subscription builder at all).
	ActionTypes []string `json:"action_types,omitempty"`

	// SupportsPush reports whether this provider can be the target of the
	// synchronous "push selected contacts" action surfaced contextually in the
	// dashboard (Contacts, Deals). True only for CRM providers with an upsert
	// handler.
	SupportsPush bool `json:"supports_push"`

	// Capability is the configurable-action descriptor the dashboard renders the
	// onboarding + field-mapping UI from. Nil for providers with no descriptor.
	Capability *ProviderCapability `json:"capability,omitempty"`

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

	// ConfigCapabilities is the per-connection onboarding/capability snapshot
	// (selected objects, enabled use-cases, picker selections). Non-secret, read
	// before execution. Distinct from the sealed secrets in config_encrypted.
	ConfigCapabilities json.RawMessage `json:"config_capabilities,omitempty"`
	// SyncDirection is the connection's data-flow direction: push | pull | both.
	SyncDirection string `json:"sync_direction"`

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
	IntegrationActionSalesforceUpsert   IntegrationAction = "salesforce.upsert_contact"
	IntegrationActionCloseUpsert        IntegrationAction = "close.upsert_lead"
	IntegrationActionGenericWebhookPing IntegrationAction = "webhook.ping"

	// Native (Warmbly-internal) actions: CRM/contact mutations that need no
	// external connection. Run against the contact resolved from the event data.
	IntegrationActionAddTag        IntegrationAction = "warmbly.add_tag"
	IntegrationActionRemoveTag     IntegrationAction = "warmbly.remove_tag"
	IntegrationActionCreateTask    IntegrationAction = "warmbly.create_task"
	IntegrationActionCreateDeal    IntegrationAction = "warmbly.create_deal"
	IntegrationActionMoveDealStage IntegrationAction = "warmbly.move_deal_stage"
	IntegrationActionUnsubscribe   IntegrationAction = "warmbly.unsubscribe"
	// IntegrationActionLabelEmail applies unibox conversation labels (categories)
	// to the thread the event belongs to. Reply triggers carry the thread_id +
	// mailbox owner; on other triggers (no thread) it is a logged no-op.
	IntegrationActionLabelEmail IntegrationAction = "warmbly.label_email"
	// IntegrationActionRunAutomation launches another automation's flow, passing
	// the current event data through. Bounded by the chain-depth guard so it
	// cannot loop forever or fan out unbounded compute.
	IntegrationActionRunAutomation IntegrationAction = "warmbly.run_automation"
	// IntegrationActionHTTPRequest makes a configurable outbound HTTP call
	// (method/url/headers/query/body, all templated from the event + prior step
	// output) and writes the response back into the event data so downstream
	// nodes can use it (e.g. {{.response.body.id}}) and condition nodes can
	// branch on {{.response.ok}}. SSRF-guarded + bounded retry. This is the
	// generic "send a webhook / call any API" node.
	IntegrationActionHTTPRequest IntegrationAction = "warmbly.http_request"
	// IntegrationActionSetVariables computes one or more named values from Go
	// templates (against the event + prior step output) and writes them back into
	// the event data, so later nodes can reuse a transformed/normalized value
	// without recomputing it. The safe "transform" node — it runs the same
	// sandboxed text/template engine as every other action value (no I/O, no
	// arbitrary code), not a general code runtime.
	IntegrationActionSetVariables IntegrationAction = "warmbly.set_variables"
	// IntegrationActionFireEvent publishes a developer-defined custom event to the
	// realtime gateway (org-scoped). The event name + a fully-custom key/value
	// payload are Go-templated against the event data. Subscribers (an API key
	// with REALTIME_SUBSCRIBE on the org websocket) receive it with no public URL,
	// so it replaces an outbound webhook for "tell my system this happened".
	IntegrationActionFireEvent IntegrationAction = "warmbly.fire_event"
)

// IsNativeAction reports whether an action is a Warmbly-internal CRM/contact
// mutation (no external connection required).
func IsNativeAction(a IntegrationAction) bool {
	switch a {
	case IntegrationActionAddTag, IntegrationActionRemoveTag, IntegrationActionCreateTask,
		IntegrationActionCreateDeal, IntegrationActionMoveDealStage, IntegrationActionUnsubscribe,
		IntegrationActionRunAutomation, IntegrationActionLabelEmail, IntegrationActionHTTPRequest,
		IntegrationActionSetVariables, IntegrationActionFireEvent:
		return true
	default:
		return false
	}
}

// IntegrationEventSubscription routes a Warmbly event to a provider action.
type IntegrationEventSubscription struct {
	ID             uuid.UUID         `json:"id"`
	ConnectionID   uuid.UUID         `json:"connection_id"`
	OrganizationID uuid.UUID         `json:"organization_id"`
	EventType      string            `json:"event_type"`
	Action         IntegrationAction `json:"action"`
	Config         json.RawMessage   `json:"config"`
	Enabled        bool              `json:"enabled"`
	// UseCase is a discriminator describing what this automation is for (e.g.
	// "crm_sync", "notify", "custom"). Drives projection + which handler runs.
	UseCase string `json:"use_case"`
	// AutomationID groups this subscription as one step of an Automation (the
	// visual flow builder). Nil = a legacy/standalone subscription.
	AutomationID *uuid.UUID `json:"automation_id,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// Automation is a branching flow: "when <trigger_event> fires, walk this graph"
// — the data behind the visual flow builder. The graph holds a trigger node,
// condition (IF) nodes, and action nodes connected by edges; the executor walks
// it on each matching event, evaluating conditions and running the action nodes
// on the matched paths (reusing the event-subscription action handlers).
type Automation struct {
	ID             uuid.UUID `json:"id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	Name           string    `json:"name"`
	Enabled        bool      `json:"enabled"`
	TriggerEvent   string    `json:"trigger_event"`
	// Filter is an optional automation-wide gate (intents / min_confidence)
	// applied to every action, on top of any condition nodes.
	Filter    json.RawMessage `json:"filter,omitempty"`
	Graph     AutomationGraph `json:"graph"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
	// InboundToken is the per-automation secret embedded in the inbound-webhook
	// URL, set only when TriggerEvent is inbound.webhook. Server-only; the URL
	// that carries it is surfaced to clients via InboundURL instead.
	InboundToken string `json:"-"`
	// InboundURL is the public POST path that fires this automation when its
	// trigger is the inbound webhook. Computed from InboundToken, never stored.
	InboundURL string `json:"inbound_url,omitempty"`
}

// AutomationGraph is the editable flow: nodes + the edges connecting them.
type AutomationGraph struct {
	Nodes []AutomationNode `json:"nodes"`
	Edges []AutomationEdge `json:"edges"`
}

// AutomationNode is one node on the canvas. Type is "trigger" (exactly one, id
// "trigger"), "condition" (an IF with true/false outgoing edges), or "action".
type AutomationNode struct {
	ID           string               `json:"id"`
	Type         string               `json:"type"`
	Action       IntegrationAction    `json:"action,omitempty"`        // action nodes
	ConnectionID *uuid.UUID           `json:"connection_id,omitempty"` // action nodes
	Config       json.RawMessage      `json:"config,omitempty"`        // action node config
	Condition    *AutomationCondition `json:"condition,omitempty"`     // condition nodes
	X            float64              `json:"x"`
	Y            float64              `json:"y"`
}

// AutomationEdge connects two nodes. When is "" for plain edges (from the
// trigger or after an action) and "true"/"false" for the two outgoing edges of
// a condition node.
type AutomationEdge struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
	When   string `json:"when,omitempty"`
}

// AutomationCondition is an IF test evaluated against the trigger event's data.
// For the generic "field" type, Key names the event-data key to test. For the
// "expression" type, Expression is a Go-template predicate (truthy when it
// renders a non-empty, non-false value) evaluated against the native event data,
// giving full string/number/boolean logic.
type AutomationCondition struct {
	Field      string `json:"field"`
	Key        string `json:"key,omitempty"`
	Operator   string `json:"operator"`
	Value      any    `json:"value,omitempty"`
	Expression string `json:"expression,omitempty"`
}

// AutomationWrite is the create/update payload from the flow builder.
type AutomationWrite struct {
	Name         string          `json:"name"`
	Enabled      bool            `json:"enabled"`
	TriggerEvent string          `json:"trigger_event"`
	Filter       json.RawMessage `json:"filter,omitempty"`
	Graph        AutomationGraph `json:"graph"`
}

// AutomationRun is one execution of an automation graph (per fired event or
// manual launch), with per-node outcomes for the builder's history panel.
type AutomationRun struct {
	ID             uuid.UUID              `json:"id"`
	AutomationID   uuid.UUID              `json:"automation_id"`
	OrganizationID uuid.UUID              `json:"organization_id"`
	TriggerEvent   string                 `json:"trigger_event"`
	Status         string                 `json:"status"` // running | success | error
	NodeResults    []AutomationNodeResult `json:"node_results"`
	ErrorDetail    string                 `json:"error_detail,omitempty"`
	StartedAt      time.Time              `json:"started_at"`
	FinishedAt     *time.Time             `json:"finished_at,omitempty"`
}

// AutomationNodeResult is one node's outcome in a run (or a dry-run trace).
type AutomationNodeResult struct {
	NodeID  string         `json:"node_id"`
	Type    string         `json:"type"`             // trigger | condition | action
	Action  string         `json:"action,omitempty"` // action nodes
	Label   string         `json:"label,omitempty"`  // human summary (e.g. "Slack · #sales")
	Status  string         `json:"status"`           // success | error | skipped | branch_true | branch_false
	Error   string         `json:"error,omitempty"`
	Preview map[string]any `json:"preview,omitempty"` // dry-run: what the action would send
}

// DryRunRequest tests an automation without side effects. Data is the sample
// event payload; when empty the server builds a sample from the trigger.
// SkipNodeIDs are action nodes the caller toggled off for this test: they are
// recorded as "skipped" in the trace and never previewed.
type DryRunRequest struct {
	Data        map[string]any `json:"data,omitempty"`
	SkipNodeIDs []string       `json:"skip_node_ids,omitempty"`
}

// DryRunResponse is the trace of a dry run.
type DryRunResponse struct {
	Trace []AutomationNodeResult `json:"trace"`
	Data  map[string]any         `json:"data"`
}

// IntegrationFieldMapping is one Warmbly-field -> provider-field mapping row.
// SubscriptionID scopes a mapping to a single automation; when nil the mapping
// is a connection default applied to every automation for that object/direction.
type IntegrationFieldMapping struct {
	ID             uuid.UUID  `json:"id"`
	ConnectionID   uuid.UUID  `json:"connection_id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	SubscriptionID *uuid.UUID `json:"subscription_id,omitempty"`
	Direction      string     `json:"direction"`
	ObjectName     string     `json:"object_name"`
	WarmblyField   string     `json:"warmbly_field"`
	ExternalField  string     `json:"external_field"`
	Transform      string     `json:"transform"`
	StaticValue    string     `json:"static_value"`
	IsDefault      bool       `json:"is_default"`
	CreatedAt      time.Time  `json:"created_at"`
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

// MeetingBookingStatus is the lifecycle state of a booked meeting.
type MeetingBookingStatus string

const (
	MeetingBooked      MeetingBookingStatus = "booked"
	MeetingRescheduled MeetingBookingStatus = "rescheduled"
	MeetingCanceled    MeetingBookingStatus = "canceled"
	MeetingCompleted   MeetingBookingStatus = "completed"
	MeetingNoShow      MeetingBookingStatus = "no_show"
)

// MeetingBooking represents one booked meeting from Calendly/Cal.com, tracked
// through its full lifecycle (booked -> rescheduled / canceled).
type MeetingBooking struct {
	ID              uuid.UUID            `json:"id"`
	OrganizationID  uuid.UUID            `json:"organization_id"`
	Source          string               `json:"source"`
	ExternalEventID string               `json:"external_event_id"`
	Status          MeetingBookingStatus `json:"status"`
	InviteeEmail    string               `json:"invitee_email"`
	InviteeName     string               `json:"invitee_name"`
	EventName       string               `json:"event_name"`
	EventType       string               `json:"event_type,omitempty"`
	ScheduledFor    *time.Time           `json:"scheduled_for,omitempty"`
	EndTime         *time.Time           `json:"end_time,omitempty"`
	JoinURL         string               `json:"join_url,omitempty"`
	Location        string               `json:"location,omitempty"`
	CancelURL       string               `json:"cancel_url,omitempty"`
	RescheduleURL   string               `json:"reschedule_url,omitempty"`
	CanceledReason  string               `json:"canceled_reason,omitempty"`
	ContactID       *uuid.UUID           `json:"contact_id,omitempty"`
	CampaignID      *uuid.UUID           `json:"campaign_id,omitempty"`
	RawPayload      json.RawMessage      `json:"raw_payload,omitempty"`
	CreatedAt       time.Time            `json:"created_at"`
	UpdatedAt       time.Time            `json:"updated_at"`

	// Joined for list display (not stored on the row).
	ContactName string `json:"contact_name,omitempty"`
}

// MeetingBookingFilter scopes a Meetings-page search.
type MeetingBookingFilter struct {
	// Timeframe: "upcoming" (scheduled_for >= now, not canceled),
	// "past" (scheduled_for < now), or "" for all.
	Timeframe string
	Status    string // exact status filter, or "" for any
	Search    string // matches invitee name/email or event name
	Limit     int
	Offset    int
}

// MeetingBookingSummary powers the sidebar count + page header stats.
type MeetingBookingSummary struct {
	Upcoming int `json:"upcoming"`
	Today    int `json:"today"`
	Total    int `json:"total"`
	Canceled int `json:"canceled"`
}

// MeetingBookingPage is a meetings result. Offset pagination under the hood, but
// it exposes the standard {total, next_cursor, has_more} envelope with an OPAQUE
// cursor like every other list (Total is exact so the UI can show "N of M").
type MeetingBookingPage struct {
	Data       []MeetingBooking `json:"data"`
	Pagination Pagination       `json:"pagination"`
}
