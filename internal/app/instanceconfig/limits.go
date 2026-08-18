package instanceconfig

import (
	"strconv"

	"github.com/warmbly/warmbly/internal/config"
)

// LimitEntry is one number an operator may be looking for.
type LimitEntry struct {
	Name        string `json:"name"`
	Value       string `json:"value"`
	Unit        string `json:"unit"`
	Description string `json:"description"`
}

// LimitGroup is a rendered section of the effective-limits page.
type LimitGroup struct {
	Title   string       `json:"title"`
	Entries []LimitEntry `json:"entries"`
}

// Limits is the effective-limits document. Every value is a compiled constant
// from internal/config/constants.go, so this page is read-only by construction:
// per-organization variance goes through the admin override flow instead.
func Limits() []LimitGroup {
	return []LimitGroup{
		{
			Title: "Sending",
			Entries: []LimitEntry{
				{"Default campaign cap", n(config.CampaignLimitDefault), "emails/day", "Per mailbox, before any organization override."},
				{"Minimum gap between sends", n(config.MinWaitTimeDefault), "seconds", "Per mailbox. Spacing matters more than throughput."},
				{"Highest campaign cap accepted", n(config.LimitMax), "emails/day", "The largest per-mailbox cap the API will store."},
				{"Lowest campaign cap accepted", n(config.LimitMin), "emails/day", "The smallest per-mailbox cap the API will store."},
				{"Campaign ramp start", n(config.CampaignRampStartDefault), "emails/day", "Where a new campaign sender starts before the ramp lifts it."},
				{"Campaign ramp increment", n(config.CampaignRampIncrementDefault), "emails/day", "Added each day, applied only as a lower bound against the mailbox cap."},
				{"Campaign ramp ceiling", n(config.CampaignRampCeilingDefault), "emails/day", "Where the ramp stops lifting."},
				{"Sender weight ceiling", n(config.CampaignSenderWeightMax), "weight", "The largest share one mailbox can take in a rotation."},
				{"New leads per campaign", n(config.CampaignMaxNewLeadsMax), "contacts", "The most contacts one campaign can pull in per run."},
			},
		},
		{
			Title: "Warmup",
			Entries: []LimitEntry{
				{"Warmup start", n(config.WarmupBaseDefault), "emails/day", "Where a mailbox begins warming."},
				{"Warmup ceiling", n(config.WarmupMaxDefault), "emails/day", "Where the warmup ramp stops."},
				{"Warmup ramp", "+" + n(config.WarmupIncreaseDefault), "emails/day", "Added each day while the mailbox stays healthy."},
			},
		},
		{
			Title: "Organization hard caps",
			Entries: []LimitEntry{
				{"Connected mailboxes", n(config.HardCapMailboxes), "per organization", "The backstop when neither a plan nor an override sets one."},
				{"Campaigns created", n(config.HardCapCampaignsTotal), "per organization", "Total campaigns ever created."},
				{"Active campaigns", n(config.HardCapCampaignsActive), "per organization", "Running at the same time."},
				{"Team members", n(config.HardCapTeamMembers), "seats", "Members in one organization."},
				{"Contacts", n(config.HardCapContacts), "per organization", "Stored contacts."},
				{"Campaign sends", n(config.HardCapDailyCampaignSends), "emails/day", "Across every mailbox in one organization."},
			},
		},
		{
			Title: "Daily creation throttles",
			Entries: []LimitEntry{
				{"New campaigns", n(config.DailyThrottleNewCampaigns), "per organization/day", "Resets at UTC midnight. Not raisable per organization."},
				{"Newly connected mailboxes", n(config.DailyThrottleNewMailboxes), "per organization/day", "Resets at UTC midnight."},
				{"New workspaces", n(config.DailyThrottleNewOrgs), "per owner/day", "Resets at UTC midnight."},
			},
		},
		{
			Title: "Scheduled and undo sends",
			Entries: []LimitEntry{
				{"New scheduled sends", n(config.DailyThrottleNewScheduledSends), "per user/day", "Rolling 24 hour window."},
				{"Pending scheduled sends", n(config.MaxPendingScheduledSendsPerUser), "per user", "Queued at once. Bounds queue storage rather than cost."},
				{"Undo send window", n(config.UndoSendSecondsDefault), "seconds", "Default hold before an instant send leaves. Range " + n(config.UndoSendSecondsMin) + " to " + n(config.UndoSendSecondsMax) + "."},
			},
		},
		{
			Title: "Webhooks and integrations",
			Entries: []LimitEntry{
				{"Dispatch floor", n(config.WebhookDispatchBasePerMinute), "events/minute", "Every organization gets at least this, including free ones."},
				{"Added per allowed mailbox", n(config.WebhookDispatchPerMailboxPerMinute), "events/minute", "The cap scales with the plan's mailbox allowance."},
				{"Dispatch ceiling", n(config.WebhookDispatchMaxPerMinute), "events/minute", "The hard ceiling for any plan."},
			},
		},
		{
			Title: "Content and inbox",
			Entries: []LimitEntry{
				{"Email body", n(config.MaxEmailBodySize / 1024), "KB", "The largest body the platform stores."},
				{"Contact record", n(config.MaxContactSize / 1024), "KB", "The largest single contact payload."},
				{"Mailbox folders synced", n(config.MaxEmailFolders), "folders", "Per mailbox."},
				{"IMAP fetch batch", n(config.ImapFetchBatchSize), "messages", "How many envelopes one sync window holds in memory."},
			},
		},
		{
			Title: "Mailbox sync fair use",
			Entries: []LimitEntry{
				{"Live burst", n(config.SyncBurstPer5Min), "messages/5 min", "New mail one mailbox may store in any five minute window. Over it, mail waits; nothing is dropped."},
				{"Live hourly", n(config.SyncHourlyPerMailbox), "messages/hour", "Per mailbox, any clock hour."},
				{"Backfill pace", n(config.SyncBackfillPerMinute), "messages/minute", "Per mailbox during the initial import of history."},
				{"Flood threshold", n(config.SyncFloodPerHour), "messages/hour", "New live mail seen in one hour that deactivates the mailbox outright."},
				{"Chronic overage", n(config.SyncThrottleEscalationDays), "days of 7", "Daily budget exhausted on this many of the last seven days deactivates the mailbox. The daily budgets and the backfill window are editable on Instance settings."},
				{"Unibox page size", n(config.UniboxLimitDefault), "messages", "Default page size, range " + n(config.UniboxLimitMin) + " to " + n(config.UniboxLimitMax) + "."},
				{"Sequence step delay", n(config.SequenceWaitAfterMax), "days", "The largest per-step wait a sequence can store."},
				{"Sequence subject", n(config.SequenceSubjectLimit), "characters", "Per step."},
				{"Sequence body", n(config.SequenceBodyLimit), "characters", "Per step."},
			},
		},
		{
			Title: "Notifications",
			Entries: []LimitEntry{
				{"Email digest window", n(config.NotificationEmailWindowDefaultMinutes), "minutes", "Default hold before notifications flush as one email. Range " + n(config.NotificationEmailWindowMinMinutes) + " to " + n(config.NotificationEmailWindowMaxMinutes) + "."},
			},
		},
	}
}

func n(v int) string { return strconv.Itoa(v) }
