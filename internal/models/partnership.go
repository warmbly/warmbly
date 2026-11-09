package models

import (
	"time"

	"github.com/google/uuid"
)

// IsPartnership reports the Partnership plan in force; a Stripe subscription to another plan outranks a grant of it.
func (s *Subscription) IsPartnership() bool {
	return s.IsWarmupOnly() && s.EffectivePlanID() == PartnershipPlanID
}

// MailboxOAuthAppProviders are the mailbox providers a workspace may bring its own OAuth client for.
var MailboxOAuthAppProviders = []InboxProvider{InboxProviderGoogle, InboxProviderOutlook}

// ValidMailboxOAuthAppProvider reports a provider that signs in with OAuth.
func ValidMailboxOAuthAppProvider(p InboxProvider) bool {
	return p == InboxProviderGoogle || p == InboxProviderOutlook
}

// MailboxOAuthApp is a workspace's own OAuth client for one mailbox provider.
type MailboxOAuthApp struct {
	ID             uuid.UUID     `json:"id"`
	OrganizationID uuid.UUID     `json:"organization_id"`
	Provider       InboxProvider `json:"provider"`
	ClientID       string        `json:"client_id"`
	// ClientSecret is plaintext only in memory; it is never serialized.
	ClientSecret string `json:"-"`
	// HasSecret is false after an import that carried no secrets; the app needs its secret saved again.
	HasSecret bool   `json:"has_secret"`
	AppName   string `json:"app_name"`
	// RedirectURL is the callback the client must register; CustomRedirect says the workspace chose it.
	RedirectURL    string     `json:"redirect_url"`
	CustomRedirect bool       `json:"custom_redirect"`
	CreatedBy      *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	// Mailboxes counts mailboxes whose grant this client issued.
	Mailboxes int `json:"mailboxes"`
}

// MailboxOAuthAppInput saves a workspace's OAuth client; an empty ClientSecret keeps the stored one.
type MailboxOAuthAppInput struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	AppName      string `json:"app_name"`
	RedirectURL  string `json:"redirect_url"`
}

// MailboxConnectWarmup starts warmup on a mailbox as it connects; nil fields keep the mailbox defaults.
type MailboxConnectWarmup struct {
	Enabled   bool `json:"enabled"`
	Start     *int `json:"start,omitempty"`
	Max       *int `json:"max,omitempty"`
	Increase  *int `json:"increase,omitempty"`
	ReplyRate *int `json:"reply_rate,omitempty"`
}

// Sign-in link states.
const (
	ConnectLinkPending    = "pending"
	ConnectLinkConnecting = "connecting"
	ConnectLinkConnected  = "connected"
	ConnectLinkFailed     = "failed"
	ConnectLinkRevoked    = "revoked"
	ConnectLinkExpired    = "expired"
)

// MailboxConnectLink is a single-use sign-in link a mailbox owner opens to connect one mailbox.
type MailboxConnectLink struct {
	ID             uuid.UUID            `json:"id"`
	OrganizationID uuid.UUID            `json:"organization_id"`
	CreatedBy      uuid.UUID            `json:"created_by"`
	Provider       InboxProvider        `json:"provider"`
	ExpectedEmail  string               `json:"email,omitempty"`
	Warmup         MailboxConnectWarmup `json:"warmup"`
	ReturnURL      string               `json:"return_url,omitempty"`
	// Status reads expired once a pending link is past ExpiresAt.
	Status         string     `json:"status"`
	ErrorCode      string     `json:"error_code,omitempty"`
	EmailAccountID *uuid.UUID `json:"email_account_id,omitempty"`
	ExpiresAt      time.Time  `json:"expires_at"`
	CreatedAt      time.Time  `json:"created_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	// URL is returned once, when the link is created.
	URL string `json:"url,omitempty"`
}

// EffectiveStatus folds expiry into the stored status.
func (l *MailboxConnectLink) EffectiveStatus(now time.Time) string {
	if (l.Status == ConnectLinkPending || l.Status == ConnectLinkConnecting) && !now.Before(l.ExpiresAt) {
		return ConnectLinkExpired
	}
	return l.Status
}

// NewMailboxConnectLink asks for a sign-in link.
type NewMailboxConnectLink struct {
	Provider       InboxProvider         `json:"provider"`
	Email          string                `json:"email"`
	Warmup         *MailboxConnectWarmup `json:"warmup"`
	ReturnURL      string                `json:"return_url"`
	ExpiresInHours int                   `json:"expires_in_hours"`
}

// MailboxConnectLinkState is the Redis record of one provider round trip started from a link.
type MailboxConnectLinkState struct {
	LinkID     uuid.UUID  `json:"link_id"`
	OrgID      uuid.UUID  `json:"org_id"`
	OAuthAppID *uuid.UUID `json:"oauth_app_id,omitempty"`
	Verifier   string     `json:"verifier"`
	// BindingHash is the SHA-256 of the browser cookie that pressed Continue.
	BindingHash string `json:"binding_hash"`
}

// MailboxOAuthGrant hands Warmbly a sign-in the workspace's own OAuth app already completed:
// a refresh token, or an authorization code with the redirect_uri (and PKCE verifier) it was issued for.
type MailboxOAuthGrant struct {
	Provider     InboxProvider `json:"provider"`
	RefreshToken string        `json:"refresh_token,omitempty"`
	Code         string        `json:"code,omitempty"`
	RedirectURI  string        `json:"redirect_uri,omitempty"`
	CodeVerifier string        `json:"code_verifier,omitempty"`
	// Email, when set, must be the address the sign-in belongs to.
	Email string `json:"email,omitempty"`
}

// MailboxBulkRenewed is a handed-over sign-in that renewed a mailbox the workspace already had.
const MailboxBulkRenewed MailboxBulkRowStatus = "renewed"

// GrantBulkResult answers a batch of handed-over sign-ins, one row each.
type GrantBulkResult struct {
	Data    []MailboxBulkRow `json:"data"`
	Summary struct {
		Total     int `json:"total"`
		Connected int `json:"connected"`
		Renewed   int `json:"renewed"`
		Failed    int `json:"failed"`
	} `json:"summary"`
}
