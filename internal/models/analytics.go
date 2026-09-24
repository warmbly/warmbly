package models

import (
	"time"

	"github.com/google/uuid"
)

type DateRange struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// Warmup Analytics

type WarmupAnalytics struct {
	EmailAccountID uuid.UUID          `json:"email_account_id"`
	Email          string             `json:"email"`
	DateRange      DateRange          `json:"date_range"`
	Summary        WarmupSummary      `json:"summary"`
	DailyStats     []WarmupDailyStats `json:"daily_stats"`
}

type WarmupSummary struct {
	TotalSent    int `json:"total_sent"`
	TotalReplied int `json:"total_replied"`
	// TotalReceived is verified warmup mail that arrived from partners in the
	// range: the other half of the exchange, so a mailbox that sends and is
	// never written to can be seen from the numbers.
	TotalReceived int     `json:"total_received"`
	AverageDaily  float64 `json:"average_daily"`
	ReplyRate     float64 `json:"reply_rate"` // percentage
	// TargetProgress is actual sends divided by planned target volume for active days.
	TargetProgress float64 `json:"target_progress"`
	DaysActive     int     `json:"days_active"`
}

type WarmupDailyStats struct {
	Date           string `json:"date"` // YYYY-MM-DD
	EmailsSent     int    `json:"emails_sent"`
	EmailsReplied  int    `json:"emails_replied"`
	EmailsReceived int    `json:"emails_received"`
	TargetVolume   int    `json:"target_volume"`
	// Active is whether the mailbox had a warmup plan that day. A day with
	// arrivals but no plan still lists, but does not count as a day active.
	Active bool `json:"-"`
}

// Campaign Analytics

type CampaignAnalytics struct {
	CampaignID uuid.UUID            `json:"campaign_id"`
	Name       string               `json:"name"`
	Status     string               `json:"status"`
	DateRange  DateRange            `json:"date_range"`
	Summary    CampaignSummary      `json:"summary"`
	Sequences  []SequenceStats      `json:"steps"`
	DailyStats []CampaignDailyStats `json:"daily_stats,omitempty"`
	// Engagement is where and on what people opened and clicked, from the
	// per-event logs. Human events only.
	Engagement *CampaignEngagementBreakdown `json:"engagement,omitempty"`
}

// EngagementBucket is one slice of a breakdown: how many distinct contacts
// opened and clicked from that country, client, or device.
type EngagementBucket struct {
	Key    string `json:"key"`
	Opens  int    `json:"opens"`
	Clicks int    `json:"clicks"`
}

// CampaignEngagementBreakdown is the "where from, on what" view of a
// campaign's human opens and clicks. Buckets are ordered by activity, capped,
// and keyed by ISO country code, client or browser name, device type, and
// surface (device and app or webmail together). Unknown is the empty key.
type CampaignEngagementBreakdown struct {
	Countries []EngagementBucket `json:"countries"`
	Clients   []EngagementBucket `json:"clients"`
	Devices   []EngagementBucket `json:"devices"`
	Surfaces  []EngagementBucket `json:"surfaces"`
}

// Surface keys beyond the device types themselves and their `_app` forms
// (`mobile_app`, `desktop_app`, `tablet_app`).
const (
	// EngagementSurfaceHidden is a fetch by a mailbox provider's image
	// proxy, which hides the reader's device.
	EngagementSurfaceHidden  = "hidden"
	EngagementSurfaceWebmail = "webmail"
)

type CampaignSummary struct {
	TotalContacts int `json:"total_contacts"`
	EmailsSent    int `json:"emails_sent"`
	EmailsPending int `json:"emails_pending"`
	UniqueOpens   int `json:"unique_opens"`
	// MachineOpens is the subset of UniqueOpens from automated fetchers
	// (Apple MPP prefetch, UA-less clients). Human opens = unique - machine.
	MachineOpens int `json:"machine_opens"`
	UniqueClicks int `json:"unique_clicks"`
	// MachineClicks counts the contacts whose only clicks on a step came from
	// automated fetchers (security gateways walking the links). They are not
	// part of UniqueClicks, which only ever counts a person's click.
	MachineClicks int `json:"machine_clicks"`
	Replies       int `json:"replies"`
	Bounces       int `json:"bounces"`
	Unsubscribes  int `json:"unsubscribes"`

	OpenRate   float64 `json:"open_rate"`   // percentage
	ClickRate  float64 `json:"click_rate"`  // percentage
	ReplyRate  float64 `json:"reply_rate"`  // percentage
	BounceRate float64 `json:"bounce_rate"` // percentage
}

type SequenceStats struct {
	SequenceID uuid.UUID `json:"step_id"`
	Name       string    `json:"name"`
	// Position is 1-based among the campaign's email steps, in canvas order.
	Position   int `json:"position"`
	EmailsSent int `json:"emails_sent"`
	Opens      int `json:"opens"`
	// MachineOpens is the subset of Opens from automated fetchers, by the
	// same rule the summary uses. Human opens = Opens - MachineOpens.
	MachineOpens int `json:"machine_opens"`
	Clicks       int `json:"clicks"`
	// MachineClicks counts this step's contacts whose only clicks were
	// automated; they are not part of Clicks.
	MachineClicks int `json:"machine_clicks"`
	Replies       int `json:"replies"`
	Bounces       int `json:"bounces"`

	// Rates are percentages of this step's own EmailsSent, so steps that
	// reached different numbers of contacts still compare.
	OpenRate   float64 `json:"open_rate"`
	ClickRate  float64 `json:"click_rate"`
	ReplyRate  float64 `json:"reply_rate"`
	BounceRate float64 `json:"bounce_rate"`
}

type CampaignDailyStats struct {
	Date    string `json:"date"`
	Sent    int    `json:"sent"`
	Opens   int    `json:"opens"`
	Clicks  int    `json:"clicks"`
	Replies int    `json:"replies"`
}

// Email Account Status

type EmailAccountStatus struct {
	ID           uuid.UUID         `json:"id"`
	Email        string            `json:"email"`
	Provider     string            `json:"provider"`
	Status       string            `json:"status"`
	LastSyncedAt *time.Time        `json:"last_synced_at"`
	Health       AccountHealth     `json:"health"`
	Errors       []AccountError    `json:"errors"`
	DailyUsage   AccountDailyUsage `json:"daily_usage"`
	WarmupStatus *WarmupStatusInfo `json:"warmup_status,omitempty"`
	// WarmupHealth is the mailbox's warmup-pool reputation (spam placement,
	// complaints, throttle/quarantine state). Folded into Health.Score and
	// also exposed in detail here. Nil when the mailbox is not in a pool.
	WarmupHealth *WarmupHealthInfo `json:"warmup_health,omitempty"`
	// InCampaign reports whether the mailbox currently backs a live campaign.
	// When true a low-volume health-check warmup keeps running even if the
	// user has warmup paused/off.
	InCampaign bool `json:"in_campaign"`
	// SendLifecycle is whether the mailbox is in cold rotation, present only
	// when it is NOT: an active mailbox needs no explanation.
	SendLifecycle *SendLifecycleState `json:"send_lifecycle,omitempty"`
	// ColdRamp is the warmup-to-cold graduation ceiling, present only while it
	// is below the mailbox's own cap. Without it the cap just reads lower than
	// the number the owner configured.
	ColdRamp *ColdRampInfo `json:"cold_ramp,omitempty"`
}

// ColdRampInfo explains a cold cap held below the mailbox's configured limit.
type ColdRampInfo struct {
	// Ceiling is today's allowance; MailboxCap is what the owner configured.
	Ceiling    int `json:"ceiling"`
	MailboxCap int `json:"mailbox_cap"`
	// DaysToFullCap is how many clean days remain before Ceiling reaches
	// MailboxCap, 0 when it arrives today.
	DaysToFullCap int `json:"days_to_full_cap"`
	// Held is set when a recent spam placement is pausing the climb.
	Held bool `json:"held"`
}

type WarmupHealthInfo struct {
	State  string  `json:"state"` // healthy/watch/throttled/quarantined/blocked
	Score  float64 `json:"score"`
	Reason string  `json:"reason,omitempty"`
	// SpamScore is always 0. The accumulating score it reported was retired in
	// #491 because it tracked volume rather than misbehaviour; the key stays so
	// a published v1 client does not break, and goes at the next API version.
	// Read Score and Reason instead. Do not wire anything back into it.
	SpamScore    int        `json:"spam_score"`
	BlockedUntil *time.Time `json:"blocked_until,omitempty"`
	EvaluatedAt  *time.Time `json:"evaluated_at,omitempty"`
	// Partner diversity counts confirmed warmup deliveries over seven days.
	PartnerMailboxes7d     int `json:"partner_mailboxes_7d"`
	PartnerDomains7d       int `json:"partner_domains_7d"`
	PartnerOrganizations7d int `json:"partner_organizations_7d"`
	// The receiving side over the same window: verified warmup arrivals and
	// the distinct partners they came from.
	Received7d int `json:"received_7d"`
	Senders7d  int `json:"senders_7d"`
}

type AccountHealth struct {
	Status string   `json:"status"` // healthy, warning, error
	Score  int      `json:"score"`  // 0-100
	Issues []string `json:"issues,omitempty"`
}

type AccountError struct {
	ID             uuid.UUID `json:"id"`
	ErrorCode      string    `json:"error_code"`
	Severity       string    `json:"severity"`
	Title          string    `json:"title"`
	Message        string    `json:"message"`
	ActionRequired *string   `json:"action_required,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type AccountDailyUsage struct {
	Date          string `json:"date"`
	CampaignSent  int    `json:"campaign_sent"`
	CampaignLimit int    `json:"campaign_limit"`
	WarmupSent    int    `json:"warmup_sent,omitempty"`
	WarmupLimit   int    `json:"warmup_limit,omitempty"`
}

type WarmupStatusInfo struct {
	Enabled       bool       `json:"enabled"`
	Paused        bool       `json:"paused"`
	PausedAt      *time.Time `json:"paused_at,omitempty"`
	StartedAt     time.Time  `json:"started_at"`
	CurrentVolume int        `json:"current_volume"`
	TargetVolume  int        `json:"target_volume"`
	MaxVolume     int        `json:"max_volume"`
	// ReplyRate is the configured share of warmup sends that receive synthetic replies.
	ReplyRate  int `json:"reply_rate"`
	DaysActive int `json:"days_active"`
	// RampHold explains a ramp that is not climbing, so a target below the
	// plain ramp is never an unexplained drop.
	RampHold *WarmupRampHold `json:"ramp_hold,omitempty"`
}

// WarmupRampHold explains a ramp that is not climbing. Present for the whole
// freeze; VolumeCut says whether today's volume is also reduced, which lasts a
// shorter window.
type WarmupRampHold struct {
	// Placements and Sends cover the last 48 hours.
	Placements int  `json:"placements"`
	Sends      int  `json:"sends"`
	VolumeCut  bool `json:"volume_cut"`
	// ResumesAt is when the ramp climbs again if nothing else lands.
	ResumesAt time.Time `json:"resumes_at"`
}

// Usage Overview

type UsageOverview struct {
	UserID uuid.UUID `json:"user_id"`
	Period string    `json:"period"` // day, week, month

	EmailAccounts AccountsUsage  `json:"email_accounts"`
	Campaigns     CampaignsUsage `json:"campaigns"`
	Contacts      ContactsUsage  `json:"contacts"`
	API           APIUsage       `json:"api"`
}

type AccountsUsage struct {
	Total      int `json:"total"`
	Active     int `json:"active"`
	InWarmup   int `json:"in_warmup"`
	WithErrors int `json:"with_errors"`
}

type CampaignsUsage struct {
	Total  int `json:"total"`
	Active int `json:"active"`
	Paused int `json:"paused"`
	Draft  int `json:"draft"`
	// EmailsSent counts sent email steps inside UsageOverview.Period.
	EmailsSent int `json:"emails_sent"`
}

type ContactsUsage struct {
	Total      int `json:"total"`
	Subscribed int `json:"subscribed"`
	AddedToday int `json:"added_today"`
}

type APIUsage struct {
	TotalCalls   int             `json:"total_calls"`
	DailyLimit   int             `json:"daily_limit"`
	TopEndpoints []EndpointUsage `json:"top_endpoints"`
}

type EndpointUsage struct {
	Endpoint string `json:"endpoint"`
	Calls    int    `json:"calls"`
}

// Dashboard Analytics

// DashboardAnalytics is the main dashboard overview combining multiple stats
type DashboardAnalytics struct {
	Period         string                `json:"period"` // 7d, 30d, 90d
	OverallStats   DashboardOverallStats `json:"overall_stats"`
	RecentActivity []RecentActivityItem  `json:"recent_activity"`
	TopCampaigns   []TopCampaignStats    `json:"top_campaigns"`
	AccountHealth  AccountHealthSummary  `json:"account_health"`
	DailyTrend     []DashboardDailyStats `json:"daily_trend"`
	// CapacityToday is what the workspace's mailboxes can send today under
	// the scheduler's clamps; the sidebar meter's denominator. Absent when
	// it could not be computed.
	CapacityToday *WorkspaceSendCapacity `json:"capacity_today,omitempty"`
}

// DashboardOverallStats contains aggregate statistics for the dashboard
type DashboardOverallStats struct {
	TotalEmailsSent int `json:"total_emails_sent"`
	TotalOpens      int `json:"total_opens"`
	// MachineOpens is the subset of TotalOpens from automated fetchers.
	MachineOpens int `json:"machine_opens"`
	TotalClicks  int `json:"total_clicks"`
	// MachineClicks counts the contacts whose only clicks on a step came from
	// automated fetchers; they are not part of TotalClicks.
	MachineClicks   int     `json:"machine_clicks"`
	TotalReplies    int     `json:"total_replies"`
	TotalBounces    int     `json:"total_bounces"`
	OpenRate        float64 `json:"open_rate"`
	ClickRate       float64 `json:"click_rate"`
	ReplyRate       float64 `json:"reply_rate"`
	BounceRate      float64 `json:"bounce_rate"`
	ActiveCampaigns int     `json:"active_campaigns"`
	ActiveAccounts  int     `json:"active_accounts"`
}

// RecentActivityItem represents a single activity event
type RecentActivityItem struct {
	Type         string    `json:"type"` // opened, clicked, replied, bounced, sent
	CampaignID   uuid.UUID `json:"campaign_id"`
	CampaignName string    `json:"campaign_name"`
	ContactEmail string    `json:"contact_email"`
	ContactID    uuid.UUID `json:"contact_id,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
	Link         string    `json:"link,omitempty"` // For click events
	// Origin is the client, device and location of a person's open or
	// click, when it was logged per event.
	Origin *EngagementOrigin `json:"origin,omitempty"`
}

// TopCampaignStats represents performance stats for a top campaign
type TopCampaignStats struct {
	CampaignID uuid.UUID `json:"campaign_id"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	EmailsSent int       `json:"emails_sent"`
	OpenRate   float64   `json:"open_rate"`
	ClickRate  float64   `json:"click_rate"`
	ReplyRate  float64   `json:"reply_rate"`
}

// AccountHealthSummary provides a summary of all email account health
type AccountHealthSummary struct {
	TotalAccounts   int `json:"total_accounts"`
	HealthyAccounts int `json:"healthy_accounts"`
	WarningAccounts int `json:"warning_accounts"`
	ErrorAccounts   int `json:"error_accounts"`
}

// DashboardDailyStats represents daily statistics for trend charts
type DashboardDailyStats struct {
	Date    string `json:"date"` // YYYY-MM-DD
	Sent    int    `json:"sent"`
	Opens   int    `json:"opens"`
	Clicks  int    `json:"clicks"`
	Replies int    `json:"replies"`
}

// CampaignHourlyStats represents hourly statistics for a campaign
type CampaignHourlyStats struct {
	Hour    int `json:"hour"` // 0-23
	Sent    int `json:"sent"`
	Opens   int `json:"opens"`
	Clicks  int `json:"clicks"`
	Replies int `json:"replies"`
}

// CampaignComparison allows comparing multiple campaigns
type CampaignComparison struct {
	Campaigns []CampaignComparisonItem `json:"campaigns"`
	Period    DateRange                `json:"period"`
}

// CampaignComparisonItem represents a single campaign in a comparison
type CampaignComparisonItem struct {
	CampaignID uuid.UUID `json:"campaign_id"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	EmailsSent int       `json:"emails_sent"`
	OpenRate   float64   `json:"open_rate"`
	ClickRate  float64   `json:"click_rate"`
	ReplyRate  float64   `json:"reply_rate"`
	BounceRate float64   `json:"bounce_rate"`
}

// DirectMailAnalytics reports on mail written by hand rather than sent by a
// campaign. Two sources, deliberately, because they answer different questions
// and cover different sets of messages:
//
//   - Volume and replies come from the synced mailbox (unibox_emails), so they
//     cover everything the mailbox sent, including mail written in Gmail or on
//     a phone, and they cover history from before any of this shipped.
//   - Opens and clicks come from the send records (email_tasks), so they cover
//     only mail sent through Warmbly by a mailbox with tracking switched on,
//     and only from the moment it was switched on.
//
// Reporting them as one blended rate would be a lie, so they stay apart and the
// UI labels each for what it is.
type DirectMailAnalytics struct {
	Period      string                   `json:"period"`
	Volume      DirectMailVolume         `json:"volume"`
	Tracking    DirectMailTracking       `json:"tracking"`
	DailyTrend  []DirectMailDailyStats   `json:"daily_trend"`
	Mailboxes   []DirectMailMailboxStats `json:"mailboxes"`
	TopContacts []DirectMailContact      `json:"top_contacts"`
}

// DirectMailVolume is the "how much did we actually send and hear back" half,
// measured from the synced mailbox.
type DirectMailVolume struct {
	Sent     int `json:"sent"`
	Received int `json:"received"`
	// ThreadsStarted counts outbound threads whose first message was ours.
	ThreadsStarted int `json:"threads_started"`
	// Replied counts those that got an inbound message back.
	Replied   int     `json:"replied"`
	ReplyRate float64 `json:"reply_rate"`
	// Bounced counts the delivery failures that came back. Excluded from
	// Replied, and reported here because it is the most actionable number on
	// the page.
	Bounced int `json:"bounced"`
	// MedianReplyMinutes is how long the contact took to answer, across the
	// threads that were answered. Zero when nothing has been.
	MedianReplyMinutes int `json:"median_reply_minutes"`
}

// DirectMailTracking is the opt-in half. TrackedSent is the denominator for
// both rates: untracked sends are not failures to open, they are messages that
// were never asked.
type DirectMailTracking struct {
	// MailboxesOptedIn says how much of the picture this covers.
	MailboxesOptedIn int     `json:"mailboxes_opted_in"`
	MailboxesTotal   int     `json:"mailboxes_total"`
	TrackedSent      int     `json:"tracked_sent"`
	Opened           int     `json:"opened"`
	MachineOpened    int     `json:"machine_opened"`
	Clicked          int     `json:"clicked"`
	OpenRate         float64 `json:"open_rate"`
	ClickRate        float64 `json:"click_rate"`
}

type DirectMailDailyStats struct {
	Date     time.Time `json:"date"`
	Sent     int       `json:"sent"`
	Received int       `json:"received"`
}

type DirectMailMailboxStats struct {
	EmailAccountID  uuid.UUID `json:"email_account_id"`
	Email           string    `json:"email"`
	TrackDirectMail bool      `json:"track_direct_mail"`
	Sent            int       `json:"sent"`
	Received        int       `json:"received"`
}

// DirectMailContact is one correspondent, ranked by how much was sent to them.
type DirectMailContact struct {
	Email    string    `json:"email"`
	Sent     int       `json:"sent"`
	Received int       `json:"received"`
	LastAt   time.Time `json:"last_at"`
}
