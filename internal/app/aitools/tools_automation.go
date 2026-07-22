package aitools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

func (d Deps) registerAutomationTools(r *Registry) {
	r.Register(Tool{
		Name:        "create_automation_draft",
		Description: "Create a new DISABLED automation with a trigger, as a starting point the user opens in the automation canvas to add steps and enable. Never runs on its own. Returns a link to open it.",
		InputSchema: objectSchema(map[string]any{
			"name": strProp("Automation name (required)."),
			"trigger_event": enumProp("The event that starts the flow.",
				"campaign.reply_received", "inbox.reply_received", "campaign.email_opened",
				"campaign.email_clicked", "campaign.completed", "meeting.booked"),
		}, "name"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageSettings,
		RequiredAPIPerm: models.APIPermIntegrations,
		Handler:         d.createAutomationDraft,
	})

	r.Register(Tool{
		Name:            "list_automations",
		Description:     "List the workspace's automations (id, name, enabled, trigger).",
		InputSchema:     objectSchema(map[string]any{}),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermManageSettings,
		RequiredAPIPerm: models.APIPermIntegrations,
		Handler:         d.listAutomations,
	})

	r.Register(Tool{
		Name:        "get_automation",
		Description: "Get one automation's full definition (trigger, filter, node graph).",
		InputSchema: objectSchema(map[string]any{
			"automation_id": strProp("The automation's UUID."),
		}, "automation_id"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermManageSettings,
		RequiredAPIPerm: models.APIPermIntegrations,
		Handler:         d.getAutomation,
	})

	r.Register(Tool{
		Name:        "update_automation",
		Description: "Update an automation's name, trigger, or enabled state. The node graph is preserved. Enabling makes the automation run on real future events.",
		InputSchema: objectSchema(map[string]any{
			"automation_id": strProp("The automation's UUID."),
			"name":          strProp("New name."),
			"enabled":       boolProp("Enable or disable the automation."),
			"trigger_event": enumProp("New trigger event.",
				"campaign.reply_received", "inbox.reply_received", "campaign.email_opened",
				"campaign.email_clicked", "campaign.completed", "meeting.booked"),
		}, "automation_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageSettings,
		RequiredAPIPerm: models.APIPermIntegrations,
		Handler:         d.updateAutomation,
	})

	r.Register(Tool{
		Name:        "set_automation_enabled",
		Description: "Enable or disable an automation. Enabling activates real, event-driven execution (which can include sends inside the flow).",
		InputSchema: objectSchema(map[string]any{
			"automation_id": strProp("The automation's UUID."),
			"enabled":       boolProp("true to enable, false to disable."),
		}, "automation_id", "enabled"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageSettings,
		RequiredAPIPerm: models.APIPermIntegrations,
		Handler:         d.setAutomationEnabled,
	})

	r.Register(Tool{
		Name:        "delete_automation",
		Description: "Delete an automation. Destructive; requires user approval.",
		InputSchema: objectSchema(map[string]any{
			"automation_id": strProp("The automation's UUID."),
		}, "automation_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageSettings,
		RequiredAPIPerm: models.APIPermIntegrations,
		Handler:         d.deleteAutomation,
	})
}

func (d Deps) listAutomations(ctx context.Context, inv Invocation, _ json.RawMessage) (string, error) {
	autos, err := d.Automations.ListAutomations(ctx, inv.OrgID)
	if err != nil {
		return "", err
	}
	out := make([]map[string]any, 0, len(autos))
	for _, a := range autos {
		out = append(out, map[string]any{
			"automation_id": a.ID.String(),
			"name":          a.Name,
			"enabled":       a.Enabled,
			"trigger_event": a.TriggerEvent,
		})
	}
	return jsonResult(map[string]any{"automations": out, "count": len(out)})
}

func (d Deps) getAutomation(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	id, err := d.automationID(args)
	if err != nil {
		return "", err
	}
	auto, gerr := d.Automations.GetAutomation(ctx, inv.OrgID, id)
	if gerr != nil {
		return "", gerr
	}
	return jsonResult(auto)
}

func (d Deps) updateAutomation(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		AutomationID string  `json:"automation_id"`
		Name         *string `json:"name"`
		Enabled      *bool   `json:"enabled"`
		TriggerEvent string  `json:"trigger_event"`
	}](args)
	if err != nil {
		return "", err
	}
	id, err := parseUUIDArg(in.AutomationID)
	if err != nil {
		return "", err
	}
	auto, gerr := d.Automations.GetAutomation(ctx, inv.OrgID, id)
	if gerr != nil {
		return "", gerr
	}
	// Read-modify-write so the node graph and filter are preserved.
	w := models.AutomationWrite{
		Name:         auto.Name,
		Enabled:      auto.Enabled,
		TriggerEvent: auto.TriggerEvent,
		Filter:       auto.Filter,
		Graph:        auto.Graph,
	}
	if in.Name != nil {
		w.Name = *in.Name
	}
	if in.Enabled != nil {
		w.Enabled = *in.Enabled
	}
	if trigger := strings.TrimSpace(in.TriggerEvent); trigger != "" {
		if !models.IsValidWebhookEventType(trigger) {
			return "", ErrInvalidArgs
		}
		w.TriggerEvent = trigger
	}
	updated, uerr := d.Automations.UpdateAutomation(ctx, inv.OrgID, id, w)
	if uerr != nil {
		return "", uerr
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityAutomation, &id, nil)
	return jsonResult(updated)
}

func (d Deps) setAutomationEnabled(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		AutomationID string `json:"automation_id"`
		Enabled      bool   `json:"enabled"`
	}](args)
	if err != nil {
		return "", err
	}
	id, err := parseUUIDArg(in.AutomationID)
	if err != nil {
		return "", err
	}
	auto, gerr := d.Automations.GetAutomation(ctx, inv.OrgID, id)
	if gerr != nil {
		return "", gerr
	}
	w := models.AutomationWrite{
		Name:         auto.Name,
		Enabled:      in.Enabled,
		TriggerEvent: auto.TriggerEvent,
		Filter:       auto.Filter,
		Graph:        auto.Graph,
	}
	updated, uerr := d.Automations.UpdateAutomation(ctx, inv.OrgID, id, w)
	if uerr != nil {
		return "", uerr
	}
	action := models.AuditActionStop
	if in.Enabled {
		action = models.AuditActionStart
	}
	d.logAudit(ctx, inv, action, models.AuditEntityAutomation, &id, nil)
	return jsonResult(map[string]any{"ok": true, "automation_id": id.String(), "enabled": updated.Enabled})
}

func (d Deps) deleteAutomation(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	id, err := d.automationID(args)
	if err != nil {
		return "", err
	}
	if derr := d.Automations.DeleteAutomation(ctx, inv.OrgID, id); derr != nil {
		return "", derr
	}
	d.logAudit(ctx, inv, models.AuditActionDelete, models.AuditEntityAutomation, &id, nil)
	return jsonResult(map[string]any{"ok": true, "automation_id": id.String()})
}

// automationID decodes and parses the automation_id argument.
func (d Deps) automationID(args json.RawMessage) (uuid.UUID, error) {
	in, err := decodeArgs[struct {
		AutomationID string `json:"automation_id"`
	}](args)
	if err != nil {
		return uuid.Nil, err
	}
	return parseUUIDArg(in.AutomationID)
}

func (d Deps) createAutomationDraft(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		Name         string `json:"name"`
		TriggerEvent string `json:"trigger_event"`
	}](args)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(in.Name) == "" {
		return "", ErrInvalidArgs
	}

	trigger := strings.TrimSpace(in.TriggerEvent)
	if trigger == "" {
		trigger = string(models.WebhookEventCampaignReplyReceived)
	}
	if !models.IsValidWebhookEventType(trigger) {
		return "", ErrInvalidArgs
	}

	// A minimal, valid, disabled flow: one trigger node the user extends in the
	// canvas. The graph validator requires exactly one trigger when any node is
	// present, and nothing more for an empty flow.
	w := models.AutomationWrite{
		Name:         in.Name,
		Enabled:      false,
		TriggerEvent: trigger,
		Graph: models.AutomationGraph{
			Nodes: []models.AutomationNode{{ID: "trigger", Type: models.AutomationNodeTrigger, X: 0, Y: 0}},
		},
	}
	auto, aerr := d.Automations.CreateAutomation(ctx, inv.OrgID, w)
	if aerr != nil {
		return "", aerr
	}
	d.logAudit(ctx, inv, models.AuditActionCreate, models.AuditEntityAutomation, &auto.ID, map[string]string{"name": auto.Name})
	return jsonResult(map[string]any{
		"ok":            true,
		"automation_id": auto.ID.String(),
		"enabled":       false,
		"open_url":      d.link("/app/automations/" + auto.ID.String()),
	})
}
