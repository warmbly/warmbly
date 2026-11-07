package instancesettings

import (
	"time"

	"github.com/warmbly/warmbly/internal/config"
)

// Bounds on the invitation lifetime. An invitation is an initial credential, so
// it lives at most 48 hours; a stored longer value is read back as 48.
const (
	TTLHoursMin     = 1
	TTLHoursMax     = 48
	TTLHoursDefault = 48
)

// Invitations holds the invite-flow knobs.
type Invitations struct {
	// LinksEnabled exposes the copyable invite link. It is a bearer
	// credential, so a regulated deployment turns it off and relies on mail.
	LinksEnabled bool `json:"links_enabled"`
	// TTLHours is how long an invitation stays valid.
	TTLHours int `json:"ttl_hours"`
}

// Access holds the account-creation knobs.
type Access struct {
	// AllowInvitedSignup lets the invitee create their own account. Off means
	// an admin must create it for them.
	AllowInvitedSignup bool `json:"allow_invited_signup"`
}

// Sync holds the mailbox sync fair-use budgets. Zero means "compiled
// default" on read, so a document written before the section existed still
// resolves; the accepted ranges are clamped in Normalize.
type Sync struct {
	// BackfillDays is how far back the initial import reaches when a mailbox
	// is connected.
	BackfillDays int `json:"backfill_days"`
	// BackfillMessages caps how many messages that initial import stores per
	// mailbox, newest first.
	BackfillMessages int `json:"backfill_messages"`
	// DailyMessagesPerMailbox caps new (live) messages stored per mailbox per
	// UTC day. Replies to the mailbox's own sends have their own equal budget.
	DailyMessagesPerMailbox int `json:"daily_messages_per_mailbox"`
	// DailyMessagesPerOrg caps new plus backfilled messages stored across one
	// organization per UTC day.
	DailyMessagesPerOrg int `json:"daily_messages_per_org"`
}

// Retention holds how long event-level history is kept. Every window here
// bounds personal data: opens and clicks carry a client, a device and a
// location, funnel events carry a visitor's path, and the audit trail carries
// IP addresses, user agents and change payloads. Shortening one is the only
// way an operator can hold less without patching the binary.
//
// None of them affect a count, a filter or a routing decision: campaign
// progress keeps its own summary of opens and clicks, which outlives the
// per-event log.
type Retention struct {
	// EngagementEventDays is how long the per-event open and click logs are
	// kept.
	EngagementEventDays int `json:"engagement_event_days"`
	// FormEventDays is how long form funnel events are kept. Forms analytics
	// ranges top out at 90 days, so anything shorter than that shortens what
	// the funnel report can show.
	FormEventDays int `json:"form_event_days"`
	// AuditLogDays is how long the audit trail is kept.
	AuditLogDays int `json:"audit_log_days"`
	// WarmupMailDays is how long warmup mail stays in a mailbox before the
	// platform deletes it from the warmup folder. A mailbox may set its own
	// window; this is the one every other mailbox follows.
	WarmupMailDays int `json:"warmup_mail_days"`
	// WarmupEventDays is how long the per-message warmup records (tokens,
	// receipts, tampering and spam reports) are kept. The daily warmup
	// statistics behind the analytics are separate and never pruned.
	WarmupEventDays int `json:"warmup_event_days"`
	// CampaignLogDays is how long a campaign's activity feed keeps an entry; 0 keeps every entry.
	CampaignLogDays int `json:"campaign_log_days"`
	// NotificationDays is how long in-app notifications are kept, read or not; 0 keeps them all.
	NotificationDays int `json:"notification_days"`
	// WebhookDeliveryDays is how long a settled webhook delivery stays in its endpoint's log; 0 keeps them all.
	WebhookDeliveryDays int `json:"webhook_delivery_days"`
}

// Tracking holds the engagement-classification windows. Zero means "compiled
// default" on read, so a document written before the section existed still
// resolves; the accepted range is clamped in Normalize.
//
// Nothing here drops an event. A classified-automated open or click is still
// stored as delivery evidence and still shown on the timeline; it just does
// not stamp the step, fire a branch or automation, or send a webhook.
type Tracking struct {
	// MachineWindowOpenSeconds is how soon after a step was dispatched an open
	// is treated as automated. The clock starts at dispatch to the worker, so
	// this window also covers provider queueing and transit to the recipient.
	MachineWindowOpenSeconds int `json:"machine_window_open_seconds"`
	// MachineWindowClickSeconds is the same window for click tickets, kept
	// separately because a misjudged click costs an automation rather than a
	// metric.
	MachineWindowClickSeconds int `json:"machine_window_click_seconds"`
	// MachineWindowProbableSeconds replaces both of the above when the
	// tracking edge recognised the source as a scanner network that also
	// carries people's own requests, which browser isolation makes true of
	// Proofpoint, Mimecast and Cisco. Such a match moves the odds without
	// settling them, so it widens the window instead of deciding: inside it
	// the event is the delivery-time scan, outside it the person who got to
	// the mail later. Never applied shorter than the window for the kind of
	// event in hand, so it can only ever catch more.
	MachineWindowProbableSeconds int `json:"machine_window_probable_seconds"`
}

// DefaultTracking is the compiled classification window for each event kind.
func DefaultTracking() Tracking {
	return Tracking{
		MachineWindowOpenSeconds:     config.TrackingMachineWindowOpenSecondsDefault,
		MachineWindowClickSeconds:    config.TrackingMachineWindowClickSecondsDefault,
		MachineWindowProbableSeconds: config.TrackingMachineWindowProbableSecondsDefault,
	}
}

// Normalize clamps every window into its accepted range. Zero and negative
// resolve to the compiled default rather than to "never automated", so a
// document written before this section existed keeps the shipped behaviour.
func (t *Tracking) Normalize() {
	clamp := func(v, def, lo, hi int) int {
		if v <= 0 {
			return def
		}
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	machine := func(v, def int) int {
		return clamp(v, def, config.TrackingMachineWindowSecondsMin, config.TrackingMachineWindowSecondsMax)
	}
	t.MachineWindowOpenSeconds = machine(t.MachineWindowOpenSeconds, config.TrackingMachineWindowOpenSecondsDefault)
	t.MachineWindowClickSeconds = machine(t.MachineWindowClickSeconds, config.TrackingMachineWindowClickSecondsDefault)
	// The probable window has bounds of its own: it reaches a day, because how
	// long a security vendor takes to detonate a link is the vendor's property.
	t.MachineWindowProbableSeconds = clamp(
		t.MachineWindowProbableSeconds,
		config.TrackingMachineWindowProbableSecondsDefault,
		config.TrackingMachineWindowProbableSecondsMin,
		config.TrackingMachineWindowProbableSecondsMax,
	)
}

// OpenWindow and ClickWindow are the normalized windows as durations.
func (t Tracking) OpenWindow() time.Duration {
	return time.Duration(t.MachineWindowOpenSeconds) * time.Second
}

func (t Tracking) ClickWindow() time.Duration {
	return time.Duration(t.MachineWindowClickSeconds) * time.Second
}

// ProbableWindow is the window a probable-scanner source is measured against,
// never shorter than `kind`, the window that event would get on its own. The
// floor is here rather than in Normalize so the two settings stay independent
// on the way in and the rule stays one-way on the way out: naming a source can
// only ever catch more, never fewer.
func (t Tracking) ProbableWindow(kind time.Duration) time.Duration {
	if probable := time.Duration(t.MachineWindowProbableSeconds) * time.Second; probable > kind {
		return probable
	}
	return kind
}

// Bounds on the domain-authentication grace window. One hour is the shortest
// window that still absorbs a resolver blip; 30 days is the longest a domain
// should keep sending cold mail unauthenticated while being warned about it.
const (
	AuthGraceHoursMin     = 1
	AuthGraceHoursMax     = 720
	AuthGraceHoursDefault = 72
)

// Deliverability holds the sending-domain authentication gate.
type Deliverability struct {
	// EnforceDomainAuth stops cold sends and warmup sends from a mailbox whose
	// sending domain has been failing SPF/DMARC for longer than the grace
	// window. Off leaves the state observe-only: still checked, still shown,
	// still raised by the advisor, but never blocking.
	EnforceDomainAuth bool `json:"enforce_domain_auth"`
	// AuthGraceHours is how long a domain must stay failing before the gate
	// applies. The clock starts when the sweep first sees the failure, so this
	// is also how much warning the owner gets.
	AuthGraceHours int `json:"auth_grace_hours"`
}

// MailboxSignin holds how mailboxes change the way they sign in.
type MailboxSignin struct {
	// AutoVendorReconnect has an inbox vendor that can sign in for Warmbly reconnect its Google mailboxes
	// that lack full mail access, with no click. Off leaves the reconnect to a member.
	AutoVendorReconnect bool `json:"auto_vendor_reconnect"`
}

// Placement holds the inbox placement test knobs.
type Placement struct {
	// TestsPerMonthTrial and TestsPerMonthPaid are how many tests one workspace
	// may run on the instance and cloud seed panels per calendar month. Tests
	// on a workspace's own seed mailboxes are not counted.
	TestsPerMonthTrial int `json:"tests_per_month_trial"`
	TestsPerMonthPaid  int `json:"tests_per_month_paid"`
	// SeedsPerTest caps how many seed mailboxes one test sends to, which is
	// also how many sends it takes from the sender's day.
	SeedsPerTest int `json:"seeds_per_test"`
	// SpacingSeconds is the gap between two probes from one sender.
	SpacingSeconds int `json:"spacing_seconds"`
	// CreditsPerTest is what a test past the monthly allowance costs in
	// credits. Zero turns paid tests off; nil is the compiled default.
	CreditsPerTest *int `json:"credits_per_test"`
	// BatchSendersMax is the most senders one batch may hold, an
	// infrastructure safeguard separate from how many run at once.
	BatchSendersMax int `json:"batch_senders_max"`
	// BatchSenderConcurrency is how many of one workspace's batch senders
	// may be sending probes at the same time.
	BatchSenderConcurrency int `json:"batch_sender_concurrency"`
	// BatchInstanceConcurrency caps batch senders sending at once across
	// every workspace, which bounds what the shared seed panel receives.
	BatchInstanceConcurrency int `json:"batch_instance_concurrency"`
	// BatchStartsPerMinute paces how fast one batch starts senders.
	BatchStartsPerMinute int `json:"batch_starts_per_minute"`
}

// CreditPrice is the resolved price of a paid test, zero when off.
func (p Placement) CreditPrice() int {
	if p.CreditsPerTest == nil {
		return config.PlacementCreditsPerTestDefault
	}
	return *p.CreditsPerTest
}

// DefaultPlacement is the compiled placement test allowance and pacing.
func DefaultPlacement() Placement {
	price := config.PlacementCreditsPerTestDefault
	return Placement{
		TestsPerMonthTrial: config.PlacementTestsPerMonthTrialDefault,
		TestsPerMonthPaid:  config.PlacementTestsPerMonthPaidDefault,
		SeedsPerTest:       config.PlacementSeedsPerTestDefault,
		SpacingSeconds:     config.PlacementSpacingSecondsDefault,
		CreditsPerTest:     &price,

		BatchSendersMax:          config.PlacementBatchSendersMaxDefault,
		BatchSenderConcurrency:   config.PlacementBatchSenderConcurrencyDefault,
		BatchInstanceConcurrency: config.PlacementBatchInstanceConcurrencyDefault,
		BatchStartsPerMinute:     config.PlacementBatchStartsPerMinuteDefault,
	}
}

// Normalize clamps every knob; zero and negative resolve to the compiled
// default, so a document written before this section existed keeps it.
func (p *Placement) Normalize() {
	month := func(v, def int) int {
		if v <= 0 {
			return def
		}
		return min(v, config.PlacementTestsPerMonthMax)
	}
	p.TestsPerMonthTrial = month(p.TestsPerMonthTrial, config.PlacementTestsPerMonthTrialDefault)
	p.TestsPerMonthPaid = month(p.TestsPerMonthPaid, config.PlacementTestsPerMonthPaidDefault)
	if p.SeedsPerTest <= 0 {
		p.SeedsPerTest = config.PlacementSeedsPerTestDefault
	}
	p.SeedsPerTest = min(p.SeedsPerTest, config.PlacementSeedsPerTestMax)
	if p.SpacingSeconds <= 0 {
		p.SpacingSeconds = config.PlacementSpacingSecondsDefault
	}
	p.SpacingSeconds = max(config.PlacementSpacingSecondsMin, min(p.SpacingSeconds, config.PlacementSpacingSecondsMax))
	v := config.PlacementCreditsPerTestDefault
	if p.CreditsPerTest != nil {
		v = max(0, min(*p.CreditsPerTest, config.PlacementCreditsPerTestMax))
	}
	p.CreditsPerTest = &v
	clamp := func(v, def, ceiling int) int {
		if v <= 0 {
			return def
		}
		return min(v, ceiling)
	}
	p.BatchSendersMax = clamp(p.BatchSendersMax, config.PlacementBatchSendersMaxDefault, config.PlacementBatchSendersMaxCeiling)
	p.BatchSenderConcurrency = clamp(p.BatchSenderConcurrency, config.PlacementBatchSenderConcurrencyDefault, config.PlacementBatchSenderConcurrencyMax)
	p.BatchInstanceConcurrency = clamp(p.BatchInstanceConcurrency, config.PlacementBatchInstanceConcurrencyDefault, config.PlacementBatchInstanceConcurrencyMax)
	p.BatchStartsPerMinute = clamp(p.BatchStartsPerMinute, config.PlacementBatchStartsPerMinuteDefault, config.PlacementBatchStartsPerMinuteMax)
}

// QuickSpacing is the gap between two probes of a quick test.
func (p Placement) QuickSpacing() time.Duration {
	return time.Duration(min(p.SpacingSeconds, config.PlacementQuickSpacingSeconds)) * time.Second
}

// Spacing is the gap between two probes as a duration.
func (p Placement) Spacing() time.Duration {
	return time.Duration(p.SpacingSeconds) * time.Second
}

// Document is the whole Tier B settings document. Every key here is one no
// environment variable owns, so there is no precedence to resolve.
type Document struct {
	Invitations    Invitations    `json:"invitations"`
	Access         Access         `json:"access"`
	Sync           Sync           `json:"sync"`
	Retention      Retention      `json:"retention"`
	Tracking       Tracking       `json:"tracking"`
	Deliverability Deliverability `json:"deliverability"`
	Placement      Placement      `json:"placement"`
	Notifications  Notifications  `json:"notifications"`
	MailboxSignin  MailboxSignin  `json:"mailbox_signin"`
	Usage          Usage          `json:"usage"`
}

// Defaults is the document a fresh instance behaves as if it had.
func Defaults() Document {
	return Document{
		Invitations: Invitations{
			LinksEnabled: true,
			TTLHours:     TTLHoursDefault,
		},
		Access: Access{
			AllowInvitedSignup: true,
		},
		Sync:           DefaultSync(),
		Retention:      DefaultRetention(),
		Tracking:       DefaultTracking(),
		Deliverability: DefaultDeliverability(),
		Placement:      DefaultPlacement(),
		MailboxSignin:  MailboxSignin{AutoVendorReconnect: true},
		Usage:          DefaultUsage(),
	}
}

// DefaultDeliverability enforces the gate. Unauthenticated cold mail is a hard
// Gmail/Yahoo/Outlook rejection and the fastest way to burn a shared warmup
// pool, and the grace window means nothing stops until a domain has been
// provably broken for three days with the owner notified throughout.
func DefaultDeliverability() Deliverability {
	return Deliverability{
		EnforceDomainAuth: true,
		AuthGraceHours:    AuthGraceHoursDefault,
	}
}

// DefaultSync is the compiled sync fair-use budget.
func DefaultSync() Sync {
	return Sync{
		BackfillDays:            config.SyncBackfillDaysDefault,
		BackfillMessages:        config.SyncBackfillMessagesDefault,
		DailyMessagesPerMailbox: config.SyncDailyMessagesMailboxDefault,
		DailyMessagesPerOrg:     config.SyncDailyMessagesOrgDefault,
	}
}

// DefaultRetention is the compiled retention window for each event log.
func DefaultRetention() Retention {
	return Retention{
		EngagementEventDays: config.EngagementEventRetentionDaysDefault,
		FormEventDays:       config.FormEventsRetentionDaysDefault,
		AuditLogDays:        config.AuditLogRetentionDaysDefault,
		WarmupMailDays:      config.WarmupMailRetentionDaysDefault,
		WarmupEventDays:     config.WarmupEventRetentionDaysDefault,
		CampaignLogDays:     config.CampaignLogRetentionDaysDefault,
		NotificationDays:    config.NotificationRetentionDaysDefault,
		WebhookDeliveryDays: config.WebhookDeliveryRetentionDaysDefault,
	}
}

// Normalize clamps every window into its accepted range. Zero and negative
// resolve to the compiled default rather than to "keep nothing": a document
// written before this section existed must not silently start deleting
// everything on the next sweep.
func (r *Retention) Normalize() {
	clamp := func(v, def, floor int) int {
		if v <= 0 {
			return def
		}
		if v < floor {
			return floor
		}
		if v > config.RetentionDaysMax {
			return config.RetentionDaysMax
		}
		return v
	}
	r.EngagementEventDays = clamp(r.EngagementEventDays, config.EngagementEventRetentionDaysDefault, config.RetentionDaysMin)
	r.FormEventDays = clamp(r.FormEventDays, config.FormEventsRetentionDaysDefault, config.RetentionDaysMin)
	r.AuditLogDays = clamp(r.AuditLogDays, config.AuditLogRetentionDaysDefault, config.RetentionDaysMin)
	// The two warmup windows have floors of their own: mail has to outlive
	// the engagement legs and a reply-back, and the records have to outlive
	// the thirty-day health bands that read them.
	r.WarmupMailDays = clamp(r.WarmupMailDays, config.WarmupMailRetentionDaysDefault, config.WarmupMailRetentionDaysMin)
	r.WarmupEventDays = clamp(r.WarmupEventDays, config.WarmupEventRetentionDaysDefault, config.WarmupEventRetentionDaysMin)
	// The feed windows treat 0 as keep everything, which is also their default.
	optional := func(v int) int {
		if v <= 0 {
			return 0
		}
		return clamp(v, 0, config.RetentionDaysMin)
	}
	r.CampaignLogDays = optional(r.CampaignLogDays)
	r.NotificationDays = optional(r.NotificationDays)
	r.WebhookDeliveryDays = optional(r.WebhookDeliveryDays)
}

// Normalize clamps a document into its accepted range. It is applied on read
// as well as on write, so a row written by an older version still resolves.
func (d *Document) Normalize() {
	if d.Invitations.TTLHours <= 0 {
		d.Invitations.TTLHours = TTLHoursDefault
	}
	if d.Invitations.TTLHours < TTLHoursMin {
		d.Invitations.TTLHours = TTLHoursMin
	}
	if d.Invitations.TTLHours > TTLHoursMax {
		d.Invitations.TTLHours = TTLHoursMax
	}
	d.Sync.Normalize()
	d.Retention.Normalize()
	d.Tracking.Normalize()
	d.Deliverability.Normalize()
	d.Placement.Normalize()
	d.Notifications.Normalize()
	d.Usage.Normalize()
}

// Normalize clamps the grace window. Zero and negative resolve to the compiled
// default rather than to "no grace": a document that predates this section, or
// one edited by hand, must not accidentally mean "gate everything immediately".
func (d *Deliverability) Normalize() {
	if d.AuthGraceHours <= 0 {
		d.AuthGraceHours = AuthGraceHoursDefault
	}
	if d.AuthGraceHours < AuthGraceHoursMin {
		d.AuthGraceHours = AuthGraceHoursMin
	}
	if d.AuthGraceHours > AuthGraceHoursMax {
		d.AuthGraceHours = AuthGraceHoursMax
	}
}

// AuthGrace is the domain-authentication grace window as a duration.
func (d Deliverability) AuthGrace() time.Duration {
	return time.Duration(d.AuthGraceHours) * time.Hour
}

// Normalize clamps every budget into its accepted range; zero and negative
// resolve to the compiled default rather than to "off". There is no way to
// switch fair use off from this document: an operator who wants no cap sets
// the maximum, which is still a number.
func (s *Sync) Normalize() {
	clamp := func(v, def, max int) int {
		if v <= 0 {
			return def
		}
		if v > max {
			return max
		}
		return v
	}
	s.BackfillDays = clamp(s.BackfillDays, config.SyncBackfillDaysDefault, config.SyncBackfillDaysMax)
	s.BackfillMessages = clamp(s.BackfillMessages, config.SyncBackfillMessagesDefault, config.SyncBackfillMessagesMax)
	s.DailyMessagesPerMailbox = clamp(s.DailyMessagesPerMailbox, config.SyncDailyMessagesMailboxDefault, config.SyncDailyMessagesMailboxMax)
	s.DailyMessagesPerOrg = clamp(s.DailyMessagesPerOrg, config.SyncDailyMessagesOrgDefault, config.SyncDailyMessagesOrgMax)
}

// TTL is the invitation lifetime as a duration.
func (d Document) TTL() time.Duration {
	return time.Duration(d.Invitations.TTLHours) * time.Hour
}

// Patch is a partial update. Absent fields keep their stored value, so a
// client that does not know about a key cannot clear it.
type Patch struct {
	Invitations *struct {
		LinksEnabled *bool `json:"links_enabled"`
		TTLHours     *int  `json:"ttl_hours"`
	} `json:"invitations"`
	Access *struct {
		AllowInvitedSignup *bool `json:"allow_invited_signup"`
	} `json:"access"`
	Sync *struct {
		BackfillDays            *int `json:"backfill_days"`
		BackfillMessages        *int `json:"backfill_messages"`
		DailyMessagesPerMailbox *int `json:"daily_messages_per_mailbox"`
		DailyMessagesPerOrg     *int `json:"daily_messages_per_org"`
	} `json:"sync"`
	Retention *struct {
		EngagementEventDays *int `json:"engagement_event_days"`
		FormEventDays       *int `json:"form_event_days"`
		AuditLogDays        *int `json:"audit_log_days"`
		WarmupMailDays      *int `json:"warmup_mail_days"`
		WarmupEventDays     *int `json:"warmup_event_days"`
		CampaignLogDays     *int `json:"campaign_log_days"`
		NotificationDays    *int `json:"notification_days"`
		WebhookDeliveryDays *int `json:"webhook_delivery_days"`
	} `json:"retention"`
	Tracking *struct {
		MachineWindowOpenSeconds     *int `json:"machine_window_open_seconds"`
		MachineWindowClickSeconds    *int `json:"machine_window_click_seconds"`
		MachineWindowProbableSeconds *int `json:"machine_window_probable_seconds"`
	} `json:"tracking"`
	Deliverability *struct {
		EnforceDomainAuth *bool `json:"enforce_domain_auth"`
		AuthGraceHours    *int  `json:"auth_grace_hours"`
	} `json:"deliverability"`
	Placement *struct {
		TestsPerMonthTrial *int `json:"tests_per_month_trial"`
		TestsPerMonthPaid  *int `json:"tests_per_month_paid"`
		SeedsPerTest       *int `json:"seeds_per_test"`
		SpacingSeconds     *int `json:"spacing_seconds"`
		CreditsPerTest     *int `json:"credits_per_test"`

		BatchSendersMax          *int `json:"batch_senders_max"`
		BatchSenderConcurrency   *int `json:"batch_sender_concurrency"`
		BatchInstanceConcurrency *int `json:"batch_instance_concurrency"`
		BatchStartsPerMinute     *int `json:"batch_starts_per_minute"`
	} `json:"placement"`
	// Channels replaces the whole list when present. A channel that comes back
	// with a masked target or secret keeps the stored value, so the admin panel
	// can round-trip a redacted read without wiping credentials.
	Notifications *struct {
		Channels *[]NotifyChannel `json:"channels"`
	} `json:"notifications"`
	MailboxSignin *struct {
		AutoVendorReconnect *bool `json:"auto_vendor_reconnect"`
	} `json:"mailbox_signin"`
	// Models and Units replace the whole list or map when present.
	Usage *struct {
		MarkupPercent *int                `json:"markup_percent"`
		Models        *[]ModelPrice       `json:"models"`
		Units         *map[string]float64 `json:"units"`
	} `json:"usage"`
}

// Apply merges a patch onto a document.
func (p Patch) Apply(doc Document) Document {
	if p.Invitations != nil {
		if p.Invitations.LinksEnabled != nil {
			doc.Invitations.LinksEnabled = *p.Invitations.LinksEnabled
		}
		if p.Invitations.TTLHours != nil {
			// An explicit zero is a caller mistake, not "use the default": the
			// documented accepted range starts at one hour.
			doc.Invitations.TTLHours = *p.Invitations.TTLHours
			if doc.Invitations.TTLHours < TTLHoursMin {
				doc.Invitations.TTLHours = TTLHoursMin
			}
		}
	}
	if p.Access != nil && p.Access.AllowInvitedSignup != nil {
		doc.Access.AllowInvitedSignup = *p.Access.AllowInvitedSignup
	}
	if p.Sync != nil {
		if p.Sync.BackfillDays != nil {
			doc.Sync.BackfillDays = *p.Sync.BackfillDays
		}
		if p.Sync.BackfillMessages != nil {
			doc.Sync.BackfillMessages = *p.Sync.BackfillMessages
		}
		if p.Sync.DailyMessagesPerMailbox != nil {
			doc.Sync.DailyMessagesPerMailbox = *p.Sync.DailyMessagesPerMailbox
		}
		if p.Sync.DailyMessagesPerOrg != nil {
			doc.Sync.DailyMessagesPerOrg = *p.Sync.DailyMessagesPerOrg
		}
	}
	if p.Retention != nil {
		if p.Retention.EngagementEventDays != nil {
			doc.Retention.EngagementEventDays = *p.Retention.EngagementEventDays
		}
		if p.Retention.FormEventDays != nil {
			doc.Retention.FormEventDays = *p.Retention.FormEventDays
		}
		if p.Retention.AuditLogDays != nil {
			doc.Retention.AuditLogDays = *p.Retention.AuditLogDays
		}
		if p.Retention.WarmupMailDays != nil {
			doc.Retention.WarmupMailDays = *p.Retention.WarmupMailDays
		}
		if p.Retention.WarmupEventDays != nil {
			doc.Retention.WarmupEventDays = *p.Retention.WarmupEventDays
		}
		if p.Retention.CampaignLogDays != nil {
			doc.Retention.CampaignLogDays = *p.Retention.CampaignLogDays
		}
		if p.Retention.NotificationDays != nil {
			doc.Retention.NotificationDays = *p.Retention.NotificationDays
		}
		if p.Retention.WebhookDeliveryDays != nil {
			doc.Retention.WebhookDeliveryDays = *p.Retention.WebhookDeliveryDays
		}
	}
	if p.Tracking != nil {
		if p.Tracking.MachineWindowOpenSeconds != nil {
			doc.Tracking.MachineWindowOpenSeconds = *p.Tracking.MachineWindowOpenSeconds
		}
		if p.Tracking.MachineWindowClickSeconds != nil {
			doc.Tracking.MachineWindowClickSeconds = *p.Tracking.MachineWindowClickSeconds
		}
		if p.Tracking.MachineWindowProbableSeconds != nil {
			doc.Tracking.MachineWindowProbableSeconds = *p.Tracking.MachineWindowProbableSeconds
		}
	}
	if p.Deliverability != nil {
		if p.Deliverability.EnforceDomainAuth != nil {
			doc.Deliverability.EnforceDomainAuth = *p.Deliverability.EnforceDomainAuth
		}
		if p.Deliverability.AuthGraceHours != nil {
			// An explicit zero is a caller mistake, not "no grace": the
			// documented accepted range starts at one hour.
			doc.Deliverability.AuthGraceHours = *p.Deliverability.AuthGraceHours
			if doc.Deliverability.AuthGraceHours < AuthGraceHoursMin {
				doc.Deliverability.AuthGraceHours = AuthGraceHoursMin
			}
		}
	}
	if p.Placement != nil {
		if p.Placement.TestsPerMonthTrial != nil {
			doc.Placement.TestsPerMonthTrial = *p.Placement.TestsPerMonthTrial
		}
		if p.Placement.TestsPerMonthPaid != nil {
			doc.Placement.TestsPerMonthPaid = *p.Placement.TestsPerMonthPaid
		}
		if p.Placement.SeedsPerTest != nil {
			doc.Placement.SeedsPerTest = *p.Placement.SeedsPerTest
		}
		if p.Placement.SpacingSeconds != nil {
			doc.Placement.SpacingSeconds = *p.Placement.SpacingSeconds
		}
		if p.Placement.CreditsPerTest != nil {
			v := *p.Placement.CreditsPerTest
			doc.Placement.CreditsPerTest = &v
		}
		if p.Placement.BatchSendersMax != nil {
			doc.Placement.BatchSendersMax = *p.Placement.BatchSendersMax
		}
		if p.Placement.BatchSenderConcurrency != nil {
			doc.Placement.BatchSenderConcurrency = *p.Placement.BatchSenderConcurrency
		}
		if p.Placement.BatchInstanceConcurrency != nil {
			doc.Placement.BatchInstanceConcurrency = *p.Placement.BatchInstanceConcurrency
		}
		if p.Placement.BatchStartsPerMinute != nil {
			doc.Placement.BatchStartsPerMinute = *p.Placement.BatchStartsPerMinute
		}
	}
	if p.Notifications != nil && p.Notifications.Channels != nil {
		doc.Notifications.Channels = mergeChannels(doc.Notifications.Channels, *p.Notifications.Channels)
	}
	if p.MailboxSignin != nil && p.MailboxSignin.AutoVendorReconnect != nil {
		doc.MailboxSignin.AutoVendorReconnect = *p.MailboxSignin.AutoVendorReconnect
	}
	if p.Usage != nil {
		if p.Usage.MarkupPercent != nil {
			v := *p.Usage.MarkupPercent
			doc.Usage.MarkupPercent = &v
		}
		if p.Usage.Models != nil {
			doc.Usage.Models = append([]ModelPrice(nil), (*p.Usage.Models)...)
		}
		if p.Usage.Units != nil {
			doc.Usage.Units = map[string]float64{}
			for k, v := range *p.Usage.Units {
				doc.Usage.Units[k] = v
			}
		}
	}
	return doc
}
