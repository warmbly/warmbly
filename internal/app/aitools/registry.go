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
	// IP / UserAgent flow into the audit trail for write-class tools.
	IP        string
	UserAgent string
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
	Handler Handler
}

// allowed reports whether the invocation's permissions grant this tool. A zero
// required-permission means "no extra gate" (still runs as the user).
func (t Tool) allowed(inv Invocation) bool {
	if inv.IsAPIKey {
		if t.JWTOnly {
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
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				return t.Handler(ctx, inv, args)
			},
		})
	}
	for _, src := range r.dynamic {
		defs = append(defs, src.ToolsForInvocation(ctx, inv)...)
	}
	return defs
}

// ToolDefsByName returns bound ToolDefs for only the named tools the invocation
// is permitted to use (unknown or unpermitted names are skipped). Feature agents
// (e.g. contact research) use this to pull a specific subset like search_web +
// fetch_url without exposing the whole registry.
func (r *Registry) ToolDefsByName(inv Invocation, names ...string) []generation.ToolDef {
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
		if !t.allowed(inv) {
			continue
		}
		defs = append(defs, generation.ToolDef{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
			Risk:        t.Risk,
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				return t.Handler(ctx, inv, args)
			},
		})
	}
	return defs
}

// WebResearchTools returns fresh read-only web tools (search_web, fetch_url)
// bound to an org, for feature agents that only need public-web lookups (e.g.
// research-mode campaign AI variables at send time). Read-only web tools require
// no org permission, so a bare org-scoped invocation suffices. Returned defs are
// unbudgeted; the caller wraps them with its own per-run budget.
func (r *Registry) WebResearchTools(orgID uuid.UUID) []generation.ToolDef {
	return r.ToolDefsByName(Invocation{OrgID: orgID}, "search_web", "fetch_url")
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
	return t.Handler(ctx, inv, args)
}
