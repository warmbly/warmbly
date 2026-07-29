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
		Name:        "list_campaign_leads",
		Description: "List one campaign's leads with their derived status and per-status totals. Statuses: pending (queued, nothing sent), active (mid-sequence), completed (every step sent, no reply — these are the leads that went cold and need a follow-up), replied, bounced, unsubscribed. Use THIS to find cold leads or follow-up candidates; the inbox only shows received mail.",
		InputSchema: objectSchema(map[string]any{
			"campaign_id": strProp("The campaign's UUID (from list_campaigns)."),
			"status":      enumProp("Optional lead-status filter.", "pending", "active", "completed", "replied", "bounced", "unsubscribed"),
			"limit":       intProp("Max leads (1-50, default 20)."),
		}, "campaign_id"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewContacts,
		RequiredAPIPerm: models.APIPermReadContacts,
		Handler:         d.listCampaignLeads,
	})

	r.Register(Tool{
		Name:        "set_campaign_status",
		Description: "Start (activate) or stop (pause) a campaign. Starting resumes sending to remaining leads; stopping pauses all sending. Requires user approval.",
		InputSchema: objectSchema(map[string]any{
			"campaign_id": strProp("The campaign's UUID."),
			"action":      enumProp("start to activate/resume, stop to pause.", "start", "stop"),
		}, "campaign_id", "action"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermSendCampaigns,
		RequiredAPIPerm: models.APIPermSendCampaigns,
		Handler:         d.setCampaignStatus,
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

	r.Register(Tool{
		Name:        "get_campaign",
		Description: "Get one campaign's full configuration (settings, schedule, tracking, ramp).",
		InputSchema: objectSchema(map[string]any{
			"campaign_id": strProp("The campaign's UUID."),
		}, "campaign_id"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewCampaigns,
		RequiredAPIPerm: models.APIPermReadCampaigns,
		Handler:         d.getCampaign,
	})

	r.Register(Tool{
		Name:        "update_campaign",
		Description: "Update a campaign's settings. Only provided fields change. Editing a draft is safe; changing an active campaign takes effect on the next sends.",
		InputSchema: objectSchema(map[string]any{
			"campaign_id":    strProp("The campaign's UUID."),
			"name":           strProp("New name."),
			"description":    strProp("New description."),
			"daily_limit":    intProp("Per-day send cap for the campaign."),
			"stop_on_reply":  boolProp("Stop sequencing a lead once they reply."),
			"open_tracking":  boolProp("Track opens."),
			"link_tracking":  boolProp("Track link clicks."),
			"text_only":      boolProp("Send plain text only."),
			"ramp_enabled":   boolProp("Gradually ramp daily volume."),
			"ramp_start":     intProp("Ramp starting volume."),
			"ramp_increment": intProp("Ramp daily increment."),
			"ramp_ceiling":   intProp("Ramp ceiling."),
		}, "campaign_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageCampaigns,
		RequiredAPIPerm: models.APIPermWriteCampaigns,
		Handler:         d.updateCampaign,
	})

	r.Register(Tool{
		Name:        "delete_campaign",
		Description: "Delete a campaign. Destructive and not reversible; requires user approval.",
		InputSchema: objectSchema(map[string]any{
			"campaign_id": strProp("The campaign's UUID."),
		}, "campaign_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageCampaigns,
		RequiredAPIPerm: models.APIPermWriteCampaigns,
		Handler:         d.deleteCampaign,
	})

	r.Register(Tool{
		Name:        "list_campaign_senders",
		Description: "List the mailboxes assigned to send a campaign, with their weight and enabled state.",
		InputSchema: objectSchema(map[string]any{
			"campaign_id": strProp("The campaign's UUID."),
		}, "campaign_id"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewCampaigns,
		RequiredAPIPerm: models.APIPermReadCampaigns,
		Handler:         d.listCampaignSenders,
	})

	r.Register(Tool{
		Name:        "set_campaign_senders",
		Description: "Replace the set of sender mailboxes for a campaign (each with an optional weight and enabled flag).",
		InputSchema: objectSchema(map[string]any{
			"campaign_id": strProp("The campaign's UUID."),
			"senders": arrProp("The full sender set to apply.", objectSchema(map[string]any{
				"email_account_id": strProp("Mailbox UUID."),
				"weight":           intProp("Relative send weight."),
				"enabled":          boolProp("Whether this sender is active."),
			}, "email_account_id")),
		}, "campaign_id", "senders"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageCampaigns,
		RequiredAPIPerm: models.APIPermWriteCampaigns,
		Handler:         d.setCampaignSenders,
	})

	r.Register(Tool{
		Name:        "verify_campaign_tracking_domain",
		Description: "Verify a campaign's custom tracking-domain CNAME.",
		InputSchema: objectSchema(map[string]any{
			"campaign_id": strProp("The campaign's UUID."),
		}, "campaign_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageCampaigns,
		RequiredAPIPerm: models.APIPermWriteCampaigns,
		Handler:         d.verifyCampaignTrackingDomain,
	})

	r.Register(Tool{
		Name:        "get_campaign_logs",
		Description: "Read a campaign's recent send/event log entries.",
		InputSchema: objectSchema(map[string]any{
			"campaign_id": strProp("The campaign's UUID."),
			"limit":       intProp("Max log rows (1-100, default 50)."),
		}, "campaign_id"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewCampaigns,
		RequiredAPIPerm: models.APIPermReadCampaigns,
		Handler:         d.getCampaignLogs,
	})
}

func (d Deps) getCampaign(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		CampaignID string `json:"campaign_id"`
	}](args)
	if err != nil {
		return "", err
	}
	if _, err := parseUUIDArg(in.CampaignID); err != nil {
		return "", err
	}
	// Get scopes by org (the handler passes the org id as the first arg).
	camp, xerr := d.Campaigns.Get(ctx, inv.OrgID.String(), in.CampaignID)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(camp)
}

func (d Deps) updateCampaign(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		CampaignID    string  `json:"campaign_id"`
		Name          *string `json:"name"`
		Description   *string `json:"description"`
		DailyLimit    *int    `json:"daily_limit"`
		StopOnReply   *bool   `json:"stop_on_reply"`
		OpenTracking  *bool   `json:"open_tracking"`
		LinkTracking  *bool   `json:"link_tracking"`
		TextOnly      *bool   `json:"text_only"`
		RampEnabled   *bool   `json:"ramp_enabled"`
		RampStart     *int    `json:"ramp_start"`
		RampIncrement *int    `json:"ramp_increment"`
		RampCeiling   *int    `json:"ramp_ceiling"`
	}](args)
	if err != nil {
		return "", err
	}
	cid, err := parseUUIDArg(in.CampaignID)
	if err != nil {
		return "", err
	}
	upd := &models.UpdateCampaign{
		Name:          in.Name,
		Description:   in.Description,
		DailyLimit:    in.DailyLimit,
		StopOnReply:   in.StopOnReply,
		OpenTracking:  in.OpenTracking,
		LinkTracking:  in.LinkTracking,
		TextOnly:      in.TextOnly,
		RampEnabled:   in.RampEnabled,
		RampStart:     in.RampStart,
		RampIncrement: in.RampIncrement,
		RampCeiling:   in.RampCeiling,
	}
	camp, xerr := d.Campaigns.Update(ctx, inv.UserID.String(), in.CampaignID, upd)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityCampaign, &cid, nil)
	return jsonResult(camp)
}

func (d Deps) deleteCampaign(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
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
	if xerr := d.Campaigns.Delete(ctx, inv.UserID.String(), in.CampaignID); xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionDelete, models.AuditEntityCampaign, &cid, nil)
	return jsonResult(map[string]any{"ok": true, "campaign_id": in.CampaignID})
}

func (d Deps) listCampaignSenders(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		CampaignID string `json:"campaign_id"`
	}](args)
	if err != nil {
		return "", err
	}
	if _, err := parseUUIDArg(in.CampaignID); err != nil {
		return "", err
	}
	senders, xerr := d.Campaigns.ListCampaignSenders(ctx, inv.OrgID, in.CampaignID)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(map[string]any{"senders": senders, "count": len(senders)})
}

func (d Deps) setCampaignSenders(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		CampaignID string `json:"campaign_id"`
		Senders    []struct {
			EmailAccountID string `json:"email_account_id"`
			Weight         *int   `json:"weight"`
			Enabled        *bool  `json:"enabled"`
		} `json:"senders"`
	}](args)
	if err != nil {
		return "", err
	}
	cid, err := parseUUIDArg(in.CampaignID)
	if err != nil {
		return "", err
	}
	inputs := make([]models.CampaignSenderInput, 0, len(in.Senders))
	for _, s := range in.Senders {
		aid, perr := parseUUIDArg(s.EmailAccountID)
		if perr != nil {
			return "", perr
		}
		inputs = append(inputs, models.CampaignSenderInput{EmailAccountID: aid, Weight: s.Weight, Enabled: s.Enabled})
	}
	senders, xerr := d.Campaigns.ReplaceCampaignSenders(ctx, inv.OrgID, in.CampaignID, inputs)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityCampaign, &cid, nil)
	return jsonResult(map[string]any{"senders": senders, "count": len(senders)})
}

func (d Deps) verifyCampaignTrackingDomain(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
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
	status, xerr := d.Campaigns.VerifyCampaignTrackingDomain(ctx, inv.OrgID, in.CampaignID)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityCampaign, &cid, nil)
	return jsonResult(status)
}

func (d Deps) getCampaignLogs(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		CampaignID string `json:"campaign_id"`
		Limit      int    `json:"limit"`
	}](args)
	if err != nil {
		return "", err
	}
	if _, err := parseUUIDArg(in.CampaignID); err != nil {
		return "", err
	}
	limit := in.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	res, xerr := d.Campaigns.GetLogs(ctx, inv.UserID.String(), in.CampaignID, limit, nil)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(res)
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

func (d Deps) listCampaignLeads(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		CampaignID string `json:"campaign_id"`
		Status     string `json:"status"`
		Limit      int    `json:"limit"`
	}](args)
	if err != nil {
		return "", err
	}
	if _, err := parseUUIDArg(in.CampaignID); err != nil {
		return "", err
	}
	if in.Status != "" && !models.ValidLeadStatus(in.Status) {
		return "", ErrInvalidArgs
	}
	limit := in.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	counts, xerr := d.Contacts.CampaignLeadCounts(ctx, inv.OrgID.String(), in.CampaignID)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	res, xerr := d.Contacts.Search(ctx, inv.OrgID.String(), "", "", fmt.Sprintf("%d", limit), models.SearchContacts{
		CampaignIDs: []string{in.CampaignID},
		LeadStatus:  in.Status,
	})
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	leads := make([]map[string]any, 0, len(res.Data))
	for _, c := range res.Data {
		row := map[string]any{
			"contact_id": c.ID.String(),
			"name":       fullName(c.FirstName, c.LastName),
			"email":      c.Email,
			"company":    c.Company,
		}
		if lp := c.CampaignLead; lp != nil {
			row["status"] = lp.Status
			row["steps_sent"] = lp.Sent
			row["opened"] = lp.Opened
			row["replied"] = lp.Replied
			row["current_step"] = lp.CurrentStep
			if lp.LastActivityAt != nil {
				row["last_activity_at"] = lp.LastActivityAt
			}
		}
		leads = append(leads, row)
	}
	return jsonResult(map[string]any{
		"counts": map[string]int{
			"total":        counts.Total,
			"pending":      counts.Queued,
			"active":       counts.Processing,
			"completed":    counts.Completed,
			"replied":      counts.Replied,
			"bounced":      counts.Bounced,
			"unsubscribed": counts.Unsubscribed,
		},
		"leads": leads,
		"count": len(leads),
	})
}

func (d Deps) setCampaignStatus(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		CampaignID string `json:"campaign_id"`
		Action     string `json:"action"`
	}](args)
	if err != nil {
		return "", err
	}
	cid, err := parseUUIDArg(in.CampaignID)
	if err != nil {
		return "", err
	}
	switch in.Action {
	case "start":
		if xerr := d.Campaigns.StartCampaign(ctx, inv.OrgID, in.CampaignID); xerr != nil {
			return "", fromErrx(xerr)
		}
		d.logAudit(ctx, inv, models.AuditActionStart, models.AuditEntityCampaign, &cid, nil)
		return jsonResult(map[string]any{"ok": true, "campaign_id": in.CampaignID, "status": "active"})
	case "stop":
		if xerr := d.Campaigns.StopCampaign(ctx, inv.OrgID, in.CampaignID); xerr != nil {
			return "", fromErrx(xerr)
		}
		d.logAudit(ctx, inv, models.AuditActionStop, models.AuditEntityCampaign, &cid, nil)
		return jsonResult(map[string]any{"ok": true, "campaign_id": in.CampaignID, "status": "paused"})
	default:
		return "", ErrInvalidArgs
	}
}

// link builds an absolute dashboard URL when AppBaseURL is set, else a relative
// path the client can resolve.
func (d Deps) link(path string) string {
	if d.AppBaseURL == "" {
		return path
	}
	return d.AppBaseURL + path
}
