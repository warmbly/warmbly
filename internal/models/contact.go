package models

import (
	"bytes"
	"time"

	"github.com/google/uuid"
)

// MiniCategory is the lightweight shape we attach to contact responses
// so the UI can render category chips without doing a second lookup. It
// is a denormalised slice of the row in `categories` plus nothing else.
type MiniCategory struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
	Color string    `json:"color"`
}

type Contact struct {
	ID uuid.UUID `json:"id"`

	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Company   string `json:"company"`
	Phone     string `json:"phone"`

	CustomFields map[string]string `json:"custom_fields"`

	Subscribed bool           `json:"subscribed"`
	Campaigns  []MiniCampaign `json:"campaigns"`
	Categories []MiniCategory `json:"categories"`

	// Pre-send verification state (see internal/pkg/emailverify). Populated by
	// the verification scheduler / on-demand verify; the campaign send path uses
	// VerificationStatus == "invalid" to drop addresses before a worker sends.
	// VerificationStatus is one of: valid | risky | invalid | unknown.
	VerificationStatus    string     `json:"verification_status"`
	VerificationReason    string     `json:"verification_reason"`
	IsCatchAll            bool       `json:"is_catch_all"`
	VerificationCheckedAt *time.Time `json:"verification_checked_at,omitempty"`
	// VerificationSource says who produced the verdict (see the
	// VerificationSource* constants); VerificationProvider names the backend or
	// the external vocabulary; VerificationSubStatus refines the status
	// (catch_all, disposable, role, ...).
	VerificationSource    string `json:"verification_source"`
	VerificationProvider  string `json:"verification_provider"`
	VerificationSubStatus string `json:"verification_sub_status"`
	// VerificationConfidence is how sure the platform is of the status, 0 to
	// 100, scored from the last check plus what real mail to the address
	// showed (deliveries, opens, replies, bounces).
	VerificationConfidence int `json:"verification_confidence"`
	// VerificationRequestedAt is set while a member-requested re-check waits
	// to run; the verdict above stands until it lands.
	VerificationRequestedAt *time.Time `json:"verification_requested_at,omitempty"`

	// MailHost is who hosts the contact's inbox (a mailhost.Host value), read
	// from the domain's MX by the backend sweep; '' until it has run.
	MailHost string `json:"mail_host"`
	// ESPProvider is MailHost's family for campaign ESP matching: '' | 'gmail'
	// | 'outlook' | 'other'. Resolved with it, never on the send hot path.
	ESPProvider   string     `json:"esp_provider"`
	ESPResolvedAt *time.Time `json:"esp_resolved_at,omitempty"`

	// CampaignLead is this contact's processing state WITHIN a single campaign.
	// Populated by Search ONLY when the query filters by exactly one campaign
	// (the campaign Leads view); nil otherwise. Lets the Leads list show which
	// leads are queued, in progress, replied, bounced, or unsubscribed.
	CampaignLead *ContactCampaignProgress `json:"campaign_lead,omitempty"`

	// IsNew is set by the upsert write when this call inserted the row rather
	// than matching an existing contact. Server-side only: it decides whether
	// a contact.created event fires.
	IsNew bool `json:"-"`

	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

// ContactCampaignProgress is a contact's aggregate processing state inside one
// campaign, derived from campaign_contact_progress (across all of the campaign's
// steps) plus the contact's subscription flag.
type ContactCampaignProgress struct {
	// Status is the single derived state shown in the Leads list:
	//   pending      — a lead, but no email sent yet (queued)
	//   active       — some but not all steps sent, still progressing through the flow
	//   completed    — every sequence step was sent, no reply (done, nothing left to send)
	//   replied      — the contact has replied (terminal/positive)
	//   bounced      — a send hard-bounced (terminal/negative)
	//   failed       — the mailbox could not send a step after every retry (terminal/negative)
	//   unsubscribed — the contact is unsubscribed/suppressed (terminal)
	Status string `json:"status"`
	Sent   int    `json:"sent"`
	// Opened counts human opens only; automated fetches are in MachineOpened.
	Opened         int        `json:"opened"`
	MachineOpened  int        `json:"machine_opened"`
	Clicked        int        `json:"clicked"`
	Replied        int        `json:"replied"`
	Bounced        int        `json:"bounced"`
	LastActivityAt *time.Time `json:"last_activity_at,omitempty"`
	// CurrentStep is the label of the step the contact is on now — the latest
	// step actually sent ("Email 2", a custom step name, or an action label).
	// Empty when nothing has been sent yet (status "pending").
	CurrentStep string `json:"current_step,omitempty"`
	// FailureReason is the worker's reason for the last failed send, set only
	// when Status is "failed".
	FailureReason string `json:"failure_reason,omitempty"`
	// Sender is the mailbox address this lead's whole sequence sends from,
	// fixed when its first email went out. Empty until then.
	Sender string `json:"sender,omitempty"`
	// Hold is the per-lead pause, set only while it is live. Present on any
	// status: a held lead that has also replied still reads "replied".
	Hold *LeadHold `json:"hold,omitempty"`
}

// LeadHold is one contact's flow parked inside one campaign. Source is
// "out_of_office" when an auto-reply parked it and "manual" when a member did.
// Until is nil for a hold with no end, which only a person lifts.
type LeadHold struct {
	Since  time.Time  `json:"since"`
	Until  *time.Time `json:"until,omitempty"`
	Reason string     `json:"reason,omitempty"`
	Source string     `json:"source"`
}

// Live reports whether the hold is in force at t. A dated hold stops counting
// the moment it passes, with nothing having to write, so every surface that
// shows or enforces a hold reads it through this.
func (h *LeadHold) Live(t time.Time) bool {
	return h != nil && (h.Until == nil || h.Until.After(t))
}

// Lead hold sources, stored in campaign_leads.pause_source and matched by the
// CHECK constraint migration 000161 adds.
const (
	LeadHoldSourceManual      = "manual"
	LeadHoldSourceOutOfOffice = "out_of_office"
	// LeadHoldSourceInboxTagging is a hold a classified reply wrote: "not now"
	// for a while, a decline with no end.
	LeadHoldSourceInboxTagging = "inbox_tagging"
)

// Lead status constants for ContactCampaignProgress.Status.
const (
	LeadStatusPending      = "pending"
	LeadStatusActive       = "active"
	LeadStatusCompleted    = "completed"
	LeadStatusReplied      = "replied"
	LeadStatusBounced      = "bounced"
	LeadStatusFailed       = "failed"
	LeadStatusUnsubscribed = "unsubscribed"
	// LeadStatusPaused is a lead whose flow is held: an out-of-office
	// auto-reply parked it until the contact is back, or a member paused it by
	// hand. The lead keeps its place in the sequence and resumes where it
	// stopped; it is not unsubscribed and not removed from the campaign.
	LeadStatusPaused = "paused"
	// LeadStatusUndeliverable is a lead routing will never send to because
	// address verification refused it (invalid, or risky with the campaign's
	// "send to risky emails" toggle off). Distinct from pending, which is a
	// lead still waiting its turn.
	LeadStatusUndeliverable = "undeliverable"
)

// ValidLeadStatus reports whether s is one of the derived lead-status values.
// Used to gate the single-campaign Leads-view `lead_status` search filter.
func ValidLeadStatus(s string) bool {
	switch s {
	case LeadStatusPending, LeadStatusActive, LeadStatusCompleted, LeadStatusReplied, LeadStatusBounced, LeadStatusFailed, LeadStatusUnsubscribed, LeadStatusPaused, LeadStatusUndeliverable:
		return true
	default:
		return false
	}
}

// Lead engagement filter values for SearchContacts.Engagement. Each is a
// predicate over the contact's progress rows in one campaign; the negative
// forms only match leads that were sent at least one step, so a lead never
// emailed is neither "opened" nor "not opened".
const (
	LeadEngagementOpened     = "opened"
	LeadEngagementNotOpened  = "not_opened"
	LeadEngagementClicked    = "clicked"
	LeadEngagementNotClicked = "not_clicked"
	LeadEngagementReplied    = "replied"
	LeadEngagementNotReplied = "not_replied"
	LeadEngagementBounced    = "bounced"
)

// ValidLeadEngagement reports whether s is one of the engagement filter values.
func ValidLeadEngagement(s string) bool {
	switch s {
	case LeadEngagementOpened, LeadEngagementNotOpened, LeadEngagementClicked, LeadEngagementNotClicked, LeadEngagementReplied, LeadEngagementNotReplied, LeadEngagementBounced:
		return true
	default:
		return false
	}
}

type ContactsResult struct {
	Data       []Contact  `json:"data"`
	Pagination Pagination `json:"pagination"`
	// Counts is populated only when the search request asks for it
	// (`?counts=true`, first page). It carries org-wide facet totals for the
	// browse sidebar (all/subscribed/unsubscribed/in-campaign/not-contacted +
	// per category) and is independent of the request's filters, like the
	// campaigns-overview drawer counts.
	Counts *ContactsCounts `json:"counts,omitempty"`
	// LeadCounts carries per-status lead totals for ONE campaign (the campaign
	// Leads view). Populated only on the first page when the search filters by
	// exactly one campaign_id; nil otherwise. Independent of the request's
	// lead_status filter, so the scope chips can show every bucket's total.
	LeadCounts *CampaignLeadCounts `json:"lead_counts,omitempty"`
}

// CampaignLeadCounts are per-status lead totals within a single campaign,
// derived the same way as ContactCampaignProgress.Status (unsubscribed >
// bounced > replied > failed > completed > paused > processing >
// undeliverable > queued). Drives the Leads-view scope chips.
type CampaignLeadCounts struct {
	Total        int `json:"total"`
	Queued       int `json:"queued"`     // pending: a lead, no email sent yet
	Processing   int `json:"processing"` // active: some steps sent, more to send
	Completed    int `json:"completed"`  // done: every step sent, no reply
	Replied      int `json:"replied"`
	Bounced      int `json:"bounced"`
	Failed       int `json:"failed"` // a step could not be sent after every retry
	Unsubscribed int `json:"unsubscribed"`
	// Paused: the lead's flow is held (an out-of-office auto-reply, or a
	// member pausing it by hand) and resumes where it stopped.
	Paused int `json:"paused"`
	// Undeliverable: address verification refused it, so routing skips it.
	Undeliverable int `json:"undeliverable"`
	// Engagement totals, matching the `engagement` search filter: leads sent
	// at least one step, and of those the ones with a human open, a click, or
	// a reply on any step.
	Contacted  int `json:"contacted"`
	Opened     int `json:"opened"`
	Clicked    int `json:"clicked"`
	RepliedAny int `json:"replied_any"`
	// Providers splits the leads by their inbox's family, as ESP matching sees it.
	Providers CampaignLeadProviderCounts `json:"providers"`
}

// CampaignLeadProviderCounts are a campaign's leads by esp_provider family;
// Undetected counts the leads the provider check has not reached yet.
type CampaignLeadProviderCounts struct {
	Google     int `json:"gmail"`
	Microsoft  int `json:"outlook"`
	Other      int `json:"other"`
	Undetected int `json:"undetected"`
}

// ContactsCounts are org-wide contact facet totals for the browse sidebar.
type ContactsCounts struct {
	Total        int                       `json:"total"`
	Subscribed   int                       `json:"subscribed"`
	Unsubscribed int                       `json:"unsubscribed"`
	InCampaign   int                       `json:"in_campaign"`
	NotContacted int                       `json:"not_contacted"`
	Categories   []ContactCategoryCount    `json:"categories"`
	Verification ContactVerificationCounts `json:"verification"`
}

// ContactVerificationCounts is the org's contacts by verification status.
// Pending counts contacts never checked plus those with a re-check queued.
type ContactVerificationCounts struct {
	Valid   int `json:"valid"`
	Risky   int `json:"risky"`
	Invalid int `json:"invalid"`
	Unknown int `json:"unknown"`
	Pending int `json:"pending"`
}

// ContactVerificationDetail is the "why" behind a contact's verdict.
type ContactVerificationDetail struct {
	Status     string   `json:"status"`
	Confidence int      `json:"confidence"`
	Reasons    []string `json:"reasons"`
	// Decisive is true when real mail, not a check, decided the status.
	Decisive bool                          `json:"decisive"`
	Evidence []ContactVerificationEvidence `json:"evidence"`
	// Source and Provider name who produced the last check or verdict, and
	// ProviderLabel is the verifier's display name ("MillionVerifier").
	Source        string `json:"source"`
	Provider      string `json:"provider"`
	ProviderLabel string `json:"provider_label,omitempty"`
	// CheckStatus is what that check said before real mail was weighed in.
	CheckStatus string     `json:"check_status"`
	CheckedAt   *time.Time `json:"checked_at,omitempty"`
	// RequestedAt is set while a member-requested re-check waits to run.
	RequestedAt *time.Time `json:"requested_at,omitempty"`
}

// ContactVerificationEvidence is one observed fact about the mailbox.
type ContactVerificationEvidence struct {
	Kind       string    `json:"kind"`
	Detail     string    `json:"detail,omitempty"`
	ObservedAt time.Time `json:"observed_at"`
}

// Verification provenance values for contacts.verification_source.
const (
	VerificationSourceNone     = ""
	VerificationSourceProbe    = "probe"
	VerificationSourceProvider = "provider"
	VerificationSourceImported = "imported"
	VerificationSourceManual   = "manual"
)

// ContactVerificationWrite is a verdict to store on a contact, already
// normalised into Warmbly's vocabulary.
type ContactVerificationWrite struct {
	Status    string
	SubStatus string
	Reason    string
	Provider  string
	Source    string
}

// Actions for POST /contacts/verification.
const (
	ContactVerificationActionVerify            = "verify"
	ContactVerificationActionMarkDeliverable   = "mark_deliverable"
	ContactVerificationActionMarkUndeliverable = "mark_undeliverable"
)

// MaxContactBulkSelection bounds how many contacts one "select all matching"
// bulk action may resolve to. Past it the action is refused and the user
// narrows the filters, so a stray click can never walk a whole workspace.
const MaxContactBulkSelection = 50000

// ContactSelection names the contacts a bulk action applies to. Either an
// explicit id list (Contacts), or every contact matching a search (All +
// Filters) minus the rows the user unticked afterwards (Exclude), which is
// what the dashboard's "select all matching" sends. A selection that names
// both prefers the filter.
type ContactSelection struct {
	Contacts []string `json:"contacts"`
	// All switches the selection from the id list to Filters.
	All bool `json:"all,omitempty"`
	// Filters is the same search body /contacts/search takes, so the set
	// resolved here is exactly the set the list was showing.
	Filters *SearchContacts `json:"filters,omitempty"`
	// Exclude drops ids from the resolved set: the rows unticked after a
	// select-all. Ignored unless All is set.
	Exclude []string `json:"exclude,omitempty"`
}

// ContactVerificationRequest is the body of POST /contacts/verification.
type ContactVerificationRequest struct {
	ContactSelection
	// CampaignID selects every lead of one campaign that verification refused
	// (the "re-verify skipped leads" action), instead of listing ids.
	CampaignID string `json:"campaign_id,omitempty"`
	Action     string `json:"action"`
}

// ContactVerificationResponse reports how many contacts the action touched.
type ContactVerificationResponse struct {
	Affected int    `json:"affected"`
	Action   string `json:"action"`
	// Queued is true for the verify action: the check runs in the background
	// and each contact updates live as its verdict lands.
	Queued bool `json:"queued"`
	// Verifier and VerifierLabel name who runs a queued check ("builtin" or
	// the connected provider). VerifierError says why a connected provider
	// cannot be used right now, in which case the built-in check runs instead.
	Verifier      string `json:"verifier,omitempty"`
	VerifierLabel string `json:"verifier_label,omitempty"`
	VerifierError string `json:"verifier_error,omitempty"`
}

// VerificationOverview is what Settings shows about address verification.
type VerificationOverview struct {
	// Provider is who checks this workspace's addresses: "builtin" or
	// "millionverifier" or "cleanmylist".
	Provider string `json:"provider"`
	// ConnectionID is the integration connection behind a paid provider.
	ConnectionID *string `json:"connection_id,omitempty"`
	// Credits is the paid provider's remaining balance when it could be read.
	Credits *int `json:"credits,omitempty"`
	// ProviderError is set when the paid provider is connected but unusable
	// (bad key, no credits), in which case the built-in check is in use.
	ProviderError string `json:"provider_error,omitempty"`
	// BuiltinReady says whether the in-house probe can reach mail servers from
	// this instance (a HELO host is configured). Off, it still checks syntax,
	// MX, and disposable domains.
	BuiltinReady bool                      `json:"builtin_ready"`
	Counts       ContactVerificationCounts `json:"counts"`
}

// ContactCategoryCount is the number of org contacts carrying one category.
type ContactCategoryCount struct {
	CategoryID string `json:"category_id"`
	Count      int    `json:"count"`
}

// ContactEngagement summarises every email touchpoint we have for a
// single contact. It's denormalised on read so the contact 360 view
// can render counts and "last X" timestamps in a single round-trip.
type ContactEngagement struct {
	TotalSent       int `json:"total_sent"`
	TotalOpened     int `json:"total_opened"`
	TotalClicked    int `json:"total_clicked"`
	TotalReplied    int `json:"total_replied"`
	TotalBounced    int `json:"total_bounced"`
	TotalComplained int `json:"total_complained"`

	LastSentAt    *time.Time `json:"last_sent_at,omitempty"`
	LastOpenedAt  *time.Time `json:"last_opened_at,omitempty"`
	LastClickedAt *time.Time `json:"last_clicked_at,omitempty"`
	LastRepliedAt *time.Time `json:"last_replied_at,omitempty"`
	LastBouncedAt *time.Time `json:"last_bounced_at,omitempty"`

	// ReadsOn is how the contact reads your mail: each client and device a
	// person's opens came from, most recent first, with how often.
	ReadsOn []ContactReadingOrigin `json:"reads_on,omitempty"`
}

// ContactReadingOrigin is one client and device a contact opened mail on.
type ContactReadingOrigin struct {
	Client       string    `json:"client,omitempty"`
	ClientType   string    `json:"client_type,omitempty"`
	DeviceHidden bool      `json:"device_hidden,omitempty"`
	DeviceType   string    `json:"device_type,omitempty"`
	OS           string    `json:"os,omitempty"`
	Browser      string    `json:"browser,omitempty"`
	Opens        int       `json:"opens"`
	LastOpenedAt time.Time `json:"last_opened_at"`
}

// ContactSuppression mirrors a row from suppressed_recipients for the
// contact's email. Null on the wire when the contact is not suppressed.
type ContactSuppression struct {
	ID uuid.UUID `json:"id"`
	// Kind is "email" for the contact's own address or "domain" when the
	// whole domain is suppressed; Value is the matching list entry.
	Kind      string     `json:"kind"`
	Value     string     `json:"value"`
	Reason    string     `json:"reason"`
	Source    string     `json:"source"` // bounce | complaint | unsubscribe | manual | import
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// ContactDetail is the hydrated read model returned by GET /contacts/:id.
// It bundles everything the slide-over needs in one payload so the UI
// doesn't have to fan out a half-dozen requests on open.
type ContactDetail struct {
	Contact
	Engagement  ContactEngagement   `json:"engagement"`
	Suppression *ContactSuppression `json:"suppression,omitempty"`
	// Verification explains the verdict: the reasons behind it and the
	// observations it was scored from.
	Verification *ContactVerificationDetail `json:"verification,omitempty"`

	// First-touch attribution. Source never changes after creation.
	Source       ContactSource `json:"source"`
	SourceDetail string        `json:"source_detail"`
	FirstSeenAt  time.Time     `json:"first_seen_at"`
}

// ContactSentEmail is one row in the "Emails sent to this contact"
// list. Each row corresponds to a single delivered (or attempted)
// task. Engagement timestamps come from campaign_contact_progress
// when present; some legacy rows may have nil progress.
type ContactSentEmail struct {
	TaskID    uuid.UUID `json:"task_id"`
	Status    string    `json:"status"`
	MessageID string    `json:"message_id"`
	Subject   string    `json:"subject"`
	SentAt    time.Time `json:"sent_at"`

	// Sender mailbox
	EmailAccountID    *uuid.UUID `json:"email_account_id,omitempty"`
	EmailAccountEmail *string    `json:"email_account_email,omitempty"`
	EmailAccountName  *string    `json:"email_account_name,omitempty"`

	// Campaign + sequence context
	CampaignID   *uuid.UUID `json:"campaign_id,omitempty"`
	CampaignName *string    `json:"campaign_name,omitempty"`
	SequenceID   *uuid.UUID `json:"step_id,omitempty"`
	SequenceName *string    `json:"step_name,omitempty"`

	// Engagement (from campaign_contact_progress, may be nil).
	// OpenedAt is a person's open. An automated fetch (client prefetch,
	// security gateway) lands in MachineOpenedAt instead, so the two are
	// never mistaken for each other.
	OpenedAt        *time.Time `json:"opened_at,omitempty"`
	MachineOpenedAt *time.Time `json:"machine_opened_at,omitempty"`
	ClickedAt       *time.Time `json:"clicked_at,omitempty"`
	RepliedAt       *time.Time `json:"replied_at,omitempty"`
	BouncedAt       *time.Time `json:"bounced_at,omitempty"`
}

type ContactSentEmailsResult struct {
	Data       []ContactSentEmail `json:"data"`
	Pagination Pagination         `json:"pagination"`
}

// ContactTimelineEventType is a closed enum so the frontend can pick
// the right icon / colour without parsing free text.
type ContactTimelineEventType string

const (
	TimelineEmailSent      ContactTimelineEventType = "email_sent"
	TimelineEmailOpened    ContactTimelineEventType = "email_opened"
	TimelineEmailClicked   ContactTimelineEventType = "email_clicked"
	TimelineEmailReplied   ContactTimelineEventType = "email_replied"
	TimelineEmailBounced   ContactTimelineEventType = "email_bounced"
	TimelineReplyReceived  ContactTimelineEventType = "reply_received"
	TimelineDeliverability ContactTimelineEventType = "deliverability"
	TimelineSuppressed     ContactTimelineEventType = "suppressed"
	TimelineNote           ContactTimelineEventType = "note"

	// Meetings booked through a connected scheduling provider (Calendly /
	// Cal.com). The event time is when the booking arrived; ScheduledFor holds
	// when the call itself is set for.
	TimelineMeetingBooked      ContactTimelineEventType = "meeting_booked"
	TimelineMeetingRescheduled ContactTimelineEventType = "meeting_rescheduled"
	TimelineMeetingCanceled    ContactTimelineEventType = "meeting_canceled"

	// Lifecycle events, read from contact_activities. contact_created carries
	// the first-touch Source (and SourceDetail), which is how "imported" and
	// "created via API" are told apart without a type per origin.
	TimelineContactCreated  ContactTimelineEventType = "contact_created"
	TimelineCampaignAdded   ContactTimelineEventType = "campaign_added"
	TimelineCampaignRemoved ContactTimelineEventType = "campaign_removed"
	TimelineCategoryAdded   ContactTimelineEventType = "category_added"
	TimelineCategoryRemoved ContactTimelineEventType = "category_removed"
	TimelineFormSubmitted   ContactTimelineEventType = "form_submitted"

	// A page view from the website tracking snippet, tied to the contact
	// through an email-link ticket. Detail rides in PageHit.
	TimelinePageHit ContactTimelineEventType = "page_hit"
)

// ContactTimelineSource ranks the tables the timeline is merged from. It is
// the middle key of the feed's order (at, source, id): two events at the same
// instant sort by source, then by that source's row id, so a page boundary
// can never split a tie. The values are part of the cursor; never renumber.
type ContactTimelineSource int

const (
	// campaign_contact_progress stamps, keyed by the step (sequence) id.
	TimelineSourceProgressSent    ContactTimelineSource = 1
	TimelineSourceProgressOpened  ContactTimelineSource = 2
	TimelineSourceProgressClicked ContactTimelineSource = 3
	TimelineSourceProgressReplied ContactTimelineSource = 4
	TimelineSourceProgressBounced ContactTimelineSource = 5

	TimelineSourceLinkClick      ContactTimelineSource = 6  // email_link_clicks
	TimelineSourceOpen           ContactTimelineSource = 7  // email_opens
	TimelineSourceReplyIntent    ContactTimelineSource = 8  // reply_intents
	TimelineSourceDeliverability ContactTimelineSource = 9  // deliverability_events
	TimelineSourceSuppression    ContactTimelineSource = 10 // suppressed_recipients
	TimelineSourceNote           ContactTimelineSource = 11 // contact_notes
	TimelineSourceMeeting        ContactTimelineSource = 12 // meeting_bookings
	TimelineSourceActivity       ContactTimelineSource = 13 // contact_activities
	TimelineSourcePageHit        ContactTimelineSource = 14 // website_page_hits
)

// Valid reports whether s names a source the timeline is merged from. A
// cursor carrying any other rank is malformed: zero is reserved for the
// legacy bare-timestamp bound and anything above the last source would
// re-admit the events at the cursor's instant.
func (s ContactTimelineSource) Valid() bool {
	return s >= TimelineSourceProgressSent && s <= TimelineSourcePageHit
}

// ContactTimelineKey is one event's position in the merged feed. A page
// resumes strictly after the key of the last event it returned, comparing
// (At, Source, ID) as a tuple, which is what the opaque cursor carries.
type ContactTimelineKey struct {
	At     time.Time
	Source ContactTimelineSource
	ID     uuid.UUID
}

// Before reports whether k sorts after o in the feed's newest-first order,
// which is to say it is the older position: a smaller time, or the same time
// and a lower source rank, or the same time and source and a lower id (uuid
// order is the byte order Postgres uses, so Go and SQL agree).
func (k ContactTimelineKey) Before(o ContactTimelineKey) bool {
	if !k.At.Equal(o.At) {
		return k.At.Before(o.At)
	}
	if k.Source != o.Source {
		return k.Source < o.Source
	}
	return bytes.Compare(k.ID[:], o.ID[:]) < 0
}

// ContactTimelineEvent is one entry in the merged activity feed. The
// optional fields are tagged with omitempty so the JSON stays compact
// for event types that don't carry that data.
type ContactTimelineEvent struct {
	Type ContactTimelineEventType `json:"type"`
	At   time.Time                `json:"at"`

	// Position in the feed, used for the merged sort and the next-page
	// cursor. Not part of the wire shape: the row ids are not unique across
	// sources, so a client gets an opaque cursor instead.
	Key ContactTimelineKey `json:"-"`

	// Mailbox sender (email_sent / opened / clicked / replied / bounced).
	EmailAccountID    *uuid.UUID `json:"email_account_id,omitempty"`
	EmailAccountEmail *string    `json:"email_account_email,omitempty"`
	EmailAccountName  *string    `json:"email_account_name,omitempty"`

	// Campaign / sequence linkage. Optional because notes, suppression,
	// and out-of-campaign reply intents don't always have one.
	CampaignID   *uuid.UUID `json:"campaign_id,omitempty"`
	CampaignName *string    `json:"campaign_name,omitempty"`
	SequenceID   *uuid.UUID `json:"step_id,omitempty"`
	SequenceName *string    `json:"step_name,omitempty"`

	// Task linkage for engagement events.
	TaskID  *uuid.UUID `json:"task_id,omitempty"`
	Subject *string    `json:"subject,omitempty"`

	// Type-specific.
	Reason   *string `json:"reason,omitempty"`   // deliverability / suppression / meeting cancellation
	Source   *string `json:"source,omitempty"`   // suppression: bounce/complaint/unsubscribe; meeting: calendly/cal_com
	Provider *string `json:"provider,omitempty"` // deliverability provider
	Intent   *string `json:"intent,omitempty"`   // reply_intent classification
	Content  *string `json:"content,omitempty"`  // note body

	// Meeting events (meeting_booked / rescheduled / canceled).
	ScheduledFor *time.Time `json:"scheduled_for,omitempty"` // when the call is set for
	JoinURL      *string    `json:"join_url,omitempty"`      // video/conference link
	MeetingState *string    `json:"meeting_state,omitempty"` // booked / rescheduled / canceled

	// Lifecycle events: the category a contact joined or left, and the
	// free-form detail behind a contact_created source (file name, campaign,
	// API key).
	CategoryID    *uuid.UUID `json:"category_id,omitempty"`
	CategoryTitle *string    `json:"category_title,omitempty"`
	SourceDetail  *string    `json:"source_detail,omitempty"`

	// Form submission (form_submitted): which hosted form was filled in.
	FormID   *uuid.UUID `json:"form_id,omitempty"`
	FormName *string    `json:"form_name,omitempty"`

	// Website page view (page_hit): URL, referrer, device, UTM, location.
	PageHit *WebsitePageHit `json:"page_hit,omitempty"`

	// Engagement classification (email_opened / email_clicked). Machine is
	// true when the open or click came from an automated fetcher (a mail
	// privacy proxy, a security gateway walking the links) rather than a
	// person; MachineReason says which rule caught it.
	Machine       *bool   `json:"machine,omitempty"`
	MachineReason *string `json:"machine_reason,omitempty"`

	// The exact link behind an email_clicked event, when the click was
	// logged per link (every click since link attribution shipped).
	Link *ContactLinkClick `json:"link,omitempty"`

	// Where an email_opened / email_clicked event came from, when it was
	// logged per event: mail client, device and rough location.
	Origin *EngagementOrigin `json:"origin,omitempty"`

	// Author (notes, lifecycle events).
	UserID *uuid.UUID `json:"user_id,omitempty"`
}

// ContactLinkClick names the link a contact clicked: where it went, the
// anchor text it was minted from, and the UTM parameters the destination
// carried (automatic or hand-written).
type ContactLinkClick struct {
	ID          uuid.UUID `json:"id"`
	URL         string    `json:"url"`
	Label       string    `json:"label,omitempty"`
	UTMSource   string    `json:"utm_source,omitempty"`
	UTMMedium   string    `json:"utm_medium,omitempty"`
	UTMCampaign string    `json:"utm_campaign,omitempty"`
	UTMTerm     string    `json:"utm_term,omitempty"`
	UTMContent  string    `json:"utm_content,omitempty"`
	UserAgent   string    `json:"user_agent,omitempty"`
}

// EngagementOrigin is what an open or click said about where it came from.
// Client names the mail client when the user agent does (Gmail, Apple Mail,
// Outlook); ClientType says whether it was an installed app or webmail; the
// browser fields describe the rest. DeviceHidden marks a fetch by a mailbox
// provider's image proxy, which hides the reader's device and network. The
// location is resolved from the source network and the address itself is
// never stored. Every field is empty when unknown.
type EngagementOrigin struct {
	Client         string `json:"client,omitempty"`
	ClientType     string `json:"client_type,omitempty"`
	DeviceHidden   bool   `json:"device_hidden,omitempty"`
	DeviceType     string `json:"device_type,omitempty"`
	OS             string `json:"os,omitempty"`
	Browser        string `json:"browser,omitempty"`
	BrowserVersion string `json:"browser_version,omitempty"`
	CountryCode    string `json:"country_code,omitempty"`
	Region         string `json:"region,omitempty"`
	City           string `json:"city,omitempty"`
}

// EngagementOrigin.ClientType values, as the email_opens and
// email_link_clicks checks allow them. Empty is unknown.
const (
	EngagementClientApp     = "app"
	EngagementClientWebmail = "webmail"
)

// Empty reports whether nothing about the origin is known.
func (o EngagementOrigin) Empty() bool {
	return o == EngagementOrigin{}
}

type ContactTimelineResult struct {
	Data []ContactTimelineEvent `json:"data"`
	// Deprecated: read pagination.has_more. Kept for clients written against
	// the bare-timestamp pagination that predated the cursor envelope.
	HasMore bool `json:"has_more"`
	// NextCursor is an opaque (at, source, id) position; pass it back as
	// `cursor` for the next page. Total is never counted across the sources.
	Pagination Pagination `json:"pagination"`
}

// EvidenceStep names the campaign step a verification observation came from.
// It is what lets a late bounce or open be refused when the mail it describes
// left before the contact's address was edited: the observation is about the
// old mailbox, not the one the contact holds now. A zero value means the
// observation is not attributable to a step and is always recorded.
type EvidenceStep struct {
	CampaignID *uuid.UUID
	SequenceID *uuid.UUID
}

// Step builds an EvidenceStep from ids a caller already holds.
func Step(campaignID, sequenceID *uuid.UUID) EvidenceStep {
	return EvidenceStep{CampaignID: campaignID, SequenceID: sequenceID}
}

type UpdateContact struct {
	FirstName *string `json:"first_name"`
	LastName  *string `json:"last_name"`
	// Email replaces the contact's address. It is the contact's identity, so
	// changing it drops the verification verdict and the delivery evidence
	// that belonged to the old mailbox.
	Email            *string            `json:"email"`
	Company          *string            `json:"company"`
	Phone            *string            `json:"phone"`
	CustomFields     *map[string]string `json:"custom_fields"`
	Subscribed       *bool              `json:"subscribed"`
	Campaigns        []string           `json:"campaigns"`         // List of campaign IDs to set (nil = leave as-is)
	Categories       []string           `json:"categories"`        // List of category IDs to set (nil = leave as-is)
	AddCategories    []string           `json:"add_categories"`    // Diff-style add (ignored when Categories is set)
	RemoveCategories []string           `json:"remove_categories"` // Diff-style remove (ignored when Categories is set)
}

type AddContact struct {
	FirstName  string   `json:"first_name"`
	LastName   string   `json:"last_name"`
	Email      string   `json:"email"`
	Company    string   `json:"company"`
	Phone      string   `json:"phone"`
	Campaigns  []string `json:"campaigns"`
	Categories []string `json:"categories"`

	// Segments the new contact joins as an include override, so a contact
	// created from inside a segment belongs to it whatever the conditions
	// say. Unknown ids are a 400 before anything is written.
	Segments []string `json:"segments,omitempty"`

	CustomFields map[string]string `json:"custom_fields"`

	// VerificationStatus is a verdict the caller already holds for this
	// address, in Warmbly's vocabulary or any provider's the platform knows
	// (ZeroBounce, MillionVerifier, NeverBounce, ...). VerificationProvider
	// optionally names that vocabulary; without it the value is recognised by
	// itself. An unrecognised value is a 400. Stored as an imported verdict,
	// which the background check leaves alone until it ages out.
	VerificationStatus   string `json:"verification_status,omitempty"`
	VerificationProvider string `json:"verification_provider,omitempty"`

	// Verification is the normalised verdict derived from VerificationStatus.
	// Filled by the repository, never read from the request.
	Verification *ContactVerificationWrite `json:"-"`

	// Subscribed is the marketing-consent flag to store. nil means "don't
	// decide": a new contact defaults to subscribed, an existing one keeps
	// whatever it already had. Set explicitly by the importer when the file
	// carries a subscribed column.
	Subscribed *bool `json:"subscribed,omitempty"`

	// Source is the first-touch origin stamped on a NEW contact (an existing
	// one keeps its original). Dashboard callers may say "manual" or
	// "campaign"; every other value is decided server-side by the creation
	// path, and an API-key request is always "api". SourceDetail is never
	// read from the request body.
	Source       ContactSource `json:"source,omitempty"`
	SourceDetail string        `json:"-"`
}

// ContactSource is where a contact first came from. Mirrors the CHECK on
// contacts.source; ContactSourceUnknown is the honest value for rows created
// before attribution existed.
type ContactSource string

const (
	ContactSourceUnknown     ContactSource = "unknown"
	ContactSourceManual      ContactSource = "manual"
	ContactSourceCampaign    ContactSource = "campaign"
	ContactSourceImport      ContactSource = "import"
	ContactSourceSheetSync   ContactSource = "sheet_sync"
	ContactSourceAPI         ContactSource = "api"
	ContactSourceAIAssistant ContactSource = "ai_assistant"
	ContactSourceForm        ContactSource = "form"
	// ContactSourceAutomation is a contact an automation's "create or update
	// contact" action wrote; the detail is the automation's name.
	ContactSourceAutomation ContactSource = "automation"
)

// Valid reports whether the value is one the database accepts.
func (s ContactSource) Valid() bool {
	switch s {
	case ContactSourceUnknown, ContactSourceManual, ContactSourceCampaign, ContactSourceImport,
		ContactSourceSheetSync, ContactSourceAPI, ContactSourceAIAssistant, ContactSourceForm,
		ContactSourceAutomation:
		return true
	}
	return false
}

// RequestSettable reports whether a dashboard request body may claim the
// source. Only the two origins a dashboard user can actually be in.
func (s ContactSource) RequestSettable() bool {
	return s == ContactSourceManual || s == ContactSourceCampaign
}

type SearchContactsFilterType string

const (
	SearchContactsFilterTypeEqual      SearchContactsFilterType = "equal"
	SearchContactsFilterTypeStartsWith SearchContactsFilterType = "starts_with"
	SearchContactsFilterTypeEndsWith   SearchContactsFilterType = "ends_with"
	SearchContactsFilterTypeContains   SearchContactsFilterType = "contains"
)

type SearchContactsFilter struct {
	Name  string                   `json:"name"`
	Value string                   `json:"value"`
	Type  SearchContactsFilterType `json:"type"`
}

type SearchContacts struct {
	Query              string                 `json:"query"`                // Text search across core fields
	CustomFieldFilters []SearchContactsFilter `json:"custom_field_filters"` // Custom Field Filters
	CampaignIDs        []string               `json:"campaign_ids"`         // Contacts must be in ALL these campaigns
	LeadStatus         string                 `json:"lead_status"`          // Filter by derived lead status; requires exactly one campaign_id
	Engagement         string                 `json:"engagement"`           // Filter by lead engagement (opened, not_opened, ...); ANDed with lead_status; requires exactly one campaign_id
	CategoryIDs        []string               `json:"category_ids"`         // Contacts must have ALL these categories
	SegmentIDs         []string               `json:"segment_ids"`          // Contacts must be members of ALL these segments
	MinCampaigns       *int                   `json:"min_campaigns"`        // Minimum number of associated campaigns
	MaxCampaigns       *int                   `json:"max_campaigns"`        // Maximum number of associated campaigns
	Subscribed         *bool                  `json:"subscribed"`           // Filter by subscription status
	VerificationStatus string                 `json:"verification_status"`  // Filter by verification verdict: valid | risky | invalid | unknown
	MailHosts          []string               `json:"mail_hosts"`           // Contacts whose inbox host is any of these; "" matches not detected yet
	CreatedAfter       *time.Time             `json:"created_after"`        // Contacts created after this date
	CreatedBefore      *time.Time             `json:"created_before"`       // Contacts created before this date
	UpdatedAfter       *time.Time             `json:"updated_after"`        // Contacts updated after this date
	UpdatedBefore      *time.Time             `json:"updated_before"`       // Contacts updated before this date
	SortBy             string                 `json:"sort_by"`              // A column name (created_at, first_name, company, ...) or "custom:<key>" for a custom field
	Reverse            bool                   `json:"reverse"`              // Ascending when true; the default is descending
}

type BulkEditContactsFieldType string

const (
	BulkAddField    BulkEditContactsFieldType = "ADD"
	BulkEditField   BulkEditContactsFieldType = "EDIT"
	BulkDeleteField BulkEditContactsFieldType = "DELETE"
	BulkRenameField BulkEditContactsFieldType = "RENAME"
)

type BulkEditContactsField struct {
	Type  BulkEditContactsFieldType `json:"type"`
	Key   string                    `json:"key"`
	Value string                    `json:"value"`
}

type BulkEditContactsData struct {
	ContactSelection

	AddCampaigns     []string                `json:"add_campaigns"`
	RemoveCampaigns  []string                `json:"remove_campaigns"`
	AddCategories    []string                `json:"add_categories,omitempty"`
	RemoveCategories []string                `json:"remove_categories,omitempty"`
	Fields           []BulkEditContactsField `json:"fields"`
	Subscribe        *bool                   `json:"subscribe"`

	// SkipRows suppresses the hydrated rows in the response. Set by the
	// handler for a filter-shaped selection, which can name far more contacts
	// than are worth serializing back. Never part of the request body.
	SkipRows bool `json:"-"`
}
