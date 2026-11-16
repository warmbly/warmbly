// Package outreachsource is the provider-neutral shape of another outreach tool's
// account. Each provider package reads its own API into these types and knows
// nothing about Warmbly; the migration applies them.
package outreachsource

import (
	"context"
	"errors"
	"time"
)

// ProviderID names a tool a workspace can migrate from.
type ProviderID string

// Kind is one category of data a migration can bring over.
type Kind string

const (
	KindMailboxes Kind = "mailboxes"
	KindCampaigns Kind = "campaigns"
	KindLeads     Kind = "leads"
	KindBlocklist Kind = "blocklist"
)

// AllKinds is every kind in the order a migration applies them. Blocklist goes
// first so nobody who opted out elsewhere is enrolled by a later step.
var AllKinds = []Kind{KindBlocklist, KindMailboxes, KindCampaigns, KindLeads}

// Field is one credential input the provider needs.
type Field struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Help        string `json:"help,omitempty"`
	Secret      bool   `json:"secret"`
	Optional    bool   `json:"optional,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	// Options limits the value to a fixed set, such as a data region.
	Options []string `json:"options,omitempty"`
}

// Info describes a provider to the picker and to the API.
type Info struct {
	ID    ProviderID `json:"id"`
	Label string     `json:"label"`
	// Fields are the credentials, in the order they are asked.
	Fields []Field `json:"fields"`
	// KeyHelp tells a person where the key lives in that tool.
	KeyHelp string `json:"key_help"`
	KeyURL  string `json:"key_url,omitempty"`
	// Kinds the provider's API can read. Anything else comes from a CSV.
	Kinds []Kind `json:"kinds"`
	// Workspaces is true when one key can see several client accounts or
	// workspaces, which an agency maps onto client workspaces.
	Workspaces bool `json:"workspaces"`
	// MultiKey is true when the tool issues one key per workspace, so an
	// agency adds a key for each.
	MultiKey bool `json:"multi_key"`
	// MailboxSecrets is true when the API returns mailbox passwords, so SMTP
	// and IMAP mailboxes connect without being asked for again.
	MailboxSecrets bool `json:"mailbox_secrets"`
	// Notes are short facts a person should know before starting.
	Notes []string `json:"notes,omitempty"`
}

// Credentials are the values for Info.Fields, keyed by Field.Key.
type Credentials map[string]string

// Errors a provider returns for the person who pasted the key. Their messages
// are shown as they are, so a provider wraps them with its own name.
var (
	ErrKey         = errors.New("the key was rejected")
	ErrForbidden   = errors.New("the key is not allowed to read this")
	ErrRateLimited = errors.New("the provider is rate limiting this account")
	ErrUnavailable = errors.New("the provider did not answer")
	// ErrUnsupported is a kind the provider's API cannot read.
	ErrUnsupported = errors.New("not available through this provider's API")
)

// Provider opens sessions for one tool.
type Provider interface {
	Info() Info
	// Open checks the credentials and returns a session reading as them.
	Open(ctx context.Context, creds Credentials) (Session, error)
}

// Session reads one account. Every method is read-only against the provider.
type Session interface {
	// Account is who the key acts as.
	Account(ctx context.Context) (Account, error)
	// Workspaces lists the client accounts or workspaces the key can read.
	// A key with no such concept returns exactly one workspace with an empty ID.
	Workspaces(ctx context.Context) ([]Workspace, error)
	// Inventory counts what a workspace holds; counts the API cannot give
	// cheaply are -1.
	Inventory(ctx context.Context, ws Workspace) (Inventory, error)
	Mailboxes(ctx context.Context, ws Workspace) ([]Mailbox, error)
	Campaigns(ctx context.Context, ws Workspace) ([]Campaign, error)
	// Leads pages through a campaign's leads (campaignID set) or the
	// workspace's leads outside any campaign (campaignID empty). cursor is ""
	// for the first page; next is "" after the last.
	Leads(ctx context.Context, ws Workspace, campaignID, cursor string) (page []Lead, next string, err error)
	// Blocklist pages through blocked addresses and domains.
	Blocklist(ctx context.Context, ws Workspace, cursor string) (page []BlockEntry, next string, err error)
}

// Account is the identity behind a key.
type Account struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	// Plan is the provider's plan name when it reports one.
	Plan string `json:"plan,omitempty"`
}

// Workspace is one client account or workspace inside the provider.
type Workspace struct {
	// ID is the provider's id, empty for the account itself.
	ID   string `json:"id"`
	Name string `json:"name"`
	// Key is set when this workspace is read with a key of its own, an index
	// into the migration's credential sets. Providers leave it 0.
	Key int `json:"key,omitempty"`
}

// Inventory is what one workspace holds.
type Inventory struct {
	Mailboxes int               `json:"mailboxes"`
	Campaigns int               `json:"campaigns"`
	Leads     int               `json:"leads"`
	Blocklist int               `json:"blocklist"`
	Summaries []CampaignSummary `json:"campaign_summaries,omitempty"`
}

// CampaignSummary is one campaign as the preview lists it.
type CampaignSummary struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Status CampaignStatus `json:"status"`
	// Steps and Leads are -1 when the API cannot give them cheaply.
	Steps int `json:"steps"`
	Leads int `json:"leads"`
}

// MailboxKind is how a mailbox signs in.
type MailboxKind string

const (
	MailboxGoogle    MailboxKind = "google"
	MailboxMicrosoft MailboxKind = "microsoft"
	MailboxSMTP      MailboxKind = "smtp_imap"
	// MailboxUnknown is a mailbox whose sign-in the provider does not report.
	MailboxUnknown MailboxKind = "unknown"
)

// Security is a connection's encryption.
type Security string

const (
	SecuritySSL      Security = "ssl"
	SecurityStartTLS Security = "starttls"
	SecurityNone     Security = "none"
)

// Server is one SMTP or IMAP endpoint.
type Server struct {
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	Security Security `json:"security,omitempty"`
	Username string   `json:"username,omitempty"`
	// Password is only set when the provider's API returns it.
	Password string `json:"-"`
}

// Mailbox is one sending account.
type Mailbox struct {
	ID        string      `json:"id"`
	Email     string      `json:"email"`
	FromName  string      `json:"from_name,omitempty"`
	Kind      MailboxKind `json:"kind"`
	SMTP      *Server     `json:"smtp,omitempty"`
	IMAP      *Server     `json:"imap,omitempty"`
	DailyCap  int         `json:"daily_cap,omitempty"`
	Warmup    *bool       `json:"warmup,omitempty"`
	Signature string      `json:"-"`
	ReplyTo   string      `json:"reply_to,omitempty"`
	Tags      []string    `json:"tags,omitempty"`
}

// HasSecrets reports whether the mailbox can connect without asking anyone.
func (m Mailbox) HasSecrets() bool {
	return m.Kind == MailboxSMTP && m.SMTP != nil && m.IMAP != nil &&
		m.SMTP.Password != "" && m.IMAP.Password != "" && m.SMTP.Host != "" && m.IMAP.Host != ""
}

// CampaignStatus is a campaign's state in the provider.
type CampaignStatus string

const (
	CampaignDraft     CampaignStatus = "draft"
	CampaignActive    CampaignStatus = "active"
	CampaignPaused    CampaignStatus = "paused"
	CampaignCompleted CampaignStatus = "completed"
	CampaignArchived  CampaignStatus = "archived"
)

// Campaign is one sequence with its settings.
type Campaign struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Status   CampaignStatus `json:"status"`
	Steps    []Step         `json:"steps"`
	Schedule *Schedule      `json:"schedule,omitempty"`
	// Senders are the mailbox addresses the campaign sends from.
	Senders []string `json:"senders,omitempty"`
	// DailyLimit is the campaign's own cap across its mailboxes, 0 for none.
	DailyLimit int `json:"daily_limit,omitempty"`
	// MaxNewLeadsPerDay caps how many leads start the sequence each day, 0 for none.
	MaxNewLeadsPerDay int        `json:"max_new_leads_per_day,omitempty"`
	StopOnReply       bool       `json:"stop_on_reply"`
	TrackOpens        bool       `json:"track_opens"`
	TrackClicks       bool       `json:"track_clicks"`
	PlainText         bool       `json:"plain_text,omitempty"`
	Unsubscribe       bool       `json:"unsubscribe,omitempty"`
	LeadCount         int        `json:"lead_count"`
	CreatedAt         *time.Time `json:"created_at,omitempty"`
	UnmappedNotes     []string   `json:"unmapped_notes,omitempty"`
}

// Step is one email in a sequence. Steps other than email (LinkedIn, calls,
// manual tasks) are reported in Campaign.UnmappedNotes and left out.
type Step struct {
	// WaitDays is the delay after the previous step; the first step's is the
	// delay after a lead enters.
	WaitDays    int `json:"wait_days"`
	WaitMinutes int `json:"wait_minutes,omitempty"`
	// Variants are A/B versions; the first is the default.
	Variants []Variant `json:"variants"`
	// NewThread sends as a new conversation instead of a reply to the last
	// step. A blank subject on a later step means reply in the same thread.
	NewThread bool `json:"new_thread,omitempty"`
}

// Variant is one version of a step, already in Warmbly's template syntax.
type Variant struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
	// HTML is true when Body is HTML rather than plain text.
	HTML bool `json:"html"`
	// Disabled variants are kept but not sent.
	Disabled bool `json:"disabled,omitempty"`
}

// Schedule is when a campaign sends.
type Schedule struct {
	Timezone string `json:"timezone"` // IANA name
	// Days are ISO weekdays, 1 Monday through 7 Sunday.
	Days []int `json:"days"`
	// From and To are local "HH:MM".
	From string `json:"from"`
	To   string `json:"to"`
	// MinGapMinutes is the spacing between sends from one mailbox, 0 for the provider default.
	MinGapMinutes int        `json:"min_gap_minutes,omitempty"`
	Start         *time.Time `json:"start,omitempty"`
	End           *time.Time `json:"end,omitempty"`
}

// LeadStatus is a lead's outcome in the provider.
type LeadStatus string

const (
	LeadNotStarted   LeadStatus = "not_started"
	LeadInProgress   LeadStatus = "in_progress"
	LeadCompleted    LeadStatus = "completed"
	LeadReplied      LeadStatus = "replied"
	LeadInterested   LeadStatus = "interested"
	LeadNotInterest  LeadStatus = "not_interested"
	LeadMeeting      LeadStatus = "meeting_booked"
	LeadBounced      LeadStatus = "bounced"
	LeadUnsubscribed LeadStatus = "unsubscribed"
	LeadPaused       LeadStatus = "paused"
)

// Final reports whether no further email should go to the lead.
func (s LeadStatus) Final() bool {
	switch s {
	case LeadReplied, LeadInterested, LeadNotInterest, LeadMeeting, LeadBounced, LeadUnsubscribed, LeadCompleted:
		return true
	}
	return false
}

// Lead is one person, with where they stand in a campaign.
type Lead struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	Company   string `json:"company,omitempty"`
	Title     string `json:"title,omitempty"`
	Phone     string `json:"phone,omitempty"`
	Website   string `json:"website,omitempty"`
	LinkedIn  string `json:"linkedin,omitempty"`
	Location  string `json:"location,omitempty"`
	Timezone  string `json:"timezone,omitempty"`
	// Custom holds every other field under the provider's own name, already
	// normalised to snake_case so templates and fields agree.
	Custom map[string]string `json:"custom,omitempty"`
	Status LeadStatus        `json:"status"`
	// StepsSent is how many steps of the campaign the lead has been sent.
	StepsSent int `json:"steps_sent"`
	// StepsExact is true only when the provider reports StepsSent exactly.
	// A lead is continued mid-sequence only then; otherwise a follow-up could
	// reach someone twice.
	StepsExact bool       `json:"steps_exact"`
	LastSent   *time.Time `json:"last_sent,omitempty"`
}

// LeadCustomKeys are the custom field keys a lead's non-built-in fields are
// stored under, matching what Translate emits for their tokens.
func (l Lead) LeadCustomKeys() map[string]string {
	out := map[string]string{}
	for k, v := range map[string]string{"website": l.Website, "linkedin": l.LinkedIn, "location": l.Location, "title": l.Title, "timezone": l.Timezone} {
		if v != "" {
			out[k] = v
		}
	}
	return out
}

// BlockEntry is one blocked address or domain.
type BlockEntry struct {
	// Value is an address, or a bare domain when Domain is true.
	Value  string `json:"value"`
	Domain bool   `json:"domain"`
	Reason string `json:"reason,omitempty"`
}
