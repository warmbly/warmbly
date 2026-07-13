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
	Handler         Handler
}

// allowed reports whether the invocation's permissions grant this tool. A zero
// required-permission means "no extra gate" (still runs as the user).
func (t Tool) allowed(inv Invocation) bool {
	if inv.IsAPIKey {
		return t.RequiredAPIPerm == 0 || models.HasAPIPermission(inv.APIPerms, t.RequiredAPIPerm)
	}
	return t.RequiredOrgPerm == 0 || inv.OrgPerms&t.RequiredOrgPerm == t.RequiredOrgPerm
}

// Registry holds the tool set. It is built once at startup with the service
// dependencies bound into each handler, then queried per invocation.
type Registry struct {
	tools map[string]Tool
	order []string
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
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
// use, each bound to execute under inv. This is what the agent loop passes to
// generation.Provider.RunAgent.
func (r *Registry) ToolDefs(inv Invocation) []generation.ToolDef {
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
	return defs
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
