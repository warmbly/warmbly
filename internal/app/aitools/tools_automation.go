package aitools

import (
	"context"
	"encoding/json"
	"strings"

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
