package config

import "time"

const (
	DefaultColor = "#c4c8cf"
	// LimitMin/LimitMax bound every per-mailbox and per-campaign daily send
	// cap the API will store. 5000 covers real provider ceilings (Google
	// Workspace 2000/day, M365 10000 recipients/day); the safe cold band
	// stays 30-50/day and is steered by defaults, warnings and the advisor.
	LimitMin = 0
	LimitMax = 5000

	CampaignDailyLimitMin = 3

	// SignatureHTMLMax/SignaturePlainMax bound a stored mailbox signature.
	// The old ceiling was 1000 characters for both, which a real signature
	// exceeds the moment it carries a logo or a table: Gmail itself allows
	// 10,000, so an imported one had nowhere to go.
	SignatureHTMLMax  = 20000
	SignaturePlainMax = 10000

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

	MaxContactSize = 10240
	// MaxEmailBodySize bounds a single stored message body. 200 KB cut real
	// HTML newsletters mid-document; 512 KB clears the overwhelming majority
	// of them while still bounding what one message can cost.
	MaxEmailBodySize = 512 * 1024 // 512 KB
	// MaxEmailFolders bounds how many folders one mailbox's sync follows.
	// INBOX and the special folders are always kept; past the cap the rest
	// are taken in the server's order and the overflow is relayed once as a
	// warning rather than failing the mailbox.
	MaxEmailFolders = 100

	// MaxSearchBodyText bounds the plain-text copy of a message body kept in
	// Postgres for full-text search. The body itself lives in object storage;
	// this only has to be long enough to find a message by what it says.
	MaxSearchBodyText = 16 * 1024 // 16 KB

	// ImapFetchBatchSize bounds how many messages one IMAP sync window holds in
	// memory, so a large folder is never buffered whole before any body is read.
	ImapFetchBatchSize = 200

	// ImapCommandIdleTimeout is how long an IMAP command may wait for the
	// server to say anything before the session is declared dead and
	// re-dialed on the next pass. go-imap bounds the bytes of a response but
	// not the wait for its first byte, which is where a peer that vanished
	// without a FIN parks a command forever.
	ImapCommandIdleTimeout = 2 * time.Minute

	// Servers without CONDSTORE (Outlook.com, Microsoft 365 over IMAP,
	// Yahoo, many hosted servers) cannot say which messages changed, so read
	// state and flags are mirrored by re-reading the flags of a folder's
	// newest window every ImapFlagScanInterval and diffing against the
	// previous scan. New mail still lands within one pass through UIDNEXT.
	ImapFlagScanInterval = 10 * time.Minute
	ImapFlagScanWindow   = 5_000 // newest UIDs per folder the scan covers

	// Mailbox sync fair use. Connecting a mailbox imports its recent history
	// (the backfill), then follows new mail (live). Every number below is a
	// default: the four Sync* settings are operator-editable in the admin
	// panel's instance settings, the rest are fixed pacing constants for the
	// worker-side governor (internal/app/worker/wmail/governor.go).
	//
	// The governor defers rather than drops: mail over budget waits for the
	// next window with its cursor held, and only a flood or repeated daily
	// overage deactivates the mailbox. Replies to this mailbox's own sends
	// ride a separate priority lane so an outreach reply is never starved by
	// a bulky inbox.
	SyncBackfillDaysDefault         = 90    // how far back the initial import reaches
	SyncBackfillDaysMax             = 730   // longest window an operator may set
	SyncBackfillMessagesDefault     = 5_000 // most messages the initial import stores per mailbox
	SyncBackfillMessagesMax         = 100_000
	SyncDailyMessagesMailboxDefault = 2_000 // new (live) messages stored per mailbox per UTC day
	SyncDailyMessagesMailboxMax     = 100_000
	SyncDailyMessagesOrgDefault     = 25_000 // new + backfilled messages stored per organization per UTC day
	SyncDailyMessagesOrgMax         = 2_000_000
	SyncBurstPer5Min                = 300   // live messages one mailbox may store in any 5 minute window
	SyncHourlyPerMailbox            = 1_000 // live messages one mailbox may store in any clock hour
	SyncBackfillPerMinute           = 240   // backfill pacing per mailbox
	SyncFloodPerHour                = 5_000 // new live messages observed in one hour that mark a mailbox as flooding
	SyncThrottleEscalationDays      = 3     // throttled UTC days out of the last 7 that deactivate a mailbox
	SyncSkipFoldersMax              = 50    // folders one mailbox may exclude from sync
	SyncSkipFolderNameMax           = 255   // characters in one excluded folder name

	// Forms. Funnel events feed analytics ranges up to 90 days, so the default
	// window keeps double coverage. Operator-editable under Instance settings.
	FormEventsRetentionDaysDefault = 180

	// Inbox placement tests. The monthly allowance covers tests on the
	// instance and cloud seed panels only; a workspace's own seeds cost the
	// operator nothing. Operator-editable under Instance settings.
	PlacementTestsPerMonthTrialDefault = 3
	PlacementTestsPerMonthPaidDefault  = 40
	PlacementTestsPerMonthMax          = 100_000
	PlacementSeedsPerTestDefault       = 20
	PlacementSeedsPerTestMax           = 100
	PlacementSeedsPerTestMin           = 5  // below this a test is noise, so it is refused rather than shrunk
	PlacementSpacingSecondsDefault     = 60 // gap between two probes from one sender
	PlacementSpacingSecondsMin         = 5
	PlacementSpacingSecondsMax         = 600
	PlacementClassifyTimeoutMinutes    = 120 // after the send, a copy not seen in the seed is missing
	PlacementRunningPerOrgMax          = 3   // tests one workspace may have in flight at once
	PlacementSeedsPerWorkspaceMax      = 50  // a workspace's own seed mailboxes
	PlacementSeedDailySyncMessages     = 50_000
	PlacementMonitorIntervalDaysMin    = 1
	PlacementMonitorIntervalDaysMax    = 30
	PlacementMonitorIntervalDaysDef    = 7
	PlacementMonitorAlertBelowDefault  = 70 // inbox rate, percent

	// Sequences. Empty by default so the editor shows a smart, position-based
	// label (e.g. "Email 1") until the user names the step themselves.
	SequenceDefaultName  = ""
	SequenceSubjectLimit = 100
	SequenceBodyLimit    = 30_000
	// SequenceWaitAfterMax bounds a step's per-step delay (in days). Mirrors the
	// editor's 0–60 day cap so an API caller can't persist an absurd or negative
	// delay that the scheduler would then turn into an unreachable send time.
	SequenceWaitAfterMax = 60

	// CampaignSendMaxAttempts is how many times one (contact, step) may be
	// handed to a worker before the lead is marked failed and dropped from the
	// campaign. A worker-reported failure clears the step's sent_at so the next
	// tick retries it; this bounds that loop for a mailbox that can never send.
	CampaignSendMaxAttempts = 5

	// CampaignNotDueGraceSeconds is how far in the future a step's hard
	// constraints (wait_after, start date, sending window, mailbox min-gap)
	// may sit while a firing task still sends it. Beyond this the scheduler
	// reports the step deferred so the task reschedules instead of sending a
	// follow-up early; a task that fired on time always passes.
	CampaignNotDueGraceSeconds = 60

	// CampaignPlacementCandidates is how many due leads one scheduling pass
	// routes and tries to place before it gives up and defers the campaign.
	// Placement can refuse a single lead for a reason that is entirely that
	// lead's (ESP-strict has no mailbox for their provider, their own mailbox
	// is busy, their preferred hours are hours away); the leads behind them are
	// still sendable, so the pass moves on instead of parking the campaign on
	// the first refusal (issue #437). The leads at the head of the queue are
	// the same on every pass, so this reaches well past them; it is bounded
	// because a campaign whose every lead is refused must still end the pass
	// rather than walk a million-row list.
	CampaignPlacementCandidates = 200

	// WarmupReputationLedgerDays is how long the standing of a removed mailbox
	// is held against its address, counted from the later of its removal and
	// the end of its block. Long enough that removing and re-adding a mailbox
	// is never a shortcut past a block, short enough that the address of a
	// mailbox nobody re-added is not kept indefinitely. Fixed, not a setting.
	WarmupReputationLedgerDays = 90

	// CampaignMaxDeferMinutes bounds how far ahead a DEFERRED campaign tick may
	// park its successor. A deferral means "nothing is sendable right now", and
	// the reasons it says that (no lead is due, the new-lead cap is spent, no
	// same-provider mailbox) all change from outside the chain: leads get
	// imported, a reply routes a contact onto a live branch, a mailbox comes
	// back under budget. A campaign is a single self-perpetuating task, so a
	// park at the literal next-due time (days out for a "wait 3 days" step) is
	// also the next time anything re-reads that state — which is how a campaign
	// with freshly imported leads sits at "Queued / Not started" for days.
	// Re-checking on this horizon costs one scheduling pass per idle campaign
	// per interval and bounds that staleness. It applies ONLY to deferrals: a
	// tick that actually sent parks its successor at the paced interval, which
	// is the send spacing and must not be shortened.
	CampaignMaxDeferMinutes = 15

	// CampaignTickRetrySeconds is how far a campaign pass that failed is moved
	// before it runs again, and how soon the next pass follows one that could
	// not hand its send to a worker. The in-process dispatcher fires a due
	// task every second, so a retry kept at its old slot would run in a loop.
	CampaignTickRetrySeconds = 60

	// CampaignStaleParkHours is when the reconciler starts distrusting a parked
	// wakeup. Even-distribution can only push a successor to the end of the
	// mailbox's current day, so a pending tick further out than this was parked
	// by a deferral (including ones written before deferrals were capped) and is
	// re-checked against the campaign's real next-due time.
	CampaignStaleParkHours = 24

	// CampaignReparkMarginMinutes is how much earlier the recomputed slot must
	// be before the reconciler moves a stale park. Slot selection carries
	// jitter, so without a margin a campaign whose window genuinely is days out
	// (a Monday-only schedule) would be re-parked a few minutes earlier on every
	// pass forever.
	CampaignReparkMarginMinutes = 60

	// CampaignSendReclaimAfterMinutes is how long a reserved-but-unresolved send
	// (dispatched_at set, no worker result, no sent_at) is left alone before the
	// reclaimer treats its outcome as lost and walks it back as a failed
	// attempt. A live worker answers every SEND_EMAIL within seconds, so this
	// only ever fires when the worker died mid-send or the result was lost, and
	// it must stay well clear of a slow provider handshake.
	CampaignSendReclaimAfterMinutes = 30

	// TrackingMachineWindowOpenSecondsDefault and
	// TrackingMachineWindowClickSecondsDefault are how soon after a step was
	// dispatched an open or a click is treated as automated rather than a
	// person. Operator-editable under Instance settings.
	//
	// The clock starts when the send is handed to the worker, NOT when the
	// recipient's server received it, so the window has to absorb the worker's
	// SMTP handshake, the sending provider's outbound queue and the transit to
	// the recipient's MX before the gateway that scans on arrival even starts.
	// That is why these are not the "no human could read this fast" numbers
	// they look like: against this anchor, ten seconds routinely expired before
	// the scan it was meant to catch.
	//
	// Opens get the longer window. The two failure modes are not symmetric: a
	// misjudged open costs a metric and an open-triggered branch, while a
	// misjudged click costs an interested lead the automation behind it, and
	// clicks have the scanner-network catalogue covering them as well.
	TrackingMachineWindowOpenSecondsDefault  = 60
	TrackingMachineWindowClickSecondsDefault = 30

	// Bounds on both windows. One second is the floor rather than zero because
	// it is effectively "off" while still keeping the rule's shape, and 15
	// minutes is past any plausible delivery lag.
	TrackingMachineWindowSecondsMin = 1
	TrackingMachineWindowSecondsMax = 900

	// TrackingMachineWindowProbableSecondsDefault is the window used instead
	// of the two above when the tracking edge recognised the source as a
	// scanner network that ALSO carries people's own requests: Proofpoint
	// Isolation and Mimecast Browser Isolation render a clicked page in the
	// vendor's own cloud, so the request may have a person behind it.
	//
	// Such a match cannot settle the verdict, but it moves the odds a long
	// way, which is what buys the wider window: inside it the event is the
	// delivery-time scan, outside it the person who got to the mail later. Ten
	// minutes covers a gateway scanning on arrival behind a slow provider
	// queue while leaving all but the fastest recipients on the human side.
	//
	// Never shorter than the per-kind window in effect: the classifier takes
	// the wider of the two, so this setting can only ever catch more.
	TrackingMachineWindowProbableSecondsDefault = 600

	// Bounds on that window. It reaches a day because how long a vendor takes
	// to detonate a link is the vendor's property, not the instance's, and an
	// operator whose recipients sit almost entirely behind one may want the
	// whole of it.
	TrackingMachineWindowProbableSecondsMin = 1
	TrackingMachineWindowProbableSecondsMax = 86400

	// TrackingClickBurstSeconds is the window inside which clicks on two
	// different links of the same email from the same source are treated as
	// a scanner walking the message. A person follows one link at a time.
	TrackingClickBurstSeconds = 5

	// EngagementEventRetentionDaysDefault is how long the per-event open and
	// click logs (client, device, location) are kept. The summary on the
	// progress row outlives them, so counts and routing never change.
	// Operator-editable under Instance settings.
	EngagementEventRetentionDaysDefault = 365

	// AuditLogRetentionDaysDefault is how long the audit trail is kept. The
	// trail carries IP addresses, user agents and change payloads, so this
	// window is also how long that PII is held; a privacy-conscious operator
	// shortens it under Instance settings.
	AuditLogRetentionDaysDefault = 90

	// Ten years is the ceiling every retention window shares. It is not a
	// recommendation: it is the point past which "keep it" and "keep it
	// forever" stop differing, and it bounds a typo. One day is the floor, so
	// there is always a window in which an event can be read.
	RetentionDaysMin = 1
	RetentionDaysMax = 3650

	// Warmup mail is real mail in the customer's mailbox, and nothing about
	// it is worth keeping once its engagement has been recorded. The platform
	// deletes it from the warmup folder after this many days (per mailbox
	// override in email_accounts.warmup_retention_days), so a mailbox on a
	// fixed quota never fills up with it and the deletion is the platform's
	// own. The floor leaves room for the delayed engagement legs and for a
	// reply-back in the thread to finish before its opener goes.
	WarmupMailRetentionDaysDefault = 30
	WarmupMailRetentionDaysMin     = 3

	// WarmupEventRetentionDaysDefault is how long the per-message warmup
	// records (tokens, receipts, tampering and spam reports) are kept. The
	// health bands read at most thirty days, which is the floor; the daily
	// warmup_statistics rows carry the analytics and are never pruned.
	WarmupEventRetentionDaysDefault = 365
	WarmupEventRetentionDaysMin     = 30

	// WarmupDeletionStrikeHours is how soon after arrival a deletion of a
	// warmup email still costs the pool its engagement and so counts as
	// tampering. Later on it is housekeeping: the platform was going to delete
	// it anyway, and a mailbox owner tidying a folder, a provider purging its
	// Trash or a server retention rule must not read as harm.
	WarmupDeletionStrikeHours = 24

	// CampaignSendStampAttempts is how many times the control plane retries the
	// sent_at stamp after a send is already on the bus. The reservation is what
	// keeps the step from being re-sent, so a lost stamp is a pacing problem,
	// not a duplicate — but it is still worth a couple of quick retries before
	// falling back to the worker result to repair it.
	CampaignSendStampAttempts = 3

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

	// VerificationRecheckDays is how long a verification verdict is trusted
	// before the address is checked again. Mailboxes get created and closed;
	// a verdict from last quarter is a guess.
	VerificationRecheckDays = 90
	// VerificationUnknownRecheckDays is the shorter shelf life of an
	// inconclusive verdict (greylisted, timeout, undisclosing provider).
	VerificationUnknownRecheckDays = 30
	// VerificationEvidenceFreshDays is how long real mail to an address (a
	// delivery, an open, a reply) excuses it from being re-checked.
	VerificationEvidenceFreshDays = 180
	// VerificationDeliveryWindowHours is how long after a send with no
	// bounce the delivery counts as evidence the mailbox exists.
	VerificationDeliveryWindowHours = 72
	// VerificationBatchSize is how many contacts one scheduler pass checks.
	VerificationBatchSize = 200
	// VerificationIntervalSeconds is how often the scheduler passes. A pass
	// that finds a full batch runs again immediately, so a large import drains
	// at the verifier's speed rather than one batch per interval.
	VerificationIntervalSeconds = 60
	// VerificationProbeConcurrency bounds parallel in-house SMTP probes.
	VerificationProbeConcurrency = 4
	// VerificationProviderConcurrency bounds parallel paid-provider lookups.
	VerificationProviderConcurrency = 8
	// VerificationExhaustedCooldownMinutes is how long an out-of-allowance
	// account is left alone when its provider publishes no balance endpoint.
	// Nothing but a billable check can tell such an account has been topped up,
	// so retrying sooner only re-runs the batch that found it empty.
	VerificationExhaustedCooldownMinutes = 15
	// VerificationBreakerWindow and VerificationBreakerInvalidPct are the
	// in-house probe's self-check: when this share of the last window of
	// probe verdicts is "invalid", the probe itself is suspect (issue #200,
	// #264) and its invalid verdicts are filed as unknown for
	// VerificationBreakerCooldownMinutes.
	VerificationBreakerWindow          = 200
	VerificationBreakerInvalidPct      = 40.0
	VerificationBreakerCooldownMinutes = 60

	// WarmupVerifyHeader is the custom header carrying the warmup
	// verification token on outbound warmup mail. The name is intentionally
	// generic (not "X-Warmbly-*") so anti-spam vendors cannot trivially
	// cluster on the header name to fingerprint warmup traffic.
	WarmupVerifyHeader = "X-Mailtrace-Verify"

	// WarmupFolderDefault is the folder (a label on Gmail) warmup mail is
	// filed into when a mailbox has not named its own. Both directions land
	// there — the copy a mailbox received and the copy of what it sent — so
	// the folder is what identifies warmup traffic in a real mail client.
	WarmupFolderDefault = "Warmbly"
	// WarmupFolderMaxLen bounds a customer-supplied folder name. It becomes a
	// mailbox name on the provider, so it is short and single-level.
	WarmupFolderMaxLen = 64

	// Product-level hard caps. These are the backstop for plans that
	// advertise "unlimited" on campaigns, seats, contacts and daily sends.
	// Each cap is the floor that GetEffectiveLimits falls back to when both
	// the per-org override and the plan column are unset.
	//
	// Admins can grant strictly larger caps per-org through the
	// override flow when there is a legitimate business reason. Growth
	// above these defaults goes through the limit-increase request
	// workflow so the decision is audited and the org has a paper trail
	// acknowledging the new ceiling.
	//
	// These numbers are deliberately generous enough that ordinary use
	// never trips them. Mailboxes are not in this list: see
	// FairUseSendsPerMailbox below.
	HardCapCampaignsTotal     = 500       // total campaigns ever created
	HardCapCampaignsActive    = 50        // simultaneously active campaigns
	HardCapTeamMembers        = 100       // seats per org
	HardCapContacts           = 1_000_000 // contacts per org
	HardCapDailyCampaignSends = 1000      // campaign emails per org per day

	// Mailboxes have no hard cap. A paid workspace's allowance is fair use
	// derived from the daily sends its plan includes: one mailbox for every
	// FairUseSendsPerMailbox sends a day. At 1 a 15,000/day plan holds 15,000
	// mailboxes, one per daily send, which is deliberately far more than safe
	// sending ever needs: the allowance must never be the reason a customer
	// runs a mailbox hotter. A plan with no daily send cap holds unlimited
	// mailboxes, and an approved limit-increase request raises the allowance
	// for one workspace.
	FairUseSendsPerMailbox = 1

	// Bulk connect: how many SMTP/IMAP rows one request may carry, and how
	// many of them are validated against a worker at the same time. The
	// dashboard streams a CSV through batches of this size so a 3,000 row
	// file shows live progress instead of one request that times out.
	MailboxBulkBatchMax    = 50
	MailboxBulkConcurrency = 8

	// Mailbox import (background job): rows per file, upload size, how many
	// rows connect at once overall and per mail host (a burst of sign-ins to
	// one provider from one address earns an auth throttle), how long a row
	// may run before another pass takes it back, how long failed rows keep
	// their sealed credentials for a retry, and when a finished import goes.
	MailboxImportMaxRows            = 5000
	MailboxImportMaxBytes           = 10 << 20
	MailboxImportConcurrency        = 8
	MailboxImportPerHostConcurrency = 3
	MailboxImportLeaseSeconds       = 120
	MailboxImportCredentialDays     = 7
	MailboxImportRetentionDays      = 30

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

	// Pool link: mailboxes a self-hosted instance may enroll in the hosted
	// warmup pool without a paid pool plan, and the handshake lifetimes.
	PoolLinkCodeTTLMinutes       = 15
	PoolLinkPollIntervalSeconds  = 3
	PoolLinkPlanID               = "00000000-0000-0000-0000-000000000002"
	PoolLinkPlanPriceUSD         = 15
	WarmupPoolTierFallbackFloor  = 25 // below this many recipients outside its workspace (or its warmup max, if higher), a premium sender borrows that many proven free mailboxes
	WarmupPoolBorrowSeasonedDays = 14 // a borrowed free mailbox this long in the pool ranks ahead of a newer one
	WarmupPoolFallbackMinAgeDays = 3  // a free mailbox must have been a pool member this long before premium may borrow it, or write back to one
	WarmupPoolReturnVisitDays    = 14 // a proven free mailbox may write back to a paying mailbox that wrote to it this recently
	// What one inbox may receive from the pool in a day: WarmupInboundDailyMultiple
	// times its own daily sends, never below the floor (a recipient-only mailbox
	// sends nothing) and never above the ceiling. A recipient at its cap is left
	// out of every draw for the rest of the day, so paying the pool's debt to a
	// thin tier never turns into flooding it.
	WarmupInboundDailyFloor    = 10
	WarmupInboundDailyCeiling  = 60
	WarmupInboundDailyMultiple = 2
	// A free sender stops at this share of an inbox's daily cap, so the rest
	// of every inbox's day is left for premium senders.
	WarmupFreeInboundSharePercent = 75
	DailyThrottleNewOrgs          = 3 // new workspaces per owner per day

	// CLI sign-in handshake (`warmbly auth login`). Shorter-lived than the pool
	// link handshake because a person is watching the terminal while it runs.
	CLIAuthCodeTTLMinutes      = 10
	CLIAuthPollIntervalSeconds = 3

	// DailyThrottleNewScheduledSends caps how many NEW scheduled-send
	// schedules a single user can create in a rolling 24h window. The
	// real defense against burst abuse — someone writing a loop that
	// queues thousands of scheduled sends in seconds. Set high enough
	// that no human-driven volume comes close (a power user replying
	// to 200 inbound messages a day couldn't hit it organically).
	DailyThrottleNewScheduledSends = 1000

	// MaxPendingScheduledSendsPerOrg caps how many pending scheduled
	// email sends one workspace can have queued at once, across all of
	// its mailboxes. The DAILY rate (DailyThrottleNewScheduledSends) is
	// the primary abuse defense; this is the DB-bloat defense — each
	// pending row carries a body (~5KB), so capping pending count keeps
	// total scheduled-queue storage bounded per workspace.
	//
	// 10,000 is generous: a team scheduling 100 sends/day for the next
	// 100 days hits this exactly once. The combination of "1K new/day"
	// + "10K total pending" means legitimate use cannot organically
	// hit either, while a scripted attacker is bounded on both axes.
	//
	// Cloud Tasks cost is negligible at this size — at $0.40/M
	// operations, 10K pending = 20K ops = $0.008/workspace even at the
	// hardest abuse. The cap exists for DB sanity, not cost.
	//
	// Future: per-plan ceiling lookup. Today: single backstop.
	MaxPendingScheduledSendsPerOrg = 10000

	// Undo send: instant sends are queued this many seconds in the
	// future so the sender can still cancel. Per-user setting stored in
	// users.undo_send_seconds; the migration CHECK mirrors these bounds.
	UndoSendSecondsMin     = 5
	UndoSendSecondsDefault = 30
	UndoSendSecondsMax     = 120

	// Notification email window: how long email-channel notifications hold
	// before flushing as one bundled email. Per-user setting; the 30 minute
	// floor is deliberate — there is no per-event email mode, so the channel
	// can never become a per-notification firehose. Security sign-in alerts
	// bypass the window entirely.
	NotificationEmailWindowMinMinutes     = 30
	NotificationEmailWindowDefaultMinutes = 30
	NotificationEmailWindowMaxMinutes     = 1440
)

// InboundClassificationHeaders are the internet headers the API-based inbound
// sync (Gmail, Microsoft Graph) surfaces into EmailMessageData.Flags as
// "Header:value" pseudo-flags. The sync model carries no arbitrary-header field,
// so this is how the consumer's reply/bounce classifier (replyclassify via
// buildReplyHeaders) sees the machine-reply and delivery-status-report markers.
// From/Subject already ride the envelope; these add the high-signal headers.
var InboundClassificationHeaders = []string{
	"Auto-Submitted",
	"Precedence",
	"Content-Type",
	"Return-Path",
	"X-Autoreply",
	"X-Autorespond",
	"X-Auto-Response-Suppress",
	// A failure notice with no RFC 3464 status part names its recipient here.
	"X-Failed-Recipients",
	// List mail (newsletters, notifications): never a person answering us.
	"List-Unsubscribe",
	"List-Id",
}
