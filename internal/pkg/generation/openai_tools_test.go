package generation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func writeAgentResponse(w http.ResponseWriter, stream bool, text string, calls ...ToolCall) {
	wireCalls := make([]map[string]any, 0, len(calls))
	for i, call := range calls {
		wireCall := map[string]any{"id": call.ID, "type": "function", "function": map[string]string{"name": call.Name, "arguments": string(call.Args)}}
		if stream {
			wireCall["index"] = i
		}
		wireCalls = append(wireCalls, wireCall)
	}
	message := map[string]any{"role": "assistant", "content": text, "tool_calls": wireCalls}
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": message}}, "usage": map[string]int{"total_tokens": 10}})
		_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": message}}, "usage": map[string]int{"total_tokens": 10}})
}

func readAgentRequest(t *testing.T, w http.ResponseWriter, r *http.Request) oaiRequest {
	t.Helper()
	var req oaiRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Error(err)
	}
	if len(req.Tools) > 128 {
		t.Errorf("OpenAI request contains %d tools", len(req.Tools))
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","param":"tools","message":"too many tools"}}`))
	}
	return req
}

func TestOpenAIAgentGreetingWithOversizedCatalog(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, count := range []int{128, 129, 138, 1024} {
			t.Run(fmt.Sprintf("stream=%t/tools=%d", stream, count), func(t *testing.T) {
				requests := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					req := readAgentRequest(t, w, r)
					requests++
					want := 1
					if count == 128 {
						want = 128
					}
					if len(req.Tools) != want || req.Stream != stream {
						t.Errorf("request tools=%d stream=%t, want %d/%t", len(req.Tools), req.Stream, want, stream)
					}
					writeAgentResponse(w, req.Stream, "Hello!")
				}))
				defer srv.Close()
				p := &openAIProvider{baseURL: srv.URL, http: srv.Client()}
				req := AgentRequest{Tools: catalogTestTools(count), Messages: []AgentMessage{{Role: "user", Content: "Hello"}}}
				var text string
				if stream {
					req.OnEvent = func(e AgentEvent) {
						if e.Type == EventTextDelta {
							text += e.Text
						}
					}
				}
				result, err := p.RunAgent(context.Background(), req)
				if err != nil {
					t.Fatal(err)
				}
				if result.Text != "Hello!" || requests != 1 || (stream && text != result.Text) {
					t.Fatalf("greeting result=%+v requests=%d deltas=%q", result, requests, text)
				}
			})
		}
	}
}

func TestOpenAIAgentDiscoversAndExecutesOriginalTool(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, risk := range []RiskClass{RiskRead, RiskWrite, RiskSend} {
			t.Run(fmt.Sprintf("stream=%t/risk=%s", stream, risk), func(t *testing.T) {
				tools := catalogTestTools(138)
				target := &tools[137]
				target.Name, target.Risk = "mcp_crm_update_customer", risk
				target.InputSchema = map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}}}
				executions, approvals, requests, charges := 0, 0, 0, 0
				args := json.RawMessage(`{"id":"customer-1"}`)
				target.Handler = func(_ context.Context, got json.RawMessage) (string, error) {
					executions++
					if string(got) != string(args) {
						t.Errorf("handler arguments = %s", got)
					}
					return `{"updated":true}`, nil
				}
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					req := readAgentRequest(t, w, r)
					requests++
					switch requests {
					case 1:
						if len(req.Tools) != 1 || !strings.Contains(req.Messages[0].Content, req.Tools[0].Function.Name) {
							t.Error("initial request did not explain discovery")
						}
						writeAgentResponse(w, req.Stream, "", ToolCall{ID: "search", Name: req.Tools[0].Function.Name, Args: json.RawMessage(`{"query":"mcp_crm_update_customer"}`)})
					case 2:
						if len(req.Tools) != 2 || req.Tools[0].Function.Name != target.Name || !reflect.DeepEqual(req.Tools[0].Function.Parameters, target.InputSchema) {
							t.Error("discovery did not expose the original schema")
						}
						writeAgentResponse(w, req.Stream, "", ToolCall{ID: "action", Name: target.Name, Args: args})
					default:
						last := req.Messages[len(req.Messages)-1]
						if last.Role != "tool" || last.ToolCallID != "action" || last.Content != `{"updated":true}` {
							t.Errorf("original tool result missing: %+v", last)
						}
						writeAgentResponse(w, req.Stream, "Done")
					}
				}))
				defer srv.Close()
				p := &openAIProvider{baseURL: srv.URL, http: srv.Client()}
				req := AgentRequest{
					System: "You are Remie.", Tools: tools, Messages: []AgentMessage{{Role: "user", Content: "Update this customer"}},
					Approve: func(_ context.Context, tool ToolDef, call ToolCall) error {
						approvals++
						if tool.Name != target.Name || tool.Risk != risk || call.Name != target.Name || string(call.Args) != string(args) {
							t.Errorf("approval identity changed: %+v %+v", tool, call)
						}
						return nil
					},
					PreIteration: func(context.Context, int) error { charges++; return nil },
				}
				var starts []string
				if stream {
					req.OnEvent = func(e AgentEvent) {
						if e.Type == EventToolStart {
							starts = append(starts, e.ToolName)
						}
					}
				}
				result, err := p.RunAgent(context.Background(), req)
				if err != nil {
					t.Fatal(err)
				}
				wantApprovals := 1
				if risk == RiskRead {
					wantApprovals = 0
				}
				if result.Text != "Done" || executions != 1 || approvals != wantApprovals || charges != 3 || result.TokensUsed != 30 {
					t.Fatalf("result=%+v executions=%d approvals=%d charges=%d", result, executions, approvals, charges)
				}
				if stream && (len(starts) != 2 || starts[1] != target.Name) {
					t.Fatalf("tool events = %v", starts)
				}
			})
		}
	}
}

func TestOpenAIAgentDiscoveredToolPausesAndResumes(t *testing.T) {
	tools := catalogTestTools(138)
	tools[137].Risk = RiskWrite
	executions, requests := 0, 0
	tools[137].Handler = func(context.Context, json.RawMessage) (string, error) { executions++; return `{}`, nil }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := readAgentRequest(t, w, r)
		requests++
		switch requests {
		case 1:
			writeAgentResponse(w, false, "", ToolCall{ID: "search", Name: req.Tools[0].Function.Name, Args: json.RawMessage(`{"query":"tool_0137"}`)})
		case 2:
			writeAgentResponse(w, false, "", ToolCall{ID: "write", Name: tools[137].Name, Args: json.RawMessage(`{}`)})
		default:
			if len(req.Tools) != 2 || req.Tools[0].Function.Name != tools[137].Name {
				t.Error("resume did not restore the discovered tool")
			}
			writeAgentResponse(w, false, "Resumed")
		}
	}))
	defer srv.Close()
	p := &openAIProvider{baseURL: srv.URL, http: srv.Client()}
	req := AgentRequest{Tools: tools, Messages: []AgentMessage{{Role: "user", Content: "Update"}}, Approve: func(context.Context, ToolDef, ToolCall) error { return ErrApprovalRequired }}
	paused, err := p.RunAgent(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if paused.StopReason != "approval_required" || paused.Pending == nil || paused.Pending.Call.Name != tools[137].Name || executions != 0 {
		t.Fatalf("pause = %+v, executions = %d", paused, executions)
	}
	// The service executes the approved original tool and appends its result before resuming.
	call := paused.Pending.Call
	out, err := tools[137].Handler(context.Background(), call.Args)
	if err != nil {
		t.Fatal(err)
	}
	req.Messages = append(paused.Messages, AgentMessage{Role: "assistant", ToolCalls: []ToolCall{call}}, AgentMessage{Role: "tool", ToolCallID: call.ID, Content: out})
	resumed, err := p.RunAgent(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Text != "Resumed" || executions != 1 || requests != 3 {
		t.Fatalf("resumed = %+v, executions=%d requests=%d", resumed, executions, requests)
	}
}

// TestOpenAIAgentDiscoveryKeepsArgumentSchemasAcrossSearches retains required arguments after the active tool page changes.
func TestOpenAIAgentDiscoveryKeepsArgumentSchemasAcrossSearches(t *testing.T) {
	tools := catalogTestTools(138)
	tools[136].InputSchema = map[string]any{
		"type": "object", "properties": map[string]any{"contact_id": map[string]any{"type": "string"}}, "required": []string{"contact_id"},
	}
	tools[137].InputSchema = map[string]any{
		"type": "object", "properties": map[string]any{"campaign_id": map[string]any{"type": "string"}}, "required": []string{"campaign_id"},
	}
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := readAgentRequest(t, w, r)
		requests++
		if requests == 1 {
			writeAgentResponse(w, false, "",
				ToolCall{ID: "search-contact", Name: req.Tools[0].Function.Name, Args: json.RawMessage(`{"query":"tool_0136"}`)},
				ToolCall{ID: "search-campaign", Name: req.Tools[0].Function.Name, Args: json.RawMessage(`{"query":"tool_0137"}`)},
			)
			return
		}
		for i, want := range []string{"contact_id", "campaign_id"} {
			var result struct {
				Tools []struct {
					InputSchema struct {
						Required []string `json:"required"`
					} `json:"input_schema"`
				} `json:"tools"`
			}
			message := req.Messages[len(req.Messages)-2+i]
			if err := json.Unmarshal([]byte(message.Content), &result); err != nil {
				t.Error(err)
			}
			if len(result.Tools) != 1 || !reflect.DeepEqual(result.Tools[0].InputSchema.Required, []string{want}) {
				t.Errorf("discovery result lost required %s argument: %s", want, message.Content)
			}
		}
		writeAgentResponse(w, false, "Done")
	}))
	defer srv.Close()
	p := &openAIProvider{baseURL: srv.URL, http: srv.Client()}
	result, err := p.RunAgent(context.Background(), AgentRequest{
		Tools: tools, Messages: []AgentMessage{{Role: "user", Content: "Inspect the contact and campaign"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "Done" || requests != 2 {
		t.Fatalf("result=%+v requests=%d", result, requests)
	}
}
