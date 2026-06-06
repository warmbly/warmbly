package config

const (
	DefaultColor = "#c4c8cf"
	Domain       = "warmbly.com"
	LimitMin     = 10
	LimitMax     = 200

	CampaignLimitDefault  = 50
	MinWaitTimeDefault    = 600
	WarmupBaseDefault     = 10
	WarmupMaxDefault      = 40
	WarmupIncreaseDefault = 1

	// Net-new campaign send controls. The ramp mirrors the warmup ramp shape
	// (start, +increment/day, ceiling) but is applied only via min() against
	// the per-mailbox cold cap, so it can only lower effective volume.
	CampaignSenderWeightDefault  = 1
	CampaignSenderWeightMax      = 100
	CampaignRampStartDefault     = 10
	CampaignRampIncrementDefault = 5
	CampaignRampCeilingDefault   = 50
	CampaignMaxNewLeadsMax       = 1000

	MaxContactSize   = 10240
	MaxEmailBodySize = 200 * 1024 // 200 KB
	MaxEmailFolders  = 30

	// Sequences. Empty by default so the editor shows a smart, position-based
	// label (e.g. "Email 1") until the user names the step themselves.
	SequenceDefaultName  = ""
	SequenceSubjectLimit = 100
	SequenceBodyLimit    = 30_000
	// SequenceWaitAfterMax bounds a step's per-step delay (in days). Mirrors the
	// editor's 0–60 day cap so an API caller can't persist an absurd or negative
	// delay that the scheduler would then turn into an unreachable send time.
	SequenceWaitAfterMax = 60

	// Webhook/integration fan-out throttle. Caps how many events of a single
	// type one org can fan out to its webhooks + integration sinks
	// (Slack/Discord/CRM) per minute — the backstop against a campaign "notify"
	// action, or any per-contact event, flooding a customer's endpoints. Over
	// the cap, further events of that type in the same minute are dropped
	// (logged), not queued.
	//
	// The effective cap is PLAN-BASED: it scales with the org's resolved mailbox
	// allowance (override > plan > hard cap), so bigger plans get more webhook
	// throughput. These three knobs are "what we centrally allow":
	//
	//   - Base: a generous floor every org gets, including free/no-plan orgs, so
	//     normal usage never trips the throttle (good UX by default).
	//   - PerMailbox: how much each mailbox in the plan's allowance adds, since
	//     webhook volume tracks sending activity.
	//   - Max: a hard ceiling so even an "unlimited" plan stays bounded.
	//
	// Sized far above normally-spaced sending (per-mailbox daily caps + min-gap
	// spacing); only a runaway loop or a huge per-contact fan-out approaches it.
	WebhookDispatchBasePerMinute       = 600  // generous floor for any org (10/s)
	WebhookDispatchPerMailboxPerMinute = 30   // added per mailbox the plan allows
	WebhookDispatchMaxPerMinute        = 6000 // hard ceiling (100/s) for any plan

	// Unibox
	UniboxLimitMin     = 1
	UniboxLimitMax     = 100
	UniboxLimitDefault = 50

	// WarmupVerifyHeader is the custom header carrying the warmup
	// verification token on outbound warmup mail. The name is intentionally
	// generic (not "X-Warmbly-*") so anti-spam vendors cannot trivially
	// cluster on the header name to fingerprint warmup traffic.
	WarmupVerifyHeader = "X-Mailtrace-Verify"

	// Product-level hard caps. These are the backstop for plans that
	// advertise "unlimited" — marketing can keep saying unlimited, but
	// the runtime never grants truly unbounded usage. Each cap is the
	// floor that GetEffectiveLimits falls back to when both the
	// per-org override and the plan column are unset.
	//
	// Admins can grant strictly larger caps per-org through the
	// override flow when there is a legitimate business reason. Growth
	// above these defaults goes through the limit-increase request
	// workflow so the decision is audited and the org has a paper trail
	// acknowledging the new ceiling.
	//
	// These numbers are deliberately generous enough that ordinary use
	// never trips them, and conservative enough that "I want to spin up
	// 5,000 mailboxes overnight" can't happen without explicit approval.
	HardCapMailboxes          = 200       // total connected mailboxes per org
	HardCapCampaignsTotal     = 500       // total campaigns ever created
	HardCapCampaignsActive    = 50        // simultaneously active campaigns
	HardCapTeamMembers        = 100       // seats per org
	HardCapContacts           = 1_000_000 // contacts per org
	HardCapDailyCampaignSends = 1000      // campaign emails per org per day

	// Daily creation throttles. The total caps above stop "you have
	// 5000 campaigns on this org" — the throttles below stop "you
	// created 1000 campaigns today on a fresh unlimited account."
	// Different shape: a per-(org, resource, day) Redis counter that
	// resets at UTC midnight, decoupled from any plan tier.
	//
	// These are creation-rate ceilings, not total caps; raising them
	// per-org is intentionally not exposed in the override editor
	// because the per-day shape protects abuse posture rather than
	// product utility.
	DailyThrottleNewCampaigns = 20 // new campaigns per org per day
	DailyThrottleNewMailboxes = 5  // newly connected mailboxes per org per day
	DailyThrottleNewOrgs      = 3  // new workspaces per owner per day

	// DailyThrottleNewScheduledSends caps how many NEW scheduled-send
	// schedules a single user can create in a rolling 24h window. The
	// real defense against burst abuse — someone writing a loop that
	// queues thousands of scheduled sends in seconds. Set high enough
	// that no human-driven volume comes close (a power user replying
	// to 200 inbound messages a day couldn't hit it organically).
	DailyThrottleNewScheduledSends = 1000

	// MaxPendingScheduledSendsPerUser caps how many pending scheduled
	// email sends one user can have queued at once. The DAILY rate
	// (DailyThrottleNewScheduledSends) is the primary abuse defense;
	// this is the DB-bloat defense — each pending row carries a body
	// (~5KB), so capping pending count keeps total scheduled-queue
	// storage bounded per user.
	//
	// 10,000 is generous: a user scheduling 100 sends/day for the next
	// 100 days hits this exactly once. The combination of "1K new/day"
	// + "10K total pending" means a legitimate user cannot organically
	// hit either, while a scripted attacker is bounded on both axes.
	//
	// Cloud Tasks cost is negligible at this size — at $0.40/M
	// operations, 10K pending = 20K ops = $0.008/user even at the
	// hardest abuse. The cap exists for DB sanity, not cost.
	//
	// Future: per-plan ceiling lookup. Today: single backstop.
	MaxPendingScheduledSendsPerUser = 10000
)
