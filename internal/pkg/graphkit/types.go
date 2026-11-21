// Package graphkit is the client for GraphKit's infrastructure partner API:
// domains and Google Workspace / Microsoft 365 mailboxes bought for a
// workspace and billed to this instance's partner account. Money is integer
// micro-USD throughout, domain names are lowercase A-labels.
package graphkit

import (
	"encoding/json"
	"time"
)

// Contact is the ICANN registrant GraphKit registers a domain under.
type Contact struct {
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Organization string `json:"organization"`
	Email        string `json:"email"`
	Phone        string `json:"phone"`
	Address1     string `json:"address1"`
	Address2     string `json:"address2"`
	City         string `json:"city"`
	State        string `json:"state"`
	PostalCode   string `json:"postal_code"`
	Country      string `json:"country"`
}

// AccountInput is the body of an account upsert.
type AccountInput struct {
	Name       string   `json:"name"`
	Registrant *Contact `json:"registrant,omitempty"`
}

// Account is one partner customer (a workspace).
type Account struct {
	ID         string    `json:"id"`
	ExternalID string    `json:"external_id"`
	Name       string    `json:"name"`
	Registrant *Contact  `json:"registrant"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// TLDPrice is the partner's price for one TLD.
type TLDPrice struct {
	TLD             string `json:"tld"`
	RegisterMicros  int64  `json:"register_micros"`
	RenewMicros     int64  `json:"renew_micros"`
	TransferMicros  int64  `json:"transfer_micros"`
	PrivacyIncluded bool   `json:"privacy_included"`
	MinYears        int    `json:"min_years"`
	MaxYears        int    `json:"max_years"`
}

// MailboxPrice is the partner's monthly price for a provider.
type MailboxPrice struct {
	Provider      string `json:"provider"`
	MonthlyMicros int64  `json:"monthly_micros"`
	// Scheduled is the next announced price, applied from its effective date.
	Scheduled *ScheduledPrice `json:"scheduled"`
}

// ScheduledPrice is an announced mailbox price and when it applies.
type ScheduledPrice struct {
	MonthlyMicros int64     `json:"monthly_micros"`
	EffectiveFrom time.Time `json:"effective_from"`
}

// SendingPrices are GraphKit's own sending and warmup service, which the store does not use.
type SendingPrices struct {
	AddonMonthlyMicros    int64    `json:"addon_monthly_micros"`
	BoughtMonthlyMicros   int64    `json:"bought_monthly_micros"`
	OutsideMonthlyMicros  int64    `json:"outside_monthly_micros"`
	Modes                 []string `json:"modes"`
	SendsPerMailboxPerDay int      `json:"sends_per_mailbox_per_day"`
}

// Limits are GraphKit's per-domain mailbox caps.
type Limits struct {
	MailboxesPerDomainMax         int `json:"mailboxes_per_domain_max"`
	MailboxesPerDomainRecommended int `json:"mailboxes_per_domain_recommended"`
}

// Pricing is what GraphKit charges the partner.
type Pricing struct {
	Currency  string         `json:"currency"`
	UpdatedAt time.Time      `json:"updated_at"`
	Domains   []TLDPrice     `json:"domains"`
	Mailboxes []MailboxPrice `json:"mailboxes"`
	Limits    Limits         `json:"limits"`
	Sending   *SendingPrices `json:"sending,omitempty"`
	Social    *SocialPrices  `json:"social,omitempty"`
}

// MinYears is the shortest registration a TLD takes, 1 when it is not listed.
func (p *Pricing) MinYears(tld string) int {
	if p != nil {
		for _, d := range p.Domains {
			if d.TLD == tld && d.MinYears > 1 {
				return d.MinYears
			}
		}
	}
	return 1
}

// DomainOffer is one search or check result.
type DomainOffer struct {
	Domain      string `json:"domain"`
	DisplayName string `json:"display_name"`
	TLD         string `json:"tld"`
	Available   bool   `json:"available"`
	Premium     bool   `json:"premium"`
	// Per year; nil for a TLD that is not offered.
	RegisterMicros *int64  `json:"register_micros"`
	RenewMicros    *int64  `json:"renew_micros"`
	Exact          bool    `json:"exact"`
	Reason         *string `json:"reason"`
}

// SearchResult is a domain search.
type SearchResult struct {
	Query      string        `json:"query"`
	PriceStale bool          `json:"price_stale"`
	Data       []DomainOffer `json:"data"`
}

// Item types on quotes and orders.
const (
	ItemDomain    = "domain"
	ItemMailboxes = "mailboxes"
)

// Mail providers.
const (
	ProviderGoogle    = "google"
	ProviderMicrosoft = "microsoft"
)

// NewMailbox is one address on a quote. Admin makes it the domain's Workspace
// or tenant admin; a new domain without a flag gets its first mailbox.
type NewMailbox struct {
	LocalPart string `json:"local_part"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Admin     bool   `json:"admin,omitempty"`
}

// QuoteItem is one requested line: a domain, or mailboxes on a domain.
type QuoteItem struct {
	Type      string       `json:"type"`
	Domain    string       `json:"domain"`
	Years     int          `json:"years,omitempty"`
	Provider  string       `json:"provider,omitempty"`
	Mailboxes []NewMailbox `json:"mailboxes,omitempty"`
}

// QuoteLine is one priced line of a quote.
type QuoteLine struct {
	Type          string   `json:"type"`
	Domain        string   `json:"domain"`
	Years         int      `json:"years,omitempty"`
	Provider      string   `json:"provider,omitempty"`
	Count         int      `json:"count,omitempty"`
	AmountMicros  int64    `json:"amount_micros"`
	RenewMicros   int64    `json:"renew_micros,omitempty"`
	MonthlyMicros int64    `json:"monthly_micros,omitempty"`
	Emails        []string `json:"emails,omitempty"`
	AdminEmail    string   `json:"admin_email,omitempty"`
	// NameserversChange means the customer sets the mailbox provider's nameservers after the order.
	NameserversChange bool `json:"nameservers_change,omitempty"`
}

// Quote freezes prices for fifteen minutes.
type Quote struct {
	ID                     string      `json:"id"`
	AccountID              string      `json:"account_id"`
	OrderID                *string     `json:"order_id"`
	CreatedAt              time.Time   `json:"created_at"`
	ExpiresAt              time.Time   `json:"expires_at"`
	Currency               string      `json:"currency"`
	Lines                  []QuoteLine `json:"lines"`
	DueNowMicros           int64       `json:"due_now_micros"`
	RecurringMonthlyMicros int64       `json:"recurring_monthly_micros"`
}

// Order statuses.
const (
	OrderProcessing      = "processing"
	OrderCompleted       = "completed"
	OrderPartiallyFailed = "partially_failed"
	OrderFailed          = "failed"
)

// StepKeys are the fixed order steps, in order.
var StepKeys = []string{"register", "dns", "mail_auth", "mailboxes", "delegation", "ready"}

// Step statuses.
const (
	StepPending = "pending"
	StepRunning = "running"
	StepDone    = "done"
	StepFailed  = "failed"
	StepSkipped = "skipped"
)

// OrderStep is one stage of an order's progress track.
type OrderStep struct {
	Key    string     `json:"key"`
	Status string     `json:"status"`
	At     *time.Time `json:"at,omitempty"`
}

// OrderItem is one line of an order and how it is going.
type OrderItem struct {
	Type           string   `json:"type"`
	Domain         string   `json:"domain"`
	Status         string   `json:"status"`
	Provider       string   `json:"provider,omitempty"`
	Years          int      `json:"years,omitempty"`
	AmountMicros   int64    `json:"amount_micros"`
	DomainID       string   `json:"domain_id,omitempty"`
	MailboxIDs     []string `json:"mailbox_ids,omitempty"`
	ErrorCode      string   `json:"error_code,omitempty"`
	RefundedMicros int64    `json:"refunded_micros,omitempty"`
}

// Order is a placed quote.
type Order struct {
	ID            string      `json:"id"`
	AccountID     string      `json:"account_id"`
	QuoteID       string      `json:"quote_id"`
	Status        string      `json:"status"`
	DueNowMicros  int64       `json:"due_now_micros"`
	ForwardingURL *string     `json:"forwarding_url"`
	CreatedAt     time.Time   `json:"created_at"`
	CompletedAt   *time.Time  `json:"completed_at"`
	Items         []OrderItem `json:"items"`
	Steps         []OrderStep `json:"steps"`
}

// Domain statuses.
const (
	DomainRegistering    = "registering"
	DomainActive         = "active"
	DomainExpired        = "expired"
	DomainRedemption     = "redemption"
	DomainTransferring   = "transferring_out"
	DomainTransferredOut = "transferred_out"
	DomainFailed         = "failed"
	// DomainConnecting is a domain registered elsewhere that is not connected yet.
	DomainConnecting = "connecting"
	DomainRemoved    = "removed"
)

// How a domain is on the account: registered through GraphKit, or connected.
const (
	ConnectionRegistered  = "registered"
	ConnectionNameservers = "nameservers"
	ConnectionCloudflare  = "cloudflare"
	ConnectionManual      = "manual"
)

// Connection statuses of a domain registered elsewhere.
const (
	ConnectionPending = "pending"
	ConnectionActive  = "active"
	ConnectionMoved   = "moved"
	ConnectionExpired = "expired"
)

// Who hosts a domain's DNS.
const (
	DNSHostGraphKit        = "graphkit"
	DNSHostMailboxProvider = "mailbox_provider"
	DNSHostCustomer        = "customer"
)

// RegistrantVerification is the ICANN registrant email check.
type RegistrantVerification struct {
	Status   string     `json:"status"`
	Email    string     `json:"email"`
	Deadline *time.Time `json:"deadline"`
}

// TransferRequest is another registrar asking for the domain.
type TransferRequest struct {
	Status        string     `json:"status"`
	RequestedAt   *time.Time `json:"requested_at"`
	AutoApproveAt *time.Time `json:"auto_approve_at"`
}

// DNSConflict is a record that blocks one GraphKit publishes. RecordID is
// set for a record in a zone GraphKit writes; Reason is GraphKit's wording.
type DNSConflict struct {
	Type     string  `json:"type"`
	Name     string  `json:"name"`
	Content  string  `json:"content"`
	Reason   string  `json:"reason"`
	RecordID *string `json:"record_id"`
}

// MailAuth is the state of a domain's mail records.
type MailAuth struct {
	MX        string     `json:"mx"`
	SPF       string     `json:"spf"`
	DKIM      string     `json:"dkim"`
	DMARC     string     `json:"dmarc"`
	CheckedAt *time.Time `json:"checked_at,omitempty"`
}

// Domain is a registered domain.
type Domain struct {
	ID             string     `json:"id"`
	AccountID      string     `json:"account_id"`
	Domain         string     `json:"domain"`
	DisplayName    string     `json:"display_name"`
	Status         string     `json:"status"`
	RegisteredAt   *time.Time `json:"registered_at"`
	ExpiresAt      *time.Time `json:"expires_at"`
	RenewMicros    int64      `json:"renew_micros"`
	RenewableFrom  *time.Time `json:"renewable_from"`
	RenewableUntil *time.Time `json:"renewable_until"`
	Privacy        bool       `json:"privacy"`
	Locked         bool       `json:"locked"`
	UnlockedUntil  *time.Time `json:"unlocked_until"`
	ForwardingURL  *string    `json:"forwarding_url"`
	Provider       string     `json:"provider"`
	DMARCPolicy    string     `json:"dmarc_policy"`
	MailAuth       MailAuth   `json:"mail_auth"`
	Nameservers    []string   `json:"nameservers"`
	MailboxCount   int        `json:"mailbox_count"`
	CreatedAt      time.Time  `json:"created_at"`
	// ForwardingStatus is none, pending (certificate being issued) or active.
	ForwardingStatus       string                  `json:"forwarding_status"`
	RegistrantVerification *RegistrantVerification `json:"registrant_verification"`
	Transfer               *TransferRequest        `json:"transfer"`
	// Connection is registered for a domain registered here; the rest are existing domains.
	Connection          string        `json:"connection"`
	ConnectionStatus    string        `json:"connection_status"`
	ConnectionExpiresAt *time.Time    `json:"connection_expires_at"`
	ConnectionCheckedAt *time.Time    `json:"connection_checked_at"`
	CurrentNameservers  []string      `json:"current_nameservers"`
	DNSHost             string        `json:"dns_host"`
	DNSConflicts        []DNSConflict `json:"dns_conflicts"`
	DNSConnectionID     *string       `json:"dns_connection_id"`
}

// Registered reports a domain registered through GraphKit, which renewal and transfer-out apply to.
func (d *Domain) Registered() bool {
	return d.Connection == "" || d.Connection == ConnectionRegistered
}

// DomainPatch changes a domain's settings; nil fields are left alone, and
// ClearForwarding sends forwarding_url null to stop forwarding.
type DomainPatch struct {
	ForwardingURL   *string
	ClearForwarding bool
	Privacy         *bool
	DMARCPolicy     *string
}

// MarshalJSON writes only the fields set, with null for a cleared forward.
func (p DomainPatch) MarshalJSON() ([]byte, error) {
	m := map[string]any{}
	switch {
	case p.ClearForwarding:
		m["forwarding_url"] = nil
	case p.ForwardingURL != nil:
		m["forwarding_url"] = *p.ForwardingURL
	}
	if p.Privacy != nil {
		m["privacy"] = *p.Privacy
	}
	if p.DMARCPolicy != nil {
		m["dmarc_policy"] = *p.DMARCPolicy
	}
	return json.Marshal(m)
}

// TransferOut is what a transfer to another registrar needs.
type TransferOut struct {
	AuthCode      string     `json:"auth_code"`
	UnlockedUntil *time.Time `json:"unlocked_until"`
	Domain        *Domain    `json:"domain,omitempty"`
}

// DNSRecord is one record in a domain's zone. Managed records are GraphKit's.
type DNSRecord struct {
	ID        string     `json:"id"`
	Type      string     `json:"type"`
	Name      string     `json:"name"`
	Content   string     `json:"content"`
	TTL       int        `json:"ttl"`
	Priority  *int       `json:"priority,omitempty"`
	Managed   bool       `json:"managed"`
	Purpose   string     `json:"purpose,omitempty"`
	Proxied   bool       `json:"proxied"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
	// Status is set by a DNS check: ok or missing.
	Status string `json:"status,omitempty"`
}

// DNSRecordInput adds a record the partner owns.
type DNSRecordInput struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl,omitempty"`
	Priority *int   `json:"priority,omitempty"`
}

// DNSRecordPatch changes a partner record in place; the type stays.
type DNSRecordPatch struct {
	Name     *string `json:"name,omitempty"`
	Content  *string `json:"content,omitempty"`
	TTL      *int    `json:"ttl,omitempty"`
	Priority *int    `json:"priority,omitempty"`
}

// DNSCheck is a public DNS lookup of every record.
type DNSCheck struct {
	Data        []DNSRecord `json:"data"`
	Nameservers []string    `json:"nameservers"`
	CheckedAt   *time.Time  `json:"checked_at"`
}

// Mailbox statuses.
const (
	MailboxProvisioning = "provisioning"
	MailboxActive       = "active"
	MailboxSuspended    = "suspended"
	MailboxCancelling   = "cancelling"
	MailboxCancelled    = "cancelled"
	MailboxFailed       = "failed"
)

// Connection methods: delegation to the partner's own identity is the default
// for both providers, token is GraphKit's Google broker (the fallback, and the
// method for an account whose Google identity has no client id), and password
// is the last resort for either.
const (
	ConnectToken      = "token"
	ConnectDelegation = "delegation"
	ConnectPassword   = "password"
	// ConnectOAuth (Google, with GoogleDelegation.SignInClientID registered): the mail vendor's team
	// signs the mailbox in through a link the partner registers (PutMailboxSignInLink); the partner
	// reports it connected (MailboxSignedIn) and it is ready.
	ConnectOAuth = "oauth"
)

// Connection says how the partner can reach a mailbox.
type Connection struct {
	Method string `json:"method"`
	Ready  bool   `json:"ready"`
	// SignIn is set only for ConnectOAuth.
	SignIn *ConnectionSignIn `json:"sign_in,omitempty"`
}

// ConnectionSignIn is an oauth mailbox's sign-in: the registered link's expiry (nil when none) and when it was signed in.
type ConnectionSignIn struct {
	LinkExpiresAt *time.Time `json:"link_expires_at"`
	SignedInAt    *time.Time `json:"signed_in_at"`
}

// Mailbox is one Google Workspace or Microsoft 365 mailbox.
type Mailbox struct {
	ID            string  `json:"id"`
	AccountID     string  `json:"account_id"`
	DomainID      string  `json:"domain_id"`
	Email         string  `json:"email"`
	Provider      string  `json:"provider"`
	Status        string  `json:"status"`
	FirstName     string  `json:"first_name"`
	LastName      string  `json:"last_name"`
	IsAdmin       bool    `json:"is_admin"`
	PhotoURL      *string `json:"photo_url"`
	MonthlyMicros int64   `json:"monthly_micros"`
	// NextPeriodMonthlyMicros is the price of the period starting at CurrentPeriodEnd; nil unless active or suspended.
	NextPeriodMonthlyMicros *int64     `json:"next_period_monthly_micros"`
	CurrentPeriodEnd        *time.Time `json:"current_period_end"`
	CancelsAt               *time.Time `json:"cancels_at"`
	CreatedAt               time.Time  `json:"created_at"`
	Connection              Connection `json:"connection"`
}

// MailboxPatch renames a mailbox.
type MailboxPatch struct {
	FirstName *string `json:"first_name,omitempty"`
	LastName  *string `json:"last_name,omitempty"`
}

// Server is one mail endpoint in a credentials answer.
type Server struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Security string `json:"security"`
}

// Credentials is the password fallback for a mailbox delegation could not
// reach, and the admin mailbox's sign-in. Never log or persist one.
type Credentials struct {
	Email           string `json:"email"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	AppPassword     string `json:"app_password"`
	IMAP            Server `json:"imap"`
	SMTP            Server `json:"smtp"`
	LoginURL        string `json:"login_url"`
	AdminConsoleURL string `json:"admin_console_url,omitempty"`
}

// GoogleDelegation is the service account authorized for domain-wide
// delegation with Scopes. Required on the partner; on an account, no ClientID
// connects its Google mailboxes by token, limited to Scopes.
type GoogleDelegation struct {
	ClientID string   `json:"client_id,omitempty"`
	Scopes   []string `json:"scopes"`
	// SignInClientID is the OAuth web client the mail vendor signs Google mailboxes in to (ConnectOAuth).
	SignInClientID string `json:"sign_in_client_id,omitempty"`
}

// AccessToken is a brokered Google access token for one mailbox, valid at most an hour.
// Never log or persist one.
type AccessToken struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresAt   time.Time `json:"expires_at"`
	Scopes      []string  `json:"scopes"`
}

// MicrosoftDelegation is the Entra application GraphKit consents to in each tenant.
type MicrosoftDelegation struct {
	AppID       string   `json:"app_id"`
	Permissions []string `json:"permissions"`
}

// Delegation is a set of connection identities, the partner's or one account's.
// A PUT replaces both providers; an omitted one is cleared.
type Delegation struct {
	Google    *GoogleDelegation    `json:"google,omitempty"`
	Microsoft *MicrosoftDelegation `json:"microsoft,omitempty"`
	UpdatedAt *time.Time           `json:"updated_at,omitempty"`
}

// Empty reports no identity for either provider.
func (d Delegation) Empty() bool { return d.Google == nil && d.Microsoft == nil }

// WebhookEndpoint is the partner's webhook. Every PUT returns the current
// secret; only a rotation changes it, and the previous one keeps signing
// until PreviousSecretExpiresAt. GET omits the secret.
type WebhookEndpoint struct {
	URL                     string     `json:"url"`
	Events                  []string   `json:"events"`
	Secret                  *string    `json:"secret,omitempty"`
	PreviousSecretExpiresAt *time.Time `json:"previous_secret_expires_at,omitempty"`
	UpdatedAt               *time.Time `json:"updated_at,omitempty"`
}

// PartnerSettings are the partner's white-label settings.
type PartnerSettings struct {
	RegistrantNotices  string `json:"registrant_notices"`
	DMARCPolicyDefault string `json:"dmarc_policy_default"`
}

// Event is a webhook envelope, also returned by the events feed.
type Event struct {
	ID                string          `json:"id"`
	Type              string          `json:"type"`
	CreatedAt         time.Time       `json:"created_at"`
	AccountExternalID *string         `json:"account_external_id"`
	Livemode          bool            `json:"livemode"`
	Data              json.RawMessage `json:"data"`
}

// Event types.
const (
	EventOrderUpdated         = "order.updated"
	EventOrderCompleted       = "order.completed"
	EventDomainRegistered     = "domain.registered"
	EventDomainFailed         = "domain.failed"
	EventDomainRenewed        = "domain.renewed"
	EventDomainExpiring       = "domain.expiring"
	EventDomainExpired        = "domain.expired"
	EventDomainUpdated        = "domain.updated"
	EventDomainTransferredOut = "domain.transferred_out"
	EventMailboxProvisioning  = "mailbox.provisioning"
	EventMailboxActive        = "mailbox.active"
	EventMailboxFailed        = "mailbox.failed"
	EventMailboxSuspended     = "mailbox.suspended"
	EventMailboxUpdated       = "mailbox.updated"
	EventMailboxCancelled     = "mailbox.cancelled"
	EventPartnerBalanceLow    = "partner.balance_low"
	EventPricingUpdated       = "pricing.updated"
	// EventDomainVerificationRequired asks the registrant to confirm their email.
	EventDomainVerificationRequired = "domain.verification_required"
	EventDomainTransferRequested    = "domain.transfer_requested"
	// EventDomainConnected and EventDomainDisconnected are an existing domain's delegation coming and going.
	EventDomainConnected    = "domain.connected"
	EventDomainDisconnected = "domain.disconnected"
	// EventSendingUpdated is GraphKit's own sending service, which the store does not use.
	EventSendingUpdated = "sending.updated"
	// Social accounts: the workspace mode, connect sessions, accounts, actions and conversations.
	EventSocialUpdated            = "social.updated"
	EventSocialConnectionUpdated  = "social.connection.updated"
	EventSocialAccountUpdated     = "social.account.updated"
	EventSocialActionUpdated      = "social.action.updated"
	EventSocialMessageReceived    = "social.message.received"
	EventSocialMessageSent        = "social.message.sent"
	EventSocialInvitationAccepted = "social.invitation.accepted"
)

// Pagination is the list cursor.
type Pagination struct {
	NextCursor string `json:"next_cursor"`
	HasMore    bool   `json:"has_more"`
}

// Page is one page of a list.
type Page[T any] struct {
	Data       []T        `json:"data"`
	Pagination Pagination `json:"pagination"`
}

// PartnerBalance is this instance's prepaid balance at GraphKit.
type PartnerBalance struct {
	BalanceMicros      int64  `json:"balance_micros"`
	LowThresholdMicros int64  `json:"low_threshold_micros"`
	Currency           string `json:"currency"`
	Livemode           bool   `json:"livemode"`
}
