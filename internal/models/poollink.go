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
	RemoteOrganizationID *uuid.UUID         `json:"remote_organization_id,omitempty"`
	ID                   uuid.UUID          `json:"id"`
	UserCode             string             `json:"user_code"`
	InstanceName         string             `json:"instance_name"`
	InstanceURL          string             `json:"instance_url"`
	InstanceVersion      string             `json:"instance_version"`
	Status               PoolLinkCodeStatus `json:"status"`
	OrganizationID       *uuid.UUID         `json:"organization_id,omitempty"`
	InstanceID           *uuid.UUID         `json:"instance_id,omitempty"`
	ExpiresAt            time.Time          `json:"expires_at"`
	CreatedAt            time.Time          `json:"created_at"`
}

// PoolLinkInstance is a linked self-hosted instance as the cloud sees it.
type PoolLinkInstance struct {
	RemoteOrganizationID *uuid.UUID `json:"remote_organization_id,omitempty"`
	ID                   uuid.UUID  `json:"id"`
	OrganizationID       uuid.UUID  `json:"organization_id"`
	Name                 string     `json:"name"`
	URL                  string     `json:"url"`
	Version              string     `json:"version"`
	CreatedBy            *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	LastSeenAt           *time.Time `json:"last_seen_at,omitempty"`
	RevokedAt            *time.Time `json:"revoked_at,omitempty"`
	// MailboxCount is filled by list endpoints only.
	MailboxCount int `json:"mailbox_count"`
}

// PoolLinkMailbox ties an enrolled cloud mailbox back to the remote one.
type PoolLinkMailbox struct {
	InstanceID     uuid.UUID `json:"instance_id"`
	RemoteID       uuid.UUID `json:"remote_id"`
	EmailAccountID uuid.UUID `json:"email_account_id"`
	EnrolledAt     time.Time `json:"enrolled_at"`
	// Managed retains the Cloud-owned mailbox on unlink; SMTP/IMAP instances keep their own credentials.
	Managed     bool       `json:"managed"`
	LastTokenAt *time.Time `json:"last_token_at,omitempty"`
	// Executor is who runs the mailbox's warmup; empty reads as cloud.
	Executor PoolLinkExecutor `json:"executor,omitempty"`
}

// PoolLinkStartRequest is what a self-hosted instance sends to begin linking.
type PoolLinkStartRequest struct {
	RemoteOrganizationID uuid.UUID `json:"remote_organization_id"`
	InstanceName         string    `json:"instance_name"`
	InstanceURL          string    `json:"instance_url"`
	InstanceVersion      string    `json:"instance_version"`
}

// PoolLinkStartResponse is the device-code grant.
type PoolLinkStartResponse struct {
	WorkspaceScoped bool   `json:"workspace_scoped"`
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
	// Warming is how many of them have warmup on and not paused; connected is not warming.
	Warming int `json:"warming"`
	// PriceUSD is the monthly price of the paid tier, for the upgrade card.
	PriceUSD int `json:"price_usd"`
	// UpgradeURL is empty when billing is off or the plan has no price.
	UpgradeURL string `json:"upgrade_url,omitempty"`
	// ManageURL is the cloud billing page; empty when billing is off.
	ManageURL string `json:"manage_url,omitempty"`
	// WarmupEntitled is false when the cloud workspace itself cannot warm.
	WarmupEntitled bool `json:"warmup_entitled"`
	// SubscriptionRequired is true when a linked instance may add no new mailboxes until the workspace pays.
	SubscriptionRequired bool `json:"subscription_required,omitempty"`
}

// PoolLinkInstanceInfo is the instance-facing status document.
type PoolLinkInstanceInfo struct {
	SMTPAssociationProtocol int              `json:"smtp_association_protocol,omitempty"`
	ManagedConsentProtocol  int              `json:"managed_consent_protocol,omitempty"`
	Instance                PoolLinkInstance `json:"instance"`
	Organization            PoolLinkOrgInfo  `json:"organization"`
	Plan                    PoolLinkPlan     `json:"plan"`
	// Redirects is nil from a Cloud that does not serve root redirects.
	Redirects *PoolLinkRedirectOffer `json:"redirects,omitempty"`
	// SelfExecution is nil from a Cloud that cannot leave credentials on the instance.
	SelfExecution *PoolLinkSelfExecution `json:"self_execution,omitempty"`
}

const SMTPAssociationProtocol = 1

type PoolLinkSMTPAssociationRequest struct {
	RemoteID       uuid.UUID `json:"remote_id"`
	EmailAccountID uuid.UUID `json:"email_account_id"`
	Email          string    `json:"email"`
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

// WarmupEveryDay explicitly selects all seven days, independently of sending-plan weekdays.
const WarmupEveryDay = 127

// Reported is false for a settings block an older or broken cloud left empty.
func (w PoolLinkWarmupSettings) Reported() bool {
	return ClockHHMM(w.StartTime) != "" && ClockHHMM(w.EndTime) != ""
}

// Mirrored is the cloud's settings in the form this instance stores them.
func (w PoolLinkWarmupSettings) Mirrored() PoolLinkWarmupSettings {
	w.StartTime, w.EndTime = ClockHHMM(w.StartTime), ClockHHMM(w.EndTime)
	return w
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
	// Executor instance carries no credential: the instance runs the mailbox's warmup.
	Executor PoolLinkExecutor `json:"executor,omitempty"`
}

// PoolLinkOAuthStartRequest asks the cloud for a Google or Microsoft consent URL on Warmbly's app.
type PoolLinkOAuthStartRequest struct {
	Protocol  int           `json:"protocol,omitempty"`
	RemoteID  uuid.UUID     `json:"remote_id,omitempty"`
	Session   string        `json:"session,omitempty"`
	Provider  InboxProvider `json:"provider"`
	ReturnURL string        `json:"return_url"`
}

// PoolLinkOAuthStartResponse: open URL (the cloud's consent page) in the browser; redeem Session once the popup returns.
type PoolLinkOAuthStartResponse struct {
	URL     string `json:"url"`
	Session string `json:"session"`
}

// PoolLinkOAuthConsent is what the cloud shows before a brokered sign-in opens the provider.
type PoolLinkOAuthConsent struct {
	Provider      InboxProvider `json:"provider"`
	InstanceName  string        `json:"instance_name"`
	InstanceHost  string        `json:"instance_host"`
	WorkspaceName string        `json:"workspace_name"`
}

// PoolLinkOAuthFinishRequest redeems a completed consent for its mailbox.
type PoolLinkOAuthFinishRequest struct {
	Protocol int       `json:"protocol,omitempty"`
	RemoteID uuid.UUID `json:"remote_id,omitempty"`
	Session  string    `json:"session"`
}

// PoolLinkAdoptRequest links a mailbox already in the workspace to the instance.
type PoolLinkAdoptRequest struct {
	Protocol       int       `json:"protocol,omitempty"`
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
	MailHost string    `json:"mail_host,omitempty"`
}

// PoolLinkWarmupDeliveryQuery asks the cloud whether one message that arrived
// in a mailbox it warms is its own warmup mail. The verify header does not
// survive every provider, so the instance cannot always tell on its own.
type PoolLinkWarmupDeliveryQuery struct {
	Sender    string `json:"sender"`
	MessageID string `json:"message_id"`
	Subject   string `json:"subject"`
	// InReplyTo asks by ancestry instead: a reply typed by hand in a warmup
	// thread names no token and no known id of its own, only the turn it
	// answers. A cloud predating the field ignores it and answers no.
	InReplyTo []string `json:"in_reply_to,omitempty"`
}

// PoolLinkMailboxState is the per-mailbox view shown in both dashboards.
type PoolLinkMailboxState struct {
	Participation      *DiagnosticParticipation `json:"participation,omitempty"`
	ConsentCompletedAt *time.Time               `json:"consent_completed_at,omitempty"`
	RemoteID           uuid.UUID                `json:"remote_id"`
	EmailAccountID     uuid.UUID                `json:"email_account_id"`
	Email              string                   `json:"email"`
	Name               string                   `json:"name"`
	Provider           string                   `json:"provider"`
	Status             string                   `json:"status"`
	EnrolledAt         time.Time                `json:"enrolled_at"`
	Managed            bool                     `json:"managed"`
	Warmup             *WarmupStatusInfo        `json:"warmup,omitempty"`
	Health             *WarmupHealthInfo        `json:"health,omitempty"`
	SentToday          int                      `json:"sent_today"`
	Sent7d             int                      `json:"sent_7d"`
	Replied7d          int                      `json:"replied_7d"`
	SpamPlaced7d       int                      `json:"spam_placed_7d"`
	Errors             []AccountError           `json:"errors,omitempty"`
	AuthState          string                   `json:"auth_state"`
	Settings           PoolLinkWarmupSettings   `json:"settings"`
	// Executor is omitted by a Cloud that predates instance execution.
	Executor PoolLinkExecutor `json:"executor,omitempty"`
}

// PoolLinkMailboxStanding is one enrolled mailbox's warmup standing, the
// cheap read an instance polls so its own send gates follow the cloud's.
type PoolLinkMailboxStanding struct {
	RemoteID uuid.UUID         `json:"remote_id"`
	Health   *WarmupHealthInfo `json:"health,omitempty"`
	Executor PoolLinkExecutor  `json:"executor,omitempty"`
	// Paused names why an instance-executed mailbox's Cloud warmup is stopped in both directions.
	Paused string `json:"paused,omitempty"`
}

// Reasons an instance-executed mailbox's Cloud warmup is paused.
const (
	PoolLinkPausedNotGranted      = "self_execution_not_granted"
	PoolLinkPausedExecutorOffline = "executor_offline"
)

// PoolLinkMailboxPatch updates a mailbox's ramp or lifecycle on the cloud.
type PoolLinkMailboxPatch struct {
	Participation *DiagnosticParticipation `json:"participation,omitempty"`
	// Lifecycle is "pause", "resume" or empty.
	Lifecycle string                   `json:"lifecycle,omitempty"`
	Warmup    *PoolLinkWarmupSettings  `json:"warmup,omitempty"`
	OAuth     *PoolLinkOAuthCredential `json:"oauth,omitempty"`
	SMTPIMAP  *SmtpImap                `json:"smtp_imap,omitempty"`
}

type DiagnosticParticipation struct {
	Mode                  TestParticipationMode `json:"mode"`
	Send                  bool                  `json:"send"`
	Receive               bool                  `json:"receive"`
	SharedDailyLimit      *int                  `json:"shared_daily_limit,omitempty"`
	RollingRecipientLimit *int                  `json:"rolling_recipient_limit,omitempty"`
}

func (p DiagnosticParticipation) Valid() bool {
	return (p.Mode == TestParticipationDiagnostic || p.Mode == TestParticipationOff && !p.Send && !p.Receive) &&
		(p.SharedDailyLimit == nil || *p.SharedDailyLimit >= 0) && (p.RollingRecipientLimit == nil || *p.RollingRecipientLimit >= 0)
}

// CloudLink belongs to a workspace; a nil OrganizationID is a legacy instance-wide link.
type CloudLink struct {
	OrganizationID    *uuid.UUID `json:"organization_id,omitempty"`
	CloudURL          string     `json:"cloud_url"`
	InstanceID        uuid.UUID  `json:"instance_id"`
	Token             string     `json:"-"`
	OrganizationName  string     `json:"organization_name"`
	ConnectedBy       *uuid.UUID `json:"connected_by,omitempty"`
	ConnectedAt       time.Time  `json:"connected_at"`
	LastSyncedAt      *time.Time `json:"last_synced_at,omitempty"`
	LastError         string     `json:"last_error,omitempty"`
	DisconnectPending bool       `json:"disconnect_pending,omitempty"`
}

// CloudLinkMailbox marks a local mailbox as warmed by the cloud.
type CloudLinkMailbox struct {
	SMTPCloudAccountID *uuid.UUID `json:"smtp_cloud_account_id,omitempty"`
	InstanceID         uuid.UUID  `json:"-"`
	EmailAccountID     uuid.UUID  `json:"email_account_id"`
	RemoteID           uuid.UUID  `json:"remote_id"`
	EnrolledAt         time.Time  `json:"enrolled_at"`
	// Managed: no local credential; the worker sends with tokens brokered by the cloud.
	Managed            bool       `json:"managed"`
	EnrollmentState    string     `json:"enrollment_state,omitempty"`
	StandingObservedAt *time.Time `json:"standing_observed_at,omitempty"`
	// SettingsObservedAt is when the local warmup columns last mirrored the cloud's settings.
	SettingsObservedAt *time.Time `json:"settings_observed_at,omitempty"`
	// Executor is who runs the warmup; instance means Cloud holds no credential.
	Executor PoolLinkExecutor `json:"executor,omitempty"`
	// Standing is the warmup health the cloud last reported; nil until it has.
	Standing *WarmupHealthInfo `json:"-"`
}

// CloudLinkStandingChange is a cloud-reported health transition the
// instance recorded, for the same realtime and webhook fan-out as a local one.
type CloudLinkStandingChange struct {
	EmailAccountID uuid.UUID
	Previous       WarmupHealthState
	Current        WarmupHealthState
	Reason         string
}

// CloudLinkOAuthStart is the instance dashboard's handle on a cloud-brokered consent.
type CloudLinkOAuthStart struct {
	URL     string `json:"url"`
	Session string `json:"session"`
}

// CloudLinkStatus is the self-hosted dashboard's view of the link.
type CloudLinkStatus struct {
	LegacyConnected bool                  `json:"legacy_connected"`
	Connected       bool                  `json:"connected"`
	Link            *CloudLink            `json:"link,omitempty"`
	Info            *PoolLinkInstanceInfo `json:"info,omitempty"`
	// Reachable is false when the cloud could not be contacted; Link still comes from the local row.
	Reachable bool   `json:"reachable"`
	Error     string `json:"error,omitempty"`
	// DefaultCloudURL is what the connect flow proposes.
	DefaultCloudURL string `json:"default_cloud_url"`
}

// CloudLinkMailboxRow merges a local mailbox with its cloud warmup state.
type CloudLinkMailboxRow struct {
	IndependentSMTP    bool                  `json:"independent_smtp,omitempty"`
	Legacy             bool                  `json:"legacy"`
	ID                 uuid.UUID             `json:"id"`
	Email              string                `json:"email"`
	Name               string                `json:"name"`
	Provider           string                `json:"provider"`
	Status             string                `json:"status"`
	Enrolled           bool                  `json:"enrolled"`
	EnrolledAt         *time.Time            `json:"enrolled_at,omitempty"`
	Managed            bool                  `json:"managed"`
	Cloud              *PoolLinkMailboxState `json:"cloud,omitempty"`
	EnrollmentState    string                `json:"enrollment_state,omitempty"`
	StandingObservedAt *time.Time            `json:"standing_observed_at,omitempty"`
	SettingsObservedAt *time.Time            `json:"settings_observed_at,omitempty"`
	Executor           PoolLinkExecutor      `json:"executor,omitempty"`
	// Commands Cloud handed this instance for the mailbox: waiting, and failed in the last day.
	CommandsPending int `json:"commands_pending,omitempty"`
	CommandsFailed  int `json:"commands_failed,omitempty"`
}

// PoolLinkOffer is the retired Warmup plan as older upgrade prompts read it; it is always unavailable.
type PoolLinkOffer struct {
	Available        bool    `json:"available"`
	MonthlyAvailable bool    `json:"monthly_available"`
	YearlyAvailable  bool    `json:"yearly_available"`
	MonthlyUSD       float32 `json:"monthly_usd"`
	YearlyUSD        float32 `json:"yearly_usd"`
	Currency         string  `json:"currency"`
}
