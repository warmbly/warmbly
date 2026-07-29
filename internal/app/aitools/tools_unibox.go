package aitools

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

func (d Deps) registerUniboxTools(r *Registry) {
	r.Register(Tool{
		Name:        "list_threads",
		Description: "List unified-inbox conversation threads (received/synced mail), newest first, with optional filters. The inbox only contains synced mailbox mail; to find leads that never replied or went cold, use list_campaign_leads instead. If a filtered call returns 0, retry without filters before concluding the inbox is empty.",
		InputSchema: objectSchema(map[string]any{
			"subject":        strProp("Optional subject contains filter."),
			"sender":         strProp("Optional sender email filter."),
			"unseen_only":    boolProp("Only threads with unread messages."),
			"awaiting_reply": boolProp("Only threads whose LATEST message was sent by one of our mailboxes (we spoke last, still waiting on them)."),
			"limit":          intProp("Max threads (1-50, default 20)."),
		}),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermAccessUnibox,
		RequiredAPIPerm: models.APIPermReadUnibox,
		Handler:         d.listThreads,
	})

	r.Register(Tool{
		Name:        "get_thread",
		Description: "Get the messages in one conversation thread (subject, participants, snippet, date per message).",
		InputSchema: objectSchema(map[string]any{
			"thread_id": strProp("The thread id."),
			"limit":     intProp("Max messages (default 20)."),
		}, "thread_id"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermAccessUnibox,
		RequiredAPIPerm: models.APIPermReadUnibox,
		Handler:         d.getThread,
	})

	r.Register(Tool{
		Name:        "draft_reply",
		Description: "Compose a reply DRAFT for a thread. This does NOT send anything: it returns a draft the user reviews and sends themselves. Resolves the recipient and subject from the thread.",
		InputSchema: objectSchema(map[string]any{
			"thread_id": strProp("The thread id to reply within."),
			"body":      strProp("The drafted reply body text."),
		}, "thread_id", "body"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermAccessUnibox,
		RequiredAPIPerm: models.APIPermReadUnibox,
		Handler:         d.draftReply,
	})
}

// requireUnibox applies the same subscription entitlement gate the HTTP unibox
// handlers enforce, so a tool cannot read inbox data an org without an active
// trial/paid plan would be 403'd from.
func (d Deps) requireUnibox(ctx context.Context, inv Invocation) error {
	if d.FeatureGate == nil {
		return nil
	}
	ok, xerr := d.FeatureGate.CanUseUnibox(ctx, inv.OrgID)
	if xerr != nil {
		return fromErrx(xerr)
	}
	if !ok {
		return errUniboxNotEntitled
	}
	return nil
}

func (d Deps) listThreads(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	if err := d.requireUnibox(ctx, inv); err != nil {
		return "", err
	}
	in, err := decodeArgs[struct {
		Subject       string `json:"subject"`
		Sender        string `json:"sender"`
		UnseenOnly    bool   `json:"unseen_only"`
		AwaitingReply bool   `json:"awaiting_reply"`
		Limit         int    `json:"limit"`
	}](args)
	if err != nil {
		return "", err
	}
	limit := in.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	params := &models.MailSearchParams{PageSize: limit}
	if in.Subject != "" {
		params.Subject = &in.Subject
	}
	if in.Sender != "" {
		params.Sender = &in.Sender
	}
	if in.UnseenOnly {
		t := true
		params.Unseen = &t
	}
	if in.AwaitingReply {
		t := true
		params.AwaitingReply = &t
	}

	res, xerr := d.Unibox.Search(ctx, inv.OrgID, inv.UserID, params)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	out := make([]map[string]any, 0, len(res.Data))
	for _, m := range res.Data {
		out = append(out, map[string]any{
			"thread_id":     m.ThreadID,
			"subject":       m.Subject,
			"from":          strings.Join(m.FromAddr, ", "),
			"snippet":       m.Snippet,
			"message_count": m.MessageCount,
			"has_unread":    m.HasUnread,
			"date":          m.InternalDate,
		})
	}
	return jsonResult(map[string]any{"threads": out, "count": len(out)})
}

func (d Deps) getThread(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	if err := d.requireUnibox(ctx, inv); err != nil {
		return "", err
	}
	in, err := decodeArgs[struct {
		ThreadID string `json:"thread_id"`
		Limit    int    `json:"limit"`
	}](args)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(in.ThreadID) == "" {
		return "", ErrInvalidArgs
	}
	limit := in.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	res, xerr := d.Unibox.GetByThread(ctx, inv.OrgID, uuid.Nil, in.ThreadID, strconv.Itoa(limit), "")
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	out := make([]map[string]any, 0, len(res.Data))
	for _, m := range res.Data {
		out = append(out, map[string]any{
			"from":    strings.Join(m.FromAddr, ", "),
			"to":      strings.Join(m.ToAddr, ", "),
			"subject": m.Subject,
			"snippet": m.Snippet,
			"date":    m.InternalDate,
			"seen":    m.Seen,
		})
	}
	return jsonResult(map[string]any{"thread_id": in.ThreadID, "messages": out})
}

func (d Deps) draftReply(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	if err := d.requireUnibox(ctx, inv); err != nil {
		return "", err
	}
	in, err := decodeArgs[struct {
		ThreadID string `json:"thread_id"`
		Body     string `json:"body"`
	}](args)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(in.ThreadID) == "" || strings.TrimSpace(in.Body) == "" {
		return "", ErrInvalidArgs
	}

	// Resolve the reply target (recipient + subject) from the thread's latest
	// message. This never sends: the result is a draft the human sends.
	res, xerr := d.Unibox.GetByThread(ctx, inv.OrgID, uuid.Nil, in.ThreadID, "1", "")
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	to := ""
	subject := ""
	if len(res.Data) > 0 {
		latest := res.Data[0]
		if len(latest.FromAddr) > 0 {
			to = latest.FromAddr[0]
		}
		subject = replySubject(latest.Subject)
	}
	return jsonResult(map[string]any{
		"draft": map[string]any{
			"thread_id": in.ThreadID,
			"to":        to,
			"subject":   subject,
			"body":      in.Body,
		},
		"note": "This is a draft only. It has not been sent; the user must review and send it.",
	})
}

// replySubject prefixes "Re: " unless already present.
func replySubject(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "Re:"
	}
	if strings.HasPrefix(strings.ToLower(s), "re:") {
		return s
	}
	return "Re: " + s
}
