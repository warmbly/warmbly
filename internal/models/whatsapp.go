package models

import (
	"time"

	"github.com/google/uuid"
)

// WhatsApp contact channel state (consent, phone provenance, service window).
// Stored per organization + contact. Public phone never implies opt-in.
type WhatsAppContactState struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	ContactID      *uuid.UUID `json:"contact_id,omitempty"`
	// Optional link to outreach staging candidate when confenge is enabled.
	OutreachCandidateID *uuid.UUID `json:"outreach_candidate_id,omitempty"`

	PhoneRaw        string     `json:"phone_raw,omitempty"`
	PhoneE164       string     `json:"phone_e164,omitempty"`
	PhoneCountry    string     `json:"phone_country,omitempty"`
	PhoneValid      bool       `json:"phone_valid"`
	PhoneSource     string     `json:"phone_source,omitempty"`
	PhoneSourceURL  string     `json:"phone_source_url,omitempty"`
	PhoneVerifiedAt *time.Time `json:"phone_verified_at,omitempty"`

	ConsentStatus       string     `json:"whatsapp_consent_status"`
	ConsentSource       string     `json:"whatsapp_consent_source,omitempty"`
	ConsentAt           *time.Time `json:"whatsapp_consent_at,omitempty"`
	ConsentScope        string     `json:"whatsapp_consent_scope,omitempty"`
	ConsentProvenanceOK bool       `json:"whatsapp_consent_provenance_ok"`
	ConsentFormVersion  string     `json:"whatsapp_consent_form_version,omitempty"`
	ConsentRecordedBy   *uuid.UUID `json:"whatsapp_consent_recorded_by,omitempty"`

	LastInboundAt      *time.Time `json:"whatsapp_last_inbound_at,omitempty"`
	ServiceWindowUntil *time.Time `json:"whatsapp_service_window_until,omitempty"`
	ChannelStatus      string     `json:"whatsapp_channel_status,omitempty"`
	OptOutAt           *time.Time `json:"whatsapp_opt_out_at,omitempty"`
	DoNotContact       bool       `json:"do_not_contact"`

	LastEmailOutboundAt    *time.Time `json:"last_email_outbound_at,omitempty"`
	LastWhatsAppOutboundAt *time.Time `json:"last_whatsapp_outbound_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// WhatsAppMessage is one inbound or outbound WhatsApp message in the CRM history.
type WhatsAppMessage struct {
	ID                uuid.UUID  `json:"id"`
	OrganizationID    uuid.UUID  `json:"organization_id"`
	ContactID         *uuid.UUID `json:"contact_id,omitempty"`
	ThreadKey         string     `json:"thread_key"` // typically phone E.164
	Direction         string     `json:"direction"`  // inbound | outbound
	Channel           string     `json:"channel"`    // WHATSAPP
	Provider          string     `json:"provider"`
	ProviderMessageID string     `json:"provider_message_id,omitempty"`
	IdempotencyKey    string     `json:"idempotency_key"`
	BodyText          string     `json:"body_text,omitempty"`
	TemplateName      string     `json:"template_name,omitempty"`
	TemplateLanguage  string     `json:"template_language,omitempty"`
	Status            string     `json:"status"` // received | queued | sent | delivered | read | failed
	FailureCode       string     `json:"failure_code,omitempty"`
	DraftID           *uuid.UUID `json:"draft_id,omitempty"`
	CampaignID        *uuid.UUID `json:"campaign_id,omitempty"`
	ReviewedBy        *uuid.UUID `json:"reviewed_by,omitempty"`
	ApprovedAt        *time.Time `json:"approved_at,omitempty"`
	SentAt            *time.Time `json:"sent_at,omitempty"`
	OccurredAt        time.Time  `json:"occurred_at"`
	CreatedAt         time.Time  `json:"created_at"`
}

// WhatsAppInstance maps an Evolution (or other) instance to an organization.
type WhatsAppInstance struct {
	ID              uuid.UUID `json:"id"`
	OrganizationID  uuid.UUID `json:"organization_id"`
	Provider        string    `json:"provider"`
	InstanceName    string    `json:"instance_name"`
	IntegrationMode string    `json:"integration_mode"` // WHATSAPP-BUSINESS | WHATSAPP-BAILEYS
	PhoneE164       string    `json:"phone_e164,omitempty"`
	WebhookSecret   string    `json:"-"` // never JSON-serialize secret
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// WhatsAppTemplate is a local mirror of an official Meta template approval state.
// Meta/BSP remains source of truth; this is operational cache only.
type WhatsAppTemplate struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	Provider       string     `json:"provider"`
	ExternalID     string     `json:"external_id,omitempty"`
	Name           string     `json:"name"`
	Language       string     `json:"language"`
	Category       string     `json:"category,omitempty"`
	Status         string     `json:"status"` // APPROVED | PAUSED | REJECTED | PENDING
	VariablesJSON  []byte     `json:"variables,omitempty"`
	LastSyncAt     *time.Time `json:"last_sync_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// WhatsAppWebhookEvent is the idempotency log for provider webhooks.
type WhatsAppWebhookEvent struct {
	ID             uuid.UUID `json:"id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	Provider       string    `json:"provider"`
	IdempotencyKey string    `json:"idempotency_key"`
	EventType      string    `json:"event_type"`
	ExternalMsgID  string    `json:"external_message_id,omitempty"`
	PayloadHash    string    `json:"payload_hash,omitempty"`
	ProcessedAt    time.Time `json:"processed_at"`
}
