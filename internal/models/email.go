package models

import (
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
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

	// SendAsEmail is the verified provider alias this mailbox sends from.
	// Empty, which is every mailbox until someone picks one, means the
	// mailbox's own address. The alias list itself is not carried here: it is
	// read through the identity endpoint, like sync state and behaviour.
	SendAsEmail string `json:"send_as_email"`

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
	//
	// AuthDKIM is a positive-only signal: true means a key was found at a
	// probed selector, false means none answered. Selectors are not
	// discoverable from DNS, so false is "unverified" and must never be
	// presented as a missing record.
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

// SendFrom is the address this mailbox's mail is actually From. A verified
// alias when one was chosen, the mailbox address otherwise.
func (e *Email) SendFrom() string {
	if s := strings.TrimSpace(e.SendAsEmail); s != "" {
		return s
	}
	return e.Email
}

// SendAsIdentity is one address the provider has verified this mailbox to send
// as, as the provider last reported it. Primary is the mailbox's own address;
// Default is the one the provider composes from by default, which Warmbly
// reports but does not follow: which alias Warmbly sends from is the
// workspace's choice and lives in Email.SendAsEmail.
type SendAsIdentity struct {
	Email     string `json:"email"`
	Name      string `json:"name"`
	IsPrimary bool   `json:"is_primary"`
	IsDefault bool   `json:"is_default"`
	// Verified reports that the provider finished verifying the address.
	// Sending as an unverified alias is refused by the provider, so these are
	// offered but never selectable.
	Verified bool `json:"verified"`
}

// Signature provenance, mirroring the email_accounts.signature_source CHECK.
// Unrelated to SignatureSync, which decides whether the signature is appended
// to outgoing mail.
const (
	// SignatureSourceManual: written in Warmbly.
	SignatureSourceManual = "manual"
	// SignatureSourceProvider: imported from the mailbox provider.
	SignatureSourceProvider = "provider"
)

// SendIdentity is the mailbox's sending identity as the dashboard reads it:
// which addresses the provider will let it send as, which one is in use, and
// where the stored signature came from.
type SendIdentity struct {
	// Supported reports whether the provider can be asked at all. Only Gmail
	// exposes send-as identities and a stored signature; an Outlook or
	// SMTP/IMAP mailbox answers with Supported false and an empty list rather
	// than an error, so the dashboard can say so instead of failing.
	Supported bool   `json:"supported"`
	Provider  string `json:"provider"`
	// MailboxEmail is the address the mailbox authenticates as, which is
	// always a legal sender and is what an empty SendAsEmail means.
	MailboxEmail string           `json:"mailbox_email"`
	SendAsEmail  string           `json:"send_as_email"`
	Identities   []SendAsIdentity `json:"identities"`
	SyncedAt     *time.Time       `json:"synced_at,omitempty"`

	SignatureSource     string     `json:"signature_source"`
	SignatureImportedAt *time.Time `json:"signature_imported_at,omitempty"`
}

// EventWorkerMailboxIdentity asks a worker to read one mailbox's sending
// identity from its provider and answer on the process channel.
//
// The worker is the only side that talks to a customer's mailbox: it holds the
// credential, and the address the provider sees for a mailbox has to stay the
// one it always sees, or the provider answers with a sign-in challenge.
type EventWorkerMailboxIdentity struct {
	EmailID uuid.UUID `json:"email_id" avro:"email_id"`
	// ProcessID names the reply channel, like a credential validation.
	ProcessID uuid.UUID `json:"process_id" avro:"process_id"`
	// WantSignature asks for the signature too. Off for a plain address
	// refresh, which is the common press.
	WantSignature bool `json:"want_signature" avro:"want_signature"`
	// SignatureFor is the address whose signature is wanted: the alias the
	// mailbox sends as, so it signs off as that alias. Empty takes the
	// provider's default identity.
	SignatureFor string `json:"signature_for,omitempty" avro:"signature_for"`
}

// MailboxIdentityResult is the worker's answer, published to the process
// channel. Everything the control plane stores comes from here; nothing about
// the provider call itself crosses back.
type MailboxIdentityResult struct {
	OK bool `json:"ok"`
	// Error is a short reason when OK is false, for the log and for the
	// message the customer sees.
	Error      string           `json:"error,omitempty"`
	Identities []SendAsIdentity `json:"identities,omitempty"`
	// SignatureHTML is empty both when none was asked for and when the
	// provider holds none, which are the same thing to the caller: nothing to
	// import, so nothing is overwritten.
	SignatureHTML string `json:"signature_html,omitempty"`
}

// ImportedSignature is a signature read from the provider, ready to store.
type ImportedSignature struct {
	HTML  string
	Plain string
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

// Mail connection security modes. Storing the mode explicitly is what lets a
// mailbox live on any port: inferring it from the port only ever worked for
// 465/587/993/143.
const (
	// MailSecurityTLS is implicit TLS: the server speaks TLS from the first
	// byte. SMTP 465 (SMTPS), IMAP 993 (IMAPS).
	MailSecurityTLS = "tls"
	// MailSecurityStartTLS is a plaintext greeting upgraded in-band with
	// STARTTLS. SMTP 587/25/2525, IMAP 143.
	MailSecurityStartTLS = "starttls"
	// MailSecurityNone is no encryption at all, and is only ever legal
	// against a loopback host (see LoopbackMailHost). It exists for the
	// local relays that speak no TLS because they never listen on a network
	// interface: Proton Bridge on 127.0.0.1:1143/1025, a Dovecot sharing a
	// host with its worker, a Mailpit-style sink. The credentials never
	// reach a wire, which is the whole reason this is allowed; anywhere
	// else it is refused, in validation and again at dial time.
	MailSecurityNone = "none"
)

// ValidMailSecurity reports whether s is a known security mode. It does not
// say whether the mode is legal for a given host: MailSecurityNone also has
// to pass LoopbackMailHost, which is the caller's job.
func ValidMailSecurity(s string) bool {
	return s == MailSecurityTLS || s == MailSecurityStartTLS || s == MailSecurityNone
}

// LoopbackMailHost reports whether host addresses this machine, the only
// place MailSecurityNone is allowed.
//
// Literals only, deliberately: a name that resolves to 127.0.0.1 today can
// resolve elsewhere at dial time, so accepting one here would be a check the
// network could walk out from under. The dialer verifies the peer it actually
// got as well, which is what closes that gap for good.
func LoopbackMailHost(host string) bool {
	host = NormalizeMailHost(host)
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// NormalizeMailHost is a stored host as the dialer and the TLS layer want it:
// trimmed, and without the brackets a bare IPv6 literal is usually pasted
// with. Brackets belong to the host:port form, not to the host, and carrying
// them into tls.Config.ServerName would fail verification against an address
// that is otherwise correct.
func NormalizeMailHost(host string) string {
	host = strings.TrimSpace(host)
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return host[1 : len(host)-1]
	}
	return host
}

// CleartextMailAllowed is the whole rule for MailSecurityNone in one place:
// the host is this machine, and this deployment runs its workers on the
// operator's own machine.
//
// Every place that opens a socket asks it, not just the connect form, because
// credentials reach a worker by more than the form. An organization archive
// exported from a self-hosted instance carries its mailboxes, so an import
// could otherwise hand a hosted worker a mailbox that dials its own loopback
// in the clear. The dialer additionally checks the peer it actually got,
// which is the half no hostname can promise.
func CleartextMailAllowed(host string) bool {
	return config.SelfHosted() && LoopbackMailHost(host)
}

// MailDialAddress builds the address to dial a mailbox leg on.
//
// net.JoinHostPort, not fmt.Sprintf: an IPv6 literal needs brackets in a
// host:port string, so "::1" on 1143 has to become "[::1]:1143". Without them
// the address reads as the host "::1:1143" with no port at all, and every
// dial to a mailbox on an IPv6 address fails looking like a dead server.
func MailDialAddress(host string, port int) string {
	return net.JoinHostPort(NormalizeMailHost(host), strconv.Itoa(port))
}

// ResolveSMTPSecurity returns the security mode to dial SMTP with: the stored
// choice when it is set, otherwise the conventional default for the port. The
// fallback keeps mailboxes connected across the rollout, when the stored value
// is empty and events from older workers carry no mode at all.
//
// MailSecurityNone is never inferred, only obeyed: dropping encryption is
// always an explicit choice, never something a port number decides.
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
// the norm (993) and 143 is the STARTTLS port. Like the SMTP resolver it
// never infers MailSecurityNone.
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
	// Allowance, when set, is enforced again inside the insert transaction
	// under the organization's mailbox lock, so concurrent connects cannot
	// both take the last slot.
	Allowance    *MailboxAllowance
	Provider     InboxProvider
	Name         string
	Email        string
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

type NewSMTPIMAPAccount struct {
	OrganizationID *uuid.UUID
	// Allowance: see NewOauthAccount.
	Allowance *MailboxAllowance
	Name      string
	Email     string
	SMTP      *Service
	IMAP      *Service
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

	// SendAsEmail picks which verified provider alias the mailbox sends from.
	// An empty string clears it back to the mailbox's own address; anything
	// else has to be an address the provider reported as verified.
	SendAsEmail *string `json:"send_as_email"`

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

// MailboxBulkRowStatus is the per-row outcome of a bulk SMTP/IMAP connect.
type MailboxBulkRowStatus string

const (
	// MailboxBulkConnected: the mailbox was validated and connected.
	MailboxBulkConnected MailboxBulkRowStatus = "connected"
	// MailboxBulkSkipped: the mailbox was already connected, so re-uploading
	// a file is safe.
	MailboxBulkSkipped MailboxBulkRowStatus = "skipped"
	// MailboxBulkFailed: the row was refused; Code says why.
	MailboxBulkFailed MailboxBulkRowStatus = "failed"
)

// MailboxBulkRow is one row's answer. Row echoes the caller's own row number
// so the dashboard can hand back the failed lines of the file it uploaded.
type MailboxBulkRow struct {
	Row     int                  `json:"row"`
	Email   string               `json:"email"`
	Status  MailboxBulkRowStatus `json:"status"`
	Code    string               `json:"code,omitempty"`
	Message string               `json:"message,omitempty"`
	ID      *uuid.UUID           `json:"id,omitempty"`
}

// MailboxBulkSummary counts the batch.
type MailboxBulkSummary struct {
	Total     int `json:"total"`
	Connected int `json:"connected"`
	Skipped   int `json:"skipped"`
	Failed    int `json:"failed"`
}

// MailboxBulkResult is the answer to POST /emails/onboarding/smtp-imap/bulk.
type MailboxBulkResult struct {
	Data    []MailboxBulkRow   `json:"data"`
	Summary MailboxBulkSummary `json:"summary"`
	// Allowance is the workspace's mailbox allowance after the batch, so the
	// dashboard can say how many more rows will fit without another call.
	Allowance *MailboxAllowance `json:"allowance,omitempty"`
}
