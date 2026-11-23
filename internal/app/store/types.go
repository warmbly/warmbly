package store

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// Card is the saved card a checkout tops up from.
type Card struct {
	Brand string `json:"brand"`
	Last4 string `json:"last4"`
}

// BalanceView is the workspace balance shown at checkout.
type BalanceView struct {
	BalanceMicros int64 `json:"balance_micros"`
	Card          *Card `json:"card"`
}

// Status is GET /v1/store/status.
type Status struct {
	Enabled bool `json:"enabled"`
	// Configured is whether the instance has a GraphKit connection at all.
	Configured bool `json:"configured"`
	// Reason says why Enabled is false: ReasonNotConfigured or ReasonPartnerManaged.
	Reason string `json:"reason,omitempty"`
	// SalesPaused is set while the operator has paused new purchases; everything already bought keeps working.
	SalesPaused bool `json:"sales_paused"`
	// Locked is set with ReasonNotConfigured on a self-host, which shows buying as a feature to set up; a hosted instance hides it.
	Locked bool `json:"locked"`
	// CanConfigure is set with Locked for an instance admin who may add the GraphKit API key.
	CanConfigure bool `json:"can_configure"`
	// ConfigureURL is the admin panel page that takes the key, when the panel's address is known.
	ConfigureURL string `json:"configure_url,omitempty"`
	// Disconnected counts this workspace's live purchases while GraphKit is not configured.
	Disconnected         *Disconnected   `json:"disconnected,omitempty"`
	Metered              bool            `json:"metered"`
	Currency             string          `json:"currency"`
	MailboxMonthlyMicros int64           `json:"mailbox_monthly_micros"`
	DomainMarkup         string          `json:"domain_markup"`
	Limits               graphkit.Limits `json:"limits"`
	Providers            []string        `json:"providers"`
	Balance              *BalanceView    `json:"balance"`
	// PlanActive is false when the workspace has no paid plan, which buying needs.
	PlanActive bool        `json:"plan_active"`
	Registrant *Registrant `json:"registrant"`
	// RegistrantPrefill is a draft from the billing details while none is saved.
	RegistrantPrefill *Registrant `json:"registrant_prefill"`
	// StripePublishableKey lets a metered checkout save a card in the page; "" when there is none.
	StripePublishableKey string `json:"stripe_publishable_key,omitempty"`
	// MinCardChargeMicros is the least a store charge puts on the card.
	MinCardChargeMicros int64 `json:"min_card_charge_micros"`
	// BillingDay is the day of the month mailboxes are billed (1-28); nil before the first mailbox.
	BillingDay *int `json:"billing_day"`
	// NextBundle is the next monthly mailbox charge; nil when nothing is due.
	NextBundle *NextBundle `json:"next_bundle"`
	// DefaultForwardingURL is the website new domains forward to unless the buyer changes it; "" for none.
	DefaultForwardingURL string `json:"default_forwarding_url"`
	// Photos is whether mailbox profile photos can be uploaded here.
	Photos bool `json:"photos"`
}

// Why the store is off for a workspace.
const (
	ReasonNotConfigured  = "not_configured"
	ReasonPartnerManaged = "partner_managed"
)

// NextBundle is the next mailbox charge: on charge_on, for mailboxes, of amount_micros.
type NextBundle struct {
	ChargeOn  time.Time `json:"charge_on"`
	Mailboxes int       `json:"mailboxes"`
	// SocialSeats are the social accounts collected in the same charge.
	SocialSeats  int   `json:"social_seats"`
	AmountMicros int64 `json:"amount_micros"`
}

// Offer is one domain search result at retail.
type Offer struct {
	Domain      string `json:"domain"`
	DisplayName string `json:"display_name"`
	TLD         string `json:"tld"`
	Available   bool   `json:"available"`
	Exact       bool   `json:"exact"`
	// Years is the TLD's minimum term, which PriceMicros covers; RenewMicros is per year.
	Years       int     `json:"years"`
	PriceMicros int64   `json:"price_micros"`
	RenewMicros int64   `json:"renew_micros"`
	Reason      *string `json:"reason"`
}

// SearchResult is GET /v1/store/domains/search.
type SearchResult struct {
	Query string  `json:"query"`
	Data  []Offer `json:"data"`
}

// QuoteLine is one line of a quote at retail.
type QuoteLine struct {
	Type          string `json:"type"`
	Domain        string `json:"domain"`
	Years         int    `json:"years,omitempty"`
	Provider      string `json:"provider,omitempty"`
	Count         int    `json:"count,omitempty"`
	AmountMicros  int64  `json:"amount_micros"`
	RenewMicros   int64  `json:"renew_micros,omitempty"`
	MonthlyMicros int64  `json:"monthly_micros,omitempty"`
	// AdminEmail is the mailbox that becomes the domain's Workspace or tenant admin; nil when it has one.
	AdminEmail *string `json:"admin_email,omitempty"`
	// NameserversChange means the customer sets new nameservers for a connected domain after the order.
	NameserversChange bool `json:"nameservers_change,omitempty"`
}

// Payment is how a checkout will be paid: from the balance, then the card,
// which tops the balance up by at least the store's minimum.
type Payment struct {
	BalanceMicros     int64 `json:"balance_micros"`
	FromBalanceMicros int64 `json:"from_balance_micros"`
	CardChargeMicros  int64 `json:"card_charge_micros"`
	// BalanceAfterMicros is what stays on the balance once the card was charged and the order paid.
	BalanceAfterMicros int64       `json:"balance_after_micros"`
	Card               *Card       `json:"card"`
	NeedsPaymentMethod bool        `json:"needs_payment_method"`
	AutoReload         *AutoReload `json:"auto_reload"`
}

// AutoReload is the workspace's automatic reload as it will be after the order.
type AutoReload struct {
	Enabled         bool  `json:"enabled"`
	ThresholdMicros int64 `json:"threshold_micros"`
	AmountMicros    int64 `json:"amount_micros"`
	// StoreManaged is a reload the store turned on and keeps sized to the mailboxes until it is edited by hand.
	StoreManaged bool `json:"store_managed"`
}

// Quote is POST /v1/store/quotes.
type Quote struct {
	ID                        uuid.UUID   `json:"id"`
	ExpiresAt                 time.Time   `json:"expires_at"`
	Lines                     []QuoteLine `json:"lines"`
	DueNowMicros              int64       `json:"due_now_micros"`
	RecurringMonthlyMicros    int64       `json:"recurring_monthly_micros"`
	YearlyDomainRenewalMicros int64       `json:"yearly_domain_renewal_micros"`
	Payment                   Payment     `json:"payment"`
}

// quoteSnapshot is what store_quotes and store_orders keep of a quote.
type quoteSnapshot struct {
	GraphKitQuoteID string               `json:"graphkit_quote_id"`
	Items           []graphkit.QuoteItem `json:"items"`
	Lines           []QuoteLine          `json:"lines"`
	Wholesale       []graphkit.QuoteLine `json:"wholesale"`
	DueNowMicros    int64                `json:"due_now_micros"`
	RecurringMicros int64                `json:"recurring_monthly_micros"`
	RenewalMicros   int64                `json:"yearly_domain_renewal_micros"`
	MailboxMonthly  int64                `json:"mailbox_monthly_micros"`
	ExpiresAt       time.Time            `json:"expires_at"`
	// Photos is the uploaded photo a domain's mailboxes take, by domain, or one mailbox's, by its address.
	Photos map[string]uuid.UUID `json:"photos,omitempty"`
	// Signatures is each mailbox's finished signature, by its address.
	Signatures map[string]MailboxSignature `json:"signatures,omitempty"`
}

// Step is one stage of an order's progress track.
type Step struct {
	Key    string     `json:"key"`
	Status string     `json:"status"`
	At     *time.Time `json:"at"`
}

// MailAuth is the state of a domain's mail records.
type MailAuth struct {
	MX    string `json:"mx"`
	SPF   string `json:"spf"`
	DKIM  string `json:"dkim"`
	DMARC string `json:"dmarc"`
}

// DNSRecord is one record in a store domain's zone. Managed records are the
// ones kept for mail and forwarding and cannot be changed.
type DNSRecord struct {
	ID       uuid.UUID `json:"id"`
	Type     string    `json:"type"`
	Name     string    `json:"name"`
	Content  string    `json:"content"`
	TTL      int       `json:"ttl"`
	Priority *int      `json:"priority"`
	Managed  bool      `json:"managed"`
	Purpose  string    `json:"purpose"`
	// Status is a DNS check's verdict: ok or missing.
	Status string `json:"status,omitempty"`
}

// DNSCheck is POST /v1/store/domains/:id/dns/check.
type DNSCheck struct {
	Nameservers []string    `json:"nameservers"`
	Records     []DNSRecord `json:"records"`
}

// DNSRecordInput is the body of POST /v1/store/domains/:id/dns.
type DNSRecordInput struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl"`
	Priority *int   `json:"priority"`
}

// DNSRecordPatch is the body of PATCH /v1/store/domains/:id/dns/:record_id.
type DNSRecordPatch struct {
	Name     *string `json:"name"`
	Content  *string `json:"content"`
	TTL      *int    `json:"ttl"`
	Priority *int    `json:"priority"`
}

// DomainConnection is how far a domain registered elsewhere is connected.
type DomainConnection struct {
	Method             string     `json:"method"`
	Status             string     `json:"status"`
	ExpiresAt          *time.Time `json:"expires_at"`
	CheckedAt          *time.Time `json:"checked_at"`
	CurrentNameservers []string   `json:"current_nameservers"`
}

// DNSConflict is a record that blocks mail; RecordID is set when it can be removed here.
type DNSConflict struct {
	RecordID *uuid.UUID `json:"record_id"`
	Type     string     `json:"type"`
	Name     string     `json:"name"`
	Content  string     `json:"content"`
}

// OwnerVerification is the domain owner's email check, which has a deadline while pending.
type OwnerVerification struct {
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

// Domain is a domain bought in the store, or one registered elsewhere and connected.
type Domain struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Status string    `json:"status"`
	// Source is purchased or connected.
	Source   string `json:"source"`
	Provider string `json:"provider"`
	// Renewable is a purchased domain that renewal, auto-renew and transfer-out apply to.
	Renewable         bool               `json:"renewable"`
	ExpiresAt         *time.Time         `json:"expires_at"`
	AutoRenew         bool               `json:"auto_renew"`
	RenewMicros       int64              `json:"renew_micros"`
	ForwardingURL     string             `json:"forwarding_url"`
	ForwardingStatus  string             `json:"forwarding_status"`
	MailAuth          MailAuth           `json:"mail_auth"`
	MailboxCount      int                `json:"mailbox_count"`
	Nameservers       []string           `json:"nameservers"`
	Connection        *DomainConnection  `json:"connection"`
	DNSConflicts      []DNSConflict      `json:"dns_conflicts"`
	OwnerVerification *OwnerVerification `json:"owner_verification"`
	Transfer          *TransferRequest   `json:"transfer"`
	TrackingHost      string             `json:"tracking_host"`
	CreatedAt         time.Time          `json:"created_at"`
	DNS               []DNSRecord        `json:"dns,omitempty"`
}

// ConnectRequest is the body of POST /v1/store/domains/connect.
type ConnectRequest struct {
	Domain string `json:"domain"`
	// Method is nameservers (move the domain's DNS here) or manual (add records at its DNS provider).
	Method string `json:"method"`
}

// Mailbox is a purchased mailbox.
type Mailbox struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	DomainID  uuid.UUID `json:"domain_id"`
	Provider  string    `json:"provider"`
	Status    string    `json:"status"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	IsAdmin   bool      `json:"is_admin"`
	// Paused is campaign sending and warmup stopped because the workspace has no plan; the mailbox still bills.
	Paused        bool       `json:"paused"`
	MonthlyMicros int64      `json:"monthly_micros"`
	PaidThrough   time.Time  `json:"paid_through"`
	CancelsAt     *time.Time `json:"cancels_at"`
	// CancelledAt is when the ended mailbox was switched off in Warmbly; its history stays until disconnected.
	CancelledAt    *time.Time `json:"cancelled_at"`
	EmailAccountID *uuid.UUID `json:"email_account_id"`
	ConnectState   string     `json:"connect_state"`
	// ActionRequired is microsoft_consent while the domain's Microsoft 365 administrator has not approved Warmbly, else null.
	ActionRequired *string `json:"action_required"`
	// ActionReason is why the automatic approval left it to the administrator, set only with ActionRequired.
	ActionReason *string `json:"action_reason"`
	// Photo is the profile photo at the provider: none, pending (sent, not taken yet) or set.
	Photo     string    `json:"photo"`
	CreatedAt time.Time `json:"created_at"`
}

// Order is one checkout and its live progress.
type Order struct {
	ID             uuid.UUID  `json:"id"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	CompletedAt    *time.Time `json:"completed_at"`
	DueNowMicros   int64      `json:"due_now_micros"`
	ChargedMicros  int64      `json:"charged_micros"`
	RefundedMicros int64      `json:"refunded_micros"`
	Steps          []Step     `json:"steps"`
	Domains        []Domain   `json:"domains"`
	Mailboxes      []Mailbox  `json:"mailboxes"`
}

// Page is a list page with an opaque cursor.
type Page[T any] struct {
	Data       []T        `json:"data"`
	Pagination Pagination `json:"pagination"`
}

// Pagination is the list cursor shape every Warmbly list uses.
type Pagination struct {
	NextCursor *string `json:"next_cursor"`
	HasMore    bool    `json:"has_more"`
}

// QuoteRequest is the body of POST /v1/store/quotes. A mailboxes item may carry
// photo_id for every mailbox on its domain, and each mailbox its own photo_id
// and signature; they are kept here and never sent to GraphKit.
type QuoteRequest struct {
	Items []graphkit.QuoteItem `json:"items"`
	// Photos maps a domain, or one mailbox's address on it, to the photo it takes.
	Photos map[string]uuid.UUID `json:"-"`
	// Signatures maps a mailbox's address to the signature it signs with.
	Signatures map[string]MailboxSignature `json:"-"`
}

// UnmarshalJSON reads items as GraphKit's, plus each item's photo_id.
func (q *QuoteRequest) UnmarshalJSON(b []byte) error {
	var raw struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	q.Items, q.Photos, q.Signatures = make([]graphkit.QuoteItem, 0, len(raw.Items)), nil, nil
	for _, r := range raw.Items {
		var it graphkit.QuoteItem
		if err := json.Unmarshal(r, &it); err != nil {
			return err
		}
		var extra struct {
			PhotoID   *uuid.UUID `json:"photo_id"`
			Mailboxes []struct {
				LocalPart string            `json:"local_part"`
				PhotoID   *uuid.UUID        `json:"photo_id"`
				Signature *MailboxSignature `json:"signature"`
			} `json:"mailboxes"`
		}
		if err := json.Unmarshal(r, &extra); err != nil {
			return err
		}
		q.addPhoto(it.Domain, extra.PhotoID)
		for _, m := range extra.Mailboxes {
			addr := strings.TrimSpace(m.LocalPart) + "@" + strings.TrimSpace(it.Domain)
			q.addPhoto(addr, m.PhotoID)
			if m.Signature != nil {
				if q.Signatures == nil {
					q.Signatures = map[string]MailboxSignature{}
				}
				q.Signatures[photoKey(addr)] = *m.Signature
			}
		}
		q.Items = append(q.Items, it)
	}
	return nil
}

func (q *QuoteRequest) addPhoto(key string, id *uuid.UUID) {
	if id == nil || *id == uuid.Nil {
		return
	}
	if q.Photos == nil {
		q.Photos = map[string]uuid.UUID{}
	}
	q.Photos[photoKey(key)] = *id
}

// photoKey is how a domain or a mailbox address names its photo: lowercase, no trailing dot.
func photoKey(s string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), ".")))
}

// MailboxPhotoRequest is the body of PUT /v1/store/mailboxes/:id/photo.
type MailboxPhotoRequest struct {
	PhotoID uuid.UUID `json:"photo_id"`
}

// OrderRequest is the body of POST /v1/store/orders.
type OrderRequest struct {
	QuoteID       uuid.UUID `json:"quote_id"`
	ForwardingURL string    `json:"forwarding_url"`
	// ExpectedDueNowMicros is the total the buyer saw; a different total is refused, not charged.
	ExpectedDueNowMicros *int64 `json:"expected_due_now_micros"`
	// EnableAutoReload turns the balance's automatic reload on, sized to the mailboxes, with the first
	// purchase; nil means yes unless the workspace set automatic reload up itself.
	EnableAutoReload *bool `json:"enable_auto_reload"`
}

// DomainPatch is the body of PATCH /v1/store/domains/:id.
type DomainPatch struct {
	AutoRenew     *bool   `json:"auto_renew"`
	ForwardingURL *string `json:"forwarding_url"`
}

// SignIn is a mailbox's sign-in, shown once; for the admin mailbox it opens the admin console.
type SignIn struct {
	Email           string  `json:"email"`
	Username        string  `json:"username"`
	Password        string  `json:"password"`
	LoginURL        string  `json:"login_url"`
	AdminConsoleURL *string `json:"admin_console_url"`
}

// MailboxPatch is the body of PATCH /v1/store/mailboxes/:id.
type MailboxPatch struct {
	FirstName *string `json:"first_name"`
	LastName  *string `json:"last_name"`
}
