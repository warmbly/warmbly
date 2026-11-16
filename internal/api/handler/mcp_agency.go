package handler

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/aitools"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// mcpOrganizationArg is the argument an agency session adds to every workspace tool.
const mcpOrganizationArg = "organization_id"

// mcpAgency serves an agency API key that named no client workspace: the agency's
// own tools, and every workspace tool its scopes allow with organization_id
// naming the client to act in. A name the agency registry holds is always the
// agency tool here.
func (h *Handler) mcpAgency(c *gin.Context, req jsonRPCRequest, agencyID uuid.UUID) {
	userID, err := middleware.GetUserUUID(c)
	if err != nil {
		c.JSON(http.StatusOK, rpcError(req.ID, -32000, "no user for this key"))
		return
	}
	inv := aitools.AgencyInvocation{
		AgencyID:  agencyID,
		UserID:    userID,
		APIPerms:  middleware.GetAPIKeyPermissions(c),
		KeyName:   middleware.GetAPIKeyName(c),
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	}
	if id := middleware.GetAgencyKeyID(c); id != nil {
		inv.KeyID = *id
	}
	switch req.Method {
	case "tools/list":
		c.JSON(http.StatusOK, rpcResult(req.ID, gin.H{"tools": h.mcpAgencyToolList(inv)}))
	case "tools/call":
		params, ok := mcpCallParams(c, req)
		if !ok {
			return
		}
		h.mcpAgencyToolCall(c, req, inv, params)
	default:
		mcpCommon(c, req, mcpAgencyInstructions)
	}
}

const mcpAgencyInstructions = `

Using this server with an agency key: tools/list shows the agency's own tools (clients, overview, mailboxes, usage, inbox, migrations) and every workspace tool your key allows. A workspace tool acts in the client named by its organization_id; list_clients gives the ids. Nothing here sends mail.`

func (h *Handler) mcpAgencyToolList(inv aitools.AgencyInvocation) []gin.H {
	out := []gin.H{}
	for _, t := range h.AgencyTools.PermittedTools(inv) {
		out = append(out, gin.H{"name": t.Name, "description": t.Description, "inputSchema": t.InputSchema})
	}
	// The member's own permissions are only known per client, so the call checks them again.
	ws := aitools.Invocation{IsAPIKey: true, APIPerms: inv.APIPerms & models.AgencyKeyWorkspaceScopes}
	for _, t := range h.AITools.PermittedTools(ws) {
		if t.Risk == generation.RiskSend || h.AgencyTools.Has(t.Name) {
			continue
		}
		out = append(out, gin.H{
			"name":        t.Name,
			"description": t.Description + " Acts in the client workspace named by organization_id.",
			"inputSchema": withOrganizationArg(t.InputSchema),
		})
	}
	return out
}

func (h *Handler) mcpAgencyToolCall(c *gin.Context, req jsonRPCRequest, inv aitools.AgencyInvocation, params mcpCallRequest) {
	if h.AgencyTools.Has(params.Name) {
		out, err := h.AgencyTools.Call(c.Request.Context(), inv, params.Name, params.Arguments)
		mcpToolResult(c, req, params.Name, out, err)
		return
	}
	if _, ok := h.AITools.Get(params.Name); !ok {
		c.JSON(http.StatusOK, rpcError(req.ID, -32601, "tool not found"))
		return
	}
	orgID, args, ok := splitOrganizationArg(params.Arguments)
	if !ok {
		c.JSON(http.StatusOK, rpcError(req.ID, -32602, "organization_id is required: the client workspace to act in, from list_clients"))
		return
	}
	client, xerr := middleware.ResolveAgencyClient(c, orgID)
	if xerr != nil {
		c.JSON(http.StatusOK, rpcError(req.ID, -32000, xerr.Message))
		return
	}
	params.Arguments = args
	h.mcpRunTool(c, aitools.Invocation{
		OrgID:         orgID,
		UserID:        inv.UserID,
		IsAPIKey:      true,
		APIPerms:      client.APIPerms,
		ActsForMember: true,
		OrgPerms:      client.Perms,
		AgencyKeyID:   inv.KeyID,
		AgencyKeyName: inv.KeyName,
		IP:            inv.IP,
		UserAgent:     inv.UserAgent,
	}, req, params)
}

// withOrganizationArg returns a copy of a tool's schema that also requires organization_id.
func withOrganizationArg(schema map[string]any) map[string]any {
	out := make(map[string]any, len(schema)+3)
	for k, v := range schema {
		out[k] = v
	}
	out["type"] = "object"
	props := map[string]any{}
	if p, ok := schema["properties"].(map[string]any); ok {
		for k, v := range p {
			props[k] = v
		}
	}
	props[mcpOrganizationArg] = map[string]any{
		"type":        "string",
		"description": "The client workspace's UUID to act in, from list_clients.",
	}
	out["properties"] = props
	required := []string{mcpOrganizationArg}
	switch r := schema["required"].(type) {
	case []string:
		required = append(required, r...)
	case []any:
		for _, v := range r {
			if s, ok := v.(string); ok {
				required = append(required, s)
			}
		}
	}
	out["required"] = required
	return out
}

// splitOrganizationArg takes organization_id out of a call's arguments, so the
// workspace tool sees exactly the arguments its own schema declares.
func splitOrganizationArg(raw json.RawMessage) (uuid.UUID, json.RawMessage, bool) {
	var args map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &args) != nil {
		return uuid.Nil, nil, false
	}
	var s string
	if json.Unmarshal(args[mcpOrganizationArg], &s) != nil {
		return uuid.Nil, nil, false
	}
	orgID, err := uuid.Parse(s)
	if err != nil || orgID == uuid.Nil {
		return uuid.Nil, nil, false
	}
	delete(args, mcpOrganizationArg)
	rest, err := json.Marshal(args)
	if err != nil {
		return uuid.Nil, nil, false
	}
	return orgID, rest, true
}
