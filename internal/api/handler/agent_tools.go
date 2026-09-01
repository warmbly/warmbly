// REST tool surface for external AI agents that do not speak MCP (Hermes-style
// function calling, OpenAI-compatible frameworks, plain HTTP). Exposes the same
// shared tool registry as POST /v1/mcp with the same rules: each tool is gated
// by its own permission bits, the list reflects only what the caller may use,
// and send-class tools are never exposed.
package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/warmbly/warmbly/internal/api/middleware"
	"github.com/warmbly/warmbly/internal/app/aitools"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// agentToolInvocation builds the registry invocation for either credential
// kind: JWT callers run as the member with their org permission bits, API-key
// and OAuth callers run under the key's permission mask.
func (h *Handler) agentToolInvocation(c *gin.Context) (aitools.Invocation, *errx.Error) {
	if middleware.GetAuthType(c) == "jwt" {
		return h.jwtInvocation(c)
	}
	orgID := middleware.GetOrganizationID(c)
	if orgID == nil {
		return aitools.Invocation{}, errx.New(errx.BadRequest, "no organization for this key")
	}
	inv := aitools.Invocation{
		OrgID:     *orgID,
		IsAPIKey:  true,
		APIPerms:  middleware.GetAPIKeyPermissions(c),
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	}
	if uid, err := middleware.GetUserUUID(c); err == nil {
		inv.UserID = uid
	}
	return inv, nil
}

// ListAgentTools — GET /ai/tools[?format=openai]. The default shape mirrors
// the registry; format=openai (alias: hermes, functions) returns OpenAI
// function-calling objects usable verbatim in an OpenAI-compatible `tools`
// array or inside a Hermes <tools> block.
func (h *Handler) ListAgentTools(c *gin.Context) {
	if h.AITools == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "AI tools are not available"))
		return
	}
	inv, xerr := h.agentToolInvocation(c)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	format := c.DefaultQuery("format", "warmbly")
	switch format {
	case "warmbly", "openai", "hermes", "functions":
	default:
		errx.JSON(c, errx.New(errx.BadRequest, "format must be warmbly, openai, hermes or functions"))
		return
	}

	tools := h.AITools.PermittedTools(inv)
	out := make([]gin.H, 0, len(tools))
	for _, t := range tools {
		if t.Risk == generation.RiskSend {
			continue
		}
		schema := t.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		if format == "warmbly" {
			out = append(out, gin.H{
				"name":         t.Name,
				"description":  t.Description,
				"input_schema": schema,
			})
			continue
		}
		out = append(out, gin.H{
			"type": "function",
			"function": gin.H{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  schema,
			},
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// CallAgentTool — POST /ai/tools/:name/call. The request body is the tool's
// argument object; the result is the tool's output, embedded as JSON when the
// tool returned JSON (they all do today) and as a string otherwise.
func (h *Handler) CallAgentTool(c *gin.Context) {
	if h.AITools == nil {
		errx.JSON(c, errx.New(errx.ServiceUnavailable, "AI tools are not available"))
		return
	}
	inv, xerr := h.agentToolInvocation(c)
	if xerr != nil {
		errx.JSON(c, xerr)
		return
	}

	name := c.Param("name")

	// Send-class tools are never exposed or callable over this surface,
	// matching the MCP endpoint.
	if t, ok := h.AITools.Get(name); ok && t.Risk == generation.RiskSend {
		errx.JSON(c, errx.New(errx.NotFound, "tool not found"))
		return
	}

	// Read rather than trusting ContentLength: a chunked request reports -1,
	// and skipping the body there would silently run the tool with no
	// arguments instead of the ones the model produced.
	args := json.RawMessage(`{}`)
	raw, rerr := io.ReadAll(c.Request.Body)
	if rerr != nil {
		errx.JSON(c, errx.New(errx.BadRequest, "the request body could not be read"))
		return
	}
	if len(bytes.TrimSpace(raw)) > 0 {
		if !json.Valid(raw) {
			errx.JSON(c, errx.New(errx.BadRequest, "the request body must be the tool's JSON argument object"))
			return
		}
		args = json.RawMessage(raw)
	}

	out, err := h.AITools.Call(c.Request.Context(), inv, name, args)
	if err != nil {
		switch {
		case errors.Is(err, aitools.ErrToolNotFound):
			errx.JSON(c, errx.New(errx.NotFound, "tool not found"))
		case errors.Is(err, aitools.ErrToolForbidden):
			errx.JSON(c, errx.New(errx.Forbidden, "your credentials lack the permission for this tool"))
		default:
			// A tool-level failure is the agent's to read and react to, not a
			// transport error: surface the message with a stable 422.
			errx.JSON(c, errx.New(errx.Unprocessable, err.Error()))
		}
		return
	}

	var result any = out
	var decoded json.RawMessage
	if json.Unmarshal([]byte(out), &decoded) == nil {
		result = decoded
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"name": name, "result": result}})
}
