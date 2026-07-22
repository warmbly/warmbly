package aitools

import (
	"context"
	"encoding/json"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// Campaign sequence-step tools. A step is one node in a campaign's sequence
// (an email, wait, or action). These mirror the /campaigns/:id/steps routes and
// run as the invoking user, so the SequenceService resolves the campaign within
// the user's organization.
func (d Deps) registerSequenceTools(r *Registry) {
	if d.Sequences == nil {
		return
	}

	r.Register(Tool{
		Name:        "list_campaign_steps",
		Description: "List a campaign's sequence steps (subject, body, wait, branching) in order.",
		InputSchema: objectSchema(map[string]any{
			"campaign_id": strProp("The campaign's UUID."),
		}, "campaign_id"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewCampaigns,
		RequiredAPIPerm: models.APIPermReadCampaigns,
		Handler:         d.listCampaignSteps,
	})

	r.Register(Tool{
		Name:        "add_campaign_step",
		Description: "Append a new blank step to a campaign's sequence. Returns the new step's id to fill in with update_campaign_step.",
		InputSchema: objectSchema(map[string]any{
			"campaign_id": strProp("The campaign's UUID."),
		}, "campaign_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageCampaigns,
		RequiredAPIPerm: models.APIPermWriteCampaigns,
		Handler:         d.addCampaignStep,
	})

	r.Register(Tool{
		Name:        "update_campaign_step",
		Description: "Update a campaign step's name, subject, body, or wait time. Only provided fields change. Subject and body may contain {{merge}} variables.",
		InputSchema: objectSchema(map[string]any{
			"campaign_id": strProp("The campaign's UUID."),
			"step_id":     strProp("The step (sequence) UUID."),
			"name":        strProp("New step name."),
			"subject":     strProp("New email subject."),
			"body":        strProp("New email body text."),
			"wait_days":   intProp("Days to wait before this step runs."),
		}, "campaign_id", "step_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageCampaigns,
		RequiredAPIPerm: models.APIPermWriteCampaigns,
		Handler:         d.updateCampaignStep,
	})

	r.Register(Tool{
		Name:        "delete_campaign_step",
		Description: "Delete a step from a campaign's sequence. Destructive; requires user approval.",
		InputSchema: objectSchema(map[string]any{
			"campaign_id": strProp("The campaign's UUID."),
			"step_id":     strProp("The step (sequence) UUID."),
		}, "campaign_id", "step_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageCampaigns,
		RequiredAPIPerm: models.APIPermWriteCampaigns,
		Handler:         d.deleteCampaignStep,
	})
}

func (d Deps) listCampaignSteps(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		CampaignID string `json:"campaign_id"`
	}](args)
	if err != nil {
		return "", err
	}
	if _, err := parseUUIDArg(in.CampaignID); err != nil {
		return "", err
	}
	steps, xerr := d.Sequences.Get(ctx, inv.UserID.String(), in.CampaignID)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(map[string]any{"steps": steps, "count": len(steps)})
}

func (d Deps) addCampaignStep(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		CampaignID string `json:"campaign_id"`
	}](args)
	if err != nil {
		return "", err
	}
	if _, err := parseUUIDArg(in.CampaignID); err != nil {
		return "", err
	}
	step, xerr := d.Sequences.Create(ctx, inv.UserID.String(), in.CampaignID)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionCreate, models.AuditEntitySequence, &step.ID, nil)
	return jsonResult(map[string]any{"ok": true, "step_id": step.ID.String()})
}

func (d Deps) updateCampaignStep(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		CampaignID string  `json:"campaign_id"`
		StepID     string  `json:"step_id"`
		Name       *string `json:"name"`
		Subject    *string `json:"subject"`
		Body       *string `json:"body"`
		WaitDays   *int    `json:"wait_days"`
	}](args)
	if err != nil {
		return "", err
	}
	if _, err := parseUUIDArg(in.CampaignID); err != nil {
		return "", err
	}
	sid, err := parseUUIDArg(in.StepID)
	if err != nil {
		return "", err
	}
	upd := &models.UpdateSequence{
		Name:      in.Name,
		Subject:   in.Subject,
		BodyPlain: in.Body,
		WaitAfter: in.WaitDays,
	}
	step, xerr := d.Sequences.Update(ctx, inv.UserID.String(), in.CampaignID, in.StepID, upd)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntitySequence, &sid, nil)
	return jsonResult(step)
}

func (d Deps) deleteCampaignStep(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		CampaignID string `json:"campaign_id"`
		StepID     string `json:"step_id"`
	}](args)
	if err != nil {
		return "", err
	}
	if _, err := parseUUIDArg(in.CampaignID); err != nil {
		return "", err
	}
	sid, err := parseUUIDArg(in.StepID)
	if err != nil {
		return "", err
	}
	if xerr := d.Sequences.Delete(ctx, inv.UserID.String(), in.CampaignID, in.StepID); xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionDelete, models.AuditEntitySequence, &sid, nil)
	return jsonResult(map[string]any{"ok": true, "step_id": in.StepID})
}
