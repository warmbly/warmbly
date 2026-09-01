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

// Sending-domain authentication states, mirroring the email_accounts.auth_state
// CHECK constraint. "unknown" is deliberately distinct from "failing": it means
// not checked yet or the DNS lookup could not complete, and never gates.
const (
	AuthStateUnknown = "unknown"
	AuthStatePassing = "passing"
	AuthStateFailing = "failing"
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
	// background auth-check sweep. AuthState is "unknown" until checked (or
	// when a DNS lookup failed transiently), distinct from a real "failing".
	// A sustained "failing" gates cold sending and warmup; see
	// DomainAuthBlocked for when that becomes enforceable.
	AuthState       string     `json:"auth_state"`
	AuthSPF         bool       `json:"auth_spf"`
	AuthDKIM        bool       `json:"auth_dkim"`
	AuthDMARC       bool       `json:"auth_dmarc"`
	AuthDMARCPolicy string     `json:"auth_dmarc_policy,omitempty"`
	AuthReason      string     `json:"auth_reason,omitempty"`
	AuthCheckedAt   *time.Time `json:"auth_checked_at,omitempty"`
	// AuthFailingSince is when the domain entered "failing". The grace window
	// runs from here, so a resolver hiccup or a record broken minutes ago
	// cannot stop sending immediately.
	AuthFailingSince *time.Time `json:"auth_failing_since,omitempty"`

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

	// SaveToSent applies to SMTP/IMAP mailboxes only: after a send, the worker
	// APPENDs a copy to the mailbox's Sent folder. Gmail and Outlook file their
	// own copy, so the flag is ignored for them.
	SaveToSent bool `json:"save_to_sent"`

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

// DomainAuthBlocked reports whether this mailbox's sending domain has been
// failing authentication long enough to stop cold sends and warmup sends.
// Only a sustained "failing" gates: "unknown", an unstamped clock, and
// anything inside the grace window all pass through.
func (e *Email) DomainAuthBlocked(now time.Time, grace time.Duration) bool {
	if e.AuthState != AuthStateFailing || e.AuthFailingSince == nil {
		return false
	}
	return !now.Before(e.AuthFailingSince.Add(grace))
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

// TrackingDomainTarget is a mailbox with a custom tracking domain that is due
// to be re-resolved. DNS is not static: a domain verified once and never
// re-checked keeps routing links long after its record changed, and a domain
// that failed to verify has no other way back once it propagates.
type TrackingDomainTarget struct {
	ID       uuid.UUID
	Domain   string
	Verified bool
}

// EmailAuthTransition is a mailbox that just entered the failing state, so the
// grace clock started on this pass. The sweep notifies its organization once
// per transition; a domain that stays failing reports nothing on later passes
// because auth_failing_since is preserved, which is the whole dedupe.
type EmailAuthTransition struct {
	ID             uuid.UUID
	Email          string
	OrganizationID *uuid.UUID
}

// Mail connection security modes. TLS is mandatory either way; the difference
// is whether it is negotiated before the protocol greeting or upgraded in-band
// after it. Storing the mode explicitly is what lets a mailbox live on any
// port: inferring it from the port only ever worked for 465/587/993/143.
const (
	// MailSecurityTLS is implicit TLS: the server speaks TLS from the first
	// byte. SMTP 465 (SMTPS), IMAP 993 (IMAPS).
	MailSecurityTLS = "tls"
	// MailSecurityStartTLS is a plaintext greeting upgraded in-band with
	// STARTTLS. SMTP 587/25/2525, IMAP 143.
	MailSecurityStartTLS = "starttls"
)

// ValidMailSecurity reports whether s is a known security mode.
func ValidMailSecurity(s string) bool {
	return s == MailSecurityTLS || s == MailSecurityStartTLS
}

// ResolveSMTPSecurity returns the security mode to dial SMTP with: the stored
// choice when it is set, otherwise the conventional default for the port. The
// fallback keeps mailboxes connected across the rollout, when the stored value
// is empty and events from older workers carry no mode at all.
func ResolveSMTPSecurity(security string, port int) string {
	if ValidMailSecurity(security) {
		return security
	}
	if port == 465 {
		return MailSecurityTLS
	}
	return MailSecurityStartTLS
}

// ResolveIMAPSecurity is ResolveSMTPSecurity for IMAP, where implicit TLS is
// the norm (993) and 143 is the STARTTLS port.
func ResolveIMAPSecurity(security string, port int) string {
	if ValidMailSecurity(security) {
		return security
	}
	if port == 143 {
		return MailSecurityStartTLS
	}
	return MailSecurityTLS
}

type Service struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	// Security is the connection mode (see MailSecurity*). Empty means "infer
	// from the port", which is how rows and events written before the field
	// existed behave.
	Security string `json:"security,omitempty"`
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
	// EmailAccountID marks a re-authorization round trip: the finish leg
	// renews this mailbox's tokens instead of connecting a new one.
	EmailAccountID *uuid.UUID `json:"email_account_id,omitempty"`
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

// TrackingDomainStatus is the state of a custom open/click tracking domain.
// The backend resolves the record on save and on an explicit verify; Verified
// is true once the customer's subdomain points at this install's tracking host.
//
// Everything below Verified is diagnostic. A bare verified flag left the
// customer with a "Pending DNS" badge and nothing to act on, which is what
// issue #173 was.
type TrackingDomainStatus struct {
	TrackingDomain           string     `json:"tracking_domain"`
	TrackingDomainVerified   bool       `json:"tracking_domain_verified"`
	TrackingDomainVerifiedAt *time.Time `json:"tracking_domain_verified_at"`

	// CNAMETarget is the value to put in the CNAME: this install's tracking
	// host (TRACKING_DOMAIN). Empty means the install has no tracking host, so
	// there is nothing to point at and nothing can verify.
	CNAMETarget string `json:"cname_target"`

	// Status is stable and machine-readable: verified, unset, no_target,
	// not_found, wrong_target, lookup_error, or pending when the value is
	// stored state rather than a fresh lookup.
	Status string `json:"status"`

	// Message explains Status in one sentence and is safe to show as-is.
	Message string `json:"message"`

	// Observed is what DNS actually returned, so a customer can compare it
	// with what they typed.
	Observed string `json:"observed,omitempty"`

	// TrackingHostUnresolvable reports that the record is correct but this
	// install's tracking host has no DNS record of its own, so nothing will be
	// recorded. That is an operator fault, not a customer one.
	TrackingHostUnresolvable bool `json:"tracking_host_unresolvable"`
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

	// Timezone is the mailbox's own IANA zone, which its sending behaviour and
	// business-hours window are evaluated in. Empty means not configured, so
	// only the campaign's window applies.
	Timezone *string `json:"timezone"`

	// SaveToSent controls the Sent folder copy on SMTP/IMAP mailboxes. Turn it
	// off when the submission server files its own copy, or the folder ends up
	// with two of everything.
	SaveToSent *bool `json:"save_to_sent"`

	Tags []string `json:"tags"`
}

// BulkEmailTags adds and removes tags across many mailboxes in one call (the
// mailboxes list bulk bar). Mailbox ids the caller doesn't own and tag ids
// they haven't defined are ignored rather than erroring, so a stale
// selection can't fail the whole batch.
type BulkEmailTags struct {
	EmailIDs   []string `json:"email_ids" binding:"required,min=1,max=1000"`
	AddTags    []string `json:"add_tags" binding:"max=100"`
	RemoveTags []string `json:"remove_tags" binding:"max=100"`
}
