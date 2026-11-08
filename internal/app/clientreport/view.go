package clientreport

import (
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

const dayLayout = "2006-01-02"

// buildView shapes the analytics into what the report shows, leaving out every section that is off.
func buildView(rep *models.ClientReport, rng models.ClientReportRange, from, to, now time.Time, a *models.ClientReportAnalytics) *models.ClientReportView {
	sec := rep.Sections
	v := &models.ClientReportView{
		Name: rep.Name, DateRange: rng, From: from.UTC().Format(dayLayout), To: to.UTC().Format(dayLayout),
		GeneratedAt: now.UTC(), Sections: sec, ShowReplySnippets: rep.ShowReplySnippets,
	}
	if sec.Overview {
		o := a.Overall
		v.Overview = &models.ClientReportOverview{
			Sent: o.TotalEmailsSent, Opened: o.TotalOpens, OpenRate: o.OpenRate, Clicked: o.TotalClicks,
			Replied: o.TotalReplies, ReplyRate: o.ReplyRate, Bounced: o.TotalBounces, BounceRate: o.BounceRate,
		}
		if sec.PositiveReplies {
			positive, rate := a.Positive, models.Rate(a.Positive, o.TotalEmailsSent)
			v.Overview.Positive, v.Overview.PositiveRate = &positive, &rate
		}
	}
	if sec.Campaigns {
		v.Campaigns = make([]models.ClientReportCampaign, 0, len(a.Campaigns))
		for _, c := range a.Campaigns {
			if !sec.PositiveReplies {
				c.Positive = nil
			}
			v.Campaigns = append(v.Campaigns, c)
		}
	}
	if sec.DailyChart {
		v.Daily = fillDays(from, to, a.Daily)
	}
	if sec.Replies || sec.PositiveReplies {
		v.Replies = make([]models.ClientReportReply, 0, len(a.Replies))
		for _, r := range a.Replies {
			if !rep.ShowReplySnippets {
				r.Snippet = ""
			}
			v.Replies = append(v.Replies, r)
		}
	}
	if sec.Mailboxes && a.Health != nil {
		v.Mailboxes = &models.ClientReportMailboxes{Total: a.Health.TotalAccounts, Healthy: a.Health.HealthyAccounts,
			Warning: a.Health.WarningAccounts, Error: a.Health.ErrorAccounts}
		if a.Warmup != nil {
			v.Mailboxes.WarmupSent, v.Mailboxes.WarmupReceived, v.Mailboxes.WarmupReplied = a.Warmup.TotalSent, a.Warmup.TotalReceived, a.Warmup.TotalReplied
		}
	}
	return v
}

// fillDays is one entry per UTC day from from to to, zero where nothing was sent.
func fillDays(from, to time.Time, rows []models.DashboardDailyStats) []models.ClientReportDay {
	byDate := make(map[string]models.DashboardDailyStats, len(rows))
	for _, r := range rows {
		byDate[r.Date] = r
	}
	first := time.Date(from.UTC().Year(), from.UTC().Month(), from.UTC().Day(), 0, 0, 0, 0, time.UTC)
	last := to.UTC().Format(dayLayout)
	out := []models.ClientReportDay{}
	for d := first; d.Format(dayLayout) <= last && len(out) < 62; d = d.AddDate(0, 0, 1) {
		key := d.Format(dayLayout)
		r := byDate[key]
		out = append(out, models.ClientReportDay{Date: key, Sent: r.Sent, Opens: r.Opens, Replies: r.Replies})
	}
	return out
}
