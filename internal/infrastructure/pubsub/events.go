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
	EventAccountConnected    EventType = "ACCOUNT_CONNECTED"
	EventAccountDisconnected EventType = "ACCOUNT_DISCONNECTED"
	EventAccountError        EventType = "ACCOUNT_ERROR"
	EventAccountSynced       EventType = "ACCOUNT_SYNCED"

	// Bulk operation events
	EventBulkStarted   EventType = "BULK_STARTED"
	EventBulkProgress  EventType = "BULK_PROGRESS"
	EventBulkCompleted EventType = "BULK_COMPLETED"
	EventBulkFailed    EventType = "BULK_FAILED"

	// Tracking events (from Rust tracking service)
	EventEmailOpened  EventType = "EMAIL_OPENED"
	EventEmailClicked EventType = "EMAIL_CLICKED"

	// Task progress events
	EventTaskProgress EventType = "TASK_PROGRESS"
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
	EmailAccountID string `json:"email_account_id"`
	Email          string `json:"email"`
	Provider       string `json:"provider,omitempty"`
	Status         string `json:"status,omitempty"`
	ErrorCode      string `json:"error_code,omitempty"`
	ErrorMessage   string `json:"error_message,omitempty"`
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
	CampaignID   string `json:"campaign_id"`
	ContactID    string `json:"contact_id,omitempty"`
	ContactEmail string `json:"contact_email,omitempty"`
	SequenceID   string `json:"sequence_id,omitempty"`
	OriginalURL  string `json:"original_url,omitempty"` // For click events
}

// TaskProgressEvent for detailed campaign task progress
type TaskProgressEvent struct {
	BaseEvent
	CampaignID     string `json:"campaign_id"`
	TaskID         string `json:"task_id"`
	Status         string `json:"status"` // pending, active, completed, failed
	ContactID      string `json:"contact_id"`
	ContactEmail   string `json:"contact_email"`
	ContactName    string `json:"contact_name"`
	SequenceID     string `json:"sequence_id"`
	SequenceName   string `json:"sequence_name"`
	SequenceIndex  int    `json:"sequence_index"`
	Progress       int    `json:"progress"` // Percentage 0-100
	TotalContacts  int    `json:"total_contacts"`
	ProcessedCount int    `json:"processed_count"`
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
