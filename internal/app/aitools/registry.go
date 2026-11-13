// Package aitools is the shared tool registry every AI surface runs on: the
// dashboard agent (M3), contact research (M5), automation AI nodes, the inbox
// agent, and the Warmbly MCP server (M8). A Tool is the LLM-facing projection
// of an existing product capability. Handlers call SERVICE-LAYER functions only
// (the same ones the HTTP handlers use), executing AS the invoking user with
// their permission bits enforced, so a tool can never do more than the caller
// could through the normal API and never touches raw SQL or a privileged path.
package aitools

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// Invocation is the identity + permission context a tool executes under. Tools
// run AS the invoking user: their org permission bits (JWT/dashboard-agent
// caller) or API-key permission bits (developer / MCP caller) are enforced
// before any handler runs.
type Invocation struct {
	OrgID    uuid.UUID
	UserID   uuid.UUID
	OrgPerms models.OrganizationPermission
	APIPerms uint64
	// IsAPIKey selects which permission mask gates the tool: API-key callers
	// (MCP, developer sockets) are gated on APIPerms; JWT callers on OrgPerms.
	IsAPIKey bool
	// ActsForMember marks an OAuth token: it also needs OrgPerms, its member's permissions.
	ActsForMember bool
	// FreshAuthed marks a member session that confirmed the account holder within the reauth window.
	FreshAuthed bool
	// ViaDashboardSession marks a person signed in to the dashboard, set only by that path; the
	// Slack agent and autopilot act as a member without being one.
	ViaDashboardSession bool
	// AgencyKeyID / AgencyKeyName name an agency API key acting in a client workspace, recorded on its audit entries.
	AgencyKeyID   uuid.UUID
	AgencyKeyName string
	// IP / UserAgent flow into the audit trail for write-class tools.
	IP        string
	UserAgent string
	// API reaches the public API as this caller; nil hides the API tools.
	API APIDispatcher
}

// APIDispatcher sends one request to the public API as the invoking caller,
// through the route's own authentication, permission and rate-limit middleware.
type APIDispatcher interface {
	Do(ctx context.Context, method, path string, query url.Values, body []byte) (status int, resp []byte, err error)
}

// Handler executes a tool. args is the raw JSON the model produced; the return
// string (usually compact JSON) is fed back to the model as the tool result.
type Handler func(ctx context.Context, inv Invocation, args json.RawMessage) (string, error)

// Tool is a registered capability. RiskClass drives the agent-loop approval
// policy (read auto-runs; write pauses for approval; send is always
// per-action). RequiredAPIPerm / RequiredOrgPerm are the permission gates.
type Tool struct {
	Name            string
	Description     string
	InputSchema     map[string]any
	Risk            generation.RiskClass
	RequiredAPIPerm uint64
	RequiredOrgPerm models.OrganizationPermission
	// JWTOnly hides the tool from API-key callers entirely. Set it on tools
	// whose HTTP route is JWT-session-only (team, org settings, billing): those
	// routes have no API-key permission bit, so a zero RequiredAPIPerm must NOT
	// be read as "open to keys". The dashboard agent (JWT) still gets them.
	JWTOnly bool
	// AlwaysAsk keeps the tool out of workspace "always allow" policies: it starts sending, or changes who has access.
	AlwaysAsk bool
	// KeyOnly offers the tool to API-key and OAuth callers only: its arguments carry a secret,
	// and the dashboard and Slack agents keep and show the arguments of every call.
	KeyOnly bool
	// FreshAuth holds a member to the same recent confirmation the tool's REST route requires.
	FreshAuth bool
	// NeedsAPI marks a tool that calls the public API through Invocation.API.
	NeedsAPI bool
	// SecretFields are top-level result keys the assistant never sees; the member is shown them once.
	SecretFields []string
	// Preview resolves a send's sender, recipients and subject for the approval card, and pins them in the returned args.
	Preview func(ctx context.Context, inv Invocation, args json.RawMessage) (*models.AgentSendPreview, json.RawMessage, error)
	Handler Handler
}

// run executes the tool, refusing a member session that has not confirmed recently when the tool needs it.
func (t Tool) run(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	if t.FreshAuth && !inv.IsAPIKey && !inv.FreshAuthed {
		return "", ErrToolNeedsFreshAuth
	}
	return t.Handler(ctx, inv, args)
}

// allowed reports whether the invocation's permissions grant this tool. A zero
// required-permission means "no extra gate" (still runs as the user).
func (t Tool) allowed(inv Invocation) bool {
	if t.NeedsAPI && inv.API == nil {
		return false
	}
	if t.KeyOnly && !inv.IsAPIKey {
		return false
	}
	if inv.IsAPIKey {
		if t.JWTOnly {
			return false
		}
		if inv.ActsForMember && inv.OrgPerms&t.RequiredOrgPerm != t.RequiredOrgPerm {
			return false
		}
		return t.RequiredAPIPerm == 0 || models.HasAPIPermission(inv.APIPerms, t.RequiredAPIPerm)
	}
	return t.RequiredOrgPerm == 0 || inv.OrgPerms&t.RequiredOrgPerm == t.RequiredOrgPerm
}

// DynamicToolSource contributes per-invocation tools that are not statically
// registered (e.g. an org's connected MCP servers, M7). Sources are queried on
// every ToolDefs call.
type DynamicToolSource interface {
	ToolsForInvocation(ctx context.Context, inv Invocation) []generation.ToolDef
}

// Registry holds the tool set. It is built once at startup with the service
// dependencies bound into each handler, then queried per invocation.
type Registry struct {
	tools   map[string]Tool
	order   []string
	dynamic []DynamicToolSource
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// AddDynamicSource registers a per-invocation tool source (post-construction).
func (r *Registry) AddDynamicSource(src DynamicToolSource) {
	if src != nil {
		r.dynamic = append(r.dynamic, src)
	}
}

// Register adds or replaces a tool, preserving first-registration order.
func (r *Registry) Register(t Tool) {
	if _, ok := r.tools[t.Name]; !ok {
		r.order = append(r.order, t.Name)
	}
	r.tools[t.Name] = t
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// PermittedTools returns the tools the invocation may use, in registration
// order (used by the MCP tools/list to reflect only what the key allows).
func (r *Registry) PermittedTools(inv Invocation) []Tool {
	out := make([]Tool, 0, len(r.order))
	for _, name := range r.order {
		t := r.tools[name]
		if t.allowed(inv) {
			out = append(out, t)
		}
	}
	return out
}

// ToolDefs returns the provider-facing tool defs the invocation is permitted to
// use (static registry tools plus any dynamic per-org tools), each bound to
// execute under inv. This is what the dashboard agent passes to
// generation.Provider.RunAgent.
func (r *Registry) ToolDefs(ctx context.Context, inv Invocation) []generation.ToolDef {
	permitted := r.PermittedTools(inv)
	defs := make([]generation.ToolDef, 0, len(permitted))
	for _, t := range permitted {
		t := t // capture
		defs = append(defs, generation.ToolDef{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
			Risk:        t.Risk,
			Deferred:    !CoreTools[t.Name],
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				return t.run(ctx, inv, args)
			},
		})
	}
	for _, src := range r.dynamic {
		for _, d := range src.ToolsForInvocation(ctx, inv) {
			d.Deferred = true
			defs = append(defs, d)
		}
	}
	return defs
}

// CoreTools are always in front of the model; every other tool is found
// through tool search, so the request stays small however large the registry grows.
var CoreTools = map[string]bool{
	"search_contacts": true, "get_contact": true,
	"list_campaigns": true, "get_campaign": true, "get_campaign_stats": true, "get_dashboard_analytics": true,
	"list_mailboxes": true, "get_mailbox": true,
	"list_threads": true, "get_thread": true, "draft_reply": true,
	"list_recommendations": true, "load_skill": true, "search_web": true, "fetch_url": true,
	"search_docs": true, "read_doc": true, "find_api": true, "describe_api": true, "api_read": true, "api_write": true,
}

// ToolDefsByName returns bound ToolDefs for only the named tools the invocation
// is permitted to use (unknown or unpermitted names are skipped). Feature agents
// (e.g. the advisor fixer) use this to pull a specific subset without exposing
// the whole registry.
func (r *Registry) ToolDefsByName(inv Invocation, names ...string) []generation.ToolDef {
	return r.bindNamed(inv, true, names...)
}

// researchTools are the tools a feature agent may run under its own route's gate: public web lookups and playbooks.
var researchTools = map[string]bool{"search_web": true, "fetch_url": true, "load_skill": true}

// ResearchTools binds search_web, fetch_url and load_skill for a feature whose
// own route or schedule already authorized the caller (contact research, AI
// variables at send time). Any other name is ignored.
func (r *Registry) ResearchTools(inv Invocation, names ...string) []generation.ToolDef {
	keep := make([]string, 0, len(names))
	for _, n := range names {
		if researchTools[n] {
			keep = append(keep, n)
		}
	}
	return r.bindNamed(inv, false, keep...)
}

func (r *Registry) bindNamed(inv Invocation, gate bool, names ...string) []generation.ToolDef {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	defs := make([]generation.ToolDef, 0, len(names))
	for _, name := range r.order {
		if !want[name] {
			continue
		}
		t := r.tools[name]
		if gate && !t.allowed(inv) {
			continue
		}
		defs = append(defs, generation.ToolDef{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
			Risk:        t.Risk,
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				return t.run(ctx, inv, args)
			},
		})
	}
	return defs
}

// WebResearchTools returns fresh read-only web tools (search_web, fetch_url)
// bound to an org, for research-mode campaign AI variables at send time, which
// the campaign's own permissions already authorized. Returned defs are
// unbudgeted; the caller wraps them with its own per-run budget.
func (r *Registry) WebResearchTools(orgID uuid.UUID) []generation.ToolDef {
	return r.ResearchTools(Invocation{OrgID: orgID}, "search_web", "fetch_url")
}

// PreviewSend resolves a gated call's send preview and pinned args; nil when the tool has no preview.
func (r *Registry) PreviewSend(ctx context.Context, inv Invocation, name string, args json.RawMessage) (*models.AgentSendPreview, json.RawMessage, error) {
	t, ok := r.tools[name]
	if !ok || t.Preview == nil || !t.allowed(inv) {
		return nil, nil, nil
	}
	return t.Preview(ctx, inv, args)
}

// Permits reports whether inv may run the named static tool; unknown names are not permitted.
func (r *Registry) Permits(inv Invocation, name string) bool {
	t, ok := r.tools[name]
	return ok && t.allowed(inv)
}

// Call invokes a single tool by name under inv, enforcing its permission gate.
// Used by the MCP server (M8) tools/call and anywhere a direct single-tool
// invocation is needed. Returns ErrToolNotFound / ErrToolForbidden as needed.
func (r *Registry) Call(ctx context.Context, inv Invocation, name string, args json.RawMessage) (string, error) {
	t, ok := r.tools[name]
	if !ok {
		return "", ErrToolNotFound
	}
	if !t.allowed(inv) {
		return "", ErrToolForbidden
	}
	return t.run(ctx, inv, args)
}
