package aitools

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/agencyhome"
	"github.com/warmbly/warmbly/internal/app/audit"
	"github.com/warmbly/warmbly/internal/app/toolmigrate"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// AgencyInvocation is an agency API key acting for its agency rather than in one
// client workspace. UserID is the staff member who created the key; every
// service call checks that member's standing in the agency, as the REST routes do.
type AgencyInvocation struct {
	AgencyID uuid.UUID
	UserID   uuid.UUID
	// APIPerms are the key's own bits, agency bits included.
	APIPerms  uint64
	KeyID     uuid.UUID
	KeyName   string
	IP        string
	UserAgent string
}

// AgencyTool is a tool over the whole agency. RequiredAPIPerm is never zero:
// an agency tool exists only for a route an agency key may already call.
type AgencyTool struct {
	Name            string
	Description     string
	InputSchema     map[string]any
	Risk            generation.RiskClass
	RequiredAPIPerm uint64
	// Check refuses a call whose arguments need a bit beyond RequiredAPIPerm.
	Check   func(inv AgencyInvocation, args json.RawMessage) error
	Handler func(ctx context.Context, inv AgencyInvocation, args json.RawMessage) (string, error)
}

func (t AgencyTool) allowed(inv AgencyInvocation) bool {
	return t.RequiredAPIPerm != 0 && models.HasAPIPermission(inv.APIPerms, t.RequiredAPIPerm)
}

// AgencyRegistry holds the agency tools. It is separate from Registry so no
// workspace invocation, dashboard agent included, can ever reach one.
type AgencyRegistry struct {
	tools map[string]AgencyTool
	order []string
}

// Register adds or replaces a tool, preserving first-registration order.
func (r *AgencyRegistry) Register(t AgencyTool) {
	if _, ok := r.tools[t.Name]; !ok {
		r.order = append(r.order, t.Name)
	}
	r.tools[t.Name] = t
}

// Has reports whether name is an agency tool, permitted or not.
func (r *AgencyRegistry) Has(name string) bool {
	if r == nil {
		return false
	}
	_, ok := r.tools[name]
	return ok
}

// PermittedTools returns the agency tools the key's bits allow, in registration order.
func (r *AgencyRegistry) PermittedTools(inv AgencyInvocation) []AgencyTool {
	if r == nil {
		return nil
	}
	out := make([]AgencyTool, 0, len(r.order))
	for _, name := range r.order {
		if t := r.tools[name]; t.allowed(inv) {
			out = append(out, t)
		}
	}
	return out
}

// Call runs one agency tool after its permission checks.
func (r *AgencyRegistry) Call(ctx context.Context, inv AgencyInvocation, name string, args json.RawMessage) (string, error) {
	if r == nil {
		return "", ErrToolNotFound
	}
	t, ok := r.tools[name]
	if !ok {
		return "", ErrToolNotFound
	}
	if !t.allowed(inv) {
		return "", ErrToolForbidden
	}
	if t.Check != nil {
		if err := t.Check(inv, args); err != nil {
			return "", err
		}
	}
	return t.Handler(ctx, inv, args)
}

// AgencyClients is the agency service the agency tools call.
type AgencyClients interface {
	Clients(ctx context.Context, agencyID, userID uuid.UUID) ([]models.AgencyClient, *errx.Error)
	CreateClient(ctx context.Context, agencyID, actorID uuid.UUID, name, timezone string) (*models.AgencyClient, *errx.Error)
	Usage(ctx context.Context, agencyID, userID uuid.UUID) (*models.AgencyUsage, *errx.Error)
	AIBalance(ctx context.Context, agencyID, userID uuid.UUID) (*models.AgencyAIBalance, *errx.Error)
}

// AgencyHome reads across the clients the caller can see.
type AgencyHome interface {
	Overview(ctx context.Context, agencyID, userID uuid.UUID) (*models.AgencyOverview, *errx.Error)
	Inbox(ctx context.Context, agencyID, userID uuid.UUID, p agencyhome.InboxParams) (*models.AgencyInboxPage, *errx.Error)
	Mailboxes(ctx context.Context, agencyID, userID uuid.UUID, f agencyhome.MailboxFilter) (*models.AgencyMailboxList, *errx.Error)
}

// AgencyDeps are the services behind the agency tools; a nil one leaves its tools out.
type AgencyDeps struct {
	Agency         AgencyClients
	Home           AgencyHome
	ToolMigrations toolmigrate.API
	Audit          audit.AuditService
}

// BuildAgencyRegistry registers every agency tool whose service is present.
func BuildAgencyRegistry(d AgencyDeps) *AgencyRegistry {
	r := &AgencyRegistry{tools: map[string]AgencyTool{}}
	d.registerAgencyClientTools(r)
	d.registerAgencyHomeTools(r)
	d.registerAgencyMigrationTools(r)
	return r
}

func (d AgencyDeps) registerAgencyClientTools(r *AgencyRegistry) {
	if d.Agency == nil {
		return
	}
	r.Register(AgencyTool{
		Name:            "list_clients",
		Description:     "List the agency's client workspaces with their ids, names and caps. Pass a client's id as organization_id to any workspace tool to act in it.",
		InputSchema:     objectSchema(map[string]any{}),
		Risk:            generation.RiskRead,
		RequiredAPIPerm: models.APIPermAgencyRead,
		Handler: func(ctx context.Context, inv AgencyInvocation, _ json.RawMessage) (string, error) {
			list, xerr := d.Agency.Clients(ctx, inv.AgencyID, inv.UserID)
			if xerr != nil {
				return "", fromErrx(xerr)
			}
			return jsonResult(map[string]any{"data": list})
		},
	})
	r.Register(AgencyTool{
		Name:        "create_client",
		Description: "Create a new client workspace under the agency. The key's creator becomes its owner. Requires user approval.",
		InputSchema: objectSchema(map[string]any{
			"name":     strProp("The client workspace's name."),
			"timezone": strProp("IANA timezone, e.g. Europe/London; empty uses the default."),
		}, "name"),
		Risk:            generation.RiskWrite,
		RequiredAPIPerm: models.APIPermAgencyManageClients,
		Handler:         d.createClient,
	})
	r.Register(AgencyTool{
		Name:            "get_agency_usage",
		Description:     "Read the agency's shared send pool (what each client used against its cap) and its AI balance.",
		InputSchema:     objectSchema(map[string]any{}),
		Risk:            generation.RiskRead,
		RequiredAPIPerm: models.APIPermAgencyReadUsage,
		Handler: func(ctx context.Context, inv AgencyInvocation, _ json.RawMessage) (string, error) {
			usage, xerr := d.Agency.Usage(ctx, inv.AgencyID, inv.UserID)
			if xerr != nil {
				return "", fromErrx(xerr)
			}
			balance, xerr := d.Agency.AIBalance(ctx, inv.AgencyID, inv.UserID)
			if xerr != nil {
				return "", fromErrx(xerr)
			}
			return jsonResult(map[string]any{"usage": usage, "ai_balance": balance})
		},
	})
}

func (d AgencyDeps) createClient(ctx context.Context, inv AgencyInvocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		Name     string `json:"name"`
		Timezone string `json:"timezone"`
	}](args)
	if err != nil {
		return "", err
	}
	client, xerr := d.Agency.CreateClient(ctx, inv.AgencyID, inv.UserID, in.Name, in.Timezone)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.auditClient(ctx, inv, client.OrganizationID, models.AuditEntityOrganization, map[string]string{"name": client.Name, "agency_id": inv.AgencyID.String()})
	return jsonResult(client)
}

// auditClient records a creation in the client it touched, which refreshes that workspace's dashboards.
func (d AgencyDeps) auditClient(ctx context.Context, inv AgencyInvocation, orgID uuid.UUID, entity models.AuditEntityType, meta map[string]string) {
	if d.Audit == nil {
		return
	}
	id := orgID
	d.Audit.LogAction(ctx, orgID, inv.UserID, models.AuditActionCreate, entity, &id, inv.IP, inv.UserAgent, nil, agencyKeyMeta(inv.KeyID, inv.KeyName, meta))
}

// agencyKeyMeta names the agency API key behind a change, as the REST handlers do.
func agencyKeyMeta(keyID uuid.UUID, keyName string, meta map[string]string) map[string]string {
	if keyID == uuid.Nil {
		return meta
	}
	out := make(map[string]string, len(meta)+3)
	for k, v := range meta {
		out[k] = v
	}
	out["actor_type"] = "agency_api_key"
	out["agency_api_key_id"] = keyID.String()
	out["agency_api_key_name"] = keyName
	return out
}

func (d AgencyDeps) registerAgencyHomeTools(r *AgencyRegistry) {
	if d.Home == nil {
		return
	}
	clientsProp := arrProp("Only these client workspace ids; empty reads every client you can see.", map[string]any{"type": "string"})
	r.Register(AgencyTool{
		Name:            "get_agency_overview",
		Description:     "Every client workspace at a glance, problems first: sending, replies, mailbox health and what needs attention.",
		InputSchema:     objectSchema(map[string]any{}),
		Risk:            generation.RiskRead,
		RequiredAPIPerm: models.APIPermAgencyRead,
		Handler: func(ctx context.Context, inv AgencyInvocation, _ json.RawMessage) (string, error) {
			o, xerr := d.Home.Overview(ctx, inv.AgencyID, inv.UserID)
			if xerr != nil {
				return "", fromErrx(xerr)
			}
			return jsonResult(o)
		},
	})
	r.Register(AgencyTool{
		Name:        "list_agency_mailboxes",
		Description: "List mailboxes across client workspaces, newest first, with their client, status and health.",
		InputSchema: objectSchema(map[string]any{
			"client_ids": clientsProp,
			"q":          strProp("Only addresses containing this text."),
			"attention":  boolProp("Only mailboxes that are disconnected, erroring or held from warmup."),
			"limit":      intProp("Max mailboxes (1-200)."),
			"cursor":     strProp("Opaque cursor from the previous page."),
		}),
		Risk:            generation.RiskRead,
		RequiredAPIPerm: models.APIPermAgencyRead,
		Handler: func(ctx context.Context, inv AgencyInvocation, args json.RawMessage) (string, error) {
			in, err := decodeArgs[struct {
				ClientIDs []string `json:"client_ids"`
				Q         string   `json:"q"`
				Attention bool     `json:"attention"`
				Limit     int      `json:"limit"`
				Cursor    string   `json:"cursor"`
			}](args)
			if err != nil {
				return "", err
			}
			clients, err := parseUUIDList(in.ClientIDs)
			if err != nil {
				return "", err
			}
			if in.Limit < 0 || in.Limit > 200 {
				in.Limit = 0
			}
			page, xerr := d.Home.Mailboxes(ctx, inv.AgencyID, inv.UserID, agencyhome.MailboxFilter{
				Clients: clients, Search: in.Q, Attention: in.Attention, Limit: in.Limit, Cursor: in.Cursor,
			})
			if xerr != nil {
				return "", fromErrx(xerr)
			}
			return jsonResult(page)
		},
	})
	r.Register(AgencyTool{
		Name:        "list_agency_inbox",
		Description: "List conversations across the client workspaces whose inbox you can read, newest first. Read-only: to reply, act in the client with organization_id.",
		InputSchema: objectSchema(map[string]any{
			"client_ids":  clientsProp,
			"unread":      boolProp("Only unread conversations."),
			"needs_reply": boolProp("Only conversations waiting on a reply."),
			"positive":    boolProp("Only conversations labelled positive."),
			"q":           strProp("Search text."),
			"limit":       intProp("Max conversations per page."),
			"cursor":      strProp("Opaque cursor from the previous page."),
		}),
		Risk:            generation.RiskRead,
		RequiredAPIPerm: models.APIPermAgencyRead | models.APIPermReadUnibox,
		Handler: func(ctx context.Context, inv AgencyInvocation, args json.RawMessage) (string, error) {
			in, err := decodeArgs[struct {
				ClientIDs  []string `json:"client_ids"`
				Unread     bool     `json:"unread"`
				NeedsReply bool     `json:"needs_reply"`
				Positive   bool     `json:"positive"`
				Q          string   `json:"q"`
				Limit      int      `json:"limit"`
				Cursor     string   `json:"cursor"`
			}](args)
			if err != nil {
				return "", err
			}
			clients, err := parseUUIDList(in.ClientIDs)
			if err != nil {
				return "", err
			}
			limit := ""
			if in.Limit > 0 {
				limit = strconv.Itoa(in.Limit)
			}
			page, xerr := d.Home.Inbox(ctx, inv.AgencyID, inv.UserID, agencyhome.InboxParams{
				Clients: clients, Unread: in.Unread, NeedsReply: in.NeedsReply, Positive: in.Positive,
				Search: in.Q, Cursor: in.Cursor, Limit: limit,
			})
			if xerr != nil {
				return "", fromErrx(xerr)
			}
			return jsonResult(page)
		},
	})
}

// registerAgencyMigrationTools is the migration set over the agency's clients: one
// source account's workspaces mapped onto client workspaces.
func (d AgencyDeps) registerAgencyMigrationTools(r *AgencyRegistry) {
	if d.ToolMigrations == nil {
		return
	}
	for _, t := range migrationTools() {
		t := t
		tool := AgencyTool{
			Name:            t.name,
			Description:     t.description,
			InputSchema:     t.schema(true),
			Risk:            t.risk,
			RequiredAPIPerm: models.APIPermAgencyMigrate,
			Handler: func(ctx context.Context, inv AgencyInvocation, args json.RawMessage) (string, error) {
				agencyID := inv.AgencyID
				scope := toolmigrate.Scope{AgencyID: &agencyID}
				return t.run(ctx, d.ToolMigrations, scope, toolmigrate.Actor{UserID: inv.UserID}, args, true)
			},
		}
		switch t.name {
		case "start_migration":
			tool.Description += " Map every scanned source workspace with targets."
			tool.Check = func(inv AgencyInvocation, args json.RawMessage) error {
				return startKeyCheck(inv.APIPerms, args)
			}
		case "link_migration_senders":
			tool.Check = func(inv AgencyInvocation, _ json.RawMessage) error {
				if xerr := toolmigrate.AgencyKeyCheck(inv.APIPerms, nil, false, true); xerr != nil {
					return xerr
				}
				return nil
			}
		}
		r.Register(tool)
	}
}

func parseUUIDList(raw []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		id, err := parseUUIDArg(s)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}
