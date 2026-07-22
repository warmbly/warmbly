package aitools

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// Unified-inbox action tools (no outbound send). Each reuses requireUnibox so a
// tool can never touch inbox data an org without an active trial/paid plan would
// be refused, and runs under the caller's user/org.
func (d Deps) registerInboxActionTools(r *Registry) {
	r.Register(Tool{
		Name:        "mark_thread_seen",
		Description: "Mark all messages in a thread seen (or unseen).",
		InputSchema: objectSchema(map[string]any{
			"thread_id": strProp("The thread id."),
			"seen":      boolProp("true to mark seen (default), false to mark unseen."),
		}, "thread_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermAccessUnibox,
		RequiredAPIPerm: models.APIPermWriteUnibox,
		Handler:         d.markThreadSeen,
	})

	r.Register(Tool{
		Name:        "set_thread_labels",
		Description: "Replace a conversation thread's label (category) set. Pass the full desired set; an empty list clears labels.",
		InputSchema: objectSchema(map[string]any{
			"thread_id":    strProp("The thread id."),
			"category_ids": arrProp("Category (label) UUIDs to apply.", strProp("Category UUID.")),
		}, "thread_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermAccessUnibox,
		RequiredAPIPerm: models.APIPermWriteUnibox,
		Handler:         d.setThreadLabels,
	})

	r.Register(Tool{
		Name:        "snooze_thread",
		Description: "Hide a thread from the inbox until a given time.",
		InputSchema: objectSchema(map[string]any{
			"thread_id":     strProp("The thread id."),
			"snoozed_until": strProp("When the thread should reappear, RFC3339 (e.g. 2026-07-25T09:00:00Z)."),
		}, "thread_id", "snoozed_until"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermAccessUnibox,
		RequiredAPIPerm: models.APIPermWriteUnibox,
		Handler:         d.snoozeThread,
	})

	r.Register(Tool{
		Name:        "unsnooze_thread",
		Description: "Remove a thread's snooze so it returns to the inbox now.",
		InputSchema: objectSchema(map[string]any{
			"thread_id": strProp("The thread id."),
		}, "thread_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermAccessUnibox,
		RequiredAPIPerm: models.APIPermWriteUnibox,
		Handler:         d.unsnoozeThread,
	})

	r.Register(Tool{
		Name:            "list_scheduled_sends",
		Description:     "List the user's queued (not-yet-sent) outbound messages, with their task ids.",
		InputSchema:     objectSchema(map[string]any{}),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermAccessUnibox,
		RequiredAPIPerm: models.APIPermReadUnibox,
		Handler:         d.listScheduledSends,
	})

	r.Register(Tool{
		Name:        "cancel_scheduled_send",
		Description: "Cancel a queued outbound message before it sends.",
		InputSchema: objectSchema(map[string]any{
			"task_id": strProp("The scheduled send's task id (from list_scheduled_sends)."),
		}, "task_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermAccessUnibox,
		RequiredAPIPerm: models.APIPermWriteUnibox,
		Handler:         d.cancelScheduledSend,
	})
}

func (d Deps) markThreadSeen(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	if err := d.requireUnibox(ctx, inv); err != nil {
		return "", err
	}
	in, err := decodeArgs[struct {
		ThreadID string `json:"thread_id"`
		Seen     *bool  `json:"seen"`
	}](args)
	if err != nil {
		return "", err
	}
	if in.ThreadID == "" {
		return "", ErrInvalidArgs
	}
	res, xerr := d.Unibox.GetByThread(ctx, inv.OrgID, uuid.Nil, in.ThreadID, "100", "")
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	ids := make([]uuid.UUID, 0, len(res.Data))
	for _, m := range res.Data {
		if m.ID != uuid.Nil {
			ids = append(ids, m.ID)
		}
	}
	if len(ids) == 0 {
		return jsonResult(map[string]any{"ok": true, "updated": 0})
	}
	seen := true
	if in.Seen != nil {
		seen = *in.Seen
	}
	if _, xerr := d.Unibox.MarkSeenBulk(ctx, inv.OrgID, &models.MarkSeen{EmailIDs: ids, Seen: seen}); xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(map[string]any{"ok": true, "updated": len(ids), "seen": seen})
}

func (d Deps) setThreadLabels(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	if err := d.requireUnibox(ctx, inv); err != nil {
		return "", err
	}
	in, err := decodeArgs[struct {
		ThreadID    string   `json:"thread_id"`
		CategoryIDs []string `json:"category_ids"`
	}](args)
	if err != nil {
		return "", err
	}
	if in.ThreadID == "" {
		return "", ErrInvalidArgs
	}
	catIDs := make([]uuid.UUID, 0, len(in.CategoryIDs))
	for _, c := range in.CategoryIDs {
		id, perr := parseUUIDArg(c)
		if perr != nil {
			return "", perr
		}
		catIDs = append(catIDs, id)
	}
	labels, xerr := d.Unibox.SetThreadLabels(ctx, inv.UserID, in.ThreadID, catIDs)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityUnibox, nil, nil)
	return jsonResult(map[string]any{"ok": true, "labels": labels})
}

func (d Deps) snoozeThread(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	if err := d.requireUnibox(ctx, inv); err != nil {
		return "", err
	}
	in, err := decodeArgs[struct {
		ThreadID     string `json:"thread_id"`
		SnoozedUntil string `json:"snoozed_until"`
	}](args)
	if err != nil {
		return "", err
	}
	if in.ThreadID == "" {
		return "", ErrInvalidArgs
	}
	until, perr := time.Parse(time.RFC3339, in.SnoozedUntil)
	if perr != nil {
		return "", ErrInvalidArgs
	}
	snooze, xerr := d.Unibox.Snooze(ctx, inv.UserID, in.ThreadID, until)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityUnibox, nil, nil)
	return jsonResult(snooze)
}

func (d Deps) unsnoozeThread(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	if err := d.requireUnibox(ctx, inv); err != nil {
		return "", err
	}
	in, err := decodeArgs[struct {
		ThreadID string `json:"thread_id"`
	}](args)
	if err != nil {
		return "", err
	}
	if in.ThreadID == "" {
		return "", ErrInvalidArgs
	}
	if xerr := d.Unibox.Unsnooze(ctx, inv.UserID, in.ThreadID); xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityUnibox, nil, nil)
	return jsonResult(map[string]any{"ok": true, "thread_id": in.ThreadID})
}

func (d Deps) listScheduledSends(ctx context.Context, inv Invocation, _ json.RawMessage) (string, error) {
	if err := d.requireUnibox(ctx, inv); err != nil {
		return "", err
	}
	items, xerr := d.Unibox.ListScheduled(ctx, inv.UserID)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(map[string]any{"scheduled": items, "count": len(items)})
}

func (d Deps) cancelScheduledSend(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	if err := d.requireUnibox(ctx, inv); err != nil {
		return "", err
	}
	in, err := decodeArgs[struct {
		TaskID string `json:"task_id"`
	}](args)
	if err != nil {
		return "", err
	}
	tid, err := parseUUIDArg(in.TaskID)
	if err != nil {
		return "", err
	}
	if xerr := d.Unibox.CancelScheduled(ctx, inv.UserID, tid); xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityUnibox, &tid, nil)
	return jsonResult(map[string]any{"ok": true, "task_id": tid.String()})
}
