package aitools

import (
	"context"
	"encoding/json"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

func (d Deps) registerAnalyticsTools(r *Registry) {
	r.Register(Tool{
		Name:        "get_dashboard_analytics",
		Description: "Org-wide sending analytics for a period: totals (sent, opens, clicks, replies, bounces), rates, top campaigns, and account-health summary. Use this for \"how are we doing\" questions.",
		InputSchema: objectSchema(map[string]any{
			"period": enumProp("Aggregation window (default 30d).", "7d", "30d", "90d"),
		}),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewAnalytics,
		RequiredAPIPerm: models.APIPermReadAnalytics,
		Handler:         d.getDashboardAnalytics,
	})

	r.Register(Tool{
		Name:            "list_mailboxes",
		Description:     "List the org's sender mailboxes with status, health score, daily usage, warmup state, and any current errors. Use this for deliverability, warmup, and \"which mailbox is unhealthy\" questions.",
		InputSchema:     objectSchema(map[string]any{}),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewAnalytics,
		RequiredAPIPerm: models.APIPermReadAnalytics,
		Handler:         d.listMailboxes,
	})
}

func (d Deps) getDashboardAnalytics(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		Period string `json:"period"`
	}](args)
	if err != nil {
		return "", err
	}
	period := in.Period
	if period == "" {
		period = "30d"
	}
	a, xerr := d.Analytics.GetDashboardAnalytics(ctx, inv.UserID, period)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	top := make([]map[string]any, 0, len(a.TopCampaigns))
	for _, c := range a.TopCampaigns {
		top = append(top, map[string]any{
			"campaign_id": c.CampaignID.String(),
			"name":        c.Name,
			"emails_sent": c.EmailsSent,
			"open_rate":   c.OpenRate,
			"reply_rate":  c.ReplyRate,
		})
	}
	s := a.OverallStats
	return jsonResult(map[string]any{
		"period": a.Period,
		"totals": map[string]any{
			"emails_sent": s.TotalEmailsSent,
			"opens":       s.TotalOpens,
			"clicks":      s.TotalClicks,
			"replies":     s.TotalReplies,
			"bounces":     s.TotalBounces,
			"open_rate":   s.OpenRate,
			"click_rate":  s.ClickRate,
			"reply_rate":  s.ReplyRate,
		},
		"top_campaigns":  top,
		"account_health": a.AccountHealth,
	})
}

func (d Deps) listMailboxes(ctx context.Context, inv Invocation, _ json.RawMessage) (string, error) {
	accounts, xerr := d.Analytics.GetAllAccountStatuses(ctx, inv.OrgID)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	out := make([]map[string]any, 0, len(accounts))
	for _, a := range accounts {
		row := map[string]any{
			"mailbox_id":    a.ID.String(),
			"email":         a.Email,
			"provider":      a.Provider,
			"status":        a.Status,
			"health_status": a.Health.Status,
			"health_score":  a.Health.Score,
			"sent_today":    a.DailyUsage,
			"in_campaign":   a.InCampaign,
		}
		if len(a.Health.Issues) > 0 {
			row["issues"] = a.Health.Issues
		}
		if a.WarmupStatus != nil {
			row["warmup"] = a.WarmupStatus
		}
		out = append(out, row)
	}
	return jsonResult(map[string]any{"mailboxes": out, "count": len(out)})
}
