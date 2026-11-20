package models

import (
	"time"

	"github.com/google/uuid"
)

// PartnerEnablement is what a partner lets one mailbox of a workspace it manages do, and what it is billed for.
type PartnerEnablement string

const (
	PartnerEnablementOff           PartnerEnablement = "off"
	PartnerEnablementWarmupOnly    PartnerEnablement = "warmup_only"
	PartnerEnablementSendingWarmup PartnerEnablement = "sending_warmup"
)

func (e PartnerEnablement) Valid() bool {
	return e == PartnerEnablementOff || e == PartnerEnablementWarmupOnly || e == PartnerEnablementSendingWarmup
}

// Campaigns reports whether campaigns may send from the mailbox. Mirrored in SQL by repository.partnerLaneSQL.
func (e PartnerEnablement) Campaigns() bool { return e == PartnerEnablementSendingWarmup }

// Warmup reports whether the mailbox may warm up. Mirrored in SQL by repository.partnerLaneSQL.
func (e PartnerEnablement) Warmup() bool {
	return e == PartnerEnablementSendingWarmup || e == PartnerEnablementWarmupOnly
}

// Partner mailbox sources: bought through the partner's infrastructure, an outside mailbox the
// partner connected, or one connected inside Warmbly.
const (
	PartnerSourceInfrastructure = "graphkit_infrastructure"
	PartnerSourcePartner        = "partner"
	PartnerSourceManual         = "manual"
)

// Partner mailbox connect states.
const (
	PartnerConnectPending    = "pending"
	PartnerConnectConnecting = "connecting"
	PartnerConnectConnected  = "connected"
	PartnerConnectFailed     = "failed"
)

// ActionMicrosoftConsent is a purchased Microsoft 365 mailbox waiting for its tenant's administrator to approve
// this instance's app. The only value of partner_mailboxes.action_required and store_mailboxes.action_required.
const ActionMicrosoftConsent = "microsoft_consent"

// Why the automatic Microsoft 365 approval with a purchased domain's admin mailbox stopped and left it to a person;
// the values of store_mailboxes.action_reason.
const (
	ConsentReasonMFA         = "mfa_required"
	ConsentReasonPassword    = "wrong_password"
	ConsentReasonNotAdmin    = "not_admin"
	ConsentReasonWrongTenant = "wrong_tenant"
	ConsentReasonBlocked     = "sign_in_blocked"
	ConsentReasonNoSignIn    = "no_admin_sign_in"
	ConsentReasonFailed      = "failed"
)

// PartnerMailbox is one mailbox of a partner-managed workspace as the partner API knows it.
type PartnerMailbox struct {
	ID               uuid.UUID         `json:"id"`
	PartnerID        uuid.UUID         `json:"partner_id"`
	Mode             PartnerMode       `json:"mode"`
	OrgExternalID    string            `json:"org_external_id"`
	OrganizationID   *uuid.UUID        `json:"organization_id"`
	EmailAccountID   *uuid.UUID        `json:"email_account_id"`
	PartnerMailboxID *string           `json:"partner_mailbox_id"`
	Source           string            `json:"source"`
	Email            string            `json:"email"`
	Provider         string            `json:"provider"`
	FirstName        string            `json:"first_name"`
	LastName         string            `json:"last_name"`
	Enablement       PartnerEnablement `json:"enablement"`
	ConnectionMethod *string           `json:"connection_method"`
	ConnectState     string            `json:"connect_state"`
	ConnectError     *string           `json:"connect_error"`
	ConnectAttempts  int               `json:"connect_attempts"`
	NextConnectAt    *time.Time        `json:"next_connect_at"`
	DetachedAt       *time.Time        `json:"detached_at"`
	RemovedAt        *time.Time        `json:"removed_at"`
	PublishedState   *string           `json:"-"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

// Attached reports whether the partner attached the mailbox and still holds it: members cannot remove or reconnect it.
func (m *PartnerMailbox) Attached() bool {
	return m != nil && m.Source != PartnerSourceManual && m.DetachedAt == nil && m.RemovedAt == nil
}

// PartnerMailboxDays is one mailbox's enabled UTC days in a month.
type PartnerMailboxDays struct {
	MailboxID         uuid.UUID `json:"mailbox_id"`
	OrgExternalID     string    `json:"external_id"`
	PartnerMailboxID  *string   `json:"partner_mailbox_id"`
	Email             string    `json:"email"`
	SendingWarmupDays int       `json:"sending_warmup_days"`
	WarmupOnlyDays    int       `json:"warmup_only_days"`
}

// PartnerGroup is a partner's group of workspaces and the brand they show.
type PartnerGroup struct {
	ID               uuid.UUID   `json:"id"`
	PartnerID        uuid.UUID   `json:"partner_id"`
	Mode             PartnerMode `json:"mode"`
	ExternalID       string      `json:"external_id"`
	Name             string      `json:"name"`
	ProductName      string      `json:"product_name"`
	LogoURL          string      `json:"logo_url"`
	AccentColor      string      `json:"accent_color"`
	WebsiteURL       string      `json:"website_url"`
	HidePoweredBy    bool        `json:"hide_powered_by"`
	PortalDomain     string      `json:"portal_domain"`
	PortalStatus     string      `json:"portal_status"`
	PortalCheckedAt  *time.Time  `json:"portal_checked_at"`
	PortalVerifiedAt *time.Time  `json:"portal_verified_at"`
}

// Branded reports whether the group carries a brand of its own.
func (g *PartnerGroup) Branded() bool {
	return g != nil && (g.ProductName != "" || g.LogoURL != "" || g.AccentColor != "")
}

// PartnerStatement is a partner's closed month of enabled mailbox-days.
type PartnerStatement struct {
	ID                     uuid.UUID              `json:"id"`
	PartnerID              uuid.UUID              `json:"partner_id"`
	Mode                   PartnerMode            `json:"mode"`
	Month                  string                 `json:"month"`
	SendingWarmupDays      int64                  `json:"sending_warmup_days"`
	WarmupOnlyDays         int64                  `json:"warmup_only_days"`
	SendingWarmupDayMicros int64                  `json:"sending_warmup_day_micros"`
	WarmupOnlyDayMicros    int64                  `json:"warmup_only_day_micros"`
	AmountMicros           int64                  `json:"amount_micros"`
	ClosedAt               *time.Time             `json:"closed_at"`
	Lines                  []PartnerStatementLine `json:"lines"`
}

// PartnerStatementLine is one workspace's share of a statement.
type PartnerStatementLine struct {
	OrgExternalID     string `json:"external_id"`
	SendingWarmupDays int    `json:"sending_warmup_days"`
	WarmupOnlyDays    int    `json:"warmup_only_days"`
	AmountMicros      int64  `json:"amount_micros"`
}

// PartnerScoreMinDeliveries is the sample a mailbox's warmup score needs: verified warmup deliveries
// at Google, Microsoft and Yahoo in seven days, the spam-placement band's own floor.
const PartnerScoreMinDeliveries = 20
