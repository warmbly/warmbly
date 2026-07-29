package aitools

import (
	"context"
	"encoding/json"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// Organization settings tools, including the AI voice profile. JWT-only (the
// routes carry no API-key permission bit). Reading is open to any member;
// changing settings requires PermManageSettings.
func (d Deps) registerSettingsTools(r *Registry) {
	if d.Org == nil {
		return
	}

	r.Register(Tool{
		Name:            "get_org_settings",
		Description:     "Get the workspace's settings: name, presence toggles, AI voice profile (product description, ICP notes, voice), and inbox-agent state.",
		InputSchema:     objectSchema(map[string]any{}),
		Risk:            generation.RiskRead,
		JWTOnly:         true,
		RequiredOrgPerm: 0,
		Handler:         d.getOrgSettings,
	})

	r.Register(Tool{
		Name:        "update_org_settings",
		Description: "Update workspace settings and the AI voice profile. Only provided fields change; an empty string clears a text field.",
		InputSchema: objectSchema(map[string]any{
			"name":                strProp("Workspace name."),
			"product_description": strProp("What the company sells (grounds AI drafting)."),
			"icp_notes":           strProp("Ideal-customer notes (grounds AI drafting)."),
			"voice_profile":       strProp("Writing-voice guidance for AI drafting."),
			"inbox_agent_enabled": boolProp("Enable or disable the inbox agent."),
		}),
		Risk:            generation.RiskWrite,
		JWTOnly:         true,
		RequiredOrgPerm: models.PermManageSettings,
		Handler:         d.updateOrgSettings,
	})
}

func (d Deps) getOrgSettings(ctx context.Context, inv Invocation, _ json.RawMessage) (string, error) {
	org, xerr := d.Org.Get(ctx, inv.OrgID)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(org)
}

func (d Deps) updateOrgSettings(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		Name               *string `json:"name"`
		ProductDescription *string `json:"product_description"`
		ICPNotes           *string `json:"icp_notes"`
		VoiceProfile       *string `json:"voice_profile"`
		InboxAgentEnabled  *bool   `json:"inbox_agent_enabled"`
	}](args)
	if err != nil {
		return "", err
	}
	req := &models.UpdateOrganizationRequest{
		Name:               in.Name,
		ProductDescription: in.ProductDescription,
		ICPNotes:           in.ICPNotes,
		VoiceProfile:       in.VoiceProfile,
		InboxAgentEnabled:  in.InboxAgentEnabled,
	}
	org, xerr := d.Org.Update(ctx, inv.OrgID, req)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntitySettings, nil, nil)
	return jsonResult(org)
}
