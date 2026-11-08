package models

import (
	"time"

	"github.com/google/uuid"
)

// ClientReportRange is the period a client report covers, in whole UTC days like the dashboard.
type ClientReportRange string

const (
	ClientReportLast7Days  ClientReportRange = "last_7_days"
	ClientReportLast30Days ClientReportRange = "last_30_days"
	ClientReportThisMonth  ClientReportRange = "this_month"
	ClientReportLastMonth  ClientReportRange = "last_month"
)

// ClientReportRanges lists every range in the order a switcher shows them.
var ClientReportRanges = []ClientReportRange{ClientReportLast7Days, ClientReportLast30Days, ClientReportThisMonth, ClientReportLastMonth}

// Valid reports whether r is a known range.
func (r ClientReportRange) Valid() bool {
	for _, v := range ClientReportRanges {
		if r == v {
			return true
		}
	}
	return false
}

// Window is the range's first instant and last instant at now, on UTC days with today included.
func (r ClientReportRange) Window(now time.Time) (from, to time.Time) {
	now = now.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	switch r {
	case ClientReportLast7Days:
		return today.AddDate(0, 0, -6), now
	case ClientReportThisMonth:
		return month, now
	case ClientReportLastMonth:
		return month.AddDate(0, -1, 0), month.Add(-time.Nanosecond)
	default:
		return today.AddDate(0, 0, -29), now
	}
}

// ClientReportCadence is how often a scheduled report email goes out.
type ClientReportCadence string

const (
	ClientReportWeekly  ClientReportCadence = "weekly"
	ClientReportMonthly ClientReportCadence = "monthly"
)

// Valid reports whether c is a known cadence.
func (c ClientReportCadence) Valid() bool {
	return c == ClientReportWeekly || c == ClientReportMonthly
}

// EmailRange is the period an email on this cadence summarizes.
func (c ClientReportCadence) EmailRange() ClientReportRange {
	if c == ClientReportMonthly {
		return ClientReportLastMonth
	}
	return ClientReportLast7Days
}

const (
	// MaxClientReportsPerOrg bounds how many reports one workspace keeps.
	MaxClientReportsPerOrg = 200
	// MaxClientReportRecipients bounds one schedule's recipient list.
	MaxClientReportRecipients = 10
	// ClientReportLinkDefaultDays is a share link's lifetime when none is asked for.
	ClientReportLinkDefaultDays = 90
	// ClientReportLinkMaxDays is the longest lifetime a share link may be given.
	ClientReportLinkMaxDays = 365
	// ClientReportEmailLinkDays is how long the link inside a scheduled email stays open.
	ClientReportEmailLinkDays = 90
	// ClientReportReplyLimit bounds the replies one report lists.
	ClientReportReplyLimit = 25
	// ClientReportCampaignLimit bounds the rows of the campaign table.
	ClientReportCampaignLimit = 200
)

// ClientReportSections are the parts of a report that are shown.
type ClientReportSections struct {
	Overview        bool `json:"overview"`
	Campaigns       bool `json:"campaigns"`
	Replies         bool `json:"replies"`
	PositiveReplies bool `json:"positive_replies"`
	Mailboxes       bool `json:"mailboxes"`
	DailyChart      bool `json:"daily_chart"`
}

// DefaultClientReportSections is every section on.
func DefaultClientReportSections() ClientReportSections {
	return ClientReportSections{Overview: true, Campaigns: true, Replies: true, PositiveReplies: true, Mailboxes: true, DailyChart: true}
}

// ClientReportLink is the state of a report's share link; the URL itself is only returned when minted.
type ClientReportLink struct {
	// Hint is the token's first characters, so a person can tell links apart.
	Hint      string     `json:"hint"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at"`
	Expired   bool       `json:"expired"`
}

// ClientReportSchedule is when a report is emailed and to whom.
type ClientReportSchedule struct {
	Recipients []string            `json:"recipients"`
	Cadence    ClientReportCadence `json:"cadence"`
	// Weekday is 0 (Sunday) to 6, used by weekly schedules.
	Weekday int `json:"weekday"`
	// MonthDay is 1 to 28, used by monthly schedules.
	MonthDay   int        `json:"month_day"`
	Hour       int        `json:"hour"`
	Timezone   string     `json:"timezone"`
	Paused     bool       `json:"paused"`
	LastSentAt *time.Time `json:"last_sent_at"`
	NextRunAt  time.Time  `json:"next_run_at"`
}

// ClientReport is a saved, scoped report a workspace shares with its client.
type ClientReport struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"-"`
	CreatedBy      *uuid.UUID `json:"created_by"`
	Name           string     `json:"name"`
	// Access is which campaigns, folders and mailboxes the report covers.
	Access            MemberAccess          `json:"access"`
	Sections          ClientReportSections  `json:"sections"`
	ShowReplySnippets bool                  `json:"show_reply_snippets"`
	DateRange         ClientReportRange     `json:"date_range"`
	Link              *ClientReportLink     `json:"link"`
	Schedule          *ClientReportSchedule `json:"schedule"`
	CreatedAt         time.Time             `json:"created_at"`
	UpdatedAt         time.Time             `json:"updated_at"`
}

// ClientReportInput is the body of POST and PATCH /reports; absent fields keep their value.
type ClientReportInput struct {
	Name              *string               `json:"name"`
	Access            *MemberAccess         `json:"access"`
	Sections          *ClientReportSections `json:"sections"`
	ShowReplySnippets *bool                 `json:"show_reply_snippets"`
	DateRange         *ClientReportRange    `json:"date_range"`
}

// CreateClientReportLinkRequest is the body of POST /reports/:id/link.
type CreateClientReportLinkRequest struct {
	// ExpiresInDays defaults to ClientReportLinkDefaultDays.
	ExpiresInDays *int `json:"expires_in_days"`
}

// ClientReportLinkResult is a freshly minted link; URL is shown this once.
type ClientReportLinkResult struct {
	URL    string           `json:"url"`
	Token  string           `json:"token"`
	Link   ClientReportLink `json:"link"`
	Report *ClientReport    `json:"report"`
}

// SetClientReportScheduleRequest is the body of PUT /reports/:id/schedule.
type SetClientReportScheduleRequest struct {
	Recipients []string            `json:"recipients"`
	Cadence    ClientReportCadence `json:"cadence"`
	Weekday    int                 `json:"weekday"`
	MonthDay   int                 `json:"month_day"`
	Hour       int                 `json:"hour"`
	Timezone   string              `json:"timezone"`
	Paused     bool                `json:"paused"`
}

// ClientReportDelivery is one scheduled run of a report email.
type ClientReportDelivery struct {
	ID             uuid.UUID
	ReportID       uuid.UUID
	OrganizationID uuid.UUID
	RunAt          time.Time
	DateRange      ClientReportRange
	Recipients     []string
	SentTo         []string
	TokenHash      []byte
	ExpiresAt      time.Time
	Attempts       int
}

// ClientReportQuery is what the analytics service reads for one report view.
type ClientReportQuery struct {
	From, To time.Time
	// Campaigns nil is every campaign in the workspace; an empty set matches none.
	Campaigns *CampaignScope
	// Mailboxes nil is every mailbox; an empty slice matches none.
	Mailboxes     []uuid.UUID
	Sections      ClientReportSections
	ReplySnippets bool
}

// ClientReportAnalytics is the numbers behind one report view, read with the dashboard's own queries.
type ClientReportAnalytics struct {
	Overall   DashboardOverallStats
	Positive  int
	Daily     []DashboardDailyStats
	Campaigns []ClientReportCampaign
	Replies   []ClientReportReply
	Health    *AccountHealthSummary
	Warmup    *WarmupSummary
}

// ClientReportView is a rendered report: what the dashboard preview and the public page show.
type ClientReportView struct {
	Name              string                 `json:"name"`
	DateRange         ClientReportRange      `json:"date_range"`
	From              string                 `json:"from"`
	To                string                 `json:"to"`
	GeneratedAt       time.Time              `json:"generated_at"`
	Sections          ClientReportSections   `json:"sections"`
	ShowReplySnippets bool                   `json:"show_reply_snippets"`
	Overview          *ClientReportOverview  `json:"overview,omitempty"`
	Daily             []ClientReportDay      `json:"daily,omitempty"`
	Campaigns         []ClientReportCampaign `json:"campaigns,omitempty"`
	Replies           []ClientReportReply    `json:"replies,omitempty"`
	Mailboxes         *ClientReportMailboxes `json:"mailboxes,omitempty"`
}

// ClientReportOverview is the KPI row. Positive figures are nil when the section is off.
type ClientReportOverview struct {
	Sent         int      `json:"sent"`
	Opened       int      `json:"opened"`
	OpenRate     float64  `json:"open_rate"`
	Clicked      int      `json:"clicked"`
	Replied      int      `json:"replied"`
	ReplyRate    float64  `json:"reply_rate"`
	Positive     *int     `json:"positive,omitempty"`
	PositiveRate *float64 `json:"positive_rate,omitempty"`
	Bounced      int      `json:"bounced"`
	BounceRate   float64  `json:"bounce_rate"`
}

// ClientReportDay is one day of the chart.
type ClientReportDay struct {
	Date    string `json:"date"`
	Sent    int    `json:"sent"`
	Opens   int    `json:"opens"`
	Replies int    `json:"replies"`
}

// ClientReportCampaign is one row of the campaign table.
type ClientReportCampaign struct {
	ID         uuid.UUID `json:"-"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Sent       int       `json:"sent"`
	Opens      int       `json:"opens"`
	OpenRate   float64   `json:"open_rate"`
	Clicks     int       `json:"clicks"`
	Replies    int       `json:"replies"`
	ReplyRate  float64   `json:"reply_rate"`
	Positive   *int      `json:"positive,omitempty"`
	Bounces    int       `json:"bounces"`
	BounceRate float64   `json:"bounce_rate"`
}

// ClientReportReply is one reply in the list; Snippet is set only when the report shows snippets.
type ClientReportReply struct {
	Name           string    `json:"name"`
	Email          string    `json:"email"`
	Campaign       string    `json:"campaign"`
	Subject        string    `json:"subject"`
	RepliedAt      time.Time `json:"replied_at"`
	Classification string    `json:"classification"`
	Snippet        string    `json:"snippet,omitempty"`
}

// ClientReportMailboxes is the mailbox health and warmup summary.
type ClientReportMailboxes struct {
	Total          int `json:"total"`
	Healthy        int `json:"healthy"`
	Warning        int `json:"warning"`
	Error          int `json:"error"`
	WarmupSent     int `json:"warmup_sent"`
	WarmupReceived int `json:"warmup_received"`
	WarmupReplied  int `json:"warmup_replied"`
}
