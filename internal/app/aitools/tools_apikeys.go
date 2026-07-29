package aitools

import (
	"context"
	"encoding/json"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// API-key management tools, gated on PermManageAPIKeys / APIPermAPIKeys (the
// same group gate as the HTTP routes). Creating a key returns its secret once,
// exactly like the dashboard.
func (d Deps) registerAPIKeyTools(r *Registry) {
	if d.APIKeys == nil {
		return
	}

	r.Register(Tool{
		Name:        "list_api_keys",
		Description: "List the workspace's API keys (metadata and scopes only, never secrets).",
		InputSchema: objectSchema(map[string]any{
			"limit": intProp("Max keys (1-100, default 50)."),
		}),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermManageAPIKeys,
		RequiredAPIPerm: models.APIPermAPIKeys,
		Handler:         d.listAPIKeys,
	})

	r.Register(Tool{
		Name:        "create_api_key",
		Description: "Create a new API key with a scope preset. The secret is returned once and cannot be retrieved again. Requires user approval.",
		InputSchema: objectSchema(map[string]any{
			"name":        strProp("A label for the key (required)."),
			"description": strProp("Optional description."),
			"preset":      enumProp("Scope preset.", "read_only", "full_access"),
		}, "name"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageAPIKeys,
		RequiredAPIPerm: models.APIPermAPIKeys,
		Handler:         d.createAPIKey,
	})

	r.Register(Tool{
		Name:        "update_api_key",
		Description: "Update an API key's name or description.",
		InputSchema: objectSchema(map[string]any{
			"key_id":      strProp("The key's UUID."),
			"name":        strProp("New name."),
			"description": strProp("New description."),
		}, "key_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageAPIKeys,
		RequiredAPIPerm: models.APIPermAPIKeys,
		Handler:         d.updateAPIKey,
	})

	r.Register(Tool{
		Name:        "revoke_api_key",
		Description: "Revoke (permanently disable) an API key. Requires user approval.",
		InputSchema: objectSchema(map[string]any{
			"key_id": strProp("The key's UUID."),
			"reason": strProp("Optional reason recorded on the key."),
		}, "key_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageAPIKeys,
		RequiredAPIPerm: models.APIPermAPIKeys,
		Handler:         d.revokeAPIKey,
	})
}

func (d Deps) listAPIKeys(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		Limit int `json:"limit"`
	}](args)
	if err != nil {
		return "", err
	}
	limit := in.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	res, xerr := d.APIKeys.List(ctx, inv.OrgID, limit, nil)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(res)
}

func (d Deps) createAPIKey(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		Name        string  `json:"name"`
		Description *string `json:"description"`
		Preset      string  `json:"preset"`
	}](args)
	if err != nil {
		return "", err
	}
	if in.Name == "" {
		return "", ErrInvalidArgs
	}
	perms := models.APIPermReadOnly
	if in.Preset == "full_access" {
		perms = models.APIPermFullAccess
	}
	key, xerr := d.APIKeys.Create(ctx, inv.OrgID, inv.UserID, &models.CreateAPIKey{
		Name:        in.Name,
		Description: in.Description,
		Permissions: perms,
	})
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionCreate, models.AuditEntityAPIKey, &key.ID, map[string]string{"name": in.Name})
	return jsonResult(key)
}

func (d Deps) updateAPIKey(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		KeyID       string  `json:"key_id"`
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}](args)
	if err != nil {
		return "", err
	}
	kid, err := parseUUIDArg(in.KeyID)
	if err != nil {
		return "", err
	}
	key, xerr := d.APIKeys.Update(ctx, inv.OrgID, kid, &models.UpdateAPIKey{Name: in.Name, Description: in.Description})
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityAPIKey, &kid, nil)
	return jsonResult(key)
}

func (d Deps) revokeAPIKey(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		KeyID  string `json:"key_id"`
		Reason string `json:"reason"`
	}](args)
	if err != nil {
		return "", err
	}
	kid, err := parseUUIDArg(in.KeyID)
	if err != nil {
		return "", err
	}
	if xerr := d.APIKeys.Revoke(ctx, inv.OrgID, kid, in.Reason); xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionRevoke, models.AuditEntityAPIKey, &kid, nil)
	return jsonResult(map[string]any{"ok": true, "key_id": kid.String()})
}
