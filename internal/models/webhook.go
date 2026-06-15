package models

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

// WebhookEventType is the canonical event-name identifier carried both in
// the subscription filter and the delivery payload. Keep these stable —
// renaming a value breaks every customer subscribed to it.
type WebhookEventType string

const (
	// Email account lifecycle
	WebhookEventEmailAccountConnected WebhookEventType = "email_account.connected"
	WebhookEventEmailAccountRemoved   WebhookEventType = "email_account.removed"

	// Campaign send pipeline
	WebhookEventCampaignEmailSent      WebhookEventType = "campaign.email_sent"
	WebhookEventCampaignEmailDelivered WebhookEventType = "campaign.email_delivered"
	WebhookEventCampaignEmailOpened    WebhookEventType = "campaign.email_opened"
	WebhookEventCampaignEmailClicked   WebhookEventType = "campaign.email_clicked"
	WebhookEventCampaignEmailBounced   WebhookEventType = "campaign.email_bounced"
	WebhookEventCampaignReplyReceived  WebhookEventType = "campaign.reply_received"
	WebhookEventCampaignUnsubscribed   WebhookEventType = "campaign.unsubscribed"
	WebhookEventCampaignStarted        WebhookEventType = "campaign.started"
	WebhookEventCampaignPaused         WebhookEventType = "campaign.paused"
	WebhookEventCampaignCompleted      WebhookEventType = "campaign.completed"
	// campaign.deliverability_warning fires when a campaign's rolling
	// bounce/complaint rate enters the early-warning band (half the pause
	// threshold) — a graduated signal short of an auto-pause.
	WebhookEventCampaignDeliverabilityWarning WebhookEventType = "campaign.deliverability_warning"
	// campaign.action fires from a "notify" action node in a sequence flow.
	WebhookEventCampaignAction WebhookEventType = "campaign.action"

	// Warmup
	WebhookEventWarmupEmailSent       WebhookEventType = "warmup.email_sent"
	WebhookEventWarmupHealthChanged   WebhookEventType = "warmup.health_changed"
	WebhookEventWarmupPlacementInSpam WebhookEventType = "warmup.placement_in_spam"
	WebhookEventWarmupQuarantined     WebhookEventType = "warmup.quarantined"
	WebhookEventWarmupBlocked         WebhookEventType = "warmup.blocked"

	// Deliverability
	WebhookEventDeliverabilityBounce    WebhookEventType = "deliverability.bounce"
	WebhookEventDeliverabilityComplaint WebhookEventType = "deliverability.complaint"

	// Meetings (inbound from Calendly / Cal.com). A booked call is one of the
	// most actionable signals an outbound team gets, so it is a first-class
	// event other connections can subscribe to (Slack ping, CRM upsert).
	WebhookEventMeetingBooked      WebhookEventType = "meeting.booked"
	WebhookEventMeetingRescheduled WebhookEventType = "meeting.rescheduled"
	WebhookEventMeetingCanceled    WebhookEventType = "meeting.canceled"

	// Inbound webhook: a trigger only (never emitted outbound). An external
	// system POSTs JSON to a per-automation URL, running that one automation with
	// the body as the event payload. Routed by its URL token, not org fan-out.
	WebhookEventInboundWebhook WebhookEventType = "inbound.webhook"

	// --- Inbox / mailbox mail (high-volume; opt-in, see firehoseEvents) ---
	WebhookEventInboxEmailReceived WebhookEventType = "inbox.email_received"
	WebhookEventInboxEmailUpdated  WebhookEventType = "inbox.email_updated"
	WebhookEventInboxEmailDeleted  WebhookEventType = "inbox.email_deleted"
	WebhookEventInboxReplyReceived WebhookEventType = "inbox.reply_received"

	// --- Email account (mailbox) lifecycle & health ---
	WebhookEventEmailAccountDisconnected  WebhookEventType = "email_account.disconnected"
	WebhookEventEmailAccountError         WebhookEventType = "email_account.error"
	WebhookEventEmailAccountSynced        WebhookEventType = "email_account.synced"
	WebhookEventEmailAccountHealthChanged WebhookEventType = "email_account.health_changed"

	// --- Contacts ---
	WebhookEventContactCreated WebhookEventType = "contact.created"
	WebhookEventContactUpdated WebhookEventType = "contact.updated"
	WebhookEventContactDeleted WebhookEventType = "contact.deleted"

	// --- Bulk import/export operations ---
	WebhookEventBulkOperationStarted   WebhookEventType = "bulk_operation.started"
	WebhookEventBulkOperationCompleted WebhookEventType = "bulk_operation.completed"
	WebhookEventBulkOperationFailed    WebhookEventType = "bulk_operation.failed"

	// --- Automations ---
	WebhookEventAutomationCreated WebhookEventType = "automation.created"
	WebhookEventAutomationUpdated WebhookEventType = "automation.updated"
	WebhookEventAutomationDeleted WebhookEventType = "automation.deleted"
	WebhookEventAutomationRun     WebhookEventType = "automation.run"

	// --- Campaign lifecycle / config (the audit-bridge twins of campaign.*) ---
	WebhookEventCampaignCreated WebhookEventType = "campaign.created"
	WebhookEventCampaignUpdated WebhookEventType = "campaign.updated"
	WebhookEventCampaignDeleted WebhookEventType = "campaign.deleted"

	// --- Templates ---
	WebhookEventTemplateCreated WebhookEventType = "template.created"
	WebhookEventTemplateUpdated WebhookEventType = "template.updated"
	WebhookEventTemplateDeleted WebhookEventType = "template.deleted"

	// --- Team & access governance ---
	WebhookEventTeamMemberInvited WebhookEventType = "team.member_invited"
	WebhookEventTeamMemberRemoved WebhookEventType = "team.member_removed"
	WebhookEventRoleCreated       WebhookEventType = "role.created"
	WebhookEventRoleUpdated       WebhookEventType = "role.updated"
	WebhookEventRoleDeleted       WebhookEventType = "role.deleted"

	// --- CRM ---
	WebhookEventCRMDealCreated    WebhookEventType = "crm.deal_created"
	WebhookEventCRMDealUpdated    WebhookEventType = "crm.deal_updated"
	WebhookEventCRMDealDeleted    WebhookEventType = "crm.deal_deleted"
	WebhookEventCRMTaskCreated    WebhookEventType = "crm.task_created"
	WebhookEventCRMTaskUpdated    WebhookEventType = "crm.task_updated"
	WebhookEventCRMNoteCreated    WebhookEventType = "crm.note_created"
	WebhookEventCRMPipelineChange WebhookEventType = "crm.pipeline_updated"

	// --- Lead sync / settings / billing ---
	WebhookEventLeadSyncSourceUpdated WebhookEventType = "lead_sync_source.updated"
	WebhookEventSettingsUpdated       WebhookEventType = "settings.updated"
	WebhookEventSubscriptionUpdated   WebhookEventType = "subscription.updated"

	// --- Developer fire-event (mirrors the gateway CUSTOM_EVENT to HTTP) ---
	WebhookEventCustom WebhookEventType = "custom.event"

	// --- System: endpoint verification ping (delivered only to the endpoint
	// being verified / tested, never fanned out org-wide) ---
	WebhookEventEndpointTest WebhookEventType = "webhook.test"
)

// AllWebhookEventTypes lists every emitted event so the CRUD endpoint can
// validate `event_types` filters and the UI can render a picker.
var AllWebhookEventTypes = []WebhookEventType{
	WebhookEventEmailAccountConnected,
	WebhookEventEmailAccountRemoved,
	WebhookEventCampaignEmailSent,
	WebhookEventCampaignEmailDelivered,
	WebhookEventCampaignEmailOpened,
	WebhookEventCampaignEmailClicked,
	WebhookEventCampaignEmailBounced,
	WebhookEventCampaignReplyReceived,
	WebhookEventCampaignUnsubscribed,
	WebhookEventCampaignStarted,
	WebhookEventCampaignPaused,
	WebhookEventCampaignCompleted,
	WebhookEventCampaignDeliverabilityWarning,
	WebhookEventCampaignAction,
	WebhookEventWarmupEmailSent,
	WebhookEventWarmupHealthChanged,
	WebhookEventWarmupPlacementInSpam,
	WebhookEventWarmupQuarantined,
	WebhookEventWarmupBlocked,
	WebhookEventDeliverabilityBounce,
	WebhookEventDeliverabilityComplaint,
	WebhookEventMeetingBooked,
	WebhookEventMeetingRescheduled,
	WebhookEventMeetingCanceled,
	WebhookEventInboundWebhook,
	WebhookEventInboxEmailReceived,
	WebhookEventInboxEmailUpdated,
	WebhookEventInboxEmailDeleted,
	WebhookEventInboxReplyReceived,
	WebhookEventEmailAccountDisconnected,
	WebhookEventEmailAccountError,
	WebhookEventEmailAccountSynced,
	WebhookEventEmailAccountHealthChanged,
	WebhookEventContactCreated,
	WebhookEventContactUpdated,
	WebhookEventContactDeleted,
	WebhookEventBulkOperationStarted,
	WebhookEventBulkOperationCompleted,
	WebhookEventBulkOperationFailed,
	WebhookEventAutomationCreated,
	WebhookEventAutomationUpdated,
	WebhookEventAutomationDeleted,
	WebhookEventAutomationRun,
	WebhookEventCampaignCreated,
	WebhookEventCampaignUpdated,
	WebhookEventCampaignDeleted,
	WebhookEventTemplateCreated,
	WebhookEventTemplateUpdated,
	WebhookEventTemplateDeleted,
	WebhookEventTeamMemberInvited,
	WebhookEventTeamMemberRemoved,
	WebhookEventRoleCreated,
	WebhookEventRoleUpdated,
	WebhookEventRoleDeleted,
	WebhookEventCRMDealCreated,
	WebhookEventCRMDealUpdated,
	WebhookEventCRMDealDeleted,
	WebhookEventCRMTaskCreated,
	WebhookEventCRMTaskUpdated,
	WebhookEventCRMNoteCreated,
	WebhookEventCRMPipelineChange,
	WebhookEventLeadSyncSourceUpdated,
	WebhookEventSettingsUpdated,
	WebhookEventSubscriptionUpdated,
	WebhookEventCustom,
}

func IsValidWebhookEventType(s string) bool {
	for _, t := range AllWebhookEventTypes {
		if string(t) == s {
			return true
		}
	}
	return false
}

// firehoseEvents are per-message, high-volume event types. They are NOT included
// in the empty-filter wildcard: an endpoint receives them only by listing them
// explicitly in event_types. This keeps a "subscribe to everything" endpoint
// from being buried under per-open/click/send traffic, while still letting a
// caller opt in deliberately. Keep in sync with the dispatch/throttle sizing.
var firehoseEvents = map[WebhookEventType]bool{
	WebhookEventCampaignEmailSent:      true,
	WebhookEventCampaignEmailDelivered: true,
	WebhookEventCampaignEmailOpened:    true,
	WebhookEventCampaignEmailClicked:   true,
	WebhookEventWarmupEmailSent:        true,
	WebhookEventInboxEmailReceived:     true,
	WebhookEventInboxEmailUpdated:      true,
	WebhookEventInboxEmailDeleted:      true,
	WebhookEventEmailAccountSynced:     true,
}

// IsFirehoseEvent reports whether an event is high-volume and therefore
// opt-in-only (excluded from the empty-filter "all events" subscription).
func IsFirehoseEvent(t WebhookEventType) bool { return firehoseEvents[t] }

// WebhookEventCategory groups events for the picker UI and the public catalog.
type WebhookEventCategory string

const (
	WebhookCatCampaign       WebhookEventCategory = "Campaign"
	WebhookCatWarmup         WebhookEventCategory = "Warmup"
	WebhookCatDeliverability WebhookEventCategory = "Deliverability"
	WebhookCatInbox          WebhookEventCategory = "Inbox"
	WebhookCatEmailAccount   WebhookEventCategory = "Mailbox"
	WebhookCatContact        WebhookEventCategory = "Contact"
	WebhookCatCRM            WebhookEventCategory = "CRM"
	WebhookCatAutomation     WebhookEventCategory = "Automation"
	WebhookCatMeeting        WebhookEventCategory = "Meeting"
	WebhookCatTeam           WebhookEventCategory = "Team & access"
	WebhookCatBulk           WebhookEventCategory = "Bulk operations"
	WebhookCatWorkspace      WebhookEventCategory = "Workspace"
	WebhookCatDeveloper      WebhookEventCategory = "Developer"
)

// WebhookEventDescriptor is one entry in the public event catalog. The dashboard
// renders these in the event picker; the catalog endpoint serves them so
// integrators can discover the full vocabulary without scraping our docs.
type WebhookEventDescriptor struct {
	Type        WebhookEventType     `json:"type"`
	Category    WebhookEventCategory `json:"category"`
	Description string               `json:"description"`
	Firehose    bool                 `json:"firehose"`
}

// WebhookEventCatalog is the ordered, described list of every outbound event.
// inbound.webhook is intentionally excluded — it is a trigger, never delivered.
var WebhookEventCatalog = buildWebhookEventCatalog()

func buildWebhookEventCatalog() []WebhookEventDescriptor {
	type row struct {
		t    WebhookEventType
		cat  WebhookEventCategory
		desc string
	}
	rows := []row{
		{WebhookEventCampaignStarted, WebhookCatCampaign, "A campaign began sending."},
		{WebhookEventCampaignPaused, WebhookCatCampaign, "A campaign was paused (manually or by deliverability guardrails)."},
		{WebhookEventCampaignCompleted, WebhookCatCampaign, "A campaign finished its last scheduled send."},
		{WebhookEventCampaignCreated, WebhookCatCampaign, "A campaign was created."},
		{WebhookEventCampaignUpdated, WebhookCatCampaign, "A campaign's settings or steps changed."},
		{WebhookEventCampaignDeleted, WebhookCatCampaign, "A campaign was deleted."},
		{WebhookEventCampaignEmailSent, WebhookCatCampaign, "A campaign email was sent to a contact."},
		{WebhookEventCampaignEmailDelivered, WebhookCatCampaign, "A campaign email was accepted by the receiving server."},
		{WebhookEventCampaignEmailOpened, WebhookCatCampaign, "A campaign email was opened."},
		{WebhookEventCampaignEmailClicked, WebhookCatCampaign, "A link in a campaign email was clicked."},
		{WebhookEventCampaignEmailBounced, WebhookCatCampaign, "A campaign email bounced."},
		{WebhookEventCampaignReplyReceived, WebhookCatCampaign, "A contact replied to a campaign email."},
		{WebhookEventCampaignUnsubscribed, WebhookCatCampaign, "A contact unsubscribed from a campaign."},
		{WebhookEventCampaignDeliverabilityWarning, WebhookCatCampaign, "A campaign entered the deliverability early-warning band."},
		{WebhookEventCampaignAction, WebhookCatCampaign, "A campaign sequence \"notify\" action fired."},
		{WebhookEventWarmupEmailSent, WebhookCatWarmup, "A warmup email was sent."},
		{WebhookEventWarmupHealthChanged, WebhookCatWarmup, "A mailbox's warmup health state changed."},
		{WebhookEventWarmupPlacementInSpam, WebhookCatWarmup, "A warmup email landed in spam."},
		{WebhookEventWarmupQuarantined, WebhookCatWarmup, "A mailbox was quarantined from the warmup pool."},
		{WebhookEventWarmupBlocked, WebhookCatWarmup, "A mailbox was blocked from the warmup pool."},
		{WebhookEventDeliverabilityBounce, WebhookCatDeliverability, "A bounce was recorded (any source)."},
		{WebhookEventDeliverabilityComplaint, WebhookCatDeliverability, "A spam complaint was recorded."},
		{WebhookEventInboxEmailReceived, WebhookCatInbox, "A new email arrived in a connected mailbox."},
		{WebhookEventInboxEmailUpdated, WebhookCatInbox, "An inbox email changed (read state, labels, category)."},
		{WebhookEventInboxEmailDeleted, WebhookCatInbox, "An inbox email was deleted."},
		{WebhookEventInboxReplyReceived, WebhookCatInbox, "A human reply was received (including non-campaign replies)."},
		{WebhookEventEmailAccountConnected, WebhookCatEmailAccount, "A mailbox was connected."},
		{WebhookEventEmailAccountDisconnected, WebhookCatEmailAccount, "A mailbox was disconnected."},
		{WebhookEventEmailAccountRemoved, WebhookCatEmailAccount, "A mailbox was removed."},
		{WebhookEventEmailAccountError, WebhookCatEmailAccount, "A mailbox hit an auth or sync error."},
		{WebhookEventEmailAccountSynced, WebhookCatEmailAccount, "A mailbox finished a sync."},
		{WebhookEventEmailAccountHealthChanged, WebhookCatEmailAccount, "A mailbox's health state changed."},
		{WebhookEventContactCreated, WebhookCatContact, "A contact was created."},
		{WebhookEventContactUpdated, WebhookCatContact, "A contact was updated."},
		{WebhookEventContactDeleted, WebhookCatContact, "A contact was deleted."},
		{WebhookEventCRMDealCreated, WebhookCatCRM, "A CRM deal was created."},
		{WebhookEventCRMDealUpdated, WebhookCatCRM, "A CRM deal changed (including stage moves)."},
		{WebhookEventCRMDealDeleted, WebhookCatCRM, "A CRM deal was deleted."},
		{WebhookEventCRMTaskCreated, WebhookCatCRM, "A CRM task was created."},
		{WebhookEventCRMTaskUpdated, WebhookCatCRM, "A CRM task changed."},
		{WebhookEventCRMNoteCreated, WebhookCatCRM, "A CRM note was added."},
		{WebhookEventCRMPipelineChange, WebhookCatCRM, "A CRM pipeline or stage changed."},
		{WebhookEventAutomationCreated, WebhookCatAutomation, "An automation was created."},
		{WebhookEventAutomationUpdated, WebhookCatAutomation, "An automation was updated."},
		{WebhookEventAutomationDeleted, WebhookCatAutomation, "An automation was deleted."},
		{WebhookEventAutomationRun, WebhookCatAutomation, "An automation run completed."},
		{WebhookEventMeetingBooked, WebhookCatMeeting, "A meeting was booked."},
		{WebhookEventMeetingRescheduled, WebhookCatMeeting, "A meeting was rescheduled."},
		{WebhookEventMeetingCanceled, WebhookCatMeeting, "A meeting was canceled."},
		{WebhookEventTeamMemberInvited, WebhookCatTeam, "A teammate was invited."},
		{WebhookEventTeamMemberRemoved, WebhookCatTeam, "A teammate was removed."},
		{WebhookEventRoleCreated, WebhookCatTeam, "A role was created."},
		{WebhookEventRoleUpdated, WebhookCatTeam, "A role was updated."},
		{WebhookEventRoleDeleted, WebhookCatTeam, "A role was deleted."},
		{WebhookEventBulkOperationStarted, WebhookCatBulk, "A bulk import/export started."},
		{WebhookEventBulkOperationCompleted, WebhookCatBulk, "A bulk import/export completed."},
		{WebhookEventBulkOperationFailed, WebhookCatBulk, "A bulk import/export failed."},
		{WebhookEventLeadSyncSourceUpdated, WebhookCatWorkspace, "A lead-sync source changed."},
		{WebhookEventSettingsUpdated, WebhookCatWorkspace, "Workspace settings changed."},
		{WebhookEventSubscriptionUpdated, WebhookCatWorkspace, "The plan or subscription changed."},
		{WebhookEventCustom, WebhookCatDeveloper, "A developer-defined custom event was fired."},
	}
	out := make([]WebhookEventDescriptor, 0, len(rows))
	for _, r := range rows {
		out = append(out, WebhookEventDescriptor{
			Type:        r.t,
			Category:    r.cat,
			Description: r.desc,
			Firehose:    firehoseEvents[r.t],
		})
	}
	return out
}

// WebhookEventRequiredScope returns the API-permission bit an OAuth app must
// hold (and the granting org must have granted) to receive a given event via its
// app-level webhook subscription. 0 means "no specific scope required". This
// mirrors the realtime gateway's per-member event gating so an app can never
// receive data its grant did not authorize.
func WebhookEventRequiredScope(t WebhookEventType) uint64 {
	s := string(t)
	switch {
	case strings.HasPrefix(s, "inbox."):
		return APIPermReadUnibox
	case strings.HasPrefix(s, "campaign."), strings.HasPrefix(s, "deliverability."), strings.HasPrefix(s, "meeting."):
		return APIPermReadCampaigns
	case strings.HasPrefix(s, "email_account."), strings.HasPrefix(s, "warmup."):
		return APIPermReadEmails
	case strings.HasPrefix(s, "contact."), strings.HasPrefix(s, "bulk_operation."):
		return APIPermReadContacts
	case strings.HasPrefix(s, "crm."):
		return APIPermReadCRM
	case strings.HasPrefix(s, "automation."):
		return APIPermIntegrations
	case strings.HasPrefix(s, "team."), strings.HasPrefix(s, "role."), strings.HasPrefix(s, "settings."), strings.HasPrefix(s, "subscription."):
		return APIPermReadAuditLogs
	case strings.HasPrefix(s, "custom."):
		return APIPermRealtimeSubscribe
	default:
		return APIPermReadCampaigns
	}
}

// EventAllowedByScopes reports whether a grant holding `scopes` may receive the
// given event.
func EventAllowedByScopes(t WebhookEventType, scopes uint64) bool {
	req := WebhookEventRequiredScope(t)
	return req == 0 || scopes&req != 0
}

// AppSubscribedEventTypes resolves the concrete event_types an app-materialized
// endpoint should carry for a grant: the app's requested `wanted` events,
// filtered to those the grant's `scopes` allow. An empty `wanted` means "all
// non-firehose events", still filtered by scope. The result is what the managed
// webhook_endpoints row stores, so the normal delivery path enforces scoping.
func AppSubscribedEventTypes(wanted []string, scopes uint64) []string {
	if len(wanted) > 0 {
		out := make([]string, 0, len(wanted))
		for _, w := range wanted {
			if EventAllowedByScopes(WebhookEventType(w), scopes) {
				out = append(out, w)
			}
		}
		return out
	}
	// Empty filter: every non-firehose event the scopes allow.
	out := make([]string, 0, len(AllWebhookEventTypes))
	for _, t := range AllWebhookEventTypes {
		if IsFirehoseEvent(t) || t == WebhookEventInboundWebhook {
			continue
		}
		if EventAllowedByScopes(t, scopes) {
			out = append(out, string(t))
		}
	}
	return out
}

// WebhookEventForAudit maps an audit-spine (entity_type, action) pair to the
// outbound webhook event it should fan out as, or ("", false) to skip. This is
// the bridge that turns every audited mutation into a typed customer webhook
// without a bespoke emit at each call site. It deliberately SKIPS:
//   - operator-internal entities (worker, aws_credentials, worker_profile,
//     release, api_key, user, organization, integration)
//   - entities that already have a dedicated, richer emit (email_account,
//     meeting) to avoid double-delivery
//   - the webhook entity itself (no self-referential delivery loops)
//   - noisy low-value entities (folder, tag, category, unibox, step,
//     warmup_routing_rule)
func WebhookEventForAudit(entityType AuditEntityType, action AuditAction) (WebhookEventType, bool) {
	switch entityType {
	case AuditEntityCampaign:
		switch action {
		case AuditActionCreate:
			return WebhookEventCampaignCreated, true
		case AuditActionUpdate:
			return WebhookEventCampaignUpdated, true
		case AuditActionDelete:
			return WebhookEventCampaignDeleted, true
		case AuditActionStart, AuditActionResume:
			// User-initiated start/resume. The automated deliverability auto-pause
			// path emits campaign.paused directly (no audit log), so there is no
			// double-emit for these.
			return WebhookEventCampaignStarted, true
		case AuditActionPause:
			return WebhookEventCampaignPaused, true
		}
	case AuditEntityContact:
		switch action {
		case AuditActionCreate:
			return WebhookEventContactCreated, true
		case AuditActionUpdate:
			return WebhookEventContactUpdated, true
		case AuditActionDelete:
			return WebhookEventContactDeleted, true
		}
	case AuditEntityTemplate:
		switch action {
		case AuditActionCreate:
			return WebhookEventTemplateCreated, true
		case AuditActionUpdate:
			return WebhookEventTemplateUpdated, true
		case AuditActionDelete:
			return WebhookEventTemplateDeleted, true
		}
	case AuditEntityAutomation:
		switch action {
		case AuditActionCreate:
			return WebhookEventAutomationCreated, true
		case AuditActionUpdate:
			return WebhookEventAutomationUpdated, true
		case AuditActionDelete:
			return WebhookEventAutomationDeleted, true
		}
	case AuditEntityRole:
		switch action {
		case AuditActionCreate:
			return WebhookEventRoleCreated, true
		case AuditActionUpdate:
			return WebhookEventRoleUpdated, true
		case AuditActionDelete:
			return WebhookEventRoleDeleted, true
		}
	case AuditEntityCRMDeal:
		switch action {
		case AuditActionCreate:
			return WebhookEventCRMDealCreated, true
		case AuditActionUpdate:
			return WebhookEventCRMDealUpdated, true
		case AuditActionDelete:
			return WebhookEventCRMDealDeleted, true
		}
	case AuditEntityCRMTask:
		switch action {
		case AuditActionCreate:
			return WebhookEventCRMTaskCreated, true
		case AuditActionUpdate:
			return WebhookEventCRMTaskUpdated, true
		}
	case AuditEntityCRMNote:
		if action == AuditActionCreate {
			return WebhookEventCRMNoteCreated, true
		}
	case AuditEntityCRMPipeline, AuditEntityCRMStage:
		return WebhookEventCRMPipelineChange, true
	case AuditEntityInvitation:
		switch action {
		case AuditActionInvite, AuditActionCreate:
			return WebhookEventTeamMemberInvited, true
		}
	case AuditEntityOrganizationMember:
		if action == AuditActionRemove {
			return WebhookEventTeamMemberRemoved, true
		}
	case AuditEntityLeadSyncSource:
		switch action {
		case AuditActionCreate, AuditActionUpdate:
			return WebhookEventLeadSyncSourceUpdated, true
		}
	case AuditEntitySettings:
		if action == AuditActionUpdate {
			return WebhookEventSettingsUpdated, true
		}
	case AuditEntitySubscription:
		if action == AuditActionUpdate {
			return WebhookEventSubscriptionUpdated, true
		}
	}
	return "", false
}

// WebhookEndpoint is a customer's subscription to events.
type WebhookEndpoint struct {
	ID                  uuid.UUID  `json:"id"`
	OrganizationID      uuid.UUID  `json:"organization_id"`
	URL                 string     `json:"url"`
	Description         string     `json:"description"`
	EventTypes          []string   `json:"event_types"`
	Enabled             bool       `json:"enabled"`
	LastSuccessAt       *time.Time `json:"last_success_at,omitempty"`
	LastFailureAt       *time.Time `json:"last_failure_at,omitempty"`
	LastFailureReason   *string    `json:"last_failure_reason,omitempty"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	// OAuthApplicationID is set for app-scoped endpoints. Their URL host must
	// stay within the owning app's allowed_webhook_domains on every write and
	// delivery; nil for normal org-level endpoints.
	OAuthApplicationID *uuid.UUID `json:"oauth_application_id,omitempty"`
	CreatedBy          *uuid.UUID `json:"created_by,omitempty"`
	// VerifiedAt is non-nil once the endpoint proved it accepts our challenge.
	// Only verified endpoints receive the real event stream.
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
	// OwnershipConfirmed is true once the receiver echoed the challenge back
	// (proves control of the URL, not just reachability).
	OwnershipConfirmed bool       `json:"ownership_confirmed"`
	AutoDisabledAt     *time.Time `json:"auto_disabled_at,omitempty"`
	DisabledReason     *string    `json:"disabled_reason,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// Verified reports whether the endpoint has passed ownership verification.
func (e *WebhookEndpoint) Verified() bool { return e.VerifiedAt != nil }

// WebhookEndpointWithSecret is returned once at creation time so the
// client can capture the secret. Subsequent reads do not include it.
type WebhookEndpointWithSecret struct {
	WebhookEndpoint
	Secret string `json:"secret"`
}

// Subscribes returns true if this endpoint should receive the given event. An
// endpoint must be enabled and verified. An explicit event_types entry always
// matches (the only way to opt into high-volume firehose events). An empty
// event_types array matches all events EXCEPT firehose ones, which must be
// subscribed to explicitly.
func (e *WebhookEndpoint) Subscribes(event WebhookEventType) bool {
	if !e.Enabled || !e.Verified() {
		return false
	}
	for _, t := range e.EventTypes {
		if t == string(event) {
			return true
		}
	}
	return len(e.EventTypes) == 0 && !IsFirehoseEvent(event)
}

// WebhookDeliveryStatus tracks where a delivery attempt sits in its
// lifecycle. 'abandoned' = retries exhausted.
type WebhookDeliveryStatus string

const (
	WebhookDeliveryPending   WebhookDeliveryStatus = "pending"
	WebhookDeliveryInFlight  WebhookDeliveryStatus = "in_flight"
	WebhookDeliveryDelivered WebhookDeliveryStatus = "delivered"
	WebhookDeliveryFailed    WebhookDeliveryStatus = "failed"
	WebhookDeliveryAbandoned WebhookDeliveryStatus = "abandoned"
)

// WebhookDelivery is one attempt-history record. Multiple rows may exist
// per (event, endpoint) across retries — each row updates in place as
// attempts progress.
type WebhookDelivery struct {
	ID                  uuid.UUID             `json:"id"`
	EndpointID          uuid.UUID             `json:"endpoint_id"`
	OrganizationID      uuid.UUID             `json:"organization_id"`
	EventType           string                `json:"event_type"`
	EventID             uuid.UUID             `json:"event_id"`
	Payload             json.RawMessage       `json:"payload"`
	Status              WebhookDeliveryStatus `json:"status"`
	AttemptCount        int                   `json:"attempt_count"`
	MaxAttempts         int                   `json:"max_attempts"`
	NextAttemptAt       time.Time             `json:"next_attempt_at"`
	LastAttemptAt       *time.Time            `json:"last_attempt_at,omitempty"`
	ResponseStatus      *int                  `json:"response_status,omitempty"`
	ResponseBodyExcerpt *string               `json:"response_body_excerpt,omitempty"`
	ErrorReason         *string               `json:"error_reason,omitempty"`
	CreatedAt           time.Time             `json:"created_at"`
	UpdatedAt           time.Time             `json:"updated_at"`
}

// WebhookDeliveryFilter narrows a delivery-log query. Empty fields are ignored.
type WebhookDeliveryFilter struct {
	EndpointID *uuid.UUID
	Status     string
	EventType  string
	Offset     int
	Limit      int
}

// WebhookDeliveriesResult is the paginated delivery-log response (data +
// opaque-cursor pagination, matching the platform's list-endpoint contract).
type WebhookDeliveriesResult struct {
	Data       []WebhookDelivery `json:"data"`
	Pagination CPagination       `json:"pagination"`
}

// WebhookEventDrop is a daily rollup of events the dispatch throttle dropped, so
// the dashboard can surface rate-limiting instead of it being silent.
type WebhookEventDrop struct {
	EventType      string    `json:"event_type"`
	Day            time.Time `json:"day"`
	DroppedWindows int       `json:"dropped_windows"`
	LastDroppedAt  time.Time `json:"last_dropped_at"`
}

// WebhookPayload is the JSON body the dispatcher POSTs to a subscriber.
type WebhookPayload struct {
	ID             uuid.UUID        `json:"id"`
	EventType      WebhookEventType `json:"event_type"`
	OrganizationID uuid.UUID        `json:"organization_id"`
	CreatedAt      time.Time        `json:"created_at"`
	Data           any              `json:"data"`
}
