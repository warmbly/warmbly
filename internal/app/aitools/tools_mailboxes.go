package aitools

import (
	"context"
	"encoding/json"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// Mailbox (email-account) management tools. Reads run as the user over their
// mailboxes; writes require PermManageEmails. Connecting a mailbox (OAuth /
// SMTP+IMAP onboarding) is deliberately NOT exposed: it is an interactive
// credential flow with no API-permission gate, so it stays a human action.
func (d Deps) registerMailboxTools(r *Registry) {
	if d.Emails == nil {
		return
	}

	r.Register(Tool{
		Name:        "get_mailbox",
		Description: "Get one sender mailbox's full configuration (status, sending limits, warmup settings, signature).",
		InputSchema: objectSchema(map[string]any{
			"email_account_id": strProp("The mailbox UUID (from list_mailboxes)."),
		}, "email_account_id"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewCampaigns,
		RequiredAPIPerm: models.APIPermReadEmails,
		Handler:         d.getMailbox,
	})

	r.Register(Tool{
		Name:        "update_mailbox",
		Description: "Update a mailbox's settings: display name, reply-to, cold-send cap (campaign_limit), minimum gap between sends, status, and warmup parameters. Only provided fields change.",
		InputSchema: objectSchema(map[string]any{
			"email_account_id":  strProp("The mailbox UUID."),
			"name":              strProp("Display name."),
			"reply_to":          strProp("Reply-to address."),
			"status":            enumProp("Mailbox status.", "active", "inactive"),
			"campaign_limit":    intProp("Max cold-campaign emails per day for this mailbox."),
			"min_wait_time":     intProp("Minimum seconds between sends."),
			"warmup":            boolProp("Enable or disable warmup."),
			"warmup_base":       intProp("Warmup starting emails/day."),
			"warmup_max":        intProp("Warmup ceiling emails/day."),
			"warmup_increase":   intProp("Warmup daily ramp increment."),
			"warmup_reply_rate": intProp("Warmup reply rate percent."),
			"warmup_days":       intProp("Warmup active days bitmask."),
		}, "email_account_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageEmails,
		RequiredAPIPerm: models.APIPermWriteEmails,
		Handler:         d.updateMailbox,
	})

	r.Register(Tool{
		Name:        "set_mailbox_warmup",
		Description: "Start, pause, resume, or stop warmup on a mailbox. start/resume preserve ramp progress; stop disables warmup.",
		InputSchema: objectSchema(map[string]any{
			"email_account_id": strProp("The mailbox UUID."),
			"action":           enumProp("Warmup lifecycle action.", "start", "pause", "resume", "stop"),
		}, "email_account_id", "action"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageEmails,
		RequiredAPIPerm: models.APIPermWriteEmails,
		Handler:         d.setMailboxWarmup,
	})

	r.Register(Tool{
		Name:        "set_mailbox_tracking_domain",
		Description: "Set (and verify) a mailbox's custom tracking domain via its CNAME.",
		InputSchema: objectSchema(map[string]any{
			"email_account_id": strProp("The mailbox UUID."),
			"domain":           strProp("The custom tracking domain to use."),
		}, "email_account_id", "domain"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageEmails,
		RequiredAPIPerm: models.APIPermWriteEmails,
		Handler:         d.setMailboxTrackingDomain,
	})

	r.Register(Tool{
		Name:        "get_warmup_ban_status",
		Description: "Read a mailbox's warmup pool standing (healthy, watched, throttled, quarantined, or blocked) and any ban reason.",
		InputSchema: objectSchema(map[string]any{
			"email_account_id": strProp("The mailbox UUID."),
		}, "email_account_id"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewCampaigns,
		RequiredAPIPerm: models.APIPermReadEmails,
		Handler:         d.getWarmupBanStatus,
	})

	r.Register(Tool{
		Name:        "submit_warmup_appeal",
		Description: "Appeal a mailbox's warmup pool ban with a reason.",
		InputSchema: objectSchema(map[string]any{
			"email_account_id": strProp("The mailbox UUID."),
			"reason":           strProp("Why the ban should be lifted."),
		}, "email_account_id", "reason"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageEmails,
		RequiredAPIPerm: models.APIPermWriteEmails,
		Handler:         d.submitWarmupAppeal,
	})

	r.Register(Tool{
		Name:        "disconnect_mailbox",
		Description: "Disconnect (remove) a mailbox from the workspace. Destructive; stops all sending from it and requires user approval.",
		InputSchema: objectSchema(map[string]any{
			"email_account_id": strProp("The mailbox UUID."),
		}, "email_account_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageEmails,
		RequiredAPIPerm: models.APIPermWriteEmails,
		Handler:         d.disconnectMailbox,
	})
}

func (d Deps) getMailbox(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		EmailAccountID string `json:"email_account_id"`
	}](args)
	if err != nil {
		return "", err
	}
	if _, err := parseUUIDArg(in.EmailAccountID); err != nil {
		return "", err
	}
	mb, xerr := d.Emails.Get(ctx, inv.UserID.String(), in.EmailAccountID)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(mb)
}

func (d Deps) updateMailbox(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		EmailAccountID  string  `json:"email_account_id"`
		Name            *string `json:"name"`
		ReplyTo         *string `json:"reply_to"`
		Status          *string `json:"status"`
		CampaignLimit   *int    `json:"campaign_limit"`
		MinWaitTime     *int    `json:"min_wait_time"`
		Warmup          *bool   `json:"warmup"`
		WarmupBase      *int    `json:"warmup_base"`
		WarmupMax       *int    `json:"warmup_max"`
		WarmupIncrease  *int    `json:"warmup_increase"`
		WarmupReplyRate *int    `json:"warmup_reply_rate"`
		WarmupDays      *int    `json:"warmup_days"`
	}](args)
	if err != nil {
		return "", err
	}
	aid, err := parseUUIDArg(in.EmailAccountID)
	if err != nil {
		return "", err
	}
	upd := &models.UpdateEmail{
		Name:            in.Name,
		ReplyTo:         in.ReplyTo,
		Status:          in.Status,
		CampaignLimit:   in.CampaignLimit,
		MinWaitTime:     in.MinWaitTime,
		Warmup:          in.Warmup,
		WarmupBase:      in.WarmupBase,
		WarmupMax:       in.WarmupMax,
		WarmupIncrease:  in.WarmupIncrease,
		WarmupReplyRate: in.WarmupReplyRate,
		WarmupDays:      in.WarmupDays,
	}
	mb, xerr := d.Emails.Update(ctx, inv.UserID.String(), in.EmailAccountID, upd)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityEmailAccount, &aid, nil)
	return jsonResult(mb)
}

func (d Deps) setMailboxWarmup(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		EmailAccountID string `json:"email_account_id"`
		Action         string `json:"action"`
	}](args)
	if err != nil {
		return "", err
	}
	aid, err := parseUUIDArg(in.EmailAccountID)
	if err != nil {
		return "", err
	}
	var action models.AuditAction
	switch in.Action {
	case "start":
		action = models.AuditActionStart
	case "pause":
		action = models.AuditActionPause
	case "resume":
		action = models.AuditActionResume
	case "stop":
		action = models.AuditActionStop
	default:
		return "", ErrInvalidArgs
	}
	mb, xerr := d.Emails.SetWarmupLifecycle(ctx, inv.UserID.String(), in.EmailAccountID, in.Action)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, action, models.AuditEntityEmailAccount, &aid, map[string]string{"warmup": in.Action})
	return jsonResult(mb)
}

func (d Deps) setMailboxTrackingDomain(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		EmailAccountID string `json:"email_account_id"`
		Domain         string `json:"domain"`
	}](args)
	if err != nil {
		return "", err
	}
	aid, err := parseUUIDArg(in.EmailAccountID)
	if err != nil {
		return "", err
	}
	if in.Domain == "" {
		return "", ErrInvalidArgs
	}
	status, xerr := d.Emails.UpdateTrackingDomain(ctx, inv.UserID.String(), in.EmailAccountID, in.Domain)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityEmailAccount, &aid, nil)
	return jsonResult(status)
}

func (d Deps) getWarmupBanStatus(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	if d.Warmup == nil {
		return "", ErrToolNotFound
	}
	in, err := decodeArgs[struct {
		EmailAccountID string `json:"email_account_id"`
	}](args)
	if err != nil {
		return "", err
	}
	aid, err := parseUUIDArg(in.EmailAccountID)
	if err != nil {
		return "", err
	}
	status, xerr := d.Warmup.GetBanStatus(ctx, inv.UserID, aid)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(status)
}

func (d Deps) submitWarmupAppeal(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	if d.Warmup == nil {
		return "", ErrToolNotFound
	}
	in, err := decodeArgs[struct {
		EmailAccountID string `json:"email_account_id"`
		Reason         string `json:"reason"`
	}](args)
	if err != nil {
		return "", err
	}
	aid, err := parseUUIDArg(in.EmailAccountID)
	if err != nil {
		return "", err
	}
	if in.Reason == "" {
		return "", ErrInvalidArgs
	}
	appealID, xerr := d.Warmup.SubmitAppeal(ctx, inv.UserID, aid, in.Reason)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityEmailAccount, &aid, nil)
	return jsonResult(map[string]any{"ok": true, "appeal_id": appealID.String()})
}

func (d Deps) disconnectMailbox(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		EmailAccountID string `json:"email_account_id"`
	}](args)
	if err != nil {
		return "", err
	}
	aid, err := parseUUIDArg(in.EmailAccountID)
	if err != nil {
		return "", err
	}
	if xerr := d.Emails.Delete(ctx, inv.UserID.String(), in.EmailAccountID); xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionDisconnect, models.AuditEntityEmailAccount, &aid, nil)
	return jsonResult(map[string]any{"ok": true, "email_account_id": in.EmailAccountID})
}
