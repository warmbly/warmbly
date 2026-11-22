package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ManagedPlanID is the private plan every partner-managed workspace runs on (migration 000404).
var ManagedPlanID = uuid.MustParse("00000000-0000-0000-0000-0000000000c1")

// PartnerManagedPlanKey is the plan key a partner sends for ManagedPlanID.
const PartnerManagedPlanKey = "graphkit_managed"

// PartnerPlanIDs maps the plan keys a partner may send to the plan they run on.
var PartnerPlanIDs = map[string]uuid.UUID{PartnerManagedPlanKey: ManagedPlanID}

// PartnerMode is live or test, decided by the partner key a request presents.
type PartnerMode string

const (
	PartnerModeLive PartnerMode = "live"
	PartnerModeTest PartnerMode = "test"
)

func (m PartnerMode) Valid() bool { return m == PartnerModeLive || m == PartnerModeTest }

// Livemode is the webhook envelope's livemode flag.
func (m PartnerMode) Livemode() bool { return m == PartnerModeLive }

type PartnerStatus string

const (
	PartnerStatusActive    PartnerStatus = "active"
	PartnerStatusSuspended PartnerStatus = "suspended"
)

// PartnerOrgState is a managed workspace's standing with its partner.
type PartnerOrgState string

const (
	PartnerOrgActive    PartnerOrgState = "active"
	PartnerOrgSuspended PartnerOrgState = "suspended"
	PartnerOrgCancelled PartnerOrgState = "cancelled"
)

// Paused reports whether the workspace's sending and warmup are held by its partner.
func (s PartnerOrgState) Paused() bool { return s == PartnerOrgSuspended || s == PartnerOrgCancelled }

// PartnerSuspendReasons are the reasons a partner may give for a suspension.
var PartnerSuspendReasons = map[string]bool{"billing": true, "admin": true, "reseller": true}

// PartnerCancelRetention is how long a cancelled workspace is kept before it is deleted.
const PartnerCancelRetention = OrganizationDeletionGraceDays * 24 * time.Hour

// Partner is a reseller that runs workspaces through the partner API.
type Partner struct {
	ID            uuid.UUID     `json:"id"`
	Slug          string        `json:"slug"`
	Name          string        `json:"name"`
	DisplayName   string        `json:"display_name"`
	Status        PartnerStatus `json:"status"`
	ServiceUserID uuid.UUID     `json:"service_user_id"`
	AllowedPlans  []string      `json:"allowed_plans"`
	AllowedHosts  []string      `json:"allowed_hosts"`
	// DelegationDomains are domains an operator vouches the partner controls; only these may gain a grant through it.
	DelegationDomains []string `json:"delegation_domains"`
	WebhookURL        *string  `json:"webhook_url"`
	// The sealed secrets never leave the server; these say whether one is set.
	WebhookSecretSet          bool       `json:"webhook_secret_set"`
	WebhookSecretRotatedAt    *time.Time `json:"webhook_secret_rotated_at,omitempty"`
	CallbackBaseURL           *string    `json:"callback_base_url"`
	CallbackKeySet            bool       `json:"callback_key_set"`
	WholesaleSendingWarmupDay int64      `json:"wholesale_sending_warmup_day_micros"`
	WholesaleWarmupOnlyDay    int64      `json:"wholesale_warmup_only_day_micros"`
	RateLimitPerSecond        int        `json:"rate_limit_per_second"`
	CreatedAt                 time.Time  `json:"created_at"`
	UpdatedAt                 time.Time  `json:"updated_at"`
}

// PartnerSecrets are a partner's secrets as stored, sealed with the instance key.
type PartnerSecrets struct {
	WebhookSecret, WebhookSecretPrevious, CallbackKey string
	WebhookSecretRotatedAt                            *time.Time
}

// PartnerAPIKey is one partner key; the secret is shown once and stored hashed.
type PartnerAPIKey struct {
	ID         uuid.UUID   `json:"id"`
	PartnerID  uuid.UUID   `json:"partner_id"`
	Mode       PartnerMode `json:"mode"`
	Name       string      `json:"name"`
	KeyPrefix  string      `json:"key_prefix"`
	KeySuffix  string      `json:"key_suffix"`
	LastUsedAt *time.Time  `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time  `json:"revoked_at,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
}

// PartnerAPIKeyWithSecret is a key at creation, the only time its secret exists.
type PartnerAPIKeyWithSecret struct {
	PartnerAPIKey
	Secret string `json:"secret"`
}

// PartnerOrganization links a workspace to the partner that manages it.
type PartnerOrganization struct {
	ID                            uuid.UUID       `json:"id"`
	PartnerID                     uuid.UUID       `json:"partner_id"`
	Mode                          PartnerMode     `json:"mode"`
	ExternalID                    string          `json:"external_id"`
	OrganizationID                *uuid.UUID      `json:"organization_id"`
	PlanKey                       string          `json:"plan_key"`
	State                         PartnerOrgState `json:"state"`
	SuspendedReason               *string         `json:"suspended_reason"`
	CampaignSendsPerMailboxPerDay int             `json:"campaign_sends_per_mailbox_per_day"`
	GroupExternalID               *string         `json:"group_external_id"`
	GroupName                     *string         `json:"group_name"`
	GroupBranding                 json.RawMessage `json:"group_branding,omitempty"`
	CancelledAt                   *time.Time      `json:"cancelled_at"`
	DeletesAt                     *time.Time      `json:"deletes_at"`
	CreatedAt                     time.Time       `json:"created_at"`
	UpdatedAt                     time.Time       `json:"updated_at"`
	// OrganizationName is joined in for admin lists.
	OrganizationName string `json:"organization_name,omitempty"`
}

// PartnerIdentity binds a person at a partner to the account they sign in as.
type PartnerIdentity struct {
	ID         uuid.UUID   `json:"id"`
	PartnerID  uuid.UUID   `json:"partner_id"`
	Mode       PartnerMode `json:"mode"`
	ExternalID string      `json:"external_id"`
	UserID     uuid.UUID   `json:"user_id"`
	// Email is the person's contact email as the partner last gave it ("" when none): shown, never mailed or signed in with.
	Email string `json:"email"`
	Name  string `json:"name"`
}

// PartnerManagedRef is what a managed workspace's members see of the partner billing it.
type PartnerManagedRef struct {
	DisplayName     string          `json:"display_name"`
	State           PartnerOrgState `json:"state"`
	SuspendedReason *string         `json:"suspended_reason,omitempty"`
	DeletesAt       *time.Time      `json:"deletes_at,omitempty"`
	// Test is a test-mode workspace, which sends no real mail.
	Test bool `json:"-"`
}

// PartnerEvent is one event for a partner, as delivered and as listed by GET /events.
type PartnerEvent struct {
	Seq                    int64           `json:"-"`
	ID                     string          `json:"id"`
	PartnerID              uuid.UUID       `json:"-"`
	Mode                   PartnerMode     `json:"-"`
	Type                   string          `json:"type"`
	OrganizationExternalID *string         `json:"organization_external_id"`
	Data                   json.RawMessage `json:"data"`
	CreatedAt              time.Time       `json:"created_at"`
	Attempts               int             `json:"-"`
}

// Partner event types. Readers ignore types they do not know.
const (
	PartnerEventOrganizationUpdated = "organization.updated"
	PartnerEventOrganizationDeleted = "organization.deleted"
	PartnerEventMailboxCreated      = "mailbox.created"
	PartnerEventMailboxUpdated      = "mailbox.updated"
	PartnerEventMailboxDeleted      = "mailbox.deleted"
)
