// Warmbly MCP server (server direction). Exposes the shared tool registry over
// the MCP streamable-HTTP transport at POST /api/v1/mcp, authenticated by an API
// key, an agency API key or an OAuth token. Each tool is gated by its
// RequiredAPIPerm bits, tools/list reflects only what the key's permission mask
// allows, and send-class tools are never exposed. tools/list carries the core tools plus
// find_tools and call_tool, which reach the rest; ?tools=all lists everything.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/aitools"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

const (
	mcpFindTools = "find_tools"
	mcpCallTool  = "call_tool"
)

const mcpProtocolVersion = "2025-03-26"

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// MCPEndpoint — POST /api/v1/mcp. Handles one JSON-RPC message.
func (h *Handler) MCPEndpoint(c *gin.Context) {
	if h.AITools == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "MCP is not available"))
		return
	}
	var req jsonRPCRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, rpcError(nil, -32700, "parse error"))
		return
	}

	// Notifications (no id) are acknowledged with no body.
	if len(req.ID) == 0 {
		c.Status(http.StatusAccepted)
		return
	}

	orgID := middleware.GetOrganizationID(c)
	if agencyID := middleware.GetAgencyKeyAgencyID(c); agencyID != nil && orgID == nil {
		h.mcpAgency(c, req, *agencyID)
		return
	}
	if orgID == nil {
		c.JSON(http.StatusOK, rpcError(req.ID, -32000, "no organization for this key"))
		return
	}
	if keyMailboxLimited(c) {
		c.JSON(http.StatusOK, rpcError(req.ID, -32000, errToolsMailboxLimited.Message))
		return
	}
	inv := aitools.Invocation{
		OrgID:     *orgID,
		IsAPIKey:  true,
		APIPerms:  middleware.GetAPIKeyPermissions(c),
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		API:       h.agentAPI(c),
	}
	if uid, err := middleware.GetUserUUID(c); err == nil {
		inv.UserID = uid
	}
	bindOAuthMember(c, &inv)
	bindAgencyKey(c, &inv)
	switch req.Method {
	case "tools/list":
		c.JSON(http.StatusOK, rpcResult(req.ID, gin.H{"tools": h.mcpToolList(inv, mcpListAll(c))}))
	case "tools/call":
		h.mcpToolCall(c, inv, req)
	default:
		mcpCommon(c, req, mcpInstructions)
	}
}

// mcpCommon answers the methods every session handles the same way.
func mcpCommon(c *gin.Context, req jsonRPCRequest, instructions string) {
	switch req.Method {
	case "initialize":
		c.JSON(http.StatusOK, rpcResult(req.ID, gin.H{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    gin.H{"tools": gin.H{}},
			"serverInfo":      gin.H{"name": "warmbly", "version": "1.0"},
			"instructions":    aitools.ProductPrimer + instructions,
		}))
	case "ping":
		c.JSON(http.StatusOK, rpcResult(req.ID, gin.H{}))
	default:
		c.JSON(http.StatusOK, rpcError(req.ID, -32601, "method not found"))
	}
}

// bindAgencyKey names an agency API key on the invocation so its audit entries show the key.
func bindAgencyKey(c *gin.Context, inv *aitools.Invocation) {
	if id := middleware.GetAgencyKeyID(c); id != nil {
		inv.AgencyKeyID = *id
		inv.AgencyKeyName = middleware.GetAPIKeyName(c)
	}
}

type mcpCallRequest struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func mcpCallParams(c *gin.Context, req jsonRPCRequest) (mcpCallRequest, bool) {
	var params mcpCallRequest
	if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
		c.JSON(http.StatusOK, rpcError(req.ID, -32602, "invalid params"))
		return params, false
	}
	return params, true
}

const mcpInstructions = `

Using this server: tools/list shows the core tools. find_tools searches every other Warmbly tool you may use and returns its input schema; run one with call_tool. search_docs and read_doc answer how a feature works. find_api, describe_api, api_read and api_write reach every documented API endpoint your credential allows. Nothing here sends mail: draft with draft_reply and send from Warmbly.`

// mcpListAll reports whether the client asked for every tool rather than the core set.
func mcpListAll(c *gin.Context) bool {
	return c.Query("tools") == "all" || c.GetHeader("X-Warmbly-Tools") == "all"
}

// mcpTools is every static tool the credential may use over MCP: send-class tools never are.
func (h *Handler) mcpTools(inv aitools.Invocation) []aitools.Tool {
	var out []aitools.Tool
	for _, t := range h.AITools.PermittedTools(inv) {
		if t.Risk != generation.RiskSend {
			out = append(out, t)
		}
	}
	return out
}

func mcpToolEntry(name, description string, schema map[string]any) gin.H {
	if schema == nil {
		schema = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return gin.H{"name": name, "description": description, "inputSchema": schema}
}

// mcpToolList reflects only the static tools the key's mask allows: the core
// set and the two discovery tools, or with all every tool.
func (h *Handler) mcpToolList(inv aitools.Invocation, all bool) []gin.H {
	tools := h.mcpTools(inv)
	out := make([]gin.H, 0, len(tools)+2)
	for _, t := range tools {
		if all || aitools.CoreTools[t.Name] {
			out = append(out, mcpToolEntry(t.Name, t.Description, t.InputSchema))
		}
	}
	if all {
		return out
	}
	return append(out,
		mcpToolEntry(mcpFindTools, "Search every Warmbly tool you may use by keywords and get each one's description and input schema. "+
			"Covers contacts, campaigns, sequences, segments, forms, suppressions, mailboxes, warmup, the inbox, CRM, automations, "+
			"placement tests, API keys and webhooks. Run a result with call_tool. An empty query lists everything.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":  map[string]any{"type": "string", "description": "Keywords, such as 'deal stage' or 'warmup'."},
				"offset": map[string]any{"type": "integer", "minimum": 0, "description": "Result offset from next_offset."},
			},
		}),
		mcpToolEntry(mcpCallTool, "Run a Warmbly tool found with find_tools by name, with arguments matching its input schema.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":      map[string]any{"type": "string", "description": "The tool name from find_tools."},
				"arguments": map[string]any{"type": "object", "description": "The tool's arguments."},
			},
			"required": []string{"name"},
		}),
	)
}

// mcpFindToolsResult searches the credential's tools, best match first, twenty at a time.
func (h *Handler) mcpFindToolsResult(inv aitools.Invocation, args json.RawMessage) (string, error) {
	var in struct {
		Query  string `json:"query"`
		Offset int    `json:"offset"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return "", aitools.ErrInvalidArgs
		}
	}
	type match struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		InputSchema map[string]any `json:"inputSchema"`
		score       int
	}
	terms := strings.Fields(strings.ToLower(in.Query))
	var matches []match
	for _, t := range h.mcpTools(inv) {
		name := strings.ToLower(t.Name)
		text := name + " " + strings.ToLower(t.Description)
		score := 0
		for _, term := range terms {
			if strings.Contains(name, term) {
				score += 2
			} else if strings.Contains(text, term) {
				score++
			}
		}
		if len(terms) == 0 || score > 0 {
			matches = append(matches, match{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema, score: score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	start := max(0, min(in.Offset, len(matches)))
	end := min(start+20, len(matches))
	res := gin.H{"tools": matches[start:end], "total": len(matches)}
	if end < len(matches) {
		res["next_offset"] = end
	}
	b, err := json.Marshal(res)
	return string(b), err
}

func (h *Handler) mcpToolCall(c *gin.Context, inv aitools.Invocation, req jsonRPCRequest) {
	params, ok := mcpCallParams(c, req)
	if !ok {
		return
	}
	h.mcpRunTool(c, inv, req, params)
}

// mcpRunTool runs one workspace tool, or the find_tools and call_tool discovery pair.
func (h *Handler) mcpRunTool(c *gin.Context, inv aitools.Invocation, req jsonRPCRequest, params mcpCallRequest) {
	var (
		out string
		err error
	)
	switch params.Name {
	case mcpFindTools:
		out, err = h.mcpFindToolsResult(inv, params.Arguments)
	case mcpCallTool:
		var inner struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(params.Arguments, &inner) != nil || inner.Name == "" || inner.Name == mcpCallTool || inner.Name == mcpFindTools {
			c.JSON(http.StatusOK, rpcError(req.ID, -32602, "call_tool needs the name of a tool from find_tools"))
			return
		}
		params.Name, params.Arguments = inner.Name, inner.Arguments
		fallthrough
	default:
		// Send-class tools are never exposed or callable over MCP.
		if t, ok := h.AITools.Get(params.Name); ok && t.Risk == generation.RiskSend {
			c.JSON(http.StatusOK, rpcError(req.ID, -32601, "tool not available"))
			return
		}
		out, err = h.AITools.Call(c.Request.Context(), inv, params.Name, params.Arguments)
	}
	mcpToolResult(c, req, params.Name, out, err)
}

// mcpToolResult renders a tool's outcome: a protocol error for an unknown or
// forbidden tool, otherwise a result that says whether the tool failed.
func mcpToolResult(c *gin.Context, req jsonRPCRequest, name, out string, err error) {
	if err != nil {
		switch {
		case errors.Is(err, aitools.ErrToolNotFound):
			c.JSON(http.StatusOK, rpcError(req.ID, -32601, "tool not found"))
		case errors.Is(err, aitools.ErrToolForbidden):
			c.JSON(http.StatusOK, rpcError(req.ID, -32000, "your API key lacks the permission for this tool"))
		default:
			// A tool error is a normal MCP result with isError, so the client can
			// react rather than treating it as a protocol failure.
			msg, ok := aitools.PublicMessage(err)
			if !ok {
				log.Error().Str("request_id", c.GetString("request_id")).Str("tool", name).Err(err).Msg("mcp tool failed")
				msg = "The tool failed on the server. Try again later."
			}
			c.JSON(http.StatusOK, rpcResult(req.ID, gin.H{
				"content": []gin.H{{"type": "text", "text": msg}},
				"isError": true,
			}))
		}
		return
	}
	c.JSON(http.StatusOK, rpcResult(req.ID, gin.H{
		"content": []gin.H{{"type": "text", "text": out}},
		"isError": false,
	}))
}

func rpcResult(id json.RawMessage, result any) gin.H {
	return gin.H{"jsonrpc": "2.0", "id": rawID(id), "result": result}
}

func rpcError(id json.RawMessage, code int, message string) gin.H {
	return gin.H{"jsonrpc": "2.0", "id": rawID(id), "error": gin.H{"code": code, "message": message}}
}

func rawID(id json.RawMessage) any {
	if len(id) == 0 {
		return nil
	}
	return id
}
