package models

import (
	"strings"

	"github.com/google/uuid"
)

// Where a verified warmup delivery landed at the recipient.
const (
	WarmupLandedInbox = "inbox"
	// WarmupLandedTabs is a Gmail category tab (Promotions, Updates, Social,
	// Forums): delivered and outside spam, just not in Primary.
	WarmupLandedTabs = "tabs"
	WarmupLandedSpam = "spam"
)

// Recipient groups a placement is broken down by, in display order.
const (
	WarmupRecipientGoogle    = "google"
	WarmupRecipientMicrosoft = "microsoft"
	WarmupRecipientYahoo     = "yahoo"
	WarmupRecipientOther     = "other"
)

// WarmupRecipientGroups lists every group in display order.
var WarmupRecipientGroups = []string{WarmupRecipientGoogle, WarmupRecipientMicrosoft, WarmupRecipientYahoo, WarmupRecipientOther}

// Placement rate windows and bands. The floor matches the pool's placement
// sample floor, so the number shown and the band enforced start at the same point.
const (
	WarmupPlacementWindowDays = 7
	WarmupPlacementMinSample  = 20
	WarmupPlacementGoodRate   = 90.0
	WarmupPlacementFairRate   = 80.0
	// WarmupUnconfirmedAfterHours is how long a sent warmup email may go
	// unseen by its recipient before it counts as unconfirmed.
	WarmupUnconfirmedAfterHours = 24
)

// Bands a mailbox's rolling inbox rate falls into.
const (
	WarmupPlacementBandGood       = "good"
	WarmupPlacementBandFair       = "fair"
	WarmupPlacementBandPoor       = "poor"
	WarmupPlacementBandCollecting = "collecting"
	WarmupPlacementBandNone       = "none"
)

// WarmupRecipientGroup buckets a recipient mailbox by who hosts it, falling
// back to how it connects when the host is not known yet. Mirrors the CASE in
// migration 000209.
func WarmupRecipientGroup(mailHost, provider string) string {
	switch mailHost {
	case "google_workspace", "gmail":
		return WarmupRecipientGoogle
	case "microsoft365", "outlook":
		return WarmupRecipientMicrosoft
	case "yahoo", "aol":
		return WarmupRecipientYahoo
	case "":
		switch InboxProvider(provider) {
		case InboxProviderGoogle:
			return WarmupRecipientGoogle
		case InboxProviderOutlook:
			return WarmupRecipientMicrosoft
		}
	}
	return WarmupRecipientOther
}

// WarmupRecipientGroupLabel is a recipient group's display name.
func WarmupRecipientGroupLabel(group string) string {
	switch group {
	case WarmupRecipientGoogle:
		return "Google"
	case WarmupRecipientMicrosoft:
		return "Microsoft"
	case WarmupRecipientYahoo:
		return "Yahoo & AOL"
	}
	return "Other providers"
}

// ClassifyWarmupLanding reads where an arriving message sits from its canonical
// folder and provider flags. The folder matters on IMAP, where a server can
// file mail into Junk without setting a junk keyword.
func ClassifyWarmupLanding(folder string, flags []string) string {
	if folder == FolderSpam || HasSpamFlag(flags) {
		return WarmupLandedSpam
	}
	for _, f := range flags {
		switch strings.ToUpper(f) {
		case "CATEGORY_PROMOTIONS", "CATEGORY_UPDATES", "CATEGORY_SOCIAL", "CATEGORY_FORUMS":
			return WarmupLandedTabs
		}
	}
	return WarmupLandedInbox
}

// WarmupPlacementBand maps a rolling inbox rate to its band; rate is nil below
// the sample floor.
func WarmupPlacementBand(delivered int, rate *float64) string {
	switch {
	case delivered == 0:
		return WarmupPlacementBandNone
	case rate == nil:
		return WarmupPlacementBandCollecting
	case *rate >= WarmupPlacementGoodRate:
		return WarmupPlacementBandGood
	case *rate >= WarmupPlacementFairRate:
		return WarmupPlacementBandFair
	default:
		return WarmupPlacementBandPoor
	}
}

// WarmupPlacementCounts is where a set of verified warmup deliveries landed.
// Inbox rate counts category tabs as inbox: the mail was delivered and not
// filtered, which is what the rate is asked to say.
type WarmupPlacementCounts struct {
	// Sent is warmup mail the mailbox sent; Delivered is what recipients
	// verifiably received (inbox + tabs + spam).
	Sent      int `json:"sent"`
	Delivered int `json:"delivered"`
	Inbox     int `json:"inbox"`
	Tabs      int `json:"tabs"`
	Spam      int `json:"spam"`
	// Rescued is spam placements the recipient's mailbox was told to move
	// back to the inbox; the move itself is not confirmed back.
	Rescued int `json:"rescued"`
	// Unconfirmed is mail sent more than WarmupUnconfirmedAfterHours ago that
	// no recipient has reported seeing.
	Unconfirmed int      `json:"unconfirmed"`
	InboxRate   *float64 `json:"inbox_rate"`
	SpamRate    *float64 `json:"spam_rate"`
}

// Finish derives Delivered and the rates from the counters.
func (c *WarmupPlacementCounts) Finish() {
	c.Delivered = c.Inbox + c.Tabs + c.Spam
	c.InboxRate, c.SpamRate = nil, nil
	if c.Delivered > 0 {
		in := pct2(c.Inbox+c.Tabs, c.Delivered)
		sp := pct2(c.Spam, c.Delivered)
		c.InboxRate, c.SpamRate = &in, &sp
	}
}

// Add accumulates o's raw counters into c.
func (c *WarmupPlacementCounts) Add(o WarmupPlacementCounts) {
	c.Sent += o.Sent
	c.Inbox += o.Inbox
	c.Tabs += o.Tabs
	c.Spam += o.Spam
	c.Rescued += o.Rescued
	c.Unconfirmed += o.Unconfirmed
}

// WarmupPlacementRate is a mailbox's headline deliverability: the inbox rate
// over the trailing window, withheld below the sample floor.
type WarmupPlacementRate struct {
	WindowDays int `json:"window_days"`
	MinSample  int `json:"min_sample"`
	Delivered  int `json:"delivered"`
	Inbox      int `json:"inbox"`
	Tabs       int `json:"tabs"`
	Spam       int `json:"spam"`
	// InboxRate is nil until Delivered reaches MinSample.
	InboxRate *float64 `json:"inbox_rate"`
	Band      string   `json:"band"`
}

// NewWarmupPlacementRate builds the rolling rate from window counters.
func NewWarmupPlacementRate(inbox, tabs, spam int) WarmupPlacementRate {
	r := WarmupPlacementRate{
		WindowDays: WarmupPlacementWindowDays,
		MinSample:  WarmupPlacementMinSample,
		Inbox:      inbox,
		Tabs:       tabs,
		Spam:       spam,
		Delivered:  inbox + tabs + spam,
	}
	if r.Delivered >= WarmupPlacementMinSample {
		v := pct2(inbox+tabs, r.Delivered)
		r.InboxRate = &v
	}
	r.Band = WarmupPlacementBand(r.Delivered, r.InboxRate)
	return r
}

// WarmupPlacementGroupCounts is one recipient group's share of a day.
type WarmupPlacementGroupCounts struct {
	Group   string `json:"group"`
	Inbox   int    `json:"inbox"`
	Tabs    int    `json:"tabs"`
	Spam    int    `json:"spam"`
	Rescued int    `json:"rescued"`
}

// WarmupPlacementDay is one UTC day of placement.
type WarmupPlacementDay struct {
	Date string `json:"date"`
	WarmupPlacementCounts
	// RollingInboxRate is the trailing-window rate ending on this day, nil
	// below the sample floor.
	RollingInboxRate *float64                     `json:"rolling_inbox_rate"`
	Groups           []WarmupPlacementGroupCounts `json:"groups"`
}

// WarmupPlacementHost is one mail host inside a recipient group.
type WarmupPlacementHost struct {
	Host string `json:"host"`
	WarmupPlacementCounts
}

// WarmupPlacementProvider is the window's placement at one recipient group.
type WarmupPlacementProvider struct {
	Group string `json:"group"`
	WarmupPlacementCounts
	Hosts []WarmupPlacementHost `json:"hosts"`
}

// WarmupPlacementMailbox is one sender's row in the workspace report.
type WarmupPlacementMailbox struct {
	EmailAccountID uuid.UUID `json:"email_account_id"`
	Email          string    `json:"email"`
	WarmupPlacementCounts
	Rate WarmupPlacementRate `json:"rate"`
	// DailyInboxRate follows the report's days; nil on a day with no deliveries.
	DailyInboxRate []*float64 `json:"daily_inbox_rate"`
}

// WarmupPlacementReport is where warmup mail landed over a date range, for one
// mailbox or for the whole workspace.
type WarmupPlacementReport struct {
	EmailAccountID *uuid.UUID                `json:"email_account_id,omitempty"`
	DateRange      DateRange                 `json:"date_range"`
	Summary        WarmupPlacementCounts     `json:"summary"`
	Rate           WarmupPlacementRate       `json:"rate"`
	Daily          []WarmupPlacementDay      `json:"daily"`
	Providers      []WarmupPlacementProvider `json:"providers"`
	// Mailboxes is present on the workspace report only, worst rate first.
	Mailboxes []WarmupPlacementMailbox `json:"mailboxes,omitempty"`
}

// pct2 is part/whole as a percentage rounded to two decimals.
func pct2(part, whole int) float64 {
	if whole <= 0 {
		return 0
	}
	v := float64(part) / float64(whole) * 100
	return float64(int(v*100+0.5)) / 100
}
