package aitools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

func (d Deps) registerCampaignTools(r *Registry) {
	r.Register(Tool{
		Name:        "list_campaigns",
		Description: "List the organization's campaigns, newest first, optionally filtered by status.",
		InputSchema: objectSchema(map[string]any{
			"status": enumProp("Optional status filter.", "draft", "active", "paused", "completed"),
			"query":  strProp("Optional name search."),
			"limit":  intProp("Max campaigns (1-50, default 20)."),
		}),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewCampaigns,
		RequiredAPIPerm: models.APIPermReadCampaigns,
		Handler:         d.listCampaigns,
	})

	r.Register(Tool{
		Name:        "get_campaign_stats",
		Description: "Get send/open/click/reply stats for one campaign.",
		InputSchema: objectSchema(map[string]any{
			"campaign_id": strProp("The campaign's UUID."),
		}, "campaign_id"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewCampaigns,
		RequiredAPIPerm: models.APIPermReadCampaigns,
		Handler:         d.getCampaignStats,
	})

	r.Register(Tool{
		Name:        "create_campaign_draft",
		Description: "Create a new DRAFT campaign with optional starter email steps. The campaign is never started; the user opens it in the sequence editor to review and launch. Returns a link to open it.",
		InputSchema: objectSchema(map[string]any{
			"name":        strProp("Campaign name (required)."),
			"description": strProp("Optional description."),
			"steps": arrProp("Optional email steps to seed the sequence.", objectSchema(map[string]any{
				"subject":   strProp("Email subject (may contain {{merge}} vars)."),
				"body":      strProp("Email body text (may contain {{merge}} vars)."),
				"wait_days": intProp("Days to wait before this step (0 for the first)."),
			}, "subject", "body")),
		}, "name"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageCampaigns,
		RequiredAPIPerm: models.APIPermWriteCampaigns,
		Handler:         d.createCampaignDraft,
	})
}

func (d Deps) listCampaigns(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		Status string `json:"status"`
		Query  string `json:"query"`
		Limit  int    `json:"limit"`
	}](args)
	if err != nil {
		return "", err
	}
	limit := in.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	res, xerr := d.Campaigns.Search(ctx, inv.OrgID.String(), in.Query, "", "", in.Status, fmt.Sprintf("%d", limit))
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	out := make([]map[string]string, 0, len(res.Data))
	for _, c := range res.Data {
		out = append(out, map[string]string{"id": c.ID.String(), "name": c.Name, "status": c.Status})
	}
	return jsonResult(map[string]any{"campaigns": out, "count": len(out)})
}

func (d Deps) getCampaignStats(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		CampaignID string `json:"campaign_id"`
	}](args)
	if err != nil {
		return "", err
	}
	cid, err := parseUUIDArg(in.CampaignID)
	if err != nil {
		return "", err
	}
	a, xerr := d.Analytics.GetCampaignAnalytics(ctx, inv.UserID, cid)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	s := a.Summary
	return jsonResult(map[string]any{
		"campaign_id":    a.CampaignID.String(),
		"name":           a.Name,
		"status":         a.Status,
		"total_contacts": s.TotalContacts,
		"emails_sent":    s.EmailsSent,
		"unique_opens":   s.UniqueOpens,
		"unique_clicks":  s.UniqueClicks,
		"replies":        s.Replies,
		"bounces":        s.Bounces,
		"open_rate":      s.OpenRate,
		"click_rate":     s.ClickRate,
		"reply_rate":     s.ReplyRate,
	})
}

func (d Deps) createCampaignDraft(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Steps       []struct {
			Subject  string `json:"subject"`
			Body     string `json:"body"`
			WaitDays *int   `json:"wait_days"`
		} `json:"steps"`
	}](args)
	if err != nil {
		return "", err
	}
	if in.Name == "" {
		return "", ErrInvalidArgs
	}

	seqs := make([]models.CreateSequenceInput, 0, len(in.Steps))
	for i, st := range in.Steps {
		seqs = append(seqs, models.CreateSequenceInput{
			Name:      fmt.Sprintf("Step %d", i+1),
			Subject:   st.Subject,
			BodyPlain: st.Body,
			WaitAfter: st.WaitDays,
		})
	}

	orgID := inv.OrgID
	camp, xerr := d.Campaigns.Create(ctx, inv.UserID.String(), &orgID, &models.CreateCampaign{
		Name:        in.Name,
		Description: in.Description,
		Sequences:   seqs,
	})
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionCreate, models.AuditEntityCampaign, &camp.ID, map[string]string{"name": camp.Name})
	return jsonResult(map[string]any{
		"ok":          true,
		"campaign_id": camp.ID.String(),
		"status":      camp.Status,
		"open_url":    d.link("/app/campaigns/" + camp.ID.String()),
	})
}

// link builds an absolute dashboard URL when AppBaseURL is set, else a relative
// path the client can resolve.
func (d Deps) link(path string) string {
	if d.AppBaseURL == "" {
		return path
	}
	return d.AppBaseURL + path
}
