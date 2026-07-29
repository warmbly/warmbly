package aitools

import (
	"context"
	"encoding/json"

	"github.com/warmbly/warmbly/internal/app/webhook"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// Webhook endpoint tools, gated on PermManageSettings / APIPermWebhooks. New
// endpoints stay HMAC-signed and HTTPS-validated by the service exactly as the
// HTTP route does.
func (d Deps) registerWebhookTools(r *Registry) {
	if d.Webhooks == nil {
		return
	}

	r.Register(Tool{
		Name:            "list_webhooks",
		Description:     "List the workspace's webhook endpoints.",
		InputSchema:     objectSchema(map[string]any{}),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermManageSettings,
		RequiredAPIPerm: models.APIPermWebhooks,
		Handler:         d.listWebhooks,
	})

	r.Register(Tool{
		Name:        "create_webhook",
		Description: "Create a webhook endpoint subscribed to event types. Must be an HTTPS URL. Returns the signing secret once.",
		InputSchema: objectSchema(map[string]any{
			"url":         strProp("HTTPS endpoint URL (required)."),
			"description": strProp("Optional description."),
			"event_types": arrProp("Event types to subscribe to.", strProp("Event type, e.g. campaign.reply_received.")),
			"enabled":     boolProp("Whether the endpoint is active (default true)."),
		}, "url"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageSettings,
		RequiredAPIPerm: models.APIPermWebhooks,
		Handler:         d.createWebhook,
	})

	r.Register(Tool{
		Name:        "update_webhook",
		Description: "Update a webhook endpoint's URL, description, event subscriptions, and enabled state.",
		InputSchema: objectSchema(map[string]any{
			"webhook_id":  strProp("The endpoint's UUID."),
			"url":         strProp("HTTPS endpoint URL (required)."),
			"description": strProp("Description."),
			"event_types": arrProp("Event types to subscribe to.", strProp("Event type.")),
			"enabled":     boolProp("Whether the endpoint is active."),
		}, "webhook_id", "url"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageSettings,
		RequiredAPIPerm: models.APIPermWebhooks,
		Handler:         d.updateWebhook,
	})

	r.Register(Tool{
		Name:        "delete_webhook",
		Description: "Delete a webhook endpoint. Destructive; requires user approval.",
		InputSchema: objectSchema(map[string]any{
			"webhook_id": strProp("The endpoint's UUID."),
		}, "webhook_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageSettings,
		RequiredAPIPerm: models.APIPermWebhooks,
		Handler:         d.deleteWebhook,
	})

	r.Register(Tool{
		Name:        "rotate_webhook_secret",
		Description: "Rotate a webhook endpoint's signing secret. Returns the new secret once.",
		InputSchema: objectSchema(map[string]any{
			"webhook_id": strProp("The endpoint's UUID."),
		}, "webhook_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageSettings,
		RequiredAPIPerm: models.APIPermWebhooks,
		Handler:         d.rotateWebhookSecret,
	})

	r.Register(Tool{
		Name:        "verify_webhook",
		Description: "Send a signed verification/test event to a webhook endpoint.",
		InputSchema: objectSchema(map[string]any{
			"webhook_id": strProp("The endpoint's UUID."),
		}, "webhook_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageSettings,
		RequiredAPIPerm: models.APIPermWebhooks,
		Handler:         d.verifyWebhook,
	})

	r.Register(Tool{
		Name:        "list_webhook_deliveries",
		Description: "List recent delivery attempts, optionally for one endpoint.",
		InputSchema: objectSchema(map[string]any{
			"webhook_id": strProp("Optional endpoint UUID filter."),
			"limit":      intProp("Max rows (1-100, default 50)."),
		}),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermManageSettings,
		RequiredAPIPerm: models.APIPermWebhooks,
		Handler:         d.listWebhookDeliveries,
	})
}

func (d Deps) listWebhooks(ctx context.Context, inv Invocation, _ json.RawMessage) (string, error) {
	endpoints, err := d.Webhooks.ListEndpoints(ctx, inv.OrgID)
	if err != nil {
		return "", err
	}
	return jsonResult(map[string]any{"webhooks": endpoints, "count": len(endpoints)})
}

func (d Deps) createWebhook(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		URL         string   `json:"url"`
		Description string   `json:"description"`
		EventTypes  []string `json:"event_types"`
		Enabled     *bool    `json:"enabled"`
	}](args)
	if err != nil {
		return "", err
	}
	if in.URL == "" {
		return "", ErrInvalidArgs
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	userID := inv.UserID
	endpoint, cerr := d.Webhooks.CreateEndpoint(ctx, inv.OrgID, webhook.EndpointInput{
		URL:         in.URL,
		Description: in.Description,
		EventTypes:  in.EventTypes,
		Enabled:     enabled,
		CreatedBy:   &userID,
	})
	if cerr != nil {
		return "", cerr
	}
	d.logAudit(ctx, inv, models.AuditActionCreate, models.AuditEntityWebhook, &endpoint.ID, nil)
	return jsonResult(endpoint)
}

func (d Deps) updateWebhook(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		WebhookID   string   `json:"webhook_id"`
		URL         string   `json:"url"`
		Description string   `json:"description"`
		EventTypes  []string `json:"event_types"`
		Enabled     *bool    `json:"enabled"`
	}](args)
	if err != nil {
		return "", err
	}
	wid, err := parseUUIDArg(in.WebhookID)
	if err != nil {
		return "", err
	}
	if in.URL == "" {
		return "", ErrInvalidArgs
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	endpoint, uerr := d.Webhooks.UpdateEndpoint(ctx, inv.OrgID, wid, in.URL, in.Description, in.EventTypes, enabled)
	if uerr != nil {
		return "", uerr
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityWebhook, &wid, nil)
	return jsonResult(endpoint)
}

func (d Deps) deleteWebhook(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		WebhookID string `json:"webhook_id"`
	}](args)
	if err != nil {
		return "", err
	}
	wid, err := parseUUIDArg(in.WebhookID)
	if err != nil {
		return "", err
	}
	if derr := d.Webhooks.DeleteEndpoint(ctx, inv.OrgID, wid); derr != nil {
		return "", derr
	}
	d.logAudit(ctx, inv, models.AuditActionDelete, models.AuditEntityWebhook, &wid, nil)
	return jsonResult(map[string]any{"ok": true, "webhook_id": wid.String()})
}

func (d Deps) rotateWebhookSecret(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		WebhookID string `json:"webhook_id"`
	}](args)
	if err != nil {
		return "", err
	}
	wid, err := parseUUIDArg(in.WebhookID)
	if err != nil {
		return "", err
	}
	secret, rerr := d.Webhooks.RotateSecret(ctx, inv.OrgID, wid)
	if rerr != nil {
		return "", rerr
	}
	d.logAudit(ctx, inv, models.AuditActionRotate, models.AuditEntityWebhook, &wid, nil)
	return jsonResult(map[string]any{"ok": true, "webhook_id": wid.String(), "secret": secret})
}

func (d Deps) verifyWebhook(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		WebhookID string `json:"webhook_id"`
	}](args)
	if err != nil {
		return "", err
	}
	wid, err := parseUUIDArg(in.WebhookID)
	if err != nil {
		return "", err
	}
	if verr := d.Webhooks.VerifyEndpoint(ctx, inv.OrgID, wid); verr != nil {
		return "", verr
	}
	return jsonResult(map[string]any{"ok": true, "webhook_id": wid.String()})
}

func (d Deps) listWebhookDeliveries(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		WebhookID string `json:"webhook_id"`
		Limit     int    `json:"limit"`
	}](args)
	if err != nil {
		return "", err
	}
	limit := in.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	filter := models.WebhookDeliveryFilter{Limit: limit}
	if in.WebhookID != "" {
		wid, perr := parseUUIDArg(in.WebhookID)
		if perr != nil {
			return "", perr
		}
		filter.EndpointID = &wid
	}
	res, derr := d.Webhooks.ListDeliveries(ctx, inv.OrgID, filter)
	if derr != nil {
		return "", derr
	}
	return jsonResult(res)
}
