package models

import (
	"time"

	"github.com/google/uuid"
)

// CampaignSendPlan is what one campaign will send today and why that number is
// what it is. It is derived on read through the scheduler's own gates and never
// stored: the same clamps that decide a real send decide these figures, so the
// plan cannot promise a volume the send path would refuse.
//
// The arithmetic is a waterfall that adds up: ConfiguredCeiling minus every
// Limits entry minus SentToday equals ExpectedRemaining.
type CampaignSendPlan struct {
	CampaignID uuid.UUID `json:"campaign_id"`
	Status     string    `json:"status"`
	// Day is the budget day the plan counts: every daily counter resets at
	// midnight UTC, whatever the campaign's timezone, so this is the UTC
	// date. The window's times are in the campaign's own timezone.
	Day        string    `json:"day"`
	Timezone   string    `json:"timezone"`
	ComputedAt time.Time `json:"computed_at"`

	// ConfiguredCeiling is the sum of the attached mailboxes' own daily caps:
	// the number the settings suggest before anything else is applied. A
	// mailbox that already sent past a cap lowered during the day counts
	// what it sent, so the waterfall still adds up.
	ConfiguredCeiling int `json:"configured_ceiling"`
	// Projected is today's total: what has gone out plus what is still
	// expected to.
	Projected int `json:"projected_today"`
	// SentToday is this campaign's sends so far today.
	SentToday int `json:"sent_today"`
	// ExpectedRemaining is what the pool can still send today for this
	// campaign after every limit, and after the leads that are actually due.
	ExpectedRemaining int `json:"expected_remaining"`
	// Bottleneck names the limit that decides Projected. One of the
	// SendLimit* constants, or "" when nothing binds below the ceiling.
	Bottleneck string `json:"bottleneck"`

	Limits    []CampaignSendLimit   `json:"limits"`
	Window    CampaignSendWindow    `json:"window"`
	Leads     CampaignLeadSupply    `json:"leads"`
	Mailboxes []CampaignMailboxPlan `json:"mailboxes"`
	// Organization is the workspace's plan-level daily allowance. Absent when
	// the plan is unlimited.
	Organization *CampaignOrgAllowance `json:"organization,omitempty"`
	// NextWakeAt is when the campaign's chain next runs. Nil when it has no
	// pending wakeup (not running, or being re-seeded).
	NextWakeAt *time.Time `json:"next_wake_at,omitempty"`
}

// CampaignSendLimit is one clamp in the waterfall and how many of today's
// emails it took off the ceiling. Only clamps that removed something are
// listed, in the order the scheduler applies them.
type CampaignSendLimit struct {
	Kind string `json:"kind"`
	// Emails is how many sends this clamp removed from today's total.
	Emails int `json:"emails"`
	// Mailboxes is how many of the pool's mailboxes it touched. Zero for a
	// campaign-level clamp.
	Mailboxes int `json:"mailboxes,omitempty"`
}

// Limit kinds, in waterfall order. The first five are the cap clamps the
// activity feed already names; keep the strings stable, the dashboard and the
// docs key on them.
const (
	SendLimitCampaignDailyLimit = "campaign_daily_limit"
	SendLimitCampaignRamp       = "campaign_ramp"
	SendLimitWarmupGraduation   = "warmup_graduation"
	SendLimitWorkspaceRisk      = "workspace_risk"
	SendLimitDomainAuth         = "domain_auth"
	SendLimitResting            = "resting"
	SendLimitHealthHold         = "warmup_health_hold"
	SendLimitOtherCampaigns     = "other_campaigns"
	SendLimitHealthPace         = "warmup_health_pace"
	SendLimitMailboxHours       = "mailbox_hours"
	SendLimitSpacing            = "spacing"
	SendLimitSendingWindow      = "sending_window"
	SendLimitNotRunning         = "not_running"
	SendLimitOrgDailyLimit      = "org_daily_limit"
	SendLimitNewLeadCap         = "new_lead_cap"
	SendLimitLeads              = "leads"
)

// CampaignSendWindow is the campaign's calendar for today.
type CampaignSendWindow struct {
	// SendingDay is false when the schedule has no window today.
	SendingDay bool `json:"sending_day"`
	OpenNow    bool `json:"open_now"`
	// OpensAt is the next opening when the window is closed now; it may be on
	// a later day.
	OpensAt *time.Time `json:"opens_at,omitempty"`
	// ClosesAt is the end of the last window today, when there is one.
	ClosesAt *time.Time `json:"closes_at,omitempty"`
	// MinutesLeft is the sending time still ahead today.
	MinutesLeft int `json:"minutes_left"`
	// StartsAt is the campaign start date when it is still ahead.
	StartsAt *time.Time `json:"starts_at,omitempty"`
	// EndsAt is the campaign end date, when one is set.
	EndsAt *time.Time `json:"ends_at,omitempty"`
}

// CampaignLeadSupply is the other half of the number: mailboxes can only send
// to leads whose step is due. Counted through the campaign's own routing.
type CampaignLeadSupply struct {
	// DueNow is the email steps that could go this minute.
	DueNow int `json:"due_now"`
	// DueLaterToday is the email steps whose wait elapses before the day ends.
	DueLaterToday int `json:"due_later_today"`
	// NewLeadsDueToday is how many of DueNow plus DueLaterToday are first
	// emails, which the new-lead cap governs.
	NewLeadsDueToday int `json:"new_leads_due_today"`
	// WaitingOnStep is the leads whose next step is due after today.
	WaitingOnStep int `json:"waiting_on_step"`
	// WaitingOnCondition is the leads inside an undecided branch window.
	WaitingOnCondition int `json:"waiting_on_condition"`
	// Held is the leads paused (out of office, or by hand).
	Held int `json:"held"`
	// WaitingOnSender is the due steps whose own mailbox has nothing left
	// today. Each contact keeps the address they first heard from, so these
	// wait for it rather than going out from another mailbox.
	WaitingOnSender int `json:"waiting_on_sender"`
	// NewLeadsStartedToday and MaxNewLeadsPerDay are the new-lead throttle;
	// the cap is 0 when unlimited.
	NewLeadsStartedToday int `json:"new_leads_started_today"`
	MaxNewLeadsPerDay    int `json:"max_new_leads_per_day"`
	// NextDueAt is the soonest moment a waiting lead becomes due, when
	// nothing is due right now.
	NextDueAt *time.Time `json:"next_due_at,omitempty"`
}

// CampaignMailboxPlan is one mailbox's day on this campaign.
type CampaignMailboxPlan struct {
	ID       uuid.UUID `json:"id"`
	Email    string    `json:"email"`
	Provider string    `json:"provider"`
	// ConfiguredCap is the mailbox's own daily cold cap.
	ConfiguredCap int `json:"configured_cap"`
	// CapToday is the cap this campaign gives it today, after the cap clamps.
	CapToday int `json:"cap_today"`
	// LimitedBy names the clamp that set CapToday: mailbox_daily_cap,
	// campaign_daily_limit, campaign_ramp, warmup_graduation or
	// workspace_risk.
	LimitedBy string `json:"limited_by"`
	// SentToday is this campaign's sends from the mailbox today;
	// SentByOtherCampaigns is what other campaigns took from the same cap.
	SentToday            int `json:"sent_today"`
	SentByOtherCampaigns int `json:"sent_by_other_campaigns"`
	// ExpectedRemaining is what this mailbox is expected to still send today
	// for this campaign.
	ExpectedRemaining int `json:"expected_remaining"`
	// State is why the mailbox is or is not sending right now. One of
	// "sending", "budget_spent", "hours_closed", "no_working_day",
	// "domain_auth", "resting", "health_hold".
	State string `json:"state"`
	// ReopensAt is when a mailbox outside its own hours is next open.
	ReopensAt *time.Time `json:"reopens_at,omitempty"`
	// Health is the warmup health band when it is not healthy.
	Health string `json:"health,omitempty"`
	// MinGapSeconds is the spacing between two of its sends, which warmup
	// mail shares.
	MinGapSeconds int `json:"min_gap_seconds"`
	// Graduation is set while the warmup graduation ceiling holds the mailbox
	// below its own cap.
	Graduation *ColdRampInfo `json:"graduation,omitempty"`
}

// Mailbox states in a send plan.
const (
	MailboxPlanSending      = "sending"
	MailboxPlanBudgetSpent  = "budget_spent"
	MailboxPlanHoursClosed  = "hours_closed"
	MailboxPlanNoWorkingDay = "no_working_day"
	MailboxPlanDomainAuth   = "domain_auth"
	MailboxPlanResting      = "resting"
	MailboxPlanHealthHold   = "health_hold"
	MailboxPlanWindowClosed = "window_closed"
	// MailboxPlanNoWorker is a mailbox no heartbeating worker holds right now.
	MailboxPlanNoWorker = "no_worker"
)

// SendBottleneckBudgetSpent is the Bottleneck of a campaign that has sent
// everything its mailboxes had today: no limit binds, the day is simply used.
const SendBottleneckBudgetSpent = "budget_spent"

// SendLimitSendingBehavior is a mailbox's rolled sending-behaviour plan
// lowering its day below the cold cap.
const SendLimitSendingBehavior = "sending_behavior"

// CampaignOrgAllowance is the workspace's plan-level daily campaign limit.
type CampaignOrgAllowance struct {
	DailyLimit int `json:"daily_limit"`
	SentToday  int `json:"sent_today"`
	Remaining  int `json:"remaining"`
}

// WorkspaceSendCapacity is what a workspace's mailboxes can send today between
// them, under the same clamps a campaign's plan applies. The dashboard's
// "sent today" meter reads its denominator from here.
type WorkspaceSendCapacity struct {
	// Capacity is today's cold sends across every mailbox that can send;
	// Remaining is what is left of it after what has already gone out.
	Capacity  int `json:"capacity"`
	Remaining int `json:"remaining_today"`
	// ConfiguredCeiling is the same mailboxes' own caps added up.
	ConfiguredCeiling int `json:"configured_ceiling"`
	// Mailboxes is how many mailboxes contribute; Held is how many are
	// attached but cannot send today.
	Mailboxes int `json:"mailboxes"`
	Held      int `json:"held"`
}
