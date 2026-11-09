package orgtransfer

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/warmbly/warmbly/internal/app/oauth"
	"github.com/warmbly/warmbly/internal/app/webhook"
	"github.com/warmbly/warmbly/internal/infrastructure/storage"
	"github.com/warmbly/warmbly/internal/models"
)

// importRules re-apply a table's write rules to its archive rows, for values
// shown to other people or followed by a browser. A value that fails is
// dropped, since nobody can be asked to correct an archive.
var importRules = map[string]func(env *ruleEnv, row map[string]json.RawMessage){
	"organization_oauth_apps": dropUnsealedOAuthSecret,
	"oauth_applications":      cleanImportedApp,
	"forms":                   cleanImportedForm,
	"webhook_endpoints":       cleanImportedWebhook,
}

// cleanImportedWebhook disables an endpoint whose address a create would refuse.
func cleanImportedWebhook(_ *ruleEnv, row map[string]json.RawMessage) {
	if raw, ok := row["url"]; ok && webhook.ValidateOutboundURL(jsonString(raw)) != nil {
		setJSON(row, "enabled", false)
	}
}

// ruleEnv is what the rules need to know about the destination.
type ruleEnv struct {
	orgID uuid.UUID
	// logos resolves this instance's public object URLs; nil when it has none.
	logos storage.PublicURLer
	// developerBlocked is an operator's block on building apps here.
	developerBlocked bool
	// heldApps are imported apps that land suspended, with the reason.
	heldApps map[uuid.UUID]string
}

const (
	heldAppSourceSuspended = "Suspended on the instance this workspace was exported from."
	heldAppDeveloperBlock  = "Imported while building apps is blocked for this workspace."
)

func cleanImportedApp(env *ruleEnv, row map[string]json.RawMessage) {
	if raw, ok := row["name"]; ok {
		setJSON(row, "name", oauth.ImportedName(jsonString(raw)))
	}
	if raw, ok := row["website_url"]; ok {
		setJSON(row, "website_url", oauth.ImportedWebsite(jsonString(raw)))
	}
	if raw, ok := row["logo_url"]; ok {
		if _, issued := oauth.IssuedLogoKey(env.logos, env.orgID, jsonString(raw)); !issued {
			setJSON(row, "logo_url", "")
		}
	}
	if raw, ok := row["redirect_uris"]; ok {
		setJSON(row, "redirect_uris", oauth.ImportedRedirectURIs(jsonStrings(raw)))
	}
	if _, ok := row["allowed_webhook_domains"]; ok {
		domains, webhookOK := oauth.ImportedWebhook(jsonString(row["webhook_url"]), jsonStrings(row["allowed_webhook_domains"]))
		if domains == nil {
			domains = []string{}
		}
		setJSON(row, "allowed_webhook_domains", domains)
		if !webhookOK {
			setJSON(row, "webhook_url", "")
			setJSON(row, "webhook_events", []string{})
		}
	}

	id, err := uuid.Parse(jsonString(row["id"]))
	if err != nil {
		return
	}
	switch {
	case jsonString(row["suspended_at"]) != "":
		env.heldApps[id] = heldAppSourceSuspended
	case env.developerBlocked:
		env.heldApps[id] = heldAppDeveloperBlock
	}
}

func cleanImportedForm(_ *ruleEnv, row map[string]json.RawMessage) {
	if raw, ok := row["name"]; ok {
		name, xerr := models.ValidateFormName(jsonString(raw))
		if xerr != nil {
			name = "Imported form"
		}
		setJSON(row, "name", name)
	}
	if raw, ok := row["redirect_url"]; ok {
		u, xerr := models.ValidateFormRedirectURL(jsonString(raw))
		if xerr != nil {
			u = ""
		}
		setJSON(row, "redirect_url", u)
	}
	if raw, ok := row["design"]; ok {
		var d models.FormDesign
		if json.Unmarshal(raw, &d) != nil || models.ValidateFormDesign(&d) != nil {
			row["design"] = json.RawMessage(`{}`)
		} else {
			models.NormalizeFormDesign(&d)
			setJSON(row, "design", d)
		}
	}
	if raw, ok := row["allowed_domains"]; ok {
		in := jsonStrings(raw)
		kept := make([]string, 0, len(in))
		for _, d := range in {
			if v, xerr := models.ValidateFormDomains([]string{d}); xerr == nil && len(kept) < models.FormMaxDomains {
				kept = append(kept, v...)
			}
		}
		setJSON(row, "allowed_domains", kept)
		// A dropped entry can only widen the allowlist, so the form waits as a draft for review.
		if len(kept) < len(in) && jsonString(row["status"]) == string(models.FormStatusPublished) {
			setJSON(row, "status", string(models.FormStatusDraft))
		}
	}
}

// holdImportedApps suspends the apps the rules held, without replacing a
// suspension the destination already has.
func holdImportedApps(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, held map[uuid.UUID]string) error {
	for id, reason := range held {
		if _, err := tx.Exec(ctx, `
			UPDATE oauth_applications
			   SET suspended_at = NOW(), suspended_reason = $3, updated_at = NOW()
			 WHERE organization_id = $1 AND id = $2 AND suspended_at IS NULL
		`, orgID, id, reason); err != nil {
			return err
		}
	}
	return nil
}

func setJSON(row map[string]json.RawMessage, col string, v any) {
	if enc, err := json.Marshal(v); err == nil {
		row[col] = enc
	}
}

func jsonStrings(raw json.RawMessage) []string {
	var out []string
	if len(raw) == 0 || json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

// dropUnsealedOAuthSecret treats an unsealed archived client secret as absent, so the destination keeps its own.
func dropUnsealedOAuthSecret(_ *ruleEnv, row map[string]json.RawMessage) {
	if raw, ok := row["client_secret"]; ok {
		if v := jsonString(raw); v != "" && !IsSealed(v) {
			setJSON(row, "client_secret", "")
		}
	}
}
