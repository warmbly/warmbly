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
