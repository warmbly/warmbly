package models

import (
	"time"

	"github.com/google/uuid"
)

// ClientConnectLinkStatus is what a client opening a connect link is told.
type ClientConnectLinkStatus string

const (
	ClientConnectLinkActive    ClientConnectLinkStatus = "active"
	ClientConnectLinkExpired   ClientConnectLinkStatus = "expired"
	ClientConnectLinkRevoked   ClientConnectLinkStatus = "revoked"
	ClientConnectLinkExhausted ClientConnectLinkStatus = "exhausted"
)

// ConnectLinkVendor is the way in that is not one mailbox: the client connects an inbox vendor account and picks its mailboxes.
const ConnectLinkVendor InboxProvider = "vendor"

// ConnectLinkProviders are the ways a client may connect: the email_provider values, and a vendor account.
var ConnectLinkProviders = []InboxProvider{InboxProviderGoogle, InboxProviderOutlook, InboxProviderSMTPIMAP, ConnectLinkVendor}

const (
	// ClientConnectLinkMaxMailboxes bounds one link's mailboxes, matching the table's check.
	ClientConnectLinkMaxMailboxes = 500
	// ClientConnectLinkDefaultDays and ClientConnectLinkMaxDays bound how long a link stays open.
	ClientConnectLinkDefaultDays = 7
	ClientConnectLinkMaxDays     = 90
)

// ClientConnectLink is one client connect link. The token is never stored, only its hash.
type ClientConnectLink struct {
	ID                 uuid.UUID               `json:"id"`
	OrganizationID     uuid.UUID               `json:"organization_id"`
	CreatedBy          *uuid.UUID              `json:"created_by"`
	Label              string                  `json:"label"`
	TokenHash          []byte                  `json:"-"`
	AllowedProviders   []string                `json:"allowed_providers"`
	MaxMailboxes       int                     `json:"max_mailboxes"`
	MailboxesConnected int                     `json:"mailboxes_connected"`
	TagIDs             []uuid.UUID             `json:"tag_ids"`
	Warmup             bool                    `json:"warmup"`
	ExpiresAt          time.Time               `json:"expires_at"`
	RevokedAt          *time.Time              `json:"revoked_at"`
	CreatedAt          time.Time               `json:"created_at"`
	LastUsedAt         *time.Time              `json:"last_used_at"`
	Status             ClientConnectLinkStatus `json:"status"`
}

// StatusAt is the link's standing at now: revoked first, then expired, then out of mailboxes.
func (l *ClientConnectLink) StatusAt(now time.Time) ClientConnectLinkStatus {
	switch {
	case l.RevokedAt != nil || l.CreatedBy == nil:
		return ClientConnectLinkRevoked
	case !now.Before(l.ExpiresAt):
		return ClientConnectLinkExpired
	case l.MailboxesConnected >= l.MaxMailboxes:
		return ClientConnectLinkExhausted
	}
	return ClientConnectLinkActive
}

// Allows reports whether the link lets a client connect through provider.
func (l *ClientConnectLink) Allows(provider InboxProvider) bool {
	for _, p := range l.AllowedProviders {
		if p == string(provider) {
			return true
		}
	}
	return false
}

// ClientConnectLinkUse is one mailbox a client connected through a link.
type ClientConnectLinkUse struct {
	ID             uuid.UUID  `json:"id"`
	LinkID         uuid.UUID  `json:"link_id"`
	EmailAccountID *uuid.UUID `json:"email_account_id"`
	Email          string     `json:"email"`
	Provider       string     `json:"provider"`
	ConnectedAt    time.Time  `json:"connected_at"`
}

// NewClientConnectLink is what a member chooses when creating a link.
type NewClientConnectLink struct {
	Label        string   `json:"label"`
	Providers    []string `json:"providers"`
	MaxMailboxes int      `json:"max_mailboxes"`
	// ExpiresInDays is 1 to ClientConnectLinkMaxDays; 0 takes the default.
	ExpiresInDays int      `json:"expires_in_days"`
	TagIDs        []string `json:"tag_ids"`
	Warmup        bool     `json:"warmup"`
}

// CreatedClientConnectLink carries the link's URL, shown once and never again.
type CreatedClientConnectLink struct {
	Link ClientConnectLink `json:"link"`
	URL  string            `json:"url"`
}

// ClientConnectLinkList is the dashboard's list of links.
type ClientConnectLinkList struct {
	Data       []ClientConnectLink `json:"data"`
	Pagination CPagination         `json:"pagination"`
}

// ClientConnectLinkOAuthState is a client's sign-in started from a connect link, held server-side and used once.
type ClientConnectLinkOAuthState struct {
	LinkID         uuid.UUID `json:"link_id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	Provider       string    `json:"provider"`
	CodeVerifier   string    `json:"code_verifier"`
	// ReturnURL is the connect page the browser is sent back to.
	ReturnURL    string    `json:"return_url"`
	RemoteIPHash string    `json:"remote_ip_hash,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
	// ResultKey names where the outcome is left for the page that started the sign-in to collect.
	ResultKey string `json:"result_key"`
}
