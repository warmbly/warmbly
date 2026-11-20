package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Store order statuses. "charging" is before GraphKit accepted the order.
const (
	StoreOrderCharging        = "charging"
	StoreOrderProcessing      = "processing"
	StoreOrderCompleted       = "completed"
	StoreOrderPartiallyFailed = "partially_failed"
	StoreOrderFailed          = "failed"
)

// Purchased mailbox connect states.
const (
	StoreConnectWaiting    = "waiting"
	StoreConnectConnecting = "connecting"
	StoreConnectConnected  = "connected"
	StoreConnectFailed     = "failed"
)

// Where a store domain came from.
const (
	StoreDomainPurchased = "purchased"
	StoreDomainConnected = "connected"
)

// StoreAccount is a workspace's GraphKit account and its registrant.
type StoreAccount struct {
	OrganizationID    uuid.UUID
	GraphKitAccountID string
	Registrant        json.RawMessage
	IdentityDigest    string
	PlanNoticeAt      *time.Time
	// BillDay is the day of the month mailboxes are billed (1-28); 0 until the first mailbox.
	BillDay        int
	NextBillAnchor *time.Time
	// The automatic reload the store manages: decided once, managed until the workspace edits it by hand.
	ReloadDecided   bool
	ReloadManaged   bool
	ReloadThreshold Micro
	ReloadAmount    Micro
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// StoreQuote is a GraphKit quote with the retail prices the buyer saw.
type StoreQuote struct {
	ID              uuid.UUID
	OrganizationID  uuid.UUID
	GraphKitQuoteID string
	Snapshot        json.RawMessage
	ExpiresAt       time.Time
	// ReplacedBy is the quote this one was priced again as once it expired.
	ReplacedBy *uuid.UUID
	CreatedAt  time.Time
}

// StoreOrder is one checkout.
type StoreOrder struct {
	ID              uuid.UUID
	OrganizationID  uuid.UUID
	QuoteID         uuid.UUID
	GraphKitOrderID *string
	Status          string
	Quote           json.RawMessage
	Steps           json.RawMessage
	DueNowMicros    Micro
	ChargeKey       string
	ChargedMicros   Micro
	RefundedMicros  Micro
	RefundedItems   []string
	ForwardingURL   string
	FailureCode     string
	// RefundPending is a refund that did not go through yet; the reconciler retries it.
	RefundPending bool
	CreatedBy     *uuid.UUID
	CreatedAt     time.Time
	UpdatedAt     time.Time
	CompletedAt   *time.Time
}

// StoreDomain is a domain a workspace bought.
type StoreDomain struct {
	ID                uuid.UUID
	OrganizationID    uuid.UUID
	OrderID           *uuid.UUID
	GraphKitDomainID  string
	Name              string
	Status            string
	Source            string
	Provider          string
	ExpiresAt         *time.Time
	AutoRenew         bool
	RenewRetailMicros Micro
	// LastSeenRenewMicros is the renewal price the workspace was last told.
	LastSeenRenewMicros Micro
	RenewalNoticeAt     *time.Time
	ForwardingURL       string
	MailAuth            json.RawMessage
	Details             json.RawMessage
	TrackingHost        string
	RenewalAttemptAt    *time.Time
	RenewalFailures     int
	// RenewalChargeKey is a renewal charged and not yet renewed or refunded.
	RenewalChargeKey *string
	LastNotice       string
	MailboxCount     int
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// StoreMailbox is a mailbox a workspace bought.
type StoreMailbox struct {
	ID                  uuid.UUID
	OrganizationID      uuid.UUID
	DomainID            uuid.UUID
	OrderID             *uuid.UUID
	GraphKitMailboxID   string
	Email               string
	Provider            string
	Status              string
	FirstName           string
	LastName            string
	IsAdmin             bool
	MonthlyRetailMicros Micro
	PaidThrough         time.Time
	// PeriodEnd is GraphKit's current_period_end: its next month is charged to the partner then.
	PeriodEnd *time.Time
	// UnpaidCancel is a cancellation the store sent because the month went unpaid.
	UnpaidCancel bool
	CancelsAt    *time.Time
	// CancelledAt is when the cancellation was applied here: the Warmbly mailbox switched off.
	CancelledAt     *time.Time
	EmailAccountID  *uuid.UUID
	ConnectMethod   string
	ConnectState    string
	ConnectError    string
	ConnectAttempts int
	// ActionRequired is the step only a person can take before the mailbox connects (ActionMicrosoftConsent), nil when none.
	ActionRequired *string
	// ActionReason is why it fell to a person: what stopped the automatic approval (ConsentReason*), nil when unknown.
	ActionReason     *string
	BillingFailures  int
	PendingChargeKey *string
	// PendingChargeMicros is what the pending charge takes for this mailbox, fixed when it was reserved.
	PendingChargeMicros Micro
	// PaidKey and PaidMicros are the charge that collected the month ending at PaidThrough; nil when no charge of its own did.
	PaidKey    *string
	PaidMicros Micro
	LastNotice string
	// PhotoID is the profile photo the mailbox should carry; PhotoAppliedID the one GraphKit last accepted.
	PhotoID        *uuid.UUID
	PhotoAppliedID *uuid.UUID
	// SignatureHTML is the signature its order line chose (nil for none chosen); the stamps say where it was set.
	SignatureHTML       *string
	SignaturePlain      string
	SignatureSetAt      *time.Time
	SignatureProviderAt *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// StorePhotoKeyPrefix is where uploaded mailbox photos live: store-photos/<organization id>/<photo id>.jpg, never served publicly.
const StorePhotoKeyPrefix = "store-photos/"

// StorePhotoKey is the private object key of one uploaded photo.
func StorePhotoKey(orgID, id uuid.UUID) string {
	return StorePhotoKeyPrefix + orgID.String() + "/" + id.String() + ".jpg"
}

// StorePhoto is a profile photo uploaded for purchased mailboxes, a normalised JPEG.
type StorePhoto struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	BlobKey        string
	SizeBytes      int
	CreatedBy      *uuid.UUID
	CreatedAt      time.Time
	UsedAt         *time.Time
}

// StorePage is a keyset position in a store list: the last row's creation time and id.
type StorePage struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// StoreAdminOrderFilter narrows the operator's instance-wide order list.
type StoreAdminOrderFilter struct {
	OrgID       *uuid.UUID
	Status      string
	FailureCode string
	// Stuck is an order in flight for longer than it should be: charged and never placed, or setting up for days.
	Stuck bool
	// Query is an order id, or a substring of the workspace name.
	Query string
}

// StoreAdminDomainFilter narrows the operator's instance-wide domain list.
type StoreAdminDomainFilter struct {
	OrgID         *uuid.UUID
	Status        string
	AutoRenew     *bool
	ExpiresBefore *time.Time
	RenewalFailed bool
	Query         string
}

// StoreAdminMailboxFilter narrows the operator's instance-wide mailbox list.
type StoreAdminMailboxFilter struct {
	OrgID         *uuid.UUID
	Status        string
	ConnectState  string
	BillingFailed bool
	Query         string
}

// StoreAdminCounts is the store's instance-wide state at a glance.
type StoreAdminCounts struct {
	OrdersOpen             int `json:"orders_open"`
	OrdersStuck            int `json:"orders_stuck"`
	OrdersFailed7d         int `json:"orders_failed_7d"`
	ConnectsFailed         int `json:"connects_failed"`
	MailboxesLive          int `json:"mailboxes_live"`
	MailboxesBillingFailed int `json:"mailboxes_billing_failed"`
	DomainsLive            int `json:"domains_live"`
	DomainsRenewalFailed   int `json:"domains_renewal_failed"`
	DomainsExpiring30d     int `json:"domains_expiring_30d"`
}

// StoreLiveMailboxes is how many live purchased mailboxes one provider has and what they bill a month.
type StoreLiveMailboxes struct {
	Provider      string
	Count         int
	MonthlyMicros Micro
}

// StoreOrgSummary is one workspace's purchased infrastructure, for an agency's overview.
type StoreOrgSummary struct {
	OrganizationID     uuid.UUID
	Domains            int
	MailboxesLive      int
	MailboxesConnected int
	MonthlyMicros      Micro
	Renewals30d        int
	OrdersOpen         int
	ConnectFailed      int
	BillingFailed      int
	RenewalFailed      int
	AutoRenewOff       int
	OrdersFailed       int
}
