package models

import (
	"time"

	"github.com/google/uuid"
)

// Pool link: a self-hosted instance's mailboxes warmed by the hosted pool.

// PoolLinkCodeStatus is the lifecycle of one device-code handshake.
type PoolLinkCodeStatus string

const (
	PoolLinkCodePending  PoolLinkCodeStatus = "pending"
	PoolLinkCodeApproved PoolLinkCodeStatus = "approved"
	// Claimed: the instance has fetched its token, the code is spent.
	PoolLinkCodeClaimed PoolLinkCodeStatus = "claimed"
	PoolLinkCodeDenied  PoolLinkCodeStatus = "denied"
)

// PoolLinkCode is one handshake row on the cloud side.
type PoolLinkCode struct {
	ID              uuid.UUID          `json:"id"`
	UserCode        string             `json:"user_code"`
	InstanceName    string             `json:"instance_name"`
	InstanceURL     string             `json:"instance_url"`
	InstanceVersion string             `json:"instance_version"`
	Status          PoolLinkCodeStatus `json:"status"`
	OrganizationID  *uuid.UUID         `json:"organization_id,omitempty"`
	InstanceID      *uuid.UUID         `json:"instance_id,omitempty"`
	ExpiresAt       time.Time          `json:"expires_at"`
	CreatedAt       time.Time          `json:"created_at"`
}

// PoolLinkInstance is a linked self-hosted instance as the cloud sees it.
type PoolLinkInstance struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	Name           string     `json:"name"`
	URL            string     `json:"url"`
	Version        string     `json:"version"`
	CreatedBy      *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	LastSeenAt     *time.Time `json:"last_seen_at,omitempty"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
	// MailboxCount is filled by list endpoints only.
	MailboxCount int `json:"mailbox_count"`
}

// PoolLinkMailbox ties an enrolled cloud mailbox back to the remote one.
type PoolLinkMailbox struct {
	InstanceID     uuid.UUID `json:"instance_id"`
	RemoteID       uuid.UUID `json:"remote_id"`
	EmailAccountID uuid.UUID `json:"email_account_id"`
	EnrolledAt     time.Time `json:"enrolled_at"`
	// Managed: the cloud holds the only credential and the instance sends with brokered tokens.
	Managed     bool       `json:"managed"`
	LastTokenAt *time.Time `json:"last_token_at,omitempty"`
}

// PoolLinkStartRequest is what a self-hosted instance sends to begin linking.
type PoolLinkStartRequest struct {
	InstanceName    string `json:"instance_name"`
	InstanceURL     string `json:"instance_url"`
	InstanceVersion string `json:"instance_version"`
}

// PoolLinkStartResponse is the device-code grant.
type PoolLinkStartResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// PoolLinkPollResponse is the poll answer; the token is present only once approved.
type PoolLinkPollResponse struct {
	Status        PoolLinkCodeStatus `json:"status"`
	InstanceID    *uuid.UUID         `json:"instance_id,omitempty"`
	InstanceToken string             `json:"instance_token,omitempty"`
	Organization  *PoolLinkOrgInfo   `json:"organization,omitempty"`
}

// PoolLinkOrgInfo is the cloud workspace a link belongs to.
type PoolLinkOrgInfo struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// PoolLinkPlan is the linked instance's allowance as the cloud computes it.
type PoolLinkPlan struct {
	// Tier is "free" or "paid".
	Tier string `json:"tier"`
	// MailboxLimit is nil when unlimited.
	MailboxLimit *int `json:"mailbox_limit"`
	Enrolled     int  `json:"enrolled"`
	// PriceUSD is the monthly price of the paid tier, for the upgrade card.
	PriceUSD int `json:"price_usd"`
	// UpgradeURL is empty when billing is off or the plan has no price.
	UpgradeURL string `json:"upgrade_url,omitempty"`
	// WarmupEntitled is false when the cloud workspace itself cannot warm.
	WarmupEntitled bool `json:"warmup_entitled"`
}

// PoolLinkInstanceInfo is the instance-facing status document.
type PoolLinkInstanceInfo struct {
	Instance     PoolLinkInstance `json:"instance"`
	Organization PoolLinkOrgInfo  `json:"organization"`
	Plan         PoolLinkPlan     `json:"plan"`
}

// PoolLinkWarmupSettings is the requested ramp; zero values mean cloud defaults.
type PoolLinkWarmupSettings struct {
	Base      int    `json:"base"`
	Max       int    `json:"max"`
	Increase  int    `json:"increase"`
	ReplyRate int    `json:"reply_rate"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	Days      int    `json:"days"`
	Timezone  string `json:"timezone"`
}

// PoolLinkOAuthCredential carries a Gmail or Microsoft refresh grant.
type PoolLinkOAuthCredential struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// PoolLinkEnrollRequest enrolls one remote mailbox; one credential block matches Provider.
type PoolLinkEnrollRequest struct {
	RemoteID uuid.UUID                `json:"remote_id"`
	Email    string                   `json:"email"`
	Name     string                   `json:"name"`
	Provider InboxProvider            `json:"provider"`
	OAuth    *PoolLinkOAuthCredential `json:"oauth,omitempty"`
	SMTPIMAP *SmtpImap                `json:"smtp_imap,omitempty"`
	Warmup   PoolLinkWarmupSettings   `json:"warmup"`
}

// PoolLinkOAuthStartRequest asks the cloud for a Google or Microsoft consent URL on Warmbly's app.
type PoolLinkOAuthStartRequest struct {
	Provider  InboxProvider `json:"provider"`
	ReturnURL string        `json:"return_url"`
}

// PoolLinkOAuthStartResponse: open URL in the browser; redeem Session once the popup returns.
type PoolLinkOAuthStartResponse struct {
	URL     string `json:"url"`
	Session string `json:"session"`
}

// PoolLinkOAuthFinishRequest redeems a completed consent for its mailbox.
type PoolLinkOAuthFinishRequest struct {
	Session string `json:"session"`
}

// PoolLinkAdoptRequest links a mailbox already in the workspace to the instance.
type PoolLinkAdoptRequest struct {
	RemoteID       uuid.UUID `json:"remote_id"`
	EmailAccountID uuid.UUID `json:"email_account_id"`
}

// PoolLinkAccessToken is a short-lived provider token minted for a managed mailbox; never a refresh token.
type PoolLinkAccessToken struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
	Provider    string    `json:"provider"`
	Email       string    `json:"email"`
}

// PoolLinkWorkspaceMailbox is a workspace mailbox an instance may adopt.
type PoolLinkWorkspaceMailbox struct {
	ID       uuid.UUID `json:"id"`
	Email    string    `json:"email"`
	Name     string    `json:"name"`
	Provider string    `json:"provider"`
	Status   string    `json:"status"`
}

// PoolLinkWarmupDeliveryQuery asks the cloud whether one message that arrived
// in a mailbox it warms is its own warmup mail. The verify header does not
// survive every provider, so the instance cannot always tell on its own.
type PoolLinkWarmupDeliveryQuery struct {
	Sender    string `json:"sender"`
	MessageID string `json:"message_id"`
	Subject   string `json:"subject"`
}

// PoolLinkMailboxState is the per-mailbox view shown in both dashboards.
type PoolLinkMailboxState struct {
	RemoteID       uuid.UUID              `json:"remote_id"`
	EmailAccountID uuid.UUID              `json:"email_account_id"`
	Email          string                 `json:"email"`
	Name           string                 `json:"name"`
	Provider       string                 `json:"provider"`
	Status         string                 `json:"status"`
	EnrolledAt     time.Time              `json:"enrolled_at"`
	Managed        bool                   `json:"managed"`
	Warmup         *WarmupStatusInfo      `json:"warmup,omitempty"`
	Health         *WarmupHealthInfo      `json:"health,omitempty"`
	SentToday      int                    `json:"sent_today"`
	Sent7d         int                    `json:"sent_7d"`
	Replied7d      int                    `json:"replied_7d"`
	SpamPlaced7d   int                    `json:"spam_placed_7d"`
	Errors         []AccountError         `json:"errors,omitempty"`
	AuthState      string                 `json:"auth_state"`
	Settings       PoolLinkWarmupSettings `json:"settings"`
}

// PoolLinkMailboxPatch updates a mailbox's ramp or lifecycle on the cloud.
type PoolLinkMailboxPatch struct {
	// Lifecycle is "pause", "resume" or empty.
	Lifecycle string                   `json:"lifecycle,omitempty"`
	Warmup    *PoolLinkWarmupSettings  `json:"warmup,omitempty"`
	OAuth     *PoolLinkOAuthCredential `json:"oauth,omitempty"`
	SMTPIMAP  *SmtpImap                `json:"smtp_imap,omitempty"`
}

// CloudLink is the self-hosted instance's single link row; Token is never serialized.
type CloudLink struct {
	CloudURL         string     `json:"cloud_url"`
	InstanceID       uuid.UUID  `json:"instance_id"`
	Token            string     `json:"-"`
	OrganizationName string     `json:"organization_name"`
	ConnectedBy      *uuid.UUID `json:"connected_by,omitempty"`
	ConnectedAt      time.Time  `json:"connected_at"`
	LastSyncedAt     *time.Time `json:"last_synced_at,omitempty"`
	LastError        string     `json:"last_error,omitempty"`
}

// CloudLinkMailbox marks a local mailbox as warmed by the cloud.
type CloudLinkMailbox struct {
	EmailAccountID uuid.UUID `json:"email_account_id"`
	RemoteID       uuid.UUID `json:"remote_id"`
	EnrolledAt     time.Time `json:"enrolled_at"`
	// Managed: no local credential; the worker sends with tokens brokered by the cloud.
	Managed bool `json:"managed"`
}

// CloudLinkOAuthStart is the instance dashboard's handle on a cloud-brokered consent.
type CloudLinkOAuthStart struct {
	URL     string `json:"url"`
	Session string `json:"session"`
}

// CloudLinkStatus is the self-hosted dashboard's view of the link.
type CloudLinkStatus struct {
	Connected bool                  `json:"connected"`
	Link      *CloudLink            `json:"link,omitempty"`
	Info      *PoolLinkInstanceInfo `json:"info,omitempty"`
	// Reachable is false when the cloud could not be contacted; Link still comes from the local row.
	Reachable bool   `json:"reachable"`
	Error     string `json:"error,omitempty"`
	// DefaultCloudURL is what the connect flow proposes.
	DefaultCloudURL string `json:"default_cloud_url"`
}

// CloudLinkMailboxRow merges a local mailbox with its cloud warmup state.
type CloudLinkMailboxRow struct {
	ID         uuid.UUID             `json:"id"`
	Email      string                `json:"email"`
	Name       string                `json:"name"`
	Provider   string                `json:"provider"`
	Status     string                `json:"status"`
	Enrolled   bool                  `json:"enrolled"`
	EnrolledAt *time.Time            `json:"enrolled_at,omitempty"`
	Managed    bool                  `json:"managed"`
	Cloud      *PoolLinkMailboxState `json:"cloud,omitempty"`
}
