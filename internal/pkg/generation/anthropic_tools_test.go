package generation

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func readAnthropic(t *testing.T, r *http.Request) antRequest {
	t.Helper()
	var req antRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Error(err)
	}
	for _, tool := range req.Tools {
		if tool.Type == anthropicWebSearchType {
			t.Errorf("hosted web search tool was attached: %+v", tool)
		}
	}
	return req
}

func TestAnthropicAgentDiscoversAndExecutesOriginalTool(t *testing.T) {
	for _, risk := range []RiskClass{RiskRead, RiskWrite, RiskSend} {
		t.Run(string(risk), func(t *testing.T) {
			tools := catalogTestTools(138)
			target := &tools[137]
			schema := map[string]any{
				"type":       "object",
				"properties": map[string]any{"contact_id": map[string]any{"type": "string"}},
				"required":   []any{"contact_id"},
			}
			target.Name, target.Risk, target.InputSchema = "mcp_crm_update_customer", risk, schema
			executions, approvals, requests := 0, 0, 0
			args := json.RawMessage(`{"contact_id":"c1"}`)
			target.Handler = func(_ context.Context, got json.RawMessage) (string, error) {
				executions++
				if string(got) != string(args) {
					t.Errorf("handler arguments = %s", got)
				}
				return `{"updated":true}`, nil
			}
			provider := agentFixtureProvider("anthropic", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				req := readAnthropic(t, r)
				requests++
				switch requests {
				case 1:
					if len(req.Tools) != 1 || !strings.Contains(req.System, req.Tools[0].Name) || req.Messages[0].Role != "user" {
						t.Fatalf("initial request tools=%d system=%q", len(req.Tools), req.System)
					}
					writeAgentFixture(w, "anthropic", "", ToolCall{ID: "search", Name: req.Tools[0].Name, Args: json.RawMessage(`{"query":"mcp_crm_update_customer"}`)})
				case 2:
					if len(req.Tools) != 2 || req.Tools[0].Name != target.Name || !reflect.DeepEqual(req.Tools[0].InputSchema, schema) {
						t.Fatalf("wire schema = %+v", req.Tools)
					}
					var found bool
					for _, m := range req.Messages {
						for _, b := range m.Content {
							if b.Type != "tool_result" {
								continue
							}
							var parsed struct {
								Tools []struct {
									InputSchema struct {
										Required []string `json:"required"`
									} `json:"input_schema"`
								} `json:"tools"`
							}
							if err := json.Unmarshal([]byte(b.Content), &parsed); err != nil {
								t.Error(err)
							}
							if len(parsed.Tools) != 1 || !reflect.DeepEqual(parsed.Tools[0].InputSchema.Required, []string{"contact_id"}) {
								t.Fatalf("search result = %s", b.Content)
							}
							found = true
						}
					}
					if !found {
						t.Fatal("search result was not a tool_result block")
					}
					writeAgentFixture(w, "anthropic", "", ToolCall{ID: "action", Name: target.Name, Args: args})
				default:
					var got string
					for _, m := range req.Messages {
						for _, b := range m.Content {
							if b.Type == "tool_result" && b.ToolUseID == "action" {
								got = b.Content
							}
						}
					}
					if got != `{"updated":true}` {
						t.Fatalf("tool result = %q", got)
					}
					writeAgentFixture(w, "anthropic", "Done")
				}
			}))
			result, err := provider.RunAgent(context.Background(), AgentRequest{
				System:   "You are Remie.",
				Tools:    tools,
				Messages: []AgentMessage{{Role: "user", Content: "Update this customer"}},
				Approve: func(_ context.Context, tool ToolDef, call ToolCall) error {
					approvals++
					if tool.Name != target.Name || tool.Risk != risk || string(call.Args) != string(args) {
						t.Errorf("approval identity changed: %+v %+v", tool, call)
					}
					return nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			wantApprovals := 1
			if risk == RiskRead {
				wantApprovals = 0
			}
			if result.Text != "Done" || executions != 1 || approvals != wantApprovals || requests != 3 {
				t.Fatalf("result=%+v executions=%d approvals=%d requests=%d", result, executions, approvals, requests)
			}
		})
	}
}

func TestAnthropicAgentDiscoveredToolPausesAndResumes(t *testing.T) {
	tools := catalogTestTools(138)
	tools[137].Risk = RiskWrite
	executions, requests := 0, 0
	tools[137].Handler = func(context.Context, json.RawMessage) (string, error) {
		executions++
		return `{}`, nil
	}
	provider := agentFixtureProvider("anthropic", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := readAnthropic(t, r)
		requests++
		switch requests {
		case 1:
			writeAgentFixture(w, "anthropic", "", ToolCall{ID: "search", Name: req.Tools[0].Name, Args: json.RawMessage(`{"query":"tool_0137"}`)})
		case 2:
			writeAgentFixture(w, "anthropic", "", ToolCall{ID: "write", Name: tools[137].Name, Args: json.RawMessage(`{}`)})
		default:
			if len(req.Tools) < 1 || req.Tools[0].Name != tools[137].Name {
				t.Fatalf("resume tools = %+v", req.Tools)
			}
			writeAgentFixture(w, "anthropic", "Resumed")
		}
	}))
	req := AgentRequest{
		Tools:    tools,
		Messages: []AgentMessage{{Role: "user", Content: "Update"}},
		Approve:  func(context.Context, ToolDef, ToolCall) error { return ErrApprovalRequired },
	}
	paused, err := provider.RunAgent(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if paused.StopReason != "approval_required" || paused.Pending == nil || paused.Pending.Call.Name != tools[137].Name || executions != 0 {
		t.Fatalf("pause = %+v executions=%d", paused, executions)
	}
	for _, m := range paused.Messages {
		for _, call := range m.ToolCalls {
			if call.Name == tools[137].Name {
				t.Fatal("unapproved assistant turn was stored")
			}
		}
	}
	call := paused.Pending.Call
	out, err := tools[137].Handler(context.Background(), call.Args)
	if err != nil {
		t.Fatal(err)
	}
	req.Messages = append(paused.Messages,
		AgentMessage{Role: "assistant", ToolCalls: []ToolCall{call}},
		AgentMessage{Role: "tool", ToolCallID: call.ID, Content: out},
	)
	resumed, err := provider.RunAgent(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Text != "Resumed" || executions != 1 || requests != 3 {
		t.Fatalf("resumed = %+v executions=%d requests=%d", resumed, executions, requests)
	}
}

func TestAnthropicCompleteSendsTemperatureOnce(t *testing.T) {
	t.Run("zero is sent", func(t *testing.T) {
		var body map[string]any
		p := &anthropicProvider{http: &http.Client{Transport: agentFixtureTransport{handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			writeAgentFixture(w, "anthropic", "ok")
		})}}}
		temp := 0.0
		result, err := p.Complete(context.Background(), CompletionRequest{System: "s", Prompt: "p", Temperature: &temp})
		if err != nil || result.Text != "ok" {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		got, ok := body["temperature"].(float64)
		if !ok || got != 0 {
			t.Fatalf("temperature = %#v", body["temperature"])
		}
		if _, ok := body["tools"]; ok {
			t.Fatalf("tools = %#v", body["tools"])
		}
	})
	t.Run("400 is not retried", func(t *testing.T) {
		requests := 0
		p := &anthropicProvider{http: &http.Client{Transport: agentFixtureTransport{handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"nope"}}`))
		})}}}
		_, err := p.Complete(context.Background(), CompletionRequest{Prompt: "p", Temperature: Deterministic()})
		if err == nil || requests != 1 {
			t.Fatalf("err=%v requests=%d", err, requests)
		}
	})
}

func TestAgentRunsKnownToolMissingFromTheWire(t *testing.T) {
	for _, kind := range []string{"openai", "anthropic"} {
		t.Run(kind, func(t *testing.T) {
			tools := catalogTestTools(138)
			schema := map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}}}
			target := &tools[137]
			target.Name, target.Risk, target.InputSchema = "mcp_target", RiskRead, schema
			executions, requests := 0, 0
			args := json.RawMessage(`{"id":"7"}`)
			target.Handler = func(_ context.Context, got json.RawMessage) (string, error) {
				executions++
				if string(got) != string(args) {
					t.Errorf("args = %s", got)
				}
				return `{"ok":true}`, nil
			}
			provider := agentFixtureProvider(kind, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw := readRaw(t, r)
				requests++
				names, schemas := wireTools(t, kind, raw)
				if requests == 1 {
					if len(names) != 1 || names[0] == target.Name {
						t.Fatalf("first tools = %v", names)
					}
					writeAgentFixture(w, kind, "",
						ToolCall{ID: "search", Name: names[0], Args: json.RawMessage(`{"query":"mcp_target"}`)},
						ToolCall{ID: "action", Name: target.Name, Args: args},
					)
					return
				}
				if schemas[target.Name] == nil || !reflect.DeepEqual(schemas[target.Name], schema) {
					t.Fatalf("second tools = %v schemas=%v", names, schemas[target.Name])
				}
				writeAgentFixture(w, kind, "Done")
			}))
			result, err := provider.RunAgent(context.Background(), AgentRequest{
				Tools:    tools,
				Messages: []AgentMessage{{Role: "user", Content: "Update"}},
			})
			if err != nil || result.Text != "Done" || executions != 1 || requests != 2 {
				t.Fatalf("result=%+v executions=%d requests=%d err=%v", result, executions, requests, err)
			}
		})
	}
}

func wireTools(t *testing.T, kind string, raw []byte) ([]string, map[string]map[string]any) {
	t.Helper()
	names := []string{}
	schemas := map[string]map[string]any{}
	if kind == "anthropic" {
		var req antRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Error(err)
		}
		for _, tool := range req.Tools {
			names = append(names, tool.Name)
			schemas[tool.Name] = tool.InputSchema
		}
		return names, schemas
	}
	var req oaiRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Error(err)
	}
	for _, tool := range req.Tools {
		names = append(names, tool.Function.Name)
		schemas[tool.Function.Name] = tool.Function.Parameters
	}
	return names, schemas
}
