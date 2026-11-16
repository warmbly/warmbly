package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/aitools"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

func callerContext(t *testing.T, header http.Header) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/mcp", strings.NewReader(`{}`))
	c.Request.RemoteAddr = "203.0.113.9:4410"
	c.Request.Host = "api.example.test"
	for k, vs := range header {
		for _, v := range vs {
			c.Request.Header.Add(k, v)
		}
	}
	return c
}

func TestAgentAPIReplaysTheCallersCredentialThroughTheRouter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	var seen *http.Request
	var seenBody []byte
	router.PATCH("/v1/contacts/:id", func(c *gin.Context) {
		seen = c.Request
		seenBody, _ = io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"id": c.Param("id"), "limit": c.Query("limit")})
	})
	h := &Handler{Router: router}

	if h.agentAPI(callerContext(t, http.Header{})) != nil {
		t.Fatal("a request with no credential must not get API access")
	}
	api := h.agentAPI(callerContext(t, http.Header{
		"Authorization":   {"Bearer wmbly_test"},
		"X-Forwarded-For": {"198.51.100.4"},
		"Cookie":          {"session=leak"},
		"Idempotency-Key": {"outer"},
		"Content-Type":    {"text/plain"},
	}))
	status, resp, err := api.Do(context.Background(), http.MethodPatch, "/contacts/c1", url.Values{"limit": {"5"}}, []byte(`{"first_name":"Ada"}`))
	if err != nil || status != http.StatusOK {
		t.Fatalf("status %d err %v", status, err)
	}
	var out map[string]string
	if err := json.Unmarshal(resp, &out); err != nil || out["id"] != "c1" || out["limit"] != "5" {
		t.Fatalf("response %s", resp)
	}
	if seen.Header.Get("Authorization") != "Bearer wmbly_test" || seen.Header.Get("X-Forwarded-For") != "198.51.100.4" ||
		seen.RemoteAddr != "203.0.113.9:4410" || seen.Host != "api.example.test" {
		t.Fatalf("the call did not carry the caller's identity: %+v", seen.Header)
	}
	if seen.Header.Get("Cookie") != "" || seen.Header.Get("Idempotency-Key") != "" || seen.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("request-specific headers leaked into the call: %+v", seen.Header)
	}
	if !bytes.Equal(seenBody, []byte(`{"first_name":"Ada"}`)) {
		t.Fatalf("body %s", seenBody)
	}

	status, _, _ = api.Do(context.Background(), http.MethodGet, "/nope", nil, nil)
	if status != http.StatusNotFound {
		t.Fatalf("unknown route answered %d", status)
	}
}

func mcpTestHandler() *Handler {
	r := aitools.NewRegistry()
	echo := func(_ context.Context, _ aitools.Invocation, args json.RawMessage) (string, error) {
		return "ran " + string(args), nil
	}
	for _, t := range []aitools.Tool{
		{Name: "search_contacts", Description: "Find contacts", Risk: generation.RiskRead, Handler: echo},
		{Name: "create_deal", Description: "Create a CRM deal", Risk: generation.RiskWrite, Handler: echo},
		{Name: "send_reply", Description: "Send a reply", Risk: generation.RiskSend, Handler: echo},
	} {
		r.Register(t)
	}
	return &Handler{AITools: r}
}

func mcpCall(t *testing.T, h *Handler, name, args string) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/mcp", nil)
	params, _ := json.Marshal(map[string]any{"name": name, "arguments": json.RawMessage(args)})
	h.mcpToolCall(c, aitools.Invocation{OrgID: uuid.New(), IsAPIKey: true, APIPerms: ^uint64(0)}, jsonRPCRequest{ID: json.RawMessage(`1`), Params: params})
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMCPListsCoreToolsAndReachesTheRestThroughDiscovery(t *testing.T) {
	h := mcpTestHandler()
	inv := aitools.Invocation{OrgID: uuid.New(), IsAPIKey: true, APIPerms: ^uint64(0), OrgPerms: models.AllPermissions}
	names := func(list []gin.H) []string {
		var out []string
		for _, e := range list {
			out = append(out, e["name"].(string))
		}
		return out
	}
	compact := strings.Join(names(h.mcpToolList(inv, false)), ",")
	if compact != "search_contacts,find_tools,call_tool" {
		t.Fatalf("compact list = %s", compact)
	}
	if all := strings.Join(names(h.mcpToolList(inv, true)), ","); all != "search_contacts,create_deal" {
		t.Fatalf("full list = %s", all)
	}

	found := mcpCall(t, h, mcpFindTools, `{"query":"deal"}`)
	text := found["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "create_deal") || strings.Contains(text, "send_reply") {
		t.Fatalf("find_tools = %s", text)
	}
	ran := mcpCall(t, h, mcpCallTool, `{"name":"create_deal","arguments":{"title":"x"}}`)
	if got := ran["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"]; got != `ran {"title":"x"}` {
		t.Fatalf("call_tool = %v", ran)
	}
	for _, args := range []string{`{"name":"send_reply","arguments":{}}`, `{"name":"call_tool"}`, `{}`} {
		if out := mcpCall(t, h, mcpCallTool, args); out["error"] == nil {
			t.Fatalf("call_tool %s ran: %v", args, out)
		}
	}
}
