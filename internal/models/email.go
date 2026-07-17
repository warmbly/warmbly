package models

import (
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

type InboxProvider string

const (
	InboxProviderGoogle   InboxProvider = "gmail"
	InboxProviderOutlook  InboxProvider = "outlook"
	InboxProviderSMTPIMAP InboxProvider = "smtp_imap"
)

type Email struct {
	ID             uuid.UUID  `json:"id"`
	UserID         string     `json:"user_id"`
	OrganizationID *uuid.UUID `json:"organization_id,omitempty"`
	WorkerID       *uuid.UUID `json:"worker_id"`
	Email          string     `json:"email"`

	Name           string `json:"name"`
	SignaturePlain string `json:"signature_plain"`
	SignatureHTML  string `json:"signature_html"`
	SignatureSync  bool   `json:"signature_sync"`
	SignatureCode  bool   `json:"signature_code"`

	Provider string `json:"provider"`
	Status   string `json:"status"`

	LastSyncedAt time.Time `json:"last_synced_at"`
	LastID       *int64    `json:"last_id"`

	CampaignLimit int    `json:"campaign_limit"`
	MinWaitTime   int    `json:"min_wait_time"`
	ReplyTo       string `json:"reply_to"`

	TrackingDomain           string     `json:"tracking_domain"`
	TrackingDomainVerified   bool       `json:"tracking_domain_verified"`
	TrackingDomainVerifiedAt *time.Time `json:"tracking_domain_verified_at"`

	// Sending-domain authentication (SPF/DKIM/DMARC), refreshed by the
	// background auth-check sweep. Observe-only: surfaced to warn about
	// unauthenticated domains, not yet a hard send gate. AuthState is "unknown"
	// until checked (or when a DNS lookup failed transiently), distinct from a
	// real "failing".
	AuthState       string     `json:"auth_state"`
	AuthSPF         bool       `json:"auth_spf"`
	AuthDKIM        bool       `json:"auth_dkim"`
	AuthDMARC       bool       `json:"auth_dmarc"`
	AuthDMARCPolicy string     `json:"auth_dmarc_policy,omitempty"`
	AuthReason      string     `json:"auth_reason,omitempty"`
	AuthCheckedAt   *time.Time `json:"auth_checked_at,omitempty"`

	Warmup          *time.Time `json:"warmup"`
	WarmupPausedAt  *time.Time `json:"warmup_paused_at"`
	WarmupBase      int        `json:"warmup_base"`
	WarmupMax       int        `json:"warmup_max"`
	WarmupIncrease  int        `json:"warmup_increase"`
	WarmupReplyRate int        `json:"warmup_reply_rate"`
	WarmupTag       string     `json:"warmup_tag"`
	WarmupPoolType  string     `json:"warmup_pool_type"`
	WarmupStartTime string     `json:"warmup_start_time"`
	WarmupEndTime   string     `json:"warmup_end_time"`
	WarmupDays      int        `json:"warmup_days"`

	Timezone string `json:"timezone"`

	Tags []string `json:"tags"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// IsWarmingActive reports whether the mailbox is actively warming up: warmup
// has been enabled (anchor set) and is not currently paused. The scheduler,
// task runner, and analytics all key off this rather than the raw Warmup
// pointer so a paused mailbox is treated as "not sending normal warmup" while
// still preserving its ramp progress.
func (e *Email) IsWarmingActive() bool {
	return e.Warmup != nil && e.WarmupPausedAt == nil
}

// IsWarmupPaused reports whether warmup is enabled but paused. A paused
// mailbox keeps its ramp progress (the anchor is shifted forward on resume).
func (e *Email) IsWarmupPaused() bool {
	return e.Warmup != nil && e.WarmupPausedAt != nil
}

// EmailAuthTarget is a mailbox due for a sending-domain authentication check,
// returned to the background sweep. Auth is a per-domain property, so the sweep
// dedupes these by the domain part of Email before running DNS lookups.
type EmailAuthTarget struct {
	ID    uuid.UUID
	Email string
}

type Service struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
}

type Oauth2Service struct {
	Host  string             `json:"host"`
	Port  int                `json:"port"`
	Token oauth2.TokenSource `json:"token"`
}

type SmtpImap struct {
	SMTP *Service `json:"smtp"`
	IMAP *Service `json:"imap"`
}

type Oauth2SmtpImap struct {
	SMTP *Oauth2Service `json:"smtp"`
	IMAP *Oauth2Service `json:"imap"`
}

type NewOauthAccount struct {
	OrganizationID *uuid.UUID
	Provider       InboxProvider
	Name           string
	Email          string
	AccessToken    string
	RefreshToken   string
	ExpiresAt      time.Time
}

type NewSMTPIMAPAccount struct {
	OrganizationID *uuid.UUID
	Name           string
	Email          string
	SMTP           *Service
	IMAP           *Service
}

// EmailOnboardingState is stored in Redis for the lifetime of an OAuth round trip.
type EmailOnboardingState struct {
	UserID         string     `json:"user_id"`
	OrganizationID *uuid.UUID `json:"organization_id,omitempty"`
	Provider       string     `json:"provider"`
	Nonce          string     `json:"nonce"`
}

// EmailOnboardingStartResponse is returned from POST /emails/onboarding/oauth/start.
type EmailOnboardingStartResponse struct {
	URL   string `json:"url"`
	State string `json:"state"`
}

type EmailsResult struct {
	Data       []Email    `json:"data"`
	Pagination Pagination `json:"pagination"`
}

// TrackingDomainStatus is returned after a tracking-domain update. The
// backend resolves the CNAME on save; Verified is true once the
// customer's subdomain points at the shared tracking host.
type TrackingDomainStatus struct {
	TrackingDomain           string     `json:"tracking_domain"`
	TrackingDomainVerified   bool       `json:"tracking_domain_verified"`
	TrackingDomainVerifiedAt *time.Time `json:"tracking_domain_verified_at"`
}

type UpdateEmail struct {
	Name *string `json:"name"`

	SignaturePlain *string `json:"signature_plain"`
	SignatureHTML  *string `json:"signature_html"`
	SignatureSync  *bool   `json:"signature_sync"`
	SignatureCode  *bool   `json:"signature_code"`

	Status *string `json:"status"` // active, inactive, revoked

	CampaignLimit *int    `json:"campaign_limit"`
	MinWaitTime   *int    `json:"min_wait_time"`
	ReplyTo       *string `json:"reply_to"`

	Warmup          *bool   `json:"warmup"`
	WarmupBase      *int    `json:"warmup_base"`
	WarmupMax       *int    `json:"warmup_max"`
	WarmupIncrease  *int    `json:"warmup_increase"`
	WarmupReplyRate *int    `json:"warmup_reply_rate"`
	WarmupTag       *string `json:"warmup_tag"`
	WarmupStartTime *string `json:"warmup_start_time"`
	WarmupEndTime   *string `json:"warmup_end_time"`
	WarmupDays      *int    `json:"warmup_days"`

	Tags []string `json:"tags"`
}
