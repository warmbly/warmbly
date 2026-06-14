package pubsub

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Additional Topics for realtime updates
const (
	TopicUserEvents   = "user-events"
	TopicEmailInbox   = "email-inbox"
	TopicBulkOps      = "bulk-operations"
	TopicContactsSync = "contacts-sync"
)

// Additional Event Types
const (
	// Email inbox events
	EventEmailReceived EventType = "EMAIL_RECEIVED"
	EventEmailUpdated  EventType = "EMAIL_UPDATED"
	EventEmailDeleted  EventType = "EMAIL_DELETED"

	// Contact events
	EventContactCreated EventType = "CONTACT_CREATED"
	EventContactUpdated EventType = "CONTACT_UPDATED"
	EventContactDeleted EventType = "CONTACT_DELETED"
	EventContactsReload EventType = "CONTACTS_RELOAD"

	// Campaign events
	EventCampaignCreated   EventType = "CAMPAIGN_CREATED"
	EventCampaignUpdated   EventType = "CAMPAIGN_UPDATED"
	EventCampaignDeleted   EventType = "CAMPAIGN_DELETED"
	EventCampaignStarted   EventType = "CAMPAIGN_STARTED"
	EventCampaignPaused    EventType = "CAMPAIGN_PAUSED"
	EventCampaignCompleted EventType = "CAMPAIGN_COMPLETED"

	// Email account events
	EventAccountConnected     EventType = "ACCOUNT_CONNECTED"
	EventAccountDisconnected  EventType = "ACCOUNT_DISCONNECTED"
	EventAccountError         EventType = "ACCOUNT_ERROR"
	EventAccountSynced        EventType = "ACCOUNT_SYNCED"
	EventAccountHealthChanged EventType = "ACCOUNT_HEALTH_CHANGED"

	// Bulk operation events
	EventBulkStarted   EventType = "BULK_STARTED"
	EventBulkProgress  EventType = "BULK_PROGRESS"
	EventBulkCompleted EventType = "BULK_COMPLETED"
	EventBulkFailed    EventType = "BULK_FAILED"

	// Tracking events (from Rust tracking service)
	EventEmailOpened  EventType = "EMAIL_OPENED"
	EventEmailClicked EventType = "EMAIL_CLICKED"

	// A human reply landed for a campaign contact (org-scoped pulse).
	EventEmailReplied EventType = "EMAIL_REPLIED"

	// Task progress events
	EventTaskProgress EventType = "TASK_PROGRESS"

	// Audit trail events (org-scoped). The web client invalidates the
	// ['audit'] query on any event type containing "AUDIT".
	EventAuditCreated EventType = "AUDIT_CREATED"

	// Automation events (org-scoped). The web client invalidates the
	// ['automations'] queries on any event type containing "AUTOMATION".
	EventAutomationCreated EventType = "AUTOMATION_CREATED"
	EventAutomationUpdated EventType = "AUTOMATION_UPDATED"
	EventAutomationDeleted EventType = "AUTOMATION_DELETED"
	EventAutomationRun     EventType = "AUTOMATION_RUN"

	// Developer "fire event": a custom, org-scoped event emitted from an
	// automation action or campaign step, delivered over the realtime gateway so
	// API-key subscribers receive it without hosting a public webhook URL.
	EventCustomFired EventType = "CUSTOM_EVENT"

	// In-app notification feed (user-scoped). The web client refreshes the bell
	// feed + may toast on any event type containing "NOTIFICATION".
	EventNotificationCreated EventType = "NOTIFICATION_CREATED"

	// Meetings. The frontend matches any event type containing "MEETING" /
	// "BOOKING" to refresh the Meetings page, contact timeline, and sidebar.
	EventMeetingBooked      EventType = "MEETING_BOOKED"
	EventMeetingRescheduled EventType = "MEETING_RESCHEDULED"
	EventMeetingCanceled    EventType = "MEETING_CANCELED"

	// Org-wide presence privacy policy changed. The realtime OrgChannel handles
	// this internally (re-track / untrack / strip activity) to apply the new
	// policy live; it is not forwarded to web clients.
	EventPresencePolicyUpdated EventType = "PRESENCE_POLICY_UPDATED"
)

// BaseEvent contains common fields for all events
type BaseEvent struct {
	EventType EventType `json:"event_type"`
	UserID    string    `json:"user_id"`
	Timestamp time.Time `json:"timestamp"`
}

// EmailInboxEvent for new/updated emails
type EmailInboxEvent struct {
	BaseEvent
	OrgID          string `json:"org_id,omitempty"`
	EmailAccountID string `json:"email_account_id"`
	MessageID      string `json:"message_id"`
	ThreadID       string `json:"thread_id,omitempty"`
	Subject        string `json:"subject,omitempty"`
	From           string `json:"from,omitempty"`
	Preview        string `json:"preview,omitempty"`
}

// ContactEvent for contact changes
type ContactEvent struct {
	BaseEvent
	ContactID string `json:"contact_id"`
	Email     string `json:"email,omitempty"`
	ListID    string `json:"list_id,omitempty"`
}

// BulkOperationEvent for bulk operation signals
type BulkOperationEvent struct {
	BaseEvent
	OperationID    string  `json:"operation_id"`
	OperationType  string  `json:"operation_type"`
	EntityType     string  `json:"entity_type"`
	TotalItems     int     `json:"total_items,omitempty"`
	ProcessedItems int     `json:"processed_items,omitempty"`
	FailedItems    int     `json:"failed_items,omitempty"`
	Progress       float64 `json:"progress,omitempty"`
	ErrorMessage   string  `json:"error_message,omitempty"`
}

// CampaignEvent for campaign changes
type CampaignEvent struct {
	BaseEvent
	OrgID      string                `json:"org_id,omitempty"`
	CampaignID string                `json:"campaign_id"`
	Name       string                `json:"name,omitempty"`
	Status     string                `json:"status,omitempty"`
	Progress   *CampaignProgressData `json:"progress,omitempty"`
}

// CampaignProgressData for campaign progress updates
type CampaignProgressData struct {
	TotalContacts int `json:"total_contacts"`
	EmailsSent    int `json:"emails_sent"`
	EmailsOpened  int `json:"emails_opened"`
	EmailsClicked int `json:"emails_clicked"`
	EmailsReplied int `json:"emails_replied"`
	EmailsBounced int `json:"emails_bounced"`
}

// AccountEvent for email account status changes
type AccountEvent struct {
	BaseEvent
	OrgID          string `json:"org_id,omitempty"`
	EmailAccountID string `json:"email_account_id"`
	Email          string `json:"email"`
	Provider       string `json:"provider,omitempty"`
	Status         string `json:"status,omitempty"`
	ErrorCode      string `json:"error_code,omitempty"`
	ErrorMessage   string `json:"error_message,omitempty"`
	// Warmup health transition fields (EventAccountHealthChanged).
	HealthState   string `json:"health_state,omitempty"`
	PreviousState string `json:"previous_state,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

// WarmupStatsEvent for warmup statistics updates
type WarmupStatsEvent struct {
	BaseEvent
	EmailAccountID string `json:"email_account_id"`
	Date           string `json:"date"`
	EmailsSent     int    `json:"emails_sent"`
	EmailsReplied  int    `json:"emails_replied"`
	TargetVolume   int    `json:"target_volume"`
}

// TrackingEventPayload for email open/click tracking events
type TrackingEventPayload struct {
	BaseEvent
	OrgID        string `json:"org_id,omitempty"`
	CampaignID   string `json:"campaign_id"`
	ContactID    string `json:"contact_id,omitempty"`
	ContactEmail string `json:"contact_email,omitempty"`
	SequenceID   string `json:"step_id,omitempty"`
	OriginalURL  string `json:"original_url,omitempty"` // For click events
	// Machine marks an automated open (Apple MPP prefetch, UA-less fetcher)
	// so live views can badge it instead of presenting it as a human open.
	Machine bool `json:"machine,omitempty"`
}

// TaskProgressEvent for detailed campaign task progress
type TaskProgressEvent struct {
	BaseEvent
	OrgID          string `json:"org_id,omitempty"`
	CampaignID     string `json:"campaign_id"`
	TaskID         string `json:"task_id"`
	Status         string `json:"status"` // pending, active, completed, failed
	ContactID      string `json:"contact_id"`
	ContactEmail   string `json:"contact_email"`
	ContactName    string `json:"contact_name"`
	SequenceID     string `json:"step_id"`
	SequenceName   string `json:"step_name"`
	SequenceIndex  int    `json:"step_index"`
	Progress       int    `json:"progress"` // Percentage 0-100
	TotalContacts  int    `json:"total_contacts"`
	ProcessedCount int    `json:"processed_count"`
}

// MeetingEvent for booked / rescheduled / canceled meetings from Calendly /
// Cal.com. Carries just enough for the dashboard to refresh the right surfaces.
type MeetingEvent struct {
	BaseEvent
	BookingID    string `json:"booking_id"`
	ContactID    string `json:"contact_id,omitempty"`
	InviteeEmail string `json:"invitee_email,omitempty"`
	EventName    string `json:"event_name,omitempty"`
	ScheduledFor string `json:"scheduled_for,omitempty"`
	Source       string `json:"source,omitempty"` // calendly / cal_com
	State        string `json:"state,omitempty"`  // booked / rescheduled / canceled
}

// PublishMeeting notifies the lead owner that a meeting was booked, rescheduled,
// or canceled so the Meetings page, contact timeline, and sidebar update live.
func (p *StreamingPublisher) PublishMeeting(ctx context.Context, userID string, eventType EventType, event *MeetingEvent) {
	if p == nil || p.client == nil || userID == "" {
		return
	}
	event.BaseEvent = BaseEvent{
		EventType: eventType,
		UserID:    userID,
		Timestamp: time.Now(),
	}
	attrs := map[string]string{
		"user_id":    userID,
		"event_type": string(eventType),
	}
	if err := p.client.Publish(ctx, TopicUserEvents, event, attrs); err != nil {
		// Best-effort: realtime is a nicety, not a requirement.
	}
}

// New publish methods

// PublishEmailReceived notifies user of new email
func (p *StreamingPublisher) PublishEmailReceived(ctx context.Context, event *EmailInboxEvent) {
	if p.client == nil {
		return
	}

	event.EventType = EventEmailReceived
	event.Timestamp = time.Now()

	attrs := map[string]string{
		"user_id":    event.UserID,
		"email_id":   event.EmailAccountID,
		"event_type": string(EventEmailReceived),
	}

	if err := p.client.Publish(ctx, TopicEmailInbox, event, attrs); err != nil {
		// Log error but don't fail
	}
}

// PublishEmailUpdated notifies user that an inbox row changed.
func (p *StreamingPublisher) PublishEmailUpdated(ctx context.Context, event *EmailInboxEvent) {
	if p.client == nil {
		return
	}

	event.EventType = EventEmailUpdated
	event.Timestamp = time.Now()

	attrs := map[string]string{
		"user_id":    event.UserID,
		"email_id":   event.EmailAccountID,
		"event_type": string(EventEmailUpdated),
	}

	if err := p.client.Publish(ctx, TopicEmailInbox, event, attrs); err != nil {
		// Log error but don't fail
	}
}

// PublishEmailDeleted notifies user that an inbox row was removed.
func (p *StreamingPublisher) PublishEmailDeleted(ctx context.Context, event *EmailInboxEvent) {
	if p.client == nil {
		return
	}

	event.EventType = EventEmailDeleted
	event.Timestamp = time.Now()

	attrs := map[string]string{
		"user_id":    event.UserID,
		"email_id":   event.EmailAccountID,
		"event_type": string(EventEmailDeleted),
	}

	if err := p.client.Publish(ctx, TopicEmailInbox, event, attrs); err != nil {
		// Log error but don't fail
	}
}

// PublishContactsReload signals frontend to reload contacts
func (p *StreamingPublisher) PublishContactsReload(ctx context.Context, userID, operationID string) {
	if p.client == nil {
		return
	}

	event := &BulkOperationEvent{
		BaseEvent: BaseEvent{
			EventType: EventContactsReload,
			UserID:    userID,
			Timestamp: time.Now(),
		},
		OperationID: operationID,
		EntityType:  "contacts",
	}

	attrs := map[string]string{
		"user_id":    userID,
		"event_type": string(EventContactsReload),
	}

	if err := p.client.Publish(ctx, TopicBulkOps, event, attrs); err != nil {
		// Log error but don't fail
	}
}

// PublishBulkProgress sends bulk operation progress update
func (p *StreamingPublisher) PublishBulkProgress(ctx context.Context, event *BulkOperationEvent) {
	if p.client == nil {
		return
	}

	event.Timestamp = time.Now()

	attrs := map[string]string{
		"user_id":      event.UserID,
		"operation_id": event.OperationID,
		"event_type":   string(event.EventType),
	}

	if err := p.client.Publish(ctx, TopicBulkOps, event, attrs); err != nil {
		// Log error but don't fail
	}
}

// PublishCampaignEvent sends campaign event
func (p *StreamingPublisher) PublishCampaignEvent(ctx context.Context, event *CampaignEvent) {
	if p.client == nil {
		return
	}

	event.Timestamp = time.Now()

	attrs := map[string]string{
		"user_id":     event.UserID,
		"campaign_id": event.CampaignID,
		"event_type":  string(event.EventType),
	}

	if err := p.client.Publish(ctx, TopicCampaignUpdate, event, attrs); err != nil {
		// Log error but don't fail
	}
}

// PublishAccountEvent sends email account event
func (p *StreamingPublisher) PublishAccountEvent(ctx context.Context, event *AccountEvent) {
	if p.client == nil {
		return
	}

	event.Timestamp = time.Now()

	attrs := map[string]string{
		"user_id":    event.UserID,
		"account_id": event.EmailAccountID,
		"event_type": string(event.EventType),
	}

	topicID := TopicUserEvents
	if event.EventType == EventAccountError {
		topicID = TopicEmailError
	}

	if err := p.client.Publish(ctx, topicID, event, attrs); err != nil {
		// Log error but don't fail
	}
}

// PublishAccountHealth pushes a mailbox warmup-health transition to the
// owning user's realtime stream. The dashboard treats it as an ACCOUNT event
// and refreshes account status live; the explicit state fields let consumers
// react without a refetch.
func (p *StreamingPublisher) PublishAccountHealth(ctx context.Context, orgID, userID, accountID, email, prevState, newState, reason string) {
	if p == nil || p.client == nil {
		return
	}
	p.PublishAccountEvent(ctx, &AccountEvent{
		BaseEvent: BaseEvent{
			EventType: EventAccountHealthChanged,
			UserID:    userID,
		},
		OrgID:          orgID,
		EmailAccountID: accountID,
		Email:          email,
		Status:         newState,
		HealthState:    newState,
		PreviousState:  prevState,
		Reason:         reason,
	})
}

// AuditEvent signals that a new audit-trail entry was recorded. It is
// org-scoped: org_id is set in both the body and the Pub/Sub attributes so the
// realtime fanout delivers it to the org channel (owners/admins watching the
// activity log), not just the acting user.
type AuditEvent struct {
	BaseEvent
	OrgID      string `json:"org_id"`
	Action     string `json:"action"`
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id,omitempty"`
}

// PublishAuditCreated emits an org-scoped audit.created signal. It carries no
// sensitive detail (no IP, user-agent, changes or metadata) — only enough for
// the dashboard to know it should refetch the audit list.
func (p *StreamingPublisher) PublishAuditCreated(ctx context.Context, orgID, actorID uuid.UUID, action, entityType string, entityID *uuid.UUID) {
	if p == nil || p.client == nil {
		return
	}

	event := &AuditEvent{
		BaseEvent: BaseEvent{
			EventType: EventAuditCreated,
			UserID:    actorID.String(),
			Timestamp: time.Now(),
		},
		OrgID:      orgID.String(),
		Action:     action,
		EntityType: entityType,
	}
	if entityID != nil {
		event.EntityID = entityID.String()
	}

	attrs := map[string]string{
		"user_id":    actorID.String(),
		"org_id":     orgID.String(),
		"event_type": string(EventAuditCreated),
	}

	if err := p.client.Publish(ctx, TopicUserEvents, event, attrs); err != nil {
		// Best-effort: realtime is a nicety, not a requirement.
	}
}

// AutomationEvent is an org-scoped automation lifecycle/run signal. The web
// client invalidates the ['automations'] queries on any "AUTOMATION" event.
type AutomationEvent struct {
	BaseEvent
	OrgID          string `json:"org_id"`
	AutomationID   string `json:"automation_id,omitempty"`
	AutomationName string `json:"automation_name,omitempty"`
	Status         string `json:"status,omitempty"`
}

// PublishAutomationEvent emits an org-scoped automation event. actorID may be
// uuid.Nil for system-triggered runs (the event reaches org:<id> subscribers).
func (p *StreamingPublisher) PublishAutomationEvent(ctx context.Context, orgID, actorID uuid.UUID, eventType EventType, automationID, name string) {
	if p == nil || p.client == nil || orgID == uuid.Nil {
		return
	}
	event := &AutomationEvent{
		BaseEvent: BaseEvent{
			EventType: eventType,
			UserID:    actorID.String(),
			Timestamp: time.Now(),
		},
		OrgID:          orgID.String(),
		AutomationID:   automationID,
		AutomationName: name,
	}
	attrs := map[string]string{
		"user_id":    actorID.String(),
		"org_id":     orgID.String(),
		"event_type": string(eventType),
	}
	if err := p.client.Publish(ctx, TopicUserEvents, event, attrs); err != nil {
		// Best-effort: realtime is a nicety, not a requirement.
	}
}

// CustomEvent is a developer-defined "fire event" signal. Name is the
// caller-chosen event name (what subscribers match on) and Payload is the
// fully-customizable key/value data. Source/SourceID record where it was fired
// from (an automation or a campaign step). Routed to org:<id> like any
// org-scoped event; the gateway delivers it to API-key websocket subscribers.
type CustomEvent struct {
	BaseEvent
	OrgID    string            `json:"org_id"`
	Name     string            `json:"name"`
	Payload  map[string]string `json:"payload,omitempty"`
	Source   string            `json:"source,omitempty"`
	SourceID string            `json:"source_id,omitempty"`
}

// PublishCustomEvent emits an org-scoped developer "fire event". actorID may be
// uuid.Nil for system-fired events. Best-effort: a publish hiccup never blocks
// the automation/campaign that fired it.
func (p *StreamingPublisher) PublishCustomEvent(ctx context.Context, orgID, actorID uuid.UUID, name string, payload map[string]string, source, sourceID string) {
	if p == nil || p.client == nil || orgID == uuid.Nil {
		return
	}
	event := &CustomEvent{
		BaseEvent: BaseEvent{
			EventType: EventCustomFired,
			UserID:    actorID.String(),
			Timestamp: time.Now(),
		},
		OrgID:    orgID.String(),
		Name:     name,
		Payload:  payload,
		Source:   source,
		SourceID: sourceID,
	}
	attrs := map[string]string{
		"user_id":    actorID.String(),
		"org_id":     orgID.String(),
		"event_type": string(EventCustomFired),
	}
	if err := p.client.Publish(ctx, TopicUserEvents, event, attrs); err != nil {
		// Best-effort: realtime is a nicety, not a requirement.
	}
}

// PresencePolicyEvent tells the realtime service to re-gate team presence for an
// org live, so a privacy toggle applies without waiting for members to reconnect.
// Handled inside the OrgChannel (not pushed to web clients).
type PresencePolicyEvent struct {
	BaseEvent
	OrgID                string `json:"org_id"`
	PresenceShowOnline   bool   `json:"presence_show_online"`
	PresenceShowActivity bool   `json:"presence_show_activity"`
}

// PublishPresencePolicy emits an org-scoped presence policy change so connected
// OrgChannels re-evaluate tracking immediately.
func (p *StreamingPublisher) PublishPresencePolicy(ctx context.Context, orgID uuid.UUID, showOnline, showActivity bool) {
	if p == nil || p.client == nil || orgID == uuid.Nil {
		return
	}
	event := &PresencePolicyEvent{
		BaseEvent: BaseEvent{
			EventType: EventPresencePolicyUpdated,
			Timestamp: time.Now(),
		},
		OrgID:                orgID.String(),
		PresenceShowOnline:   showOnline,
		PresenceShowActivity: showActivity,
	}
	attrs := map[string]string{
		"org_id":     orgID.String(),
		"event_type": string(EventPresencePolicyUpdated),
	}
	if err := p.client.Publish(ctx, TopicUserEvents, event, attrs); err != nil {
		// Best-effort: realtime is a nicety, not a requirement.
	}
}

// PublishToUser publishes a generic event to a user
func (p *StreamingPublisher) PublishToUser(ctx context.Context, userID string, event interface{}) {
	if p.client == nil {
		return
	}

	attrs := map[string]string{
		"user_id": userID,
	}

	if err := p.client.Publish(ctx, TopicUserEvents, event, attrs); err != nil {
		// Log error but don't fail
	}
}

// NotificationEvent is the user-scoped realtime signal for a new in-app
// notification (the bell). Best-effort; the feed table is the source of truth.
type NotificationEvent struct {
	BaseEvent
	NotificationID string `json:"notification_id"`
	Category       string `json:"category"`
	Title          string `json:"title"`
	Link           string `json:"link,omitempty"`
}

// PublishNotificationCreated pushes a new-notification event to a single user.
func (p *StreamingPublisher) PublishNotificationCreated(ctx context.Context, userID, notifID, category, title, link string) {
	if p == nil || p.client == nil || userID == "" {
		return
	}
	event := &NotificationEvent{
		BaseEvent: BaseEvent{
			EventType: EventNotificationCreated,
			UserID:    userID,
			Timestamp: time.Now(),
		},
		NotificationID: notifID,
		Category:       category,
		Title:          title,
		Link:           link,
	}
	attrs := map[string]string{
		"user_id":    userID,
		"event_type": string(EventNotificationCreated),
	}
	if err := p.client.Publish(ctx, TopicUserEvents, event, attrs); err != nil {
		// Best-effort: realtime is a nicety, not a requirement.
	}
}

// PublishTrackingEvent publishes an email open/click tracking event
func (p *StreamingPublisher) PublishTrackingEvent(ctx context.Context, event *TrackingEventPayload) {
	if p.client == nil {
		return
	}

	event.Timestamp = time.Now()

	attrs := map[string]string{
		"user_id":     event.UserID,
		"campaign_id": event.CampaignID,
		"event_type":  string(event.EventType),
	}

	if err := p.client.Publish(ctx, TopicCampaignUpdate, event, attrs); err != nil {
		// Log error but don't fail
	}
}

// PublishTaskProgress publishes a detailed task progress event
func (p *StreamingPublisher) PublishTaskProgress(ctx context.Context, event *TaskProgressEvent) {
	if p.client == nil {
		return
	}

	event.EventType = EventTaskProgress
	event.Timestamp = time.Now()

	attrs := map[string]string{
		"user_id":     event.UserID,
		"campaign_id": event.CampaignID,
		"task_id":     event.TaskID,
		"event_type":  string(EventTaskProgress),
	}

	if err := p.client.Publish(ctx, TopicCampaignUpdate, event, attrs); err != nil {
		// Log error but don't fail
	}
}

// PublishEmailReplied emits an org-scoped EMAIL_REPLIED pulse when a human
// reply lands for a campaign contact. Primitive-typed so app packages can wire
// it through a narrow local interface.
func (p *StreamingPublisher) PublishEmailReplied(ctx context.Context, orgID, userID, campaignID, contactID, contactEmail, sequenceID string) {
	if p == nil || p.client == nil {
		return
	}
	p.PublishTrackingEvent(ctx, &TrackingEventPayload{
		BaseEvent: BaseEvent{
			EventType: EventEmailReplied,
			UserID:    userID,
		},
		OrgID:        orgID,
		CampaignID:   campaignID,
		ContactID:    contactID,
		ContactEmail: contactEmail,
		SequenceID:   sequenceID,
	})
}

// PublishEmailSent emits an org-scoped EMAIL_SENT pulse when a campaign email
// goes out, carrying the same rich payload as task progress so the dashboard
// can show which contact/step just fired without a refetch.
func (p *StreamingPublisher) PublishEmailSent(ctx context.Context, event *TaskProgressEvent) {
	if p == nil || p.client == nil {
		return
	}

	event.EventType = EventEmailSent
	event.Timestamp = time.Now()

	attrs := map[string]string{
		"user_id":     event.UserID,
		"campaign_id": event.CampaignID,
		"event_type":  string(EventEmailSent),
	}

	if err := p.client.Publish(ctx, TopicCampaignUpdate, event, attrs); err != nil {
		// Best-effort: realtime is a nicety, not a requirement.
	}
}

// Subscription info for clients
type RealtimeSubscriptionInfo struct {
	WebsocketURL string   `json:"websocket_url"`
	Topics       []string `json:"topics"`
}

// GetSubscriptionInfo returns the subscription info for a user
func GetSubscriptionInfo(userID uuid.UUID, wsHost string) *RealtimeSubscriptionInfo {
	return &RealtimeSubscriptionInfo{
		WebsocketURL: wsHost + "/socket",
		Topics: []string{
			"user:" + userID.String(),
			"campaign:*",
			"account:*",
		},
	}
}
