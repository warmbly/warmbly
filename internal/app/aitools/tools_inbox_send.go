package aitools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/emailsend"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// Inbox send tools. These are the ONLY tools that transmit mail to a real
// external recipient, so they are RiskSend: the agent loop always pauses for
// explicit per-action approval and they can never be pre-approved (always_allow
// only elevates RiskWrite). They mirror the /unibox/reply and /unibox/compose
// handlers, including the suppression guard.
func (d Deps) registerInboxSendTools(r *Registry) {
	r.Register(Tool{
		Name:        "send_reply",
		Description: "Send a reply into an existing inbox thread from one of your mailboxes. This actually sends email to the recipient. The recipient and subject are resolved from the thread; the sending mailbox is auto-picked (the one already in the conversation) unless account_id is given.",
		InputSchema: objectSchema(map[string]any{
			"thread_id":  strProp("The thread to reply within."),
			"body":       strProp("The reply body (plain text)."),
			"body_html":  strProp("Optional HTML body."),
			"account_id": strProp("Optional mailbox UUID to send from; omit to auto-pick the mailbox in the conversation."),
		}, "thread_id", "body"),
		Risk:            generation.RiskSend,
		RequiredOrgPerm: models.PermAccessUnibox,
		RequiredAPIPerm: models.APIPermWriteUnibox,
		Handler:         d.sendReply,
	})

	r.Register(Tool{
		Name:        "compose_email",
		Description: "Send a brand-new email to a recipient from one of your mailboxes. This actually sends email. The sending mailbox is auto-picked unless account_id (or from_tag_id) is given. Suppressed recipients are refused.",
		InputSchema: objectSchema(map[string]any{
			"to":          strProp("Recipient email address."),
			"subject":     strProp("Email subject."),
			"body":        strProp("Email body (plain text)."),
			"body_html":   strProp("Optional HTML body."),
			"account_id":  strProp("Optional mailbox UUID to send from."),
			"from_tag_id": strProp("Optional mailbox-tag UUID to auto-pick a sender within that tag."),
		}, "to", "subject", "body"),
		Risk:            generation.RiskSend,
		RequiredOrgPerm: models.PermAccessUnibox,
		RequiredAPIPerm: models.APIPermWriteUnibox,
		Handler:         d.composeEmail,
	})
}

func (d Deps) sendReply(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	if err := d.requireUnibox(ctx, inv); err != nil {
		return "", err
	}
	in, err := decodeArgs[struct {
		ThreadID  string `json:"thread_id"`
		Body      string `json:"body"`
		BodyHTML  string `json:"body_html"`
		AccountID string `json:"account_id"`
	}](args)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(in.ThreadID) == "" || strings.TrimSpace(in.Body) == "" {
		return "", ErrInvalidArgs
	}

	// Resolve the reply target from the thread's latest message.
	res, xerr := d.Unibox.GetByThread(ctx, inv.OrgID, uuid.Nil, in.ThreadID, "1", "")
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	if len(res.Data) == 0 {
		return "", ErrInvalidArgs
	}
	latest := res.Data[0]
	to := firstNonEmpty(latest.FromAddr)
	if to == "" {
		return "", ErrInvalidArgs
	}
	if err := d.assertNotSuppressed(ctx, inv, to); err != nil {
		return "", err
	}

	accountID, err := d.resolveSender(ctx, inv, in.AccountID, "", to)
	if err != nil {
		return "", err
	}

	sendReq := &emailsend.SendEmailRequest{
		To:        []string{to},
		Subject:   replySubject(latest.Subject),
		BodyHTML:  in.BodyHTML,
		BodyPlain: in.Body,
		ThreadID:  in.ThreadID,
		SendMode:  "instant",
	}
	// Best-effort: pull the original Message-ID so the reply threads via
	// In-Reply-To. The thread preview does not carry it, so fetch the message.
	if full, ferr := d.Unibox.GetByID(ctx, inv.OrgID, latest.ID); ferr == nil && full != nil && full.MessageID != "" {
		sendReq.InReplyTo = []string{full.MessageID}
	}
	resp, xerr := d.EmailSend.SendEmail(ctx, inv.UserID, inv.OrgID, accountID, sendReq)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionSend, models.AuditEntityUnibox, &accountID, map[string]string{"thread_id": in.ThreadID})
	return jsonResult(map[string]any{"ok": true, "task_id": resp.TaskID.String(), "to": to, "account_id": accountID.String()})
}

func (d Deps) composeEmail(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	if err := d.requireUnibox(ctx, inv); err != nil {
		return "", err
	}
	in, err := decodeArgs[struct {
		To        string `json:"to"`
		Subject   string `json:"subject"`
		Body      string `json:"body"`
		BodyHTML  string `json:"body_html"`
		AccountID string `json:"account_id"`
		FromTagID string `json:"from_tag_id"`
	}](args)
	if err != nil {
		return "", err
	}
	to := strings.TrimSpace(in.To)
	if to == "" || strings.TrimSpace(in.Subject) == "" || strings.TrimSpace(in.Body) == "" {
		return "", ErrInvalidArgs
	}
	if err := d.assertNotSuppressed(ctx, inv, to); err != nil {
		return "", err
	}

	accountID, err := d.resolveSender(ctx, inv, in.AccountID, in.FromTagID, to)
	if err != nil {
		return "", err
	}

	resp, xerr := d.EmailSend.SendEmail(ctx, inv.UserID, inv.OrgID, accountID, &emailsend.SendEmailRequest{
		To:        []string{to},
		Subject:   in.Subject,
		BodyHTML:  in.BodyHTML,
		BodyPlain: in.Body,
		SendMode:  "instant",
	})
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionSend, models.AuditEntityUnibox, &accountID, map[string]string{"to": bareEmail(to)})
	return jsonResult(map[string]any{"ok": true, "task_id": resp.TaskID.String(), "account_id": accountID.String()})
}

// resolveSender picks the sending mailbox: the explicit accountID when given,
// otherwise the best compose candidate for the recipient (auto-pick within a
// tag when tagID is set). Mirrors the compose handler.
func (d Deps) resolveSender(ctx context.Context, inv Invocation, accountID, tagID, recipient string) (uuid.UUID, error) {
	if strings.TrimSpace(accountID) != "" && accountID != "auto" {
		id, err := parseUUIDArg(accountID)
		if err != nil {
			return uuid.Nil, err
		}
		cand, _, xerr := d.Compose.Resolve(ctx, inv.UserID, inv.OrgID, &id, nil, bareEmail(recipient))
		if xerr != nil {
			return uuid.Nil, fromErrx(xerr)
		}
		return cand.Account.ID, nil
	}
	var tag *uuid.UUID
	if strings.TrimSpace(tagID) != "" {
		id, err := parseUUIDArg(tagID)
		if err != nil {
			return uuid.Nil, err
		}
		tag = &id
	}
	cand, _, xerr := d.Compose.Resolve(ctx, inv.UserID, inv.OrgID, nil, tag, bareEmail(recipient))
	if xerr != nil {
		return uuid.Nil, fromErrx(xerr)
	}
	return cand.Account.ID, nil
}

// assertNotSuppressed refuses to send to a recipient the org suppressed
// (bounced/complained/unsubscribed), mirroring the compose handler's guard. A
// nil Advanced checker skips the gate.
func (d Deps) assertNotSuppressed(ctx context.Context, inv Invocation, recipient string) error {
	if d.Advanced == nil {
		return nil
	}
	addr := bareEmail(recipient)
	if addr == "" {
		return nil
	}
	suppressed, reason, xerr := d.Advanced.ShouldSuppressRecipient(ctx, inv.OrgID, addr)
	if xerr != nil {
		return fromErrx(xerr)
	}
	if suppressed {
		if reason != "" {
			return errRecipientSuppressed(addr + " is suppressed (" + reason + ")")
		}
		return errRecipientSuppressed(addr + " is suppressed for this workspace")
	}
	return nil
}

// bareEmail extracts the address from a "Name <addr>" form (or returns the
// trimmed input), lowercased.
func bareEmail(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "<"); i >= 0 {
		if j := strings.Index(s[i:], ">"); j >= 0 {
			s = s[i+1 : i+j]
		}
	}
	return strings.ToLower(strings.TrimSpace(s))
}

// firstNonEmpty returns the first non-empty address across the given lists.
func firstNonEmpty(lists ...[]string) string {
	for _, l := range lists {
		for _, v := range l {
			if strings.TrimSpace(v) != "" {
				return v
			}
		}
	}
	return ""
}
