package models

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Seed panels a placement test can send to.
const (
	// PlacementPanelInstance is the seed panel the operator runs for every
	// workspace on the instance (on the hosted product, Warmbly's own).
	PlacementPanelInstance = "instance"
	// PlacementPanelWorkspace is the workspace's own test inboxes.
	PlacementPanelWorkspace = "workspace"
	// PlacementPanelCloud is Warmbly Cloud's panel, for a linked self-hosted
	// instance.
	PlacementPanelCloud = "cloud"
)

// ValidPlacementPanel reports whether p names a seed panel.
func ValidPlacementPanel(p string) bool {
	switch p {
	case PlacementPanelInstance, PlacementPanelWorkspace, PlacementPanelCloud:
		return true
	}
	return false
}

// Seed scopes, stored as email_accounts.seed_scope.
const (
	SeedScopeInstance  = "instance"
	SeedScopeWorkspace = "workspace"
)

// Placement test statuses.
const (
	PlacementStatusRunning   = "running"
	PlacementStatusCompleted = "completed"
	PlacementStatusCancelled = "cancelled"
	PlacementStatusFailed    = "failed"
)

// What started a placement test.
const (
	PlacementOriginManual  = "manual"
	PlacementOriginMonitor = "monitor"
	PlacementOriginAdmin   = "admin"
	// PlacementOriginRemote is a test the cloud runs for a linked instance.
	PlacementOriginRemote = "remote"
)

// Where one probe landed, stored as placement_results.folder.
const (
	PlacementFolderPending    = "pending"
	PlacementFolderInbox      = "inbox"
	PlacementFolderPromotions = "promotions"
	// PlacementFolderOther is a Gmail tab other than Promotions (Updates,
	// Social, Forums).
	PlacementFolderOther = "other"
	PlacementFolderSpam  = "spam"
	// PlacementFolderMissing is a copy that left the sender and never showed
	// up in the seed within the classify window.
	PlacementFolderMissing = "missing"
	// PlacementFolderFailed is a copy that never left the sender.
	PlacementFolderFailed    = "failed"
	PlacementFolderCancelled = "cancelled"
)

// PlacementFolderResolved reports whether a folder is a final answer about
// where a delivered copy went (not pending, failed or cancelled).
func PlacementFolderResolved(folder string) bool {
	switch folder {
	case PlacementFolderInbox, PlacementFolderPromotions, PlacementFolderOther,
		PlacementFolderSpam, PlacementFolderMissing:
		return true
	}
	return false
}

// ClassifyPlacementLanding reads where an arriving probe sits from its
// canonical folder and provider flags, splitting Gmail's Promotions tab from
// the other tabs because Promotions is where cold mail goes to be ignored.
func ClassifyPlacementLanding(folder string, flags []string) string {
	if folder == FolderSpam || HasSpamFlag(flags) {
		return PlacementFolderSpam
	}
	tab := ""
	for _, f := range flags {
		switch strings.ToUpper(f) {
		case "CATEGORY_PROMOTIONS":
			return PlacementFolderPromotions
		case "CATEGORY_UPDATES", "CATEGORY_SOCIAL", "CATEGORY_FORUMS":
			tab = PlacementFolderOther
		}
	}
	if tab != "" {
		return tab
	}
	return PlacementFolderInbox
}

// Tracking choices when a test is created.
const (
	// PlacementTrackingCampaign copies the campaign's own open and click
	// tracking; an ad-hoc test has none.
	PlacementTrackingCampaign = "campaign"
	PlacementTrackingOn       = "on"
	PlacementTrackingOff      = "off"
	// PlacementTrackingCompare runs the same copy twice, once tracked and once
	// not, interleaved per seed so both see the same conditions.
	PlacementTrackingCompare = "compare"
)

// PlacementTest is one run: a template sent from one sender to a seed panel.
type PlacementTest struct {
	ID              uuid.UUID  `json:"id"`
	OrganizationID  *uuid.UUID `json:"-"`
	SenderAccountID *uuid.UUID `json:"sender_account_id"`
	SenderEmail     string     `json:"sender_email"`
	CreatedBy       *uuid.UUID `json:"created_by"`
	CampaignID      *uuid.UUID `json:"campaign_id"`
	SequenceID      *uuid.UUID `json:"sequence_id"`
	ContactID       *uuid.UUID `json:"contact_id"`
	MonitorID       *uuid.UUID `json:"monitor_id"`
	Subject         string     `json:"subject"`
	BodyHTML        string     `json:"body_html,omitempty"`
	BodyPlain       string     `json:"body_plain,omitempty"`
	OpenTracking    bool       `json:"open_tracking"`
	LinkTracking    bool       `json:"link_tracking"`
	CompareGroupID  *uuid.UUID `json:"compare_group_id"`
	Origin          string     `json:"origin"`
	Panel           string     `json:"panel"`
	Status          string     `json:"status"`
	Error           string     `json:"error,omitempty"`
	// RemoteInstanceID is set on the cloud's copy of a test it runs for a
	// linked instance; RemoteTestID on the instance's copy, naming the cloud's.
	RemoteInstanceID *uuid.UUID `json:"-"`
	RemoteTestID     *uuid.UUID `json:"-"`
	CreatedAt        time.Time  `json:"created_at"`
	FinishedAt       *time.Time `json:"finished_at"`
}

// Tracked reports whether the test's copies carry any tracking.
func (t PlacementTest) Tracked() bool { return t.OpenTracking || t.LinkTracking }

// PlacementResult is one probe: one copy to one seed.
type PlacementResult struct {
	ID            uuid.UUID  `json:"id"`
	TestID        uuid.UUID  `json:"test_id"`
	SeedAccountID *uuid.UUID `json:"-"`
	SeedAddress   string     `json:"seed_address"`
	// Family is who hosts the seed, a mailhost value (google_workspace,
	// gmail, microsoft365, outlook, yahoo, ...). Stored as provider.
	Family         string     `json:"family"`
	RemoteSeedID   *uuid.UUID `json:"-"`
	TaskID         *uuid.UUID `json:"-"`
	MessageID      string     `json:"-"`
	Folder         string     `json:"folder"`
	ScheduledAt    *time.Time `json:"scheduled_at"`
	SentAt         *time.Time `json:"sent_at"`
	DetectedAt     *time.Time `json:"detected_at"`
	RawFlags       string     `json:"-"`
	Error          string     `json:"error,omitempty"`
	RemoteSyncedAt *time.Time `json:"-"`
}

// PlacementCounts is where a set of probes landed.
type PlacementCounts struct {
	Total      int `json:"total"`
	Pending    int `json:"pending"`
	Inbox      int `json:"inbox"`
	Promotions int `json:"promotions"`
	Other      int `json:"other"`
	Spam       int `json:"spam"`
	Missing    int `json:"missing"`
	Failed     int `json:"failed"`
	Cancelled  int `json:"cancelled"`
	// Delivered is every copy that left and got a verdict:
	// inbox + promotions + other + spam + missing.
	Delivered int `json:"delivered"`
	// InboxRate is the primary inbox share of Delivered, TabsRate the Gmail
	// tabs, SpamRate spam and MissingRate the copies never seen. Nil until
	// something is delivered.
	InboxRate   *float64 `json:"inbox_rate"`
	TabsRate    *float64 `json:"tabs_rate"`
	SpamRate    *float64 `json:"spam_rate"`
	MissingRate *float64 `json:"missing_rate"`
}

// Add counts one probe.
func (c *PlacementCounts) Add(folder string) {
	c.Total++
	switch folder {
	case PlacementFolderInbox:
		c.Inbox++
	case PlacementFolderPromotions:
		c.Promotions++
	case PlacementFolderOther:
		c.Other++
	case PlacementFolderSpam:
		c.Spam++
	case PlacementFolderMissing:
		c.Missing++
	case PlacementFolderFailed:
		c.Failed++
	case PlacementFolderCancelled:
		c.Cancelled++
	default:
		c.Pending++
	}
}

// Finish derives Delivered and the rates.
func (c *PlacementCounts) Finish() {
	c.Delivered = c.Inbox + c.Promotions + c.Other + c.Spam + c.Missing
	c.InboxRate, c.TabsRate, c.SpamRate, c.MissingRate = nil, nil, nil, nil
	if c.Delivered == 0 {
		return
	}
	rate := func(n int) *float64 {
		r := float64(n) / float64(c.Delivered)
		return &r
	}
	c.InboxRate = rate(c.Inbox)
	c.TabsRate = rate(c.Promotions + c.Other)
	c.SpamRate = rate(c.Spam)
	c.MissingRate = rate(c.Missing)
}

// PlacementFamilyCounts is one host family's share of a test.
type PlacementFamilyCounts struct {
	Family string          `json:"family"`
	Label  string          `json:"label"`
	Counts PlacementCounts `json:"counts"`
}

// PlacementSeed is a seed mailbox as a test sees it.
type PlacementSeed struct {
	AccountID      *uuid.UUID
	RemoteSeedID   *uuid.UUID
	Address        string
	Family         string
	OrganizationID *uuid.UUID
}

// PlacementPanelFamily counts the seeds one host family contributes.
type PlacementPanelFamily struct {
	Family string `json:"family"`
	Label  string `json:"label"`
	Seeds  int    `json:"seeds"`
}

// PlacementPanelInfo says whether a workspace can test on a panel.
type PlacementPanelInfo struct {
	Panel     string `json:"panel"`
	Available bool   `json:"available"`
	// Reason explains an unavailable panel in one sentence.
	Reason   string                 `json:"reason,omitempty"`
	Seeds    int                    `json:"seeds"`
	Families []PlacementPanelFamily `json:"families"`
	// Metered panels count against the monthly allowance.
	Metered bool `json:"metered"`
}

// PlacementUsage is a workspace's monthly allowance on the metered panels.
type PlacementUsage struct {
	Used int `json:"used"`
	// Limit is nil when the instance does not meter tests (self-hosted).
	Limit       *int      `json:"limit"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
}

// Remaining is how many metered tests are left, -1 when unmetered.
func (u PlacementUsage) Remaining() int {
	if u.Limit == nil {
		return -1
	}
	return max(0, *u.Limit-u.Used)
}

// PlacementWorkspaceSeed is one of a workspace's own mailboxes as a seed
// candidate.
type PlacementWorkspaceSeed struct {
	EmailAccountID uuid.UUID `json:"email_account_id"`
	Email          string    `json:"email"`
	Family         string    `json:"family"`
	Label          string    `json:"label"`
	Status         string    `json:"status"`
	Seed           bool      `json:"seed"`
	// Blocker is why the mailbox cannot become a seed, empty when it can.
	Blocker string `json:"blocker,omitempty"`
}

// PlacementMonitor re-tests a campaign's first step on a schedule.
type PlacementMonitor struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"-"`
	CampaignID     uuid.UUID  `json:"campaign_id"`
	CreatedBy      *uuid.UUID `json:"created_by"`
	Enabled        bool       `json:"enabled"`
	IntervalDays   int        `json:"interval_days"`
	Panel          string     `json:"panel"`
	AlertBelow     int        `json:"alert_below"`
	PauseOnAlert   bool       `json:"pause_on_alert"`
	NextRunAt      time.Time  `json:"next_run_at"`
	LastRunAt      *time.Time `json:"last_run_at"`
	LastTestID     *uuid.UUID `json:"last_test_id"`
	LastSenderID   *uuid.UUID `json:"-"`
	LastAlertAt    *time.Time `json:"last_alert_at"`
	LastError      string     `json:"last_error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// PlacementCloudStartRequest is a linked instance asking Warmbly Cloud for
// seeds to run a test against.
type PlacementCloudStartRequest struct {
	// SenderDomain keeps seeds on the sender's own domain out of the panel:
	// mail inside one tenant skips the filtering the test is there to see.
	SenderDomain string `json:"sender_domain"`
	// Tests is how many tests the request covers (two for a tracking
	// comparison), each charged against the cloud workspace's allowance.
	Tests int `json:"tests"`
	// MaxSeeds is how many seeds the sender's day can pay for; zero means the
	// cloud's own cap.
	MaxSeeds int `json:"max_seeds,omitempty"`
}

// PlacementCloudSeed is one cloud seed a linked instance sends a copy to.
type PlacementCloudSeed struct {
	ID      uuid.UUID `json:"id"`
	Address string    `json:"address"`
	Family  string    `json:"family"`
}

// PlacementCloudStart is the cloud's answer: the seeds, once per test.
type PlacementCloudStart struct {
	TestIDs []uuid.UUID          `json:"test_ids"`
	Seeds   []PlacementCloudSeed `json:"seeds"`
	Usage   PlacementUsage       `json:"usage"`
}

// PlacementCloudSend reports one copy the instance sent, or failed to send.
type PlacementCloudSend struct {
	SeedID    uuid.UUID  `json:"seed_id"`
	MessageID string     `json:"message_id,omitempty"`
	SentAt    *time.Time `json:"sent_at,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// PlacementCloudSends is the batch an instance reports.
type PlacementCloudSends struct {
	Sends []PlacementCloudSend `json:"sends"`
}

// PlacementCloudVerdict is where one cloud seed saw its copy land.
type PlacementCloudVerdict struct {
	SeedID     uuid.UUID  `json:"seed_id"`
	Folder     string     `json:"folder"`
	DetectedAt *time.Time `json:"detected_at,omitempty"`
}

// PlacementCloudTest is the cloud's view of one test it runs for an instance.
type PlacementCloudTest struct {
	TestID   uuid.UUID               `json:"test_id"`
	Status   string                  `json:"status"`
	Verdicts []PlacementCloudVerdict `json:"verdicts"`
}

// PlacementCloudPanel is the cloud panel as a linked instance sees it.
type PlacementCloudPanel struct {
	Panel PlacementPanelInfo `json:"panel"`
	Usage PlacementUsage     `json:"usage"`
}
