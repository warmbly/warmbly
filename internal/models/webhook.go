package models

import (
	"encoding/json"
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
}

func IsValidWebhookEventType(s string) bool {
	for _, t := range AllWebhookEventTypes {
		if string(t) == s {
			return true
		}
	}
	return false
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
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// WebhookEndpointWithSecret is returned once at creation time so the
// client can capture the secret. Subsequent reads do not include it.
type WebhookEndpointWithSecret struct {
	WebhookEndpoint
	Secret string `json:"secret"`
}

// Subscribes returns true if this endpoint should receive the given event.
// An endpoint with an empty event_types array receives all events.
func (e *WebhookEndpoint) Subscribes(event WebhookEventType) bool {
	if !e.Enabled {
		return false
	}
	if len(e.EventTypes) == 0 {
		return true
	}
	for _, t := range e.EventTypes {
		if t == string(event) {
			return true
		}
	}
	return false
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

// WebhookPayload is the JSON body the dispatcher POSTs to a subscriber.
type WebhookPayload struct {
	ID             uuid.UUID        `json:"id"`
	EventType      WebhookEventType `json:"event_type"`
	OrganizationID uuid.UUID        `json:"organization_id"`
	CreatedAt      time.Time        `json:"created_at"`
	Data           any              `json:"data"`
}
