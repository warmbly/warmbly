package aitools

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/toolmigrate"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
)

// Migrating in from another outreach tool. Reading and writing both take the
// contacts write permission and IMPORT_TOOLS, matching the REST routes; the
// service adds the campaign and mailbox permissions per kind chosen.
func (d Deps) registerMigrationTools(r *Registry) {
	if d.ToolMigrations == nil {
		return
	}
	for _, t := range migrationTools() {
		t := t
		r.Register(Tool{
			Name:            t.name,
			Description:     t.description,
			InputSchema:     t.schema(false),
			Risk:            t.risk,
			RequiredOrgPerm: models.PermManageContacts,
			RequiredAPIPerm: models.APIPermImportTools,
			AlwaysAsk:       t.alwaysAsk,
			KeyOnly:         t.keyOnly,
			Handler: func(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
				scope := toolmigrate.Scope{OrgID: &inv.OrgID}
				actor := toolmigrate.ActorFor(inv.UserID, inv.OrgPerms, inv.APIPerms, inv.IsAPIKey, inv.ViaDashboardSession)
				return t.run(ctx, d.ToolMigrations, scope, actor, args, false)
			},
		})
	}
}

// migrationTool is one migration tool, shared by the workspace and agency registries.
type migrationTool struct {
	name        string
	description string
	risk        generation.RiskClass
	alwaysAsk   bool
	// keyOnly keeps a tool whose arguments carry a key away from the agents that store them.
	keyOnly bool
	schema  func(agency bool) map[string]any
	run     func(ctx context.Context, api toolmigrate.API, scope toolmigrate.Scope, actor toolmigrate.Actor, args json.RawMessage, agency bool) (string, error)
}

const migrationDraftNote = " Imported campaigns arrive as drafts and are never launched; a person reviews and starts each one."

func migrationTools() []migrationTool {
	idOnly := func(bool) map[string]any {
		return objectSchema(map[string]any{
			"migration_id": strProp("The migration's UUID, from create_migration or list_migrations."),
		}, "migration_id")
	}
	return []migrationTool{
		{
			name: "list_migration_providers",
			description: "List the outreach tools Warmbly can migrate from (Smartlead, Instantly, lemlist and others), the credential fields each needs, what each can bring over, and where the person finds the key. " +
				"In the dashboard, a person connects a tool under Settings > Migrate from another tool; never ask them to paste a key into the chat.",
			risk:   generation.RiskRead,
			schema: func(bool) map[string]any { return objectSchema(map[string]any{}) },
			run: func(_ context.Context, api toolmigrate.API, _ toolmigrate.Scope, _ toolmigrate.Actor, _ json.RawMessage, _ bool) (string, error) {
				return jsonResult(map[string]any{"data": api.Providers()})
			},
		},
		{
			name: "create_migration",
			description: "Connect another outreach tool with its API key and scan what it holds: blocklist, mailboxes, campaigns and leads per source workspace. Nothing is imported until start_migration. " +
				"Credential values are sealed on the server and never returned." + migrationDraftNote + " Requires user approval.",
			risk:    generation.RiskWrite,
			keyOnly: true,
			schema: func(bool) map[string]any {
				return objectSchema(map[string]any{
					"provider": strProp("The provider id from list_migration_providers, e.g. smartlead or instantly."),
					"label":    strProp("Optional name for this migration."),
					"credentials": arrProp("One object per API key, keyed by the provider's field keys (usually just api_key). Add one per workspace only for a tool that issues a key per workspace.",
						map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}),
				}, "provider", "credentials")
			},
			run: runCreateMigration,
		},
		{
			name:        "list_migrations",
			description: "List migrations from other outreach tools, newest first, with their status and what each brought over.",
			risk:        generation.RiskRead,
			schema: func(bool) map[string]any {
				return objectSchema(map[string]any{
					"cursor": strProp("Opaque cursor from the previous page."),
					"limit":  intProp("Max migrations (1-100, default 20)."),
				})
			},
			run: func(ctx context.Context, api toolmigrate.API, scope toolmigrate.Scope, _ toolmigrate.Actor, args json.RawMessage, _ bool) (string, error) {
				in, err := decodeArgs[struct {
					Cursor string `json:"cursor"`
					Limit  int    `json:"limit"`
				}](args)
				if err != nil {
					return "", err
				}
				if in.Limit <= 0 || in.Limit > 100 {
					in.Limit = 20
				}
				list, xerr := api.List(ctx, scope, in.Cursor, in.Limit)
				if xerr != nil {
					return "", fromErrx(xerr)
				}
				return jsonResult(list)
			},
		},
		{
			name:        "get_migration",
			description: "Get one migration: the scanned inventory per source workspace, its progress, the counts it brought over, and the follow-up left (mailboxes to reconnect, draft campaigns to review, senders not yet linked).",
			risk:        generation.RiskRead,
			schema:      idOnly,
			run: func(ctx context.Context, api toolmigrate.API, scope toolmigrate.Scope, _ toolmigrate.Actor, args json.RawMessage, _ bool) (string, error) {
				id, err := migrationIDArg(args)
				if err != nil {
					return "", err
				}
				m, xerr := api.Get(ctx, scope, id)
				if xerr != nil {
					return "", fromErrx(xerr)
				}
				return jsonResult(m)
			},
		},
		{
			name: "start_migration",
			description: "Start a scanned migration (status ready): choose the kinds to bring over and how to treat leads part way through a sequence. Mailboxes may connect and, with warmup on, start warming." +
				migrationDraftNote + " Requires user approval.",
			risk:      generation.RiskWrite,
			alwaysAsk: true,
			schema:    startMigrationSchema,
			run:       runStartMigration,
		},
		{
			name:        "cancel_migration",
			description: "Cancel a migration. Work in flight stops at its next checkpoint, what already arrived stays, and the stored keys are wiped. Requires user approval.",
			risk:        generation.RiskWrite,
			schema:      idOnly,
			run: func(ctx context.Context, api toolmigrate.API, scope toolmigrate.Scope, actor toolmigrate.Actor, args json.RawMessage, _ bool) (string, error) {
				id, err := migrationIDArg(args)
				if err != nil {
					return "", err
				}
				m, xerr := api.Cancel(ctx, scope, actor, id)
				if xerr != nil {
					return "", fromErrx(xerr)
				}
				return jsonResult(m)
			},
		},
		{
			name:        "link_migration_senders",
			description: "Link imported draft campaigns to sender mailboxes connected since the migration ran, matching by address. Safe to repeat. Requires user approval.",
			risk:        generation.RiskWrite,
			schema:      idOnly,
			run: func(ctx context.Context, api toolmigrate.API, scope toolmigrate.Scope, actor toolmigrate.Actor, args json.RawMessage, _ bool) (string, error) {
				id, err := migrationIDArg(args)
				if err != nil {
					return "", err
				}
				m, xerr := api.LinkSenders(ctx, scope, actor, id)
				if xerr != nil {
					return "", fromErrx(xerr)
				}
				return jsonResult(m)
			},
		},
	}
}

func migrationIDArg(args json.RawMessage) (uuid.UUID, error) {
	in, err := decodeArgs[struct {
		MigrationID string `json:"migration_id"`
	}](args)
	if err != nil {
		return uuid.Nil, err
	}
	return parseUUIDArg(in.MigrationID)
}

func runCreateMigration(ctx context.Context, api toolmigrate.API, scope toolmigrate.Scope, actor toolmigrate.Actor, args json.RawMessage, _ bool) (string, error) {
	in, err := decodeArgs[models.NewToolMigration](args)
	if err != nil {
		return "", err
	}
	if in.Provider == "" || len(in.Credentials) == 0 {
		return "", ErrInvalidArgs
	}
	m, xerr := api.Create(ctx, scope, actor, in)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(m)
}

func startMigrationSchema(agency bool) map[string]any {
	kinds := make([]string, len(source.AllKinds))
	for i, k := range source.AllKinds {
		kinds[i] = string(k)
	}
	props := map[string]any{
		"migration_id": strProp("The migration's UUID; it must be ready (scanned)."),
		"kinds":        arrProp("What to bring over; empty brings everything the provider can read.", enumProp("A kind of data.", kinds...)),
		"campaign_ids": arrProp("Only these source campaign ids (and their leads); empty brings every campaign.", map[string]any{"type": "string"}),
		"in_flight": enumProp("Leads part way through a sequence: import without enrolling (default) or restart from step one.",
			string(models.ToolMigrationSkip), string(models.ToolMigrationRestart)),
		"skip_finished": boolProp("Leave out campaigns the source marks completed or archived."),
		"warmup":        boolProp("Start warmup on mailboxes that connect, unless the source had it off."),
	}
	required := []string{"migration_id"}
	if agency {
		props["targets"] = arrProp("Where each scanned source workspace goes: an existing client (organization_id), a new client (create_name, which also needs AGENCY_MANAGE_CLIENTS), or skip. Every source workspace needs one entry.",
			objectSchema(map[string]any{
				"workspace_id":    strProp("The source workspace id from the migration's inventory (empty for the account itself)."),
				"key":             intProp("The source workspace's key index from the inventory (0 unless several keys were added)."),
				"organization_id": strProp("An existing client workspace's UUID, from list_clients."),
				"create_name":     strProp("Create a new client workspace with this name."),
				"skip":            boolProp("Leave this source workspace out."),
			}, "workspace_id"))
		required = append(required, "targets")
	}
	return objectSchema(props, required...)
}

func runStartMigration(ctx context.Context, api toolmigrate.API, scope toolmigrate.Scope, actor toolmigrate.Actor, args json.RawMessage, agency bool) (string, error) {
	in, err := decodeArgs[struct {
		MigrationID  string                             `json:"migration_id"`
		Kinds        []source.Kind                      `json:"kinds"`
		CampaignIDs  []string                           `json:"campaign_ids"`
		InFlight     models.ToolMigrationInFlight       `json:"in_flight"`
		SkipFinished bool                               `json:"skip_finished"`
		Warmup       bool                               `json:"warmup"`
		Targets      []models.ToolMigrationTargetChoice `json:"targets"`
	}](args)
	if err != nil {
		return "", err
	}
	id, err := parseUUIDArg(in.MigrationID)
	if err != nil {
		return "", err
	}
	// A workspace migration lands every source workspace in the caller's own.
	if !agency && len(in.Targets) > 0 {
		return "", ErrInvalidArgs
	}
	if len(in.Kinds) == 0 {
		in.Kinds = append([]source.Kind(nil), source.AllKinds...)
	}
	if in.InFlight == "" {
		in.InFlight = models.ToolMigrationSkip
	}
	m, xerr := api.Start(ctx, scope, actor, id, models.StartToolMigration{
		Options: models.ToolMigrationOptions{
			Kinds:        in.Kinds,
			CampaignIDs:  in.CampaignIDs,
			InFlight:     in.InFlight,
			SkipFinished: in.SkipFinished,
			Warmup:       in.Warmup,
		},
		Targets: in.Targets,
	})
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(m)
}

// startKeyCheck applies an agency key's own scopes to a start_migration call, as REST does.
func startKeyCheck(apiPerms uint64, args json.RawMessage) error {
	in, err := decodeArgs[struct {
		Kinds   []source.Kind                      `json:"kinds"`
		Targets []models.ToolMigrationTargetChoice `json:"targets"`
	}](args)
	if err != nil {
		return err
	}
	if len(in.Kinds) == 0 {
		in.Kinds = source.AllKinds
	}
	if xerr := toolmigrate.AgencyKeyCheck(apiPerms, in.Kinds, toolmigrate.CreatesClients(models.StartToolMigration{Targets: in.Targets}), false); xerr != nil {
		return xerr
	}
	return nil
}
