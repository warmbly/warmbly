package models

import (
	"time"

	"github.com/google/uuid"
)

// MailboxImportField is what a column of an import file means.
type MailboxImportField string

const (
	ImportFieldIgnore          MailboxImportField = "ignore"
	ImportFieldEmail           MailboxImportField = "email"
	ImportFieldName            MailboxImportField = "name"
	ImportFieldFirstName       MailboxImportField = "first_name"
	ImportFieldLastName        MailboxImportField = "last_name"
	ImportFieldPassword        MailboxImportField = "password"
	ImportFieldAppPassword     MailboxImportField = "app_password"
	ImportFieldSMTPPassword    MailboxImportField = "smtp_password"
	ImportFieldIMAPPassword    MailboxImportField = "imap_password"
	ImportFieldUsername        MailboxImportField = "username"
	ImportFieldSMTPUsername    MailboxImportField = "smtp_username"
	ImportFieldIMAPUsername    MailboxImportField = "imap_username"
	ImportFieldSMTPHost        MailboxImportField = "smtp_host"
	ImportFieldSMTPPort        MailboxImportField = "smtp_port"
	ImportFieldSMTPSecurity    MailboxImportField = "smtp_security"
	ImportFieldIMAPHost        MailboxImportField = "imap_host"
	ImportFieldIMAPPort        MailboxImportField = "imap_port"
	ImportFieldIMAPSecurity    MailboxImportField = "imap_security"
	ImportFieldDailyLimit      MailboxImportField = "daily_limit"
	ImportFieldMinWait         MailboxImportField = "min_wait"
	ImportFieldWarmup          MailboxImportField = "warmup"
	ImportFieldWarmupStart     MailboxImportField = "warmup_start"
	ImportFieldWarmupMax       MailboxImportField = "warmup_max"
	ImportFieldWarmupIncrease  MailboxImportField = "warmup_increase"
	ImportFieldWarmupReplyRate MailboxImportField = "warmup_reply_rate"
	ImportFieldReplyTo         MailboxImportField = "reply_to"
	ImportFieldSignature       MailboxImportField = "signature"
	ImportFieldTags            MailboxImportField = "tags"
	ImportFieldTimezone        MailboxImportField = "timezone"
)

// Row statuses of a mailbox import, mirroring the mailbox_import_rows CHECK.
const (
	ImportRowQueued      = "queued"
	ImportRowRunning     = "running"
	ImportRowConnected   = "connected"
	ImportRowUpdated     = "updated"
	ImportRowSkipped     = "skipped"
	ImportRowFailed      = "failed"
	ImportRowNeedsSignin = "needs_signin"
	ImportRowCancelled   = "cancelled"
)

// Import statuses, mirroring the mailbox_imports CHECK.
const (
	ImportRunning   = "running"
	ImportCompleted = "completed"
	ImportCancelled = "cancelled"
)

// Preview-only row states: what would happen, before anything is tried.
const (
	ImportPreviewReady       = "ready"
	ImportPreviewNeedsSignin = "needs_signin"
	ImportPreviewInvalid     = "invalid"
	ImportPreviewExisting    = "existing"
	ImportPreviewDuplicate   = "duplicate"
)

// MailboxImportSettings are applied to every mailbox an import connects or
// updates; a mapped column overrides them per row. Nil means "leave as is".
type MailboxImportSettings struct {
	TagIDs          []string `json:"tag_ids,omitempty"`
	DailyLimit      *int     `json:"daily_limit,omitempty"`
	MinWait         *int     `json:"min_wait,omitempty"`
	Warmup          *bool    `json:"warmup,omitempty"`
	WarmupStart     *int     `json:"warmup_start,omitempty"`
	WarmupMax       *int     `json:"warmup_max,omitempty"`
	WarmupIncrease  *int     `json:"warmup_increase,omitempty"`
	WarmupReplyRate *int     `json:"warmup_reply_rate,omitempty"`
	ReplyTo         *string  `json:"reply_to,omitempty"`
	Signature       *string  `json:"signature,omitempty"`
	Timezone        *string  `json:"timezone,omitempty"`
}

// MailboxImportOptions are the non-file inputs of a preview or an import.
type MailboxImportOptions struct {
	HasHeader      *bool                 `json:"has_header"`
	SharedPassword string                `json:"shared_password"`
	OnExisting     string                `json:"on_existing"`
	SaveMapping    *bool                 `json:"save_mapping"`
	Settings       MailboxImportSettings `json:"settings"`
	// TrackingDomains picks a tracking host per sending domain, e.g. {"acme.io": "track.acme.io"}.
	TrackingDomains map[string]string `json:"tracking_domains,omitempty"`
	// Redirects sends a sending domain's root to a website, e.g. {"acme.io": "https://acme.com"}.
	Redirects map[string]string `json:"redirects,omitempty"`
}

// MailboxImportMapping maps a column index (as a string, JSON object keys) to a field.
type MailboxImportMapping map[string]MailboxImportField

type MailboxImportVendor struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type MailboxImportColumn struct {
	Index      int                `json:"index"`
	Header     string             `json:"header"`
	Samples    []string           `json:"samples"`
	Secret     bool               `json:"secret"`
	Field      MailboxImportField `json:"field"`
	Source     string             `json:"source"`
	Confidence float64            `json:"confidence"`
}

type MailboxImportEndpoint struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Security string `json:"security"`
	Username string `json:"username,omitempty"`
}

type MailboxImportPreviewRow struct {
	Line       int                    `json:"line"`
	Email      string                 `json:"email"`
	Name       string                 `json:"name"`
	MailHost   string                 `json:"mail_host"`
	AuthMethod string                 `json:"auth_method"`
	Status     string                 `json:"status"`
	Cause      string                 `json:"cause,omitempty"`
	Problem    string                 `json:"problem,omitempty"`
	SMTP       *MailboxImportEndpoint `json:"smtp,omitempty"`
	IMAP       *MailboxImportEndpoint `json:"imap,omitempty"`
}

type MailboxImportDomainAuth struct {
	State string `json:"state"`
	SPF   bool   `json:"spf"`
	DKIM  bool   `json:"dkim"`
	DMARC bool   `json:"dmarc"`
}

type MailboxImportDomain struct {
	Domain         string                   `json:"domain"`
	MailHost       string                   `json:"mail_host"`
	Label          string                   `json:"label"`
	Source         string                   `json:"source"`
	Rows           int                      `json:"rows"`
	PasswordAuth   string                   `json:"password_auth"`
	AppPasswordURL string                   `json:"app_password_url,omitempty"`
	SMTP           *MailboxImportEndpoint   `json:"smtp,omitempty"`
	IMAP           *MailboxImportEndpoint   `json:"imap,omitempty"`
	Auth           *MailboxImportDomainAuth `json:"auth,omitempty"`
	// Tracking is the tracking host this domain's mailboxes can use; nil when tracking is off here.
	Tracking *TrackingSuggestion `json:"tracking,omitempty"`
	// Redirect is the domain's root redirect, when one exists.
	Redirect *DomainRedirect `json:"redirect,omitempty"`
}

// MailboxImportIssue is one cause shared by several rows, with its fix.
type MailboxImportIssue struct {
	Cause string `json:"cause"`
	Title string `json:"title"`
	Fix   string `json:"fix"`
	Count int    `json:"count"`
	Lines []int  `json:"lines"`
}

type MailboxImportSummary struct {
	Total       int `json:"total"`
	Ready       int `json:"ready"`
	NeedsSignin int `json:"needs_signin"`
	Invalid     int `json:"invalid"`
	Existing    int `json:"existing"`
	Duplicate   int `json:"duplicate"`
}

type MailboxImportPreview struct {
	Format       string                    `json:"format"`
	HasHeader    bool                      `json:"has_header"`
	Vendor       *MailboxImportVendor      `json:"vendor"`
	Columns      []MailboxImportColumn     `json:"columns"`
	Mapping      MailboxImportMapping      `json:"mapping"`
	SavedMapping bool                      `json:"saved_mapping"`
	Summary      MailboxImportSummary      `json:"summary"`
	Rows         []MailboxImportPreviewRow `json:"rows"`
	Domains      []MailboxImportDomain     `json:"domains"`
	Issues       []MailboxImportIssue      `json:"issues"`
	Allowance    *MailboxAllowance         `json:"allowance,omitempty"`
	MaxRows      int                       `json:"max_rows"`
}

type MailboxImportCounts struct {
	Queued      int `json:"queued"`
	Running     int `json:"running"`
	Connected   int `json:"connected"`
	Updated     int `json:"updated"`
	Skipped     int `json:"skipped"`
	Failed      int `json:"failed"`
	NeedsSignin int `json:"needs_signin"`
	Cancelled   int `json:"cancelled"`
}

type MailboxImportCause struct {
	Cause     string `json:"cause"`
	Title     string `json:"title"`
	Fix       string `json:"fix"`
	Count     int    `json:"count"`
	Retryable bool   `json:"retryable"`
}

type MailboxImport struct {
	ID                  uuid.UUID            `json:"id"`
	OrganizationID      uuid.UUID            `json:"-"`
	CreatedBy           *uuid.UUID           `json:"created_by,omitempty"`
	Status              string               `json:"status"`
	Source              string               `json:"source"`
	Filename            string               `json:"filename"`
	Vendor              string               `json:"vendor"`
	OnExisting          string               `json:"on_existing"`
	CreatedAt           time.Time            `json:"created_at"`
	UpdatedAt           time.Time            `json:"updated_at"`
	FinishedAt          *time.Time           `json:"finished_at,omitempty"`
	CredentialsExpireAt *time.Time           `json:"credentials_expire_at,omitempty"`
	Total               int                  `json:"total"`
	Counts              MailboxImportCounts  `json:"counts"`
	Causes              []MailboxImportCause `json:"causes"`
}

type MailboxImportRow struct {
	Line           int               `json:"line"`
	Email          string            `json:"email"`
	Name           string            `json:"name"`
	MailHost       string            `json:"mail_host"`
	Status         string            `json:"status"`
	Code           string            `json:"code"`
	Cause          string            `json:"cause"`
	Message        string            `json:"message"`
	EmailAccountID *uuid.UUID        `json:"email_account_id,omitempty"`
	Retryable      bool              `json:"retryable"`
	Fields         map[string]string `json:"fields"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

type MailboxImportLegFix struct {
	Host     *string `json:"host"`
	Port     *int    `json:"port"`
	Security *string `json:"security"`
	Username *string `json:"username"`
	Password *string `json:"password"`
}

// MailboxImportRowFix corrects one failed row; nil fields keep what was uploaded.
type MailboxImportRowFix struct {
	Password    *string              `json:"password"`
	AppPassword *string              `json:"app_password"`
	Username    *string              `json:"username"`
	SMTP        *MailboxImportLegFix `json:"smtp"`
	IMAP        *MailboxImportLegFix `json:"imap"`
}

// MailboxImportRetry requeues failed rows: all of them, one cause, or named lines.
type MailboxImportRetry struct {
	Cause    string `json:"cause"`
	Lines    []int  `json:"lines"`
	Password string `json:"password"`
}

// Admin grant providers, mirroring the mailbox_domain_grants CHECK.
const (
	GrantProviderGoogle    = "google"
	GrantProviderMicrosoft = "microsoft"
)

// DomainGrant is an administrator's grant over a Google Workspace domain or a Microsoft 365 tenant.
type DomainGrant struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"-"`
	Provider       string     `json:"provider"`
	Tenant         string     `json:"tenant"`
	AdminEmail     string     `json:"admin_email,omitempty"`
	Domains        []string   `json:"domains"`
	Status         string     `json:"status"`
	LastError      string     `json:"last_error,omitempty"`
	VerifiedAt     *time.Time `json:"verified_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	Mailboxes      int        `json:"mailboxes"`
}

// DirectoryUser is one account in a granted domain or tenant.
type DirectoryUser struct {
	// ID is the subject tokens are minted for: the address (Google) or the Graph user id (Microsoft).
	ID        string     `json:"id"`
	Email     string     `json:"email"`
	Name      string     `json:"name"`
	Enabled   bool       `json:"enabled"`
	Connected bool       `json:"connected"`
	AccountID *uuid.UUID `json:"email_account_id,omitempty"`
	// Upgrade is a connected mailbox that signs in on its own; connecting it moves it onto the grant, keeping its history.
	Upgrade bool `json:"upgrade"`
}

// SigninMigration groups a workspace's mailboxes on the retiring per-mailbox Google sign-in by domain.
type SigninMigration struct {
	Domain string `json:"domain"`
	// Kind is "workspace" (moves to an administrator's grant) or "personal" (a shared address like gmail.com, moves to an app password).
	Kind string `json:"kind"`
	// GrantID is the workspace's active grant covering the domain, when there is one.
	GrantID   *uuid.UUID         `json:"grant_id,omitempty"`
	Mailboxes []MigrationMailbox `json:"mailboxes"`
}

// MigrationMailbox is one mailbox still on per-mailbox Google sign-in.
type MigrationMailbox struct {
	ID     uuid.UUID `json:"id"`
	Email  string    `json:"email"`
	Name   string    `json:"name"`
	Status string    `json:"status"`
}

// VendorConnection is an inbox vendor account the workspace imports from. Its credentials never leave the server.
type VendorConnection struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"-"`
	Vendor         string     `json:"vendor"`
	Label          string     `json:"label"`
	Status         string     `json:"status"`
	LastError      string     `json:"last_error,omitempty"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	Mailboxes      int        `json:"mailboxes"`
	// Credentials is the sealed field map; never serialized.
	Credentials string `json:"-"`
}

// VendorMailbox is one mailbox in a vendor account, as the picker shows it.
type VendorMailbox struct {
	ID        string     `json:"id"`
	Email     string     `json:"email"`
	Name      string     `json:"name"`
	Domain    string     `json:"domain"`
	Provider  string     `json:"provider"`
	Status    string     `json:"status"`
	Workspace string     `json:"workspace,omitempty"`
	Connected bool       `json:"connected"`
	AccountID *uuid.UUID `json:"email_account_id,omitempty"`
}

// SendingDomain is one domain the workspace sends from, with what makes it look like a real business.
type SendingDomain struct {
	Domain          string              `json:"domain"`
	Mailboxes       int                 `json:"mailboxes"`
	MailHosts       []string            `json:"mail_hosts"`
	AuthState       string              `json:"auth_state"`
	AuthSPF         bool                `json:"auth_spf"`
	AuthDKIM        bool                `json:"auth_dkim"`
	AuthDMARC       bool                `json:"auth_dmarc"`
	TrackingDomains []TrackingDomainUse `json:"tracking_domains"`
	Redirect        *DomainRedirect     `json:"redirect,omitempty"`
	// Vendors are the inbox vendors this domain's mailboxes were imported from.
	Vendors             []string    `json:"vendors"`
	VendorConnectionIDs []uuid.UUID `json:"vendor_connection_ids"`
	// VendorDomain is set when a connected vendor account holds this domain.
	VendorDomain *VendorDomainLink `json:"vendor_domain,omitempty"`
}

// TrackingDomainUse is one custom tracking host the domain's mailboxes use.
type TrackingDomainUse struct {
	Host      string `json:"host"`
	Verified  bool   `json:"verified"`
	Mailboxes int    `json:"mailboxes"`
}

// DomainRedirect sends a sending domain's root to the workspace's main website.
type DomainRedirect struct {
	ID            uuid.UUID   `json:"id"`
	Domain        string      `json:"domain"`
	TargetURL     string      `json:"target_url"`
	IncludeWWW    bool        `json:"include_www"`
	Verified      bool        `json:"verified"`
	VerifiedAt    *time.Time  `json:"verified_at,omitempty"`
	LastCheckedAt *time.Time  `json:"last_checked_at,omitempty"`
	LastError     string      `json:"last_error,omitempty"`
	CreatedAt     time.Time   `json:"created_at"`
	Records       []DNSRecord `json:"records"`
	// VerifyToken is written into the TXT record; never serialized on its own.
	VerifyToken    string     `json:"-"`
	OrganizationID uuid.UUID  `json:"-"`
	CreatedBy      *uuid.UUID `json:"-"`
}

// DNSRecord is one record the customer adds at their DNS provider, and whether it is in place.
type DNSRecord struct {
	Purpose string `json:"purpose"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Value   string `json:"value"`
	OK      bool   `json:"ok"`
	// Optional records improve the result but are not required to verify.
	Optional bool `json:"optional,omitempty"`
}

// TrackingSuggestion is the tracking host a domain's import can use.
type TrackingSuggestion struct {
	Host string `json:"host"`
	// Status is "active" (a mailbox here already uses it, verified), "found" (DNS already points it at this instance) or "suggested".
	Status      string `json:"status"`
	CNAMETarget string `json:"cname_target"`
}

// VendorDomainLink is a sending domain held by an inbox vendor account, with what its API can do for it.
type VendorDomainLink struct {
	Vendor       string    `json:"vendor"`
	ConnectionID uuid.UUID `json:"connection_id"`
	Forwarding   string    `json:"forwarding,omitempty"`
	CanForward   bool      `json:"can_forward"`
	// CanUnforward is whether an empty target removes the vendor's forwarding.
	CanUnforward bool `json:"can_unforward"`
	// ForwardingReviewed means the vendor's staff apply a change later.
	ForwardingReviewed bool     `json:"forwarding_reviewed"`
	CanDNS             bool     `json:"can_dns"`
	DNSTypes           []string `json:"dns_types"`
}
