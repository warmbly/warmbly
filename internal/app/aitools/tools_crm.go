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

	r.Register(Tool{
		Name:        "get_deal",
		Description: "Get one CRM deal by id (name, value, stage, status, linked contact).",
		InputSchema: objectSchema(map[string]any{
			"deal_id": strProp("The deal's UUID."),
		}, "deal_id"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewContacts,
		RequiredAPIPerm: models.APIPermReadCRM,
		Handler:         d.getDeal,
	})

	r.Register(Tool{
		Name:        "list_deals_by_contact",
		Description: "List the CRM deals linked to a contact.",
		InputSchema: objectSchema(map[string]any{
			"contact_id": strProp("The contact's UUID."),
		}, "contact_id"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewContacts,
		RequiredAPIPerm: models.APIPermReadCRM,
		Handler:         d.listDealsByContact,
	})

	r.Register(Tool{
		Name:        "update_deal",
		Description: "Update a deal's name, value, currency, status (open/won/lost), or stage. Only provided fields change.",
		InputSchema: objectSchema(map[string]any{
			"deal_id":  strProp("The deal's UUID."),
			"name":     strProp("New name."),
			"value":    map[string]any{"type": "number", "description": "New value."},
			"currency": strProp("New ISO currency code."),
			"status":   enumProp("New status.", "open", "won", "lost"),
			"stage_id": strProp("Move to this stage UUID."),
		}, "deal_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteCRM,
		Handler:         d.updateDeal,
	})

	r.Register(Tool{
		Name:        "move_deal_stage",
		Description: "Move a deal to another pipeline stage.",
		InputSchema: objectSchema(map[string]any{
			"deal_id":  strProp("The deal's UUID."),
			"stage_id": strProp("The target stage UUID."),
		}, "deal_id", "stage_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteCRM,
		Handler:         d.moveDealStage,
	})

	r.Register(Tool{
		Name:        "delete_deal",
		Description: "Delete a CRM deal. Destructive; requires user approval.",
		InputSchema: objectSchema(map[string]any{
			"deal_id": strProp("The deal's UUID."),
		}, "deal_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteCRM,
		Handler:         d.deleteDeal,
	})

	r.Register(Tool{
		Name:        "list_tasks",
		Description: "List CRM tasks, optionally filtered by contact, deal, or status.",
		InputSchema: objectSchema(map[string]any{
			"contact_id": strProp("Optional contact UUID filter."),
			"deal_id":    strProp("Optional deal UUID filter."),
			"status":     enumProp("Optional status filter.", "pending", "in_progress", "completed", "cancelled"),
			"limit":      intProp("Max tasks (1-50, default 20)."),
		}),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewContacts,
		RequiredAPIPerm: models.APIPermReadCRM,
		Handler:         d.listTasks,
	})

	r.Register(Tool{
		Name:        "update_task",
		Description: "Update a CRM task (title, description, due date, priority, type, status). Only provided fields change.",
		InputSchema: objectSchema(map[string]any{
			"task_id":     strProp("The task's UUID."),
			"title":       strProp("New title."),
			"description": strProp("New description."),
			"due_date":    strProp("New due date, RFC3339."),
			"priority":    enumProp("New priority.", "low", "medium", "high", "urgent"),
			"status":      enumProp("New status.", "pending", "in_progress", "completed", "cancelled"),
		}, "task_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteCRM,
		Handler:         d.updateTask,
	})

	r.Register(Tool{
		Name:        "complete_task",
		Description: "Mark a CRM task as completed.",
		InputSchema: objectSchema(map[string]any{
			"task_id": strProp("The task's UUID."),
		}, "task_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteCRM,
		Handler:         d.completeTask,
	})

	r.Register(Tool{
		Name:        "delete_task",
		Description: "Delete a CRM task. Destructive; requires user approval.",
		InputSchema: objectSchema(map[string]any{
			"task_id": strProp("The task's UUID."),
		}, "task_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteCRM,
		Handler:         d.deleteTask,
	})

	r.Register(Tool{
		Name:            "list_task_types",
		Description:     "List the org's CRM task-type definitions (id, name, color).",
		InputSchema:     objectSchema(map[string]any{}),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewContacts,
		RequiredAPIPerm: models.APIPermReadCRM,
		Handler:         d.listTaskTypes,
	})

	r.Register(Tool{
		Name:        "list_contact_notes",
		Description: "List a contact's CRM notes, newest first.",
		InputSchema: objectSchema(map[string]any{
			"contact_id": strProp("The contact's UUID."),
			"limit":      intProp("Max notes (1-50, default 20)."),
		}, "contact_id"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewContacts,
		RequiredAPIPerm: models.APIPermReadCRM,
		Handler:         d.listContactNotes,
	})

	r.Register(Tool{
		Name:        "update_contact_note",
		Description: "Edit the text of a CRM note.",
		InputSchema: objectSchema(map[string]any{
			"note_id": strProp("The note's UUID."),
			"body":    strProp("The new note text."),
		}, "note_id", "body"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteCRM,
		Handler:         d.updateContactNote,
	})

	r.Register(Tool{
		Name:        "delete_contact_note",
		Description: "Delete a CRM note. Destructive; requires user approval.",
		InputSchema: objectSchema(map[string]any{
			"note_id": strProp("The note's UUID."),
		}, "note_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteCRM,
		Handler:         d.deleteContactNote,
	})

	r.Register(Tool{
		Name:        "create_pipeline",
		Description: "Create a CRM pipeline with an ordered set of stages.",
		InputSchema: objectSchema(map[string]any{
			"name": strProp("Pipeline name (required)."),
			"stages": arrProp("Stages to seed, in order.", objectSchema(map[string]any{
				"name":  strProp("Stage name."),
				"color": strProp("Stage color (hex)."),
			}, "name", "color")),
		}, "name"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteCRM,
		Handler:         d.createPipeline,
	})

	r.Register(Tool{
		Name:        "update_pipeline",
		Description: "Rename a CRM pipeline.",
		InputSchema: objectSchema(map[string]any{
			"pipeline_id": strProp("The pipeline's UUID."),
			"name":        strProp("New name."),
		}, "pipeline_id", "name"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteCRM,
		Handler:         d.updatePipeline,
	})

	r.Register(Tool{
		Name:        "delete_pipeline",
		Description: "Delete a CRM pipeline. Destructive; requires user approval.",
		InputSchema: objectSchema(map[string]any{
			"pipeline_id": strProp("The pipeline's UUID."),
		}, "pipeline_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteCRM,
		Handler:         d.deletePipeline,
	})

	r.Register(Tool{
		Name:        "create_pipeline_stage",
		Description: "Add a stage to a CRM pipeline.",
		InputSchema: objectSchema(map[string]any{
			"pipeline_id": strProp("The pipeline's UUID."),
			"name":        strProp("Stage name (required)."),
			"color":       strProp("Stage color hex (required)."),
		}, "pipeline_id", "name", "color"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteCRM,
		Handler:         d.createStage,
	})

	r.Register(Tool{
		Name:        "update_pipeline_stage",
		Description: "Rename or recolor a CRM pipeline stage.",
		InputSchema: objectSchema(map[string]any{
			"stage_id": strProp("The stage's UUID."),
			"name":     strProp("New name."),
			"color":    strProp("New color hex."),
		}, "stage_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteCRM,
		Handler:         d.updateStage,
	})

	r.Register(Tool{
		Name:        "delete_pipeline_stage",
		Description: "Delete a CRM pipeline stage. Destructive; requires user approval.",
		InputSchema: objectSchema(map[string]any{
			"stage_id": strProp("The stage's UUID."),
		}, "stage_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteCRM,
		Handler:         d.deleteStage,
	})
}

func (d Deps) getDeal(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		DealID string `json:"deal_id"`
	}](args)
	if err != nil {
		return "", err
	}
	did, err := parseUUIDArg(in.DealID)
	if err != nil {
		return "", err
	}
	deal, xerr := d.CRM.GetDeal(ctx, inv.OrgID, did)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(deal)
}

func (d Deps) listDealsByContact(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		ContactID string `json:"contact_id"`
	}](args)
	if err != nil {
		return "", err
	}
	cid, err := parseUUIDArg(in.ContactID)
	if err != nil {
		return "", err
	}
	deals, xerr := d.CRM.GetDealsByContact(ctx, inv.OrgID, cid)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(map[string]any{"deals": deals, "count": len(deals)})
}

func (d Deps) updateDeal(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		DealID   string   `json:"deal_id"`
		Name     *string  `json:"name"`
		Value    *float64 `json:"value"`
		Currency *string  `json:"currency"`
		Status   *string  `json:"status"`
		StageID  string   `json:"stage_id"`
	}](args)
	if err != nil {
		return "", err
	}
	did, err := parseUUIDArg(in.DealID)
	if err != nil {
		return "", err
	}
	upd := &models.UpdateDeal{Name: in.Name, Value: in.Value, Currency: in.Currency, Status: in.Status}
	if in.StageID != "" {
		sid, perr := parseUUIDArg(in.StageID)
		if perr != nil {
			return "", perr
		}
		upd.StageID = &sid
	}
	userID := inv.UserID
	deal, xerr := d.CRM.UpdateDeal(ctx, inv.OrgID, did, &userID, upd)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityCRMDeal, &did, nil)
	return jsonResult(deal)
}

func (d Deps) moveDealStage(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		DealID  string `json:"deal_id"`
		StageID string `json:"stage_id"`
	}](args)
	if err != nil {
		return "", err
	}
	did, err := parseUUIDArg(in.DealID)
	if err != nil {
		return "", err
	}
	sid, err := parseUUIDArg(in.StageID)
	if err != nil {
		return "", err
	}
	userID := inv.UserID
	deal, xerr := d.CRM.UpdateDeal(ctx, inv.OrgID, did, &userID, &models.UpdateDeal{StageID: &sid})
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityCRMDeal, &did, nil)
	return jsonResult(deal)
}

func (d Deps) deleteDeal(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		DealID string `json:"deal_id"`
	}](args)
	if err != nil {
		return "", err
	}
	did, err := parseUUIDArg(in.DealID)
	if err != nil {
		return "", err
	}
	if xerr := d.CRM.DeleteDeal(ctx, inv.OrgID, did); xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionDelete, models.AuditEntityCRMDeal, &did, nil)
	return jsonResult(map[string]any{"ok": true, "deal_id": did.String()})
}

func (d Deps) listTasks(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		ContactID string `json:"contact_id"`
		DealID    string `json:"deal_id"`
		Status    string `json:"status"`
		Limit     int    `json:"limit"`
	}](args)
	if err != nil {
		return "", err
	}
	limit := in.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	var contactID, dealID *uuid.UUID
	if in.ContactID != "" {
		cid, perr := parseUUIDArg(in.ContactID)
		if perr != nil {
			return "", perr
		}
		contactID = &cid
	}
	if in.DealID != "" {
		did, perr := parseUUIDArg(in.DealID)
		if perr != nil {
			return "", perr
		}
		dealID = &did
	}
	var status *string
	if in.Status != "" {
		status = &in.Status
	}
	res, xerr := d.CRM.ListCRMTasks(ctx, inv.OrgID, contactID, dealID, nil, status, limit, nil)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(res)
}

func (d Deps) updateTask(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		TaskID      string  `json:"task_id"`
		Title       *string `json:"title"`
		Description *string `json:"description"`
		DueDate     string  `json:"due_date"`
		Priority    *string `json:"priority"`
		Status      *string `json:"status"`
	}](args)
	if err != nil {
		return "", err
	}
	tid, err := parseUUIDArg(in.TaskID)
	if err != nil {
		return "", err
	}
	upd := &models.UpdateCRMTask{Title: in.Title, Description: in.Description, Priority: in.Priority, Status: in.Status}
	if in.DueDate != "" {
		t, perr := time.Parse(time.RFC3339, in.DueDate)
		if perr != nil {
			return "", ErrInvalidArgs
		}
		upd.DueDate = &t
	}
	return d.applyTaskUpdate(ctx, inv, tid, upd)
}

func (d Deps) completeTask(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
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
	status := string(models.CRMTaskStatusCompleted)
	return d.applyTaskUpdate(ctx, inv, tid, &models.UpdateCRMTask{Status: &status})
}

func (d Deps) applyTaskUpdate(ctx context.Context, inv Invocation, taskID uuid.UUID, upd *models.UpdateCRMTask) (string, error) {
	userID := inv.UserID
	task, xerr := d.CRM.UpdateCRMTask(ctx, inv.OrgID, taskID, &userID, upd)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityCRMTask, &taskID, nil)
	return jsonResult(task)
}

func (d Deps) deleteTask(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
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
	if xerr := d.CRM.DeleteCRMTask(ctx, inv.OrgID, tid); xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionDelete, models.AuditEntityCRMTask, &tid, nil)
	return jsonResult(map[string]any{"ok": true, "task_id": tid.String()})
}

func (d Deps) listTaskTypes(ctx context.Context, inv Invocation, _ json.RawMessage) (string, error) {
	types, xerr := d.CRM.ListTaskTypes(ctx, inv.OrgID)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(map[string]any{"task_types": types, "count": len(types)})
}

func (d Deps) listContactNotes(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		ContactID string `json:"contact_id"`
		Limit     int    `json:"limit"`
	}](args)
	if err != nil {
		return "", err
	}
	cid, err := parseUUIDArg(in.ContactID)
	if err != nil {
		return "", err
	}
	limit := in.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	res, xerr := d.CRM.ListNotes(ctx, inv.OrgID, cid, limit, nil)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(res)
}

func (d Deps) updateContactNote(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		NoteID string `json:"note_id"`
		Body   string `json:"body"`
	}](args)
	if err != nil {
		return "", err
	}
	nid, err := parseUUIDArg(in.NoteID)
	if err != nil {
		return "", err
	}
	if in.Body == "" {
		return "", ErrInvalidArgs
	}
	note, xerr := d.CRM.UpdateNote(ctx, inv.OrgID, nid, &models.UpdateContactNote{Content: &in.Body})
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityCRMNote, &nid, nil)
	return jsonResult(note)
}

func (d Deps) deleteContactNote(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		NoteID string `json:"note_id"`
	}](args)
	if err != nil {
		return "", err
	}
	nid, err := parseUUIDArg(in.NoteID)
	if err != nil {
		return "", err
	}
	if xerr := d.CRM.DeleteNote(ctx, inv.OrgID, nid); xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionDelete, models.AuditEntityCRMNote, &nid, nil)
	return jsonResult(map[string]any{"ok": true, "note_id": nid.String()})
}

func (d Deps) createPipeline(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		Name   string `json:"name"`
		Stages []struct {
			Name  string `json:"name"`
			Color string `json:"color"`
		} `json:"stages"`
	}](args)
	if err != nil {
		return "", err
	}
	if in.Name == "" {
		return "", ErrInvalidArgs
	}
	stages := make([]models.CreatePipelineStage, 0, len(in.Stages))
	for _, s := range in.Stages {
		stages = append(stages, models.CreatePipelineStage{Name: s.Name, Color: s.Color})
	}
	p, xerr := d.CRM.CreatePipeline(ctx, inv.OrgID, &models.CreatePipeline{Name: in.Name, Stages: stages})
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionCreate, models.AuditEntityCRMPipeline, &p.ID, map[string]string{"name": p.Name})
	return jsonResult(p)
}

func (d Deps) updatePipeline(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		PipelineID string `json:"pipeline_id"`
		Name       string `json:"name"`
	}](args)
	if err != nil {
		return "", err
	}
	pid, err := parseUUIDArg(in.PipelineID)
	if err != nil {
		return "", err
	}
	if in.Name == "" {
		return "", ErrInvalidArgs
	}
	p, xerr := d.CRM.UpdatePipeline(ctx, inv.OrgID, pid, &models.UpdatePipeline{Name: &in.Name})
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityCRMPipeline, &pid, nil)
	return jsonResult(p)
}

func (d Deps) deletePipeline(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		PipelineID string `json:"pipeline_id"`
	}](args)
	if err != nil {
		return "", err
	}
	pid, err := parseUUIDArg(in.PipelineID)
	if err != nil {
		return "", err
	}
	if xerr := d.CRM.DeletePipeline(ctx, inv.OrgID, pid); xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionDelete, models.AuditEntityCRMPipeline, &pid, nil)
	return jsonResult(map[string]any{"ok": true, "pipeline_id": pid.String()})
}

func (d Deps) createStage(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		PipelineID string `json:"pipeline_id"`
		Name       string `json:"name"`
		Color      string `json:"color"`
	}](args)
	if err != nil {
		return "", err
	}
	pid, err := parseUUIDArg(in.PipelineID)
	if err != nil {
		return "", err
	}
	if in.Name == "" || in.Color == "" {
		return "", ErrInvalidArgs
	}
	stage, xerr := d.CRM.CreateStage(ctx, inv.OrgID, pid, &models.CreatePipelineStage{Name: in.Name, Color: in.Color})
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionCreate, models.AuditEntityCRMStage, &stage.ID, nil)
	return jsonResult(stage)
}

func (d Deps) updateStage(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		StageID string  `json:"stage_id"`
		Name    *string `json:"name"`
		Color   *string `json:"color"`
	}](args)
	if err != nil {
		return "", err
	}
	sid, err := parseUUIDArg(in.StageID)
	if err != nil {
		return "", err
	}
	stage, xerr := d.CRM.UpdateStage(ctx, inv.OrgID, sid, &models.UpdatePipelineStage{Name: in.Name, Color: in.Color})
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityCRMStage, &sid, nil)
	return jsonResult(stage)
}

func (d Deps) deleteStage(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		StageID string `json:"stage_id"`
	}](args)
	if err != nil {
		return "", err
	}
	sid, err := parseUUIDArg(in.StageID)
	if err != nil {
		return "", err
	}
	if xerr := d.CRM.DeleteStage(ctx, inv.OrgID, sid); xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionDelete, models.AuditEntityCRMStage, &sid, nil)
	return jsonResult(map[string]any{"ok": true, "stage_id": sid.String()})
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
