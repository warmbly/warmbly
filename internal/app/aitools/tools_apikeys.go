package aitools

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/apikey"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// API-key management tools, gated on PermManageAPIKeys / APIPermAPIKeys (the
// same group gate as the HTTP routes). Creating a key is refused here: that
// confirmation cannot be collected in a chat.
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
		FreshAuth:   true,
		Description: "Create a new API key with a scope preset. The secret is returned once and cannot be retrieved again. Requires user approval.",
		InputSchema: objectSchema(map[string]any{
			"name":        strProp("A label for the key (required)."),
			"description": strProp("Optional description."),
			"preset":      enumProp("Scope preset.", "read_only", "full_access"),
		}, "name"),
		Risk:            generation.RiskWrite,
		AlwaysAsk:       true,
		RequiredOrgPerm: models.PermManageAPIKeys,
		RequiredAPIPerm: models.APIPermAPIKeys,
		Handler:         d.createAPIKey,
	})

	r.Register(Tool{
		Name:        "update_api_key",
		FreshAuth:   true,
		Description: "Update an API key's name or description.",
		InputSchema: objectSchema(map[string]any{
			"key_id":      strProp("The key's UUID."),
			"name":        strProp("New name."),
			"description": strProp("New description."),
		}, "key_id"),
		Risk:            generation.RiskWrite,
		AlwaysAsk:       true,
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
		AlwaysAsk:       true,
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

// createAPIKey refuses, deliberately.
//
// Creating a key through the API requires a confirmation newer than the session
// (RequireFreshAuth on POST /api-keys), because a key outlives the session that
// made it and a stolen token must not be able to leave a durable credential
// behind. This tool reaches the service directly, so honouring it would be a
// way around that gate.
//
// It stays registered rather than being removed so the assistant can say what
// to do instead of reporting an unknown tool.
func (d Deps) createAPIKey(_ context.Context, _ Invocation, _ json.RawMessage) (string, error) {
	return "", errors.New(
		"creating an API key needs you to confirm it is you, and that cannot be collected in a chat. " +
			"Open Settings > API keys in the dashboard and create it there")
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
	// A session may rename a key. A key may rename one only when the result stays inside the key making the request, matching PATCH /api-keys/:id.
	if inv.IsAPIKey {
		if err := d.apiKeyEditWithinCaller(ctx, inv, kid); err != nil {
			return "", err
		}
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

// apiKeyEditWithinCaller applies the same ceiling as the API-key branch of the REST key update.
func (d Deps) apiKeyEditWithinCaller(ctx context.Context, inv Invocation, targetID uuid.UUID) error {
	if inv.CallerKeyID == uuid.Nil || d.APIKeys == nil {
		return fromErrx(apikey.WithinKey(nil, 0, nil, nil))
	}
	caller, xerr := d.APIKeys.Get(ctx, inv.OrgID, inv.CallerKeyID)
	if xerr != nil {
		return fromErrx(xerr)
	}
	target, xerr := d.APIKeys.Get(ctx, inv.OrgID, targetID)
	if xerr != nil {
		return fromErrx(xerr)
	}
	if xerr := apikey.WithinKey(caller, target.Permissions, target.AllowedIPs, target.AllowedEmailAccounts); xerr != nil {
		return fromErrx(xerr)
	}
	return nil
}
