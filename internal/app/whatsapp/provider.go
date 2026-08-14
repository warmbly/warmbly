// Package whatsapp is the policy-gated WhatsApp channel transport for Warmbly.
// Domain code talks only to the Provider interface; provider-specific DTOs
// stay inside adapter packages (e.g. evolution).
package whatsapp

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Provider is the transport abstraction. Implementations: Evolution (Cloud API
// first), mock, and future Meta/BSP adapters. Domain never imports provider DTOs.
type Provider interface {
	// SendText sends free-form text inside an open service window or after
	// policy has approved free-text. Callers must run eligibility first.
	SendText(ctx context.Context, req SendTextRequest) (*SendResult, error)
	// SendTemplate sends an approved WhatsApp Business Platform template.
	SendTemplate(ctx context.Context, req SendTemplateRequest) (*SendResult, error)
	// GetConnectionStatus reports instance/WABA connection health.
	GetConnectionStatus(ctx context.Context, instance string) (*ConnectionStatus, error)
	// ConfigureWebhook registers the Warmbly ingress URL on the provider when supported.
	ConfigureWebhook(ctx context.Context, instance string, cfg WebhookConfig) error
	// Health checks provider reachability without side effects.
	Health(ctx context.Context) error
	// Name returns a stable provider id (e.g. "evolution", "mock").
	Name() string
}

// SendTextRequest is a domain send of free-form text.
type SendTextRequest struct {
	Instance       string
	ToE164         string // digits with country code, no leading +
	Body           string
	IdempotencyKey string
	// OrganizationID is for multi-tenant logging/audit only; not sent to provider.
	OrganizationID uuid.UUID
	ContactID      *uuid.UUID
}

// SendTemplateRequest is a domain send of an official template.
type SendTemplateRequest struct {
	Instance       string
	ToE164         string
	TemplateName   string
	Language       string
	Variables      []string
	IdempotencyKey string
	OrganizationID uuid.UUID
	ContactID      *uuid.UUID
}

// SendResult is the provider-normalized send outcome.
type SendResult struct {
	ProviderMessageID string
	Status            string // queued | sent | failed
	RawStatus         string // provider status string (never secrets)
	OccurredAt        time.Time
}

// ConnectionStatus is instance/connection health.
type ConnectionStatus struct {
	Instance  string
	State     string // open | close | connecting | unknown
	PhoneE164 string
	UpdatedAt time.Time
}

// WebhookConfig is what Warmbly asks the provider to post to.
type WebhookConfig struct {
	URL     string
	Secret  string
	Events  []string
	Enabled bool
}

// Consent statuses for WhatsApp (first-class, never inferred from public phone).
const (
	ConsentUnknown       = "UNKNOWN"
	ConsentNoOptIn       = "NO_OPT_IN"
	ConsentOptedIn       = "OPTED_IN"
	ConsentUserInitiated = "USER_INITIATED"
	ConsentOptedOut      = "OPTED_OUT"
	ConsentDoNotContact  = "DO_NOT_CONTACT"
)

// Eligibility decisions returned by the policy engine.
const (
	EligAutomatedAllowed = "AUTOMATED_ALLOWED"
	EligTemplateOnly     = "TEMPLATE_ONLY"
	EligServiceWindow    = "SERVICE_WINDOW"
	EligManualReview     = "MANUAL_REVIEW"
	EligBlocked          = "BLOCKED"
)

// Message / draft channel.
const (
	ChannelEmail    = "EMAIL"
	ChannelWhatsApp = "WHATSAPP"
)

// Normalized event types (provider-agnostic).
const (
	EventMessageReceived  = "MESSAGE_RECEIVED"
	EventMessageSent      = "MESSAGE_SENT"
	EventMessageDelivered = "MESSAGE_DELIVERED"
	EventMessageRead      = "MESSAGE_READ"
	EventMessageFailed    = "MESSAGE_FAILED"
	EventConnectionState  = "CONNECTION_STATE"
	EventTemplateError    = "TEMPLATE_ERROR"
	EventUnsupported      = "UNSUPPORTED"
)

// Provider ids.
const (
	ProviderEvolution = "evolution"
	ProviderMock      = "mock"
)

// Template statuses (WhatsApp Business Platform).
const (
	TemplateApproved = "APPROVED"
	TemplatePaused   = "PAUSED"
	TemplateRejected = "REJECTED"
	TemplatePending  = "PENDING"
	TemplateMissing  = "MISSING"
)

// Message content types we handle.
const (
	ContentText  = "text"
	ContentOther = "other"
)
