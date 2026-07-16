package aitools

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

func (d Deps) registerCRMTools(r *Registry) {
	r.Register(Tool{
		Name:            "list_pipelines",
		Description:     "List CRM pipelines with their stages (ids + names). Use this to discover pipeline_id/stage_id before creating or filtering deals.",
		InputSchema:     objectSchema(map[string]any{}),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewContacts,
		RequiredAPIPerm: models.APIPermReadCRM,
		Handler:         d.listPipelines,
	})

	r.Register(Tool{
		Name:        "list_deals",
		Description: "List CRM deals, optionally filtered by pipeline and status (open, won, lost). Includes value, stage, and linked contact.",
		InputSchema: objectSchema(map[string]any{
			"pipeline_id": strProp("Optional pipeline UUID filter."),
			"status":      enumProp("Optional status filter.", "open", "won", "lost"),
			"limit":       intProp("Max deals (1-50, default 20)."),
		}),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewContacts,
		RequiredAPIPerm: models.APIPermReadCRM,
		Handler:         d.listDeals,
	})

	r.Register(Tool{
		Name:        "add_contact_note",
		Description: "Add a note to a contact's CRM timeline (call summaries, context, next steps).",
		InputSchema: objectSchema(map[string]any{
			"contact_id": strProp("The contact's UUID."),
			"body":       strProp("The note text."),
		}, "contact_id", "body"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteCRM,
		Handler:         d.addContactNote,
	})

	r.Register(Tool{
		Name:        "create_task",
		Description: "Create a CRM task, optionally linked to a contact or deal. Use for follow-ups the user asks to schedule.",
		InputSchema: objectSchema(map[string]any{
			"title":       strProp("Task title (required)."),
			"description": strProp("Optional details."),
			"contact_id":  strProp("Optional contact UUID to link."),
			"deal_id":     strProp("Optional deal UUID to link."),
			"due_date":    strProp("Optional due date, RFC3339 (e.g. 2026-07-20T09:00:00Z)."),
			"priority":    enumProp("Optional priority.", "low", "medium", "high", "urgent"),
		}, "title"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteCRM,
		Handler:         d.createTask,
	})

	r.Register(Tool{
		Name:        "create_deal",
		Description: "Create a CRM deal in a pipeline stage, optionally linked to a contact. pipeline_id and stage_id are required; discover them from existing deals or the CRM.",
		InputSchema: objectSchema(map[string]any{
			"name":        strProp("Deal name (required)."),
			"pipeline_id": strProp("Pipeline UUID (required)."),
			"stage_id":    strProp("Stage UUID within the pipeline (required)."),
			"contact_id":  strProp("Optional contact UUID to link."),
			"value":       map[string]any{"type": "number", "description": "Optional deal value."},
			"currency":    strProp("Optional ISO currency code, e.g. USD."),
		}, "name", "pipeline_id", "stage_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteCRM,
		Handler:         d.createDeal,
	})
}

func (d Deps) listPipelines(ctx context.Context, inv Invocation, _ json.RawMessage) (string, error) {
	pipelines, xerr := d.CRM.ListPipelines(ctx, inv.OrgID)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	out := make([]map[string]any, 0, len(pipelines))
	for _, p := range pipelines {
		stages := make([]map[string]any, 0, len(p.Stages))
		for _, s := range p.Stages {
			stages = append(stages, map[string]any{"stage_id": s.ID.String(), "name": s.Name, "deal_count": s.DealCount})
		}
		out = append(out, map[string]any{"pipeline_id": p.ID.String(), "name": p.Name, "stages": stages})
	}
	return jsonResult(map[string]any{"pipelines": out, "count": len(out)})
}

func (d Deps) listDeals(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		PipelineID string `json:"pipeline_id"`
		Status     string `json:"status"`
		Limit      int    `json:"limit"`
	}](args)
	if err != nil {
		return "", err
	}
	limit := in.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	var pipelineID *uuid.UUID
	if in.PipelineID != "" {
		pid, err := parseUUIDArg(in.PipelineID)
		if err != nil {
			return "", err
		}
		pipelineID = &pid
	}
	var status *string
	if in.Status != "" {
		status = &in.Status
	}
	res, xerr := d.CRM.ListDeals(ctx, inv.OrgID, pipelineID, nil, status, limit, nil)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	out := make([]map[string]any, 0, len(res.Data))
	for _, dl := range res.Data {
		row := map[string]any{
			"deal_id":     dl.ID.String(),
			"name":        dl.Name,
			"status":      dl.Status,
			"pipeline_id": dl.PipelineID.String(),
			"stage_id":    dl.StageID.String(),
			"currency":    dl.Currency,
		}
		if dl.Value != nil {
			row["value"] = *dl.Value
		}
		if dl.ContactID != nil {
			row["contact_id"] = dl.ContactID.String()
		}
		out = append(out, row)
	}
	return jsonResult(map[string]any{"deals": out, "count": len(out)})
}

func (d Deps) addContactNote(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		ContactID string `json:"contact_id"`
		Body      string `json:"body"`
	}](args)
	if err != nil {
		return "", err
	}
	cid, err := parseUUIDArg(in.ContactID)
	if err != nil {
		return "", err
	}
	if in.Body == "" {
		return "", ErrInvalidArgs
	}
	note, xerr := d.CRM.CreateNote(ctx, inv.OrgID, cid, inv.UserID, &models.CreateContactNote{Content: in.Body})
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionCreate, models.AuditEntityCRMNote, &note.ID, nil)
	return jsonResult(map[string]any{"ok": true, "note_id": note.ID.String()})
}

func (d Deps) createTask(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		Title       string  `json:"title"`
		Description *string `json:"description"`
		ContactID   string  `json:"contact_id"`
		DealID      string  `json:"deal_id"`
		DueDate     string  `json:"due_date"`
		Priority    string  `json:"priority"`
	}](args)
	if err != nil {
		return "", err
	}
	if in.Title == "" {
		return "", ErrInvalidArgs
	}

	req := &models.CreateCRMTask{Title: in.Title, Description: in.Description, Priority: in.Priority}
	if in.ContactID != "" {
		cid, err := parseUUIDArg(in.ContactID)
		if err != nil {
			return "", err
		}
		req.ContactID = &cid
	}
	if in.DealID != "" {
		did, err := parseUUIDArg(in.DealID)
		if err != nil {
			return "", err
		}
		req.DealID = &did
	}
	if in.DueDate != "" {
		t, perr := time.Parse(time.RFC3339, in.DueDate)
		if perr != nil {
			return "", ErrInvalidArgs
		}
		req.DueDate = &t
	}

	task, xerr := d.CRM.CreateCRMTask(ctx, inv.OrgID, inv.UserID, req)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionCreate, models.AuditEntityCRMTask, &task.ID, map[string]string{"title": task.Title})
	return jsonResult(map[string]any{"ok": true, "task_id": task.ID.String()})
}

func (d Deps) createDeal(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		Name       string   `json:"name"`
		PipelineID string   `json:"pipeline_id"`
		StageID    string   `json:"stage_id"`
		ContactID  string   `json:"contact_id"`
		Value      *float64 `json:"value"`
		Currency   string   `json:"currency"`
	}](args)
	if err != nil {
		return "", err
	}
	if in.Name == "" {
		return "", ErrInvalidArgs
	}
	pid, err := parseUUIDArg(in.PipelineID)
	if err != nil {
		return "", err
	}
	sid, err := parseUUIDArg(in.StageID)
	if err != nil {
		return "", err
	}

	req := &models.CreateDeal{
		PipelineID: pid,
		StageID:    sid,
		Name:       in.Name,
		Value:      in.Value,
		Currency:   in.Currency,
	}
	if in.ContactID != "" {
		var cid uuid.UUID
		cid, err = parseUUIDArg(in.ContactID)
		if err != nil {
			return "", err
		}
		req.ContactID = &cid
	}

	deal, xerr := d.CRM.CreateDeal(ctx, inv.OrgID, req)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionCreate, models.AuditEntityCRMDeal, &deal.ID, map[string]string{"name": deal.Name})
	return jsonResult(map[string]any{"ok": true, "deal_id": deal.ID.String()})
}
