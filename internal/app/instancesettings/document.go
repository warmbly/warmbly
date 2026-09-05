package instancesettings

import (
	"time"

	"github.com/warmbly/warmbly/internal/config"
)

// Bounds on the invitation lifetime. An hour is the shortest window a person
// can realistically act in; 30 days is the longest a bearer link should live.
const (
	TTLHoursMin     = 1
	TTLHoursMax     = 720
	TTLHoursDefault = 168
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

// Document is the whole Tier B settings document. Every key here is one no
// environment variable owns, so there is no precedence to resolve.
type Document struct {
	Invitations    Invitations    `json:"invitations"`
	Access         Access         `json:"access"`
	Sync           Sync           `json:"sync"`
	Retention      Retention      `json:"retention"`
	Deliverability Deliverability `json:"deliverability"`
	Notifications  Notifications  `json:"notifications"`
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
		Deliverability: DefaultDeliverability(),
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
	}
}

// Normalize clamps every window into its accepted range. Zero and negative
// resolve to the compiled default rather than to "keep nothing": a document
// written before this section existed must not silently start deleting
// everything on the next sweep.
func (r *Retention) Normalize() {
	clamp := func(v, def int) int {
		if v <= 0 {
			return def
		}
		if v < config.RetentionDaysMin {
			return config.RetentionDaysMin
		}
		if v > config.RetentionDaysMax {
			return config.RetentionDaysMax
		}
		return v
	}
	r.EngagementEventDays = clamp(r.EngagementEventDays, config.EngagementEventRetentionDaysDefault)
	r.FormEventDays = clamp(r.FormEventDays, config.FormEventsRetentionDaysDefault)
	r.AuditLogDays = clamp(r.AuditLogDays, config.AuditLogRetentionDaysDefault)
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
	d.Deliverability.Normalize()
	d.Notifications.Normalize()
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
	} `json:"retention"`
	Deliverability *struct {
		EnforceDomainAuth *bool `json:"enforce_domain_auth"`
		AuthGraceHours    *int  `json:"auth_grace_hours"`
	} `json:"deliverability"`
	// Channels replaces the whole list when present. A channel that comes back
	// with a masked target or secret keeps the stored value, so the admin panel
	// can round-trip a redacted read without wiping credentials.
	Notifications *struct {
		Channels *[]NotifyChannel `json:"channels"`
	} `json:"notifications"`
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
	if p.Notifications != nil && p.Notifications.Channels != nil {
		doc.Notifications.Channels = mergeChannels(doc.Notifications.Channels, *p.Notifications.Channels)
	}
	return doc
}
