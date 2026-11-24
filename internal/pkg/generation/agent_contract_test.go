package generation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func readRaw(t *testing.T, r *http.Request) []byte {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Error(err)
	}
	return raw
}

func jsonMap(t *testing.T, raw []byte) map[string]json.RawMessage {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Error(err)
	}
	return m
}

func TestAgentTextOnLastIterationWithoutReserveStops(t *testing.T) {
	for _, kind := range []string{"openai", "openai-stream", "anthropic"} {
		t.Run(kind, func(t *testing.T) {
			requests := 0
			var toolCounts []int
			provider := agentFixtureProvider(kind, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw := readRaw(t, r)
				body := jsonMap(t, raw)
				var tools []json.RawMessage
				if body["tools"] != nil {
					if err := json.Unmarshal(body["tools"], &tools); err != nil {
						t.Error(err)
					}
				}
				toolCounts = append(toolCounts, len(tools))
				requests++
				if requests == 1 {
					writeAgentFixture(w, kind, "", ToolCall{ID: "1", Name: "read", Args: json.RawMessage(`{}`)})
					return
				}
				writeAgentFixture(w, kind, "Answer")
			}))
			req := AgentRequest{
				Messages:      []AgentMessage{{Role: "user", Content: "Look"}},
				Tools:         []ToolDef{{Name: "read", Risk: RiskRead, Handler: func(context.Context, json.RawMessage) (string, error) { return `{}`, nil }}},
				MaxIterations: 2,
			}
			if kind != "openai" {
				req.OnEvent = func(AgentEvent) {}
			}
			result, err := provider.RunAgent(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if requests != 2 || result.StopReason != "stop" || result.Text != "Answer" {
				t.Fatalf("result=%+v requests=%d", result, requests)
			}
			if len(toolCounts) != 2 || toolCounts[0] == 0 || toolCounts[1] == 0 {
				t.Fatalf("tool counts = %v", toolCounts)
			}
		})
	}
}

func TestAgentDefaultsIterationsAndTokens(t *testing.T) {
	for _, kind := range []string{"openai", "anthropic"} {
		t.Run(kind, func(t *testing.T) {
			requests := 0
			provider := agentFixtureProvider(kind, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw := readRaw(t, r)
				var body struct {
					MaxTokens           *int `json:"max_tokens"`
					MaxCompletionTokens *int `json:"max_completion_tokens"`
				}
				if err := json.Unmarshal(raw, &body); err != nil {
					t.Error(err)
				}
				requests++
				if body.MaxTokens == nil || *body.MaxTokens != 2048 {
					t.Errorf("max_tokens = %v", body.MaxTokens)
				}
				if kind == "openai" && body.MaxCompletionTokens != nil {
					t.Errorf("max_completion_tokens = %v", body.MaxCompletionTokens)
				}
				writeAgentFixture(w, kind, "", ToolCall{ID: "1", Name: "missing", Args: json.RawMessage(`{}`)})
			}))
			result, err := provider.RunAgent(context.Background(), AgentRequest{
				Messages: []AgentMessage{{Role: "user", Content: "Keep going"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if requests != 12 || result.Iterations != 12 || result.StopReason != "max_iterations" || result.Text != "" {
				t.Fatalf("result=%+v requests=%d", result, requests)
			}
		})
	}
}

func TestRunAgentOmitsTemperature(t *testing.T) {
	for _, kind := range []string{"openai", "anthropic"} {
		for _, withTools := range []bool{false, true} {
			if kind == "anthropic" && !withTools {
				continue
			}
			t.Run(fmt.Sprintf("%s/tools=%t", kind, withTools), func(t *testing.T) {
				provider := agentFixtureProvider(kind, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body := jsonMap(t, readRaw(t, r))
					if _, ok := body["temperature"]; ok {
						t.Error("temperature was sent")
					}
					_, hasChoice := body["tool_choice"]
					if kind == "anthropic" && hasChoice {
						t.Error("anthropic sent tool_choice")
					}
					if withTools {
						if len(body["tools"]) == 0 {
							t.Error("tools were dropped")
						}
						if kind == "openai" && string(body["tool_choice"]) != `"auto"` {
							t.Errorf("tool_choice = %s", body["tool_choice"])
						}
					} else if hasChoice {
						t.Error("tool_choice was sent without tools")
					}
					writeAgentFixture(w, kind, "ok")
				}))
				req := AgentRequest{Messages: []AgentMessage{{Role: "user", Content: "ping"}}}
				if withTools {
					req.Tools = []ToolDef{{Name: "read", Risk: RiskRead, Handler: func(context.Context, json.RawMessage) (string, error) { return `{}`, nil }}}
				}
				result, err := provider.RunAgent(context.Background(), req)
				if err != nil || result.Text != "ok" {
					t.Fatalf("result=%+v err=%v", result, err)
				}
			})
		}
	}
}

func TestAgentFollowsToolCallsNotFinishReason(t *testing.T) {
	t.Run("openai/tools-despite-stop", func(t *testing.T) {
		executions := 0
		provider := openAIFixture(t, func(w http.ResponseWriter) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{map[string]any{
					"finish_reason": "stop",
					"message": map[string]any{
						"role": "assistant", "content": "",
						"tool_calls": []any{map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "read", "arguments": "{}"}}},
					},
				}},
			})
		}, func(w http.ResponseWriter) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": "done"}}},
			})
		})
		result, err := provider.RunAgent(context.Background(), AgentRequest{
			Messages: []AgentMessage{{Role: "user", Content: "Look"}},
			Tools: []ToolDef{{Name: "read", Risk: RiskRead, Handler: func(context.Context, json.RawMessage) (string, error) {
				executions++
				return `{}`, nil
			}}},
		})
		if err != nil || executions != 1 || result.StopReason != "stop" {
			t.Fatalf("result=%+v executions=%d err=%v", result, executions, err)
		}
	})
	t.Run("openai/text-despite-tool-finish", func(t *testing.T) {
		executions := 0
		provider := openAIFixture(t, func(w http.ResponseWriter) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{map[string]any{
					"finish_reason": "tool_calls",
					"message":       map[string]any{"role": "assistant", "content": "hello"},
				}},
			})
		})
		result, err := provider.RunAgent(context.Background(), AgentRequest{
			Messages: []AgentMessage{{Role: "user", Content: "Look"}},
			Tools: []ToolDef{{Name: "read", Risk: RiskRead, Handler: func(context.Context, json.RawMessage) (string, error) {
				executions++
				return `{}`, nil
			}}},
		})
		if err != nil || executions != 0 || result.StopReason != "stop" || result.Text != "hello" {
			t.Fatalf("result=%+v executions=%d err=%v", result, executions, err)
		}
	})
	t.Run("anthropic/tools-despite-end-turn", func(t *testing.T) {
		executions := 0
		provider := anthropicFixture(t, map[string]any{
			"stop_reason": "end_turn",
			"content": []any{
				map[string]any{"type": "tool_use", "id": "c1", "name": "read", "input": map[string]any{}},
			},
			"usage": map[string]int{"input_tokens": 1, "output_tokens": 1},
		}, map[string]any{
			"stop_reason": "end_turn",
			"content":     []any{map[string]any{"type": "text", "text": "done"}},
			"usage":       map[string]int{"input_tokens": 1, "output_tokens": 1},
		})
		result, err := provider.RunAgent(context.Background(), AgentRequest{
			Messages: []AgentMessage{{Role: "user", Content: "Look"}},
			Tools: []ToolDef{{Name: "read", Risk: RiskRead, Handler: func(context.Context, json.RawMessage) (string, error) {
				executions++
				return `{}`, nil
			}}},
		})
		if err != nil || executions != 1 || result.StopReason != "stop" {
			t.Fatalf("result=%+v executions=%d err=%v", result, executions, err)
		}
	})
	t.Run("anthropic/text-despite-tool-stop", func(t *testing.T) {
		executions := 0
		provider := anthropicFixture(t, map[string]any{
			"stop_reason": "tool_use",
			"content":     []any{map[string]any{"type": "text", "text": "hello"}},
			"usage":       map[string]int{"input_tokens": 1, "output_tokens": 1},
		})
		result, err := provider.RunAgent(context.Background(), AgentRequest{
			Messages: []AgentMessage{{Role: "user", Content: "Look"}},
			Tools: []ToolDef{{Name: "read", Risk: RiskRead, Handler: func(context.Context, json.RawMessage) (string, error) {
				executions++
				return `{}`, nil
			}}},
		})
		if err != nil || executions != 0 || result.StopReason != "stop" || result.Text != "hello" {
			t.Fatalf("result=%+v executions=%d err=%v", result, executions, err)
		}
	})
}

func TestAgentFeedsToolFailureBackAndContinues(t *testing.T) {
	for _, kind := range []string{"openai", "anthropic"} {
		t.Run(kind, func(t *testing.T) {
			requests, approvals := 0, 0
			var second []byte
			provider := agentFixtureProvider(kind, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw := readRaw(t, r)
				requests++
				if requests == 2 {
					second = append([]byte(nil), raw...)
					writeAgentFixture(w, kind, "Done")
					return
				}
				writeAgentFixture(w, kind, "",
					ToolCall{ID: "u", Name: "ghost", Args: json.RawMessage(`{}`)},
					ToolCall{ID: "b", Name: "boom", Args: json.RawMessage(`{}`)},
				)
			}))
			result, err := provider.RunAgent(context.Background(), AgentRequest{
				Messages: []AgentMessage{{Role: "user", Content: "Try"}},
				Tools: []ToolDef{{Name: "boom", Risk: RiskRead, Handler: func(context.Context, json.RawMessage) (string, error) {
					return "", errors.New("boom")
				}}},
				Approve: func(context.Context, ToolDef, ToolCall) error { approvals++; return nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.StopReason != "stop" || result.Text != "Done" || approvals != 0 || len(result.Messages) != 5 {
				t.Fatalf("result=%+v approvals=%d", result, approvals)
			}
			if result.Messages[2].Role != "tool" || result.Messages[2].ToolCallID != "u" || !strings.Contains(result.Messages[2].Content, "unknown tool") {
				t.Fatalf("unknown tool message = %+v", result.Messages[2])
			}
			if result.Messages[3].Role != "tool" || result.Messages[3].ToolCallID != "b" || !strings.Contains(result.Messages[3].Content, "boom") {
				t.Fatalf("handler error message = %+v", result.Messages[3])
			}
			assertToolResultWire(t, kind, second)
		})
	}
}

func assertToolResultWire(t *testing.T, kind string, raw []byte) {
	t.Helper()
	if kind == "anthropic" {
		var req antRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Fatal(err)
		}
		carriers := 0
		var got []string
		for _, m := range req.Messages {
			if m.Role != "user" {
				continue
			}
			var here []string
			for _, b := range m.Content {
				if b.Type == "tool_result" {
					here = append(here, b.Content)
				}
			}
			if len(here) == 0 {
				continue
			}
			carriers++
			got = here
		}
		if carriers != 1 || len(got) != 2 || !strings.Contains(got[0], "unknown tool") || !strings.Contains(got[1], "boom") {
			t.Fatalf("anthropic tool results carriers=%d %#v", carriers, got)
		}
		return
	}
	var req oaiRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range req.Messages {
		if m.Role == "tool" {
			got = append(got, m.Content)
		}
	}
	if len(got) != 2 || !strings.Contains(got[0], "unknown tool") || !strings.Contains(got[1], "boom") {
		t.Fatalf("openai tool messages = %#v", got)
	}
}

func TestAgentApprovalStopsBeforeAnyHandler(t *testing.T) {
	for _, kind := range []string{"openai", "anthropic"} {
		t.Run(kind, func(t *testing.T) {
			executions := 0
			provider := agentFixtureProvider(kind, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeAgentFixture(w, kind, "",
					ToolCall{ID: "a", Name: "write_a", Args: json.RawMessage(`{}`)},
					ToolCall{ID: "b", Name: "write_b", Args: json.RawMessage(`{}`)},
				)
			}))
			handler := func(context.Context, json.RawMessage) (string, error) {
				executions++
				return `{}`, nil
			}
			result, err := provider.RunAgent(context.Background(), AgentRequest{
				Messages: []AgentMessage{{Role: "user", Content: "Change"}},
				Tools: []ToolDef{
					{Name: "write_a", Risk: RiskWrite, Handler: handler},
					{Name: "write_b", Risk: RiskWrite, Handler: handler},
				},
				Approve: func(_ context.Context, _ ToolDef, call ToolCall) error {
					if call.ID == "b" {
						return ErrApprovalRequired
					}
					return nil
				},
			})
			if err != nil || result == nil || executions != 0 || result.StopReason != "approval_required" || result.Iterations != 1 {
				t.Fatalf("result=%+v executions=%d err=%v", result, executions, err)
			}
			if result.Pending == nil || result.Pending.Call.ID != "b" || result.Pending.Risk != RiskWrite {
				t.Fatalf("pending = %+v", result.Pending)
			}
			if len(result.Messages) != 1 || result.Messages[0].Role != "user" || len(result.Messages[0].ToolCalls) != 0 {
				t.Fatalf("messages = %+v", result.Messages)
			}
		})
	}
}

func TestAgentApproveErrorAborts(t *testing.T) {
	for _, kind := range []string{"openai", "anthropic"} {
		t.Run(kind, func(t *testing.T) {
			executions := 0
			provider := agentFixtureProvider(kind, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeAgentFixture(w, kind, "", ToolCall{ID: "w", Name: "write", Args: json.RawMessage(`{}`)})
			}))
			result, err := provider.RunAgent(context.Background(), AgentRequest{
				Messages: []AgentMessage{{Role: "user", Content: "Change"}},
				Tools: []ToolDef{{Name: "write", Risk: RiskWrite, Handler: func(context.Context, json.RawMessage) (string, error) {
					executions++
					return `{}`, nil
				}}},
				Approve: func(context.Context, ToolDef, ToolCall) error { return errors.New("nope") },
			})
			if result == nil || err == nil || !strings.Contains(err.Error(), "nope") || executions != 0 {
				t.Fatalf("result=%+v executions=%d err=%v", result, executions, err)
			}
			if result.StopReason != "error" || result.Iterations != 1 || result.Pending != nil || len(result.Messages) != 1 || result.Messages[0].Role != "user" {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}

func TestOpenAIEmitsTextBeforeApprovalPause(t *testing.T) {
	for _, kind := range []string{"openai-stream", "anthropic"} {
		t.Run(kind, func(t *testing.T) {
			var events []AgentEvent
			provider := agentFixtureProvider(kind, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeAgentFixture(w, kind, "Looking", ToolCall{ID: "w", Name: "write", Args: json.RawMessage(`{}`)})
			}))
			result, err := provider.RunAgent(context.Background(), AgentRequest{
				Messages: []AgentMessage{{Role: "user", Content: "Change"}},
				Tools: []ToolDef{{Name: "write", Risk: RiskWrite, Handler: func(context.Context, json.RawMessage) (string, error) {
					t.Error("handler ran")
					return `{}`, nil
				}}},
				Approve: func(context.Context, ToolDef, ToolCall) error { return ErrApprovalRequired },
				OnEvent: func(ev AgentEvent) { events = append(events, ev) },
			})
			if err != nil || result.StopReason != "approval_required" || len(result.Messages) != 1 || result.Messages[0].Role != "user" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			for _, ev := range events {
				if ev.Type == EventToolStart {
					t.Fatal("tool_start fired before approval")
				}
			}
			if kind == "anthropic" {
				if len(events) != 1 || events[0].Type != EventIteration {
					t.Fatalf("anthropic events = %+v", events)
				}
				return
			}
			if len(events) != 3 || events[0].Type != EventIteration || events[1].Type != EventTextDelta || events[1].Text != "Looking" || events[2].Type != EventText || events[2].Text != "Looking" {
				t.Fatalf("events = %+v", events)
			}
		})
	}
}

func TestAgentTokenSplit(t *testing.T) {
	t.Run("openai", func(t *testing.T) {
		provider := openAIFixture(t, func(w http.ResponseWriter) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "hi"}}},
				"usage":   map[string]int{"prompt_tokens": 3, "completion_tokens": 4, "total_tokens": 9},
			})
		})
		result, err := provider.RunAgent(context.Background(), AgentRequest{
			Messages: []AgentMessage{{Role: "user", Content: "Hi"}},
		})
		if err != nil || result.TokensUsed != 9 || result.InputTokens != 3 || result.OutputTokens != 4 {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})
	t.Run("anthropic", func(t *testing.T) {
		provider := anthropicFixture(t, map[string]any{
			"content": []any{map[string]any{"type": "text", "text": "hi"}},
			"usage":   map[string]int{"input_tokens": 3, "output_tokens": 4},
		})
		result, err := provider.RunAgent(context.Background(), AgentRequest{
			Messages: []AgentMessage{{Role: "user", Content: "Hi"}},
		})
		if err != nil || result.TokensUsed != 7 || result.InputTokens != 3 || result.OutputTokens != 4 {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})
}

func TestRunAgentStitchesStreamedToolArguments(t *testing.T) {
	requests, executions := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/event-stream")
		if requests == 1 {
			writeSSE(w,
				map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{
					"index": 0, "id": "call-1", "type": "function",
					"function": map[string]any{"name": "lookup", "arguments": `{"a":`},
				}}}}}},
				map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{
					"index": 0, "function": map[string]any{"arguments": "1}"},
				}}}}}},
			)
			return
		}
		writeSSE(w, map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": "Done"}}}})
	}))
	defer srv.Close()
	p := &openAIProvider{baseURL: srv.URL, http: srv.Client()}
	result, err := p.RunAgent(context.Background(), AgentRequest{
		Messages: []AgentMessage{{Role: "user", Content: "Look"}},
		Tools: []ToolDef{{Name: "lookup", Risk: RiskRead, Handler: func(_ context.Context, args json.RawMessage) (string, error) {
			executions++
			if string(args) != `{"a":1}` {
				t.Errorf("args = %s", args)
			}
			return "ok", nil
		}}},
		OnEvent: func(AgentEvent) {},
	})
	if err != nil || executions != 1 || result.StopReason != "stop" || result.Text != "Done" {
		t.Fatalf("result=%+v executions=%d err=%v", result, executions, err)
	}
}

func TestAgentFinalTurnDropsCatalogInstructions(t *testing.T) {
	for _, kind := range []string{"openai", "openai-stream", "anthropic"} {
		t.Run(kind, func(t *testing.T) {
			var iters []int
			requests := 0
			provider := agentFixtureProvider(kind, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw := readRaw(t, r)
				system, tools, toolName := requestTools(t, kind, raw)
				requests++
				if requests == 1 {
					if tools != 1 || !strings.Contains(system, "Only some of your tools are listed") {
						t.Errorf("first request tools=%d system=%q", tools, system)
					}
					writeAgentFixture(w, kind, "", ToolCall{ID: "search", Name: toolName, Args: json.RawMessage(`{"query":"tool_0001"}`)})
					return
				}
				if tools != 0 || !strings.Contains(system, "MARKER-caller-system") || !strings.Contains(system, "Do not invent findings") || strings.Contains(system, "Only some of your tools are listed") || !strings.Contains(string(raw), "total_matches") {
					t.Errorf("final request tools=%d system=%q", tools, system)
				}
				writeAgentFixture(w, kind, "Answer")
			}))
			req := AgentRequest{
				System:               "MARKER-caller-system",
				Messages:             []AgentMessage{{Role: "user", Content: "Why"}},
				Tools:                catalogTestTools(129),
				MaxIterations:        2,
				ReserveFinalResponse: true,
				PreIteration: func(_ context.Context, iter int) error {
					iters = append(iters, iter)
					return nil
				},
			}
			if kind != "openai" {
				req.OnEvent = func(AgentEvent) {}
			}
			result, err := provider.RunAgent(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if result.Text != "Answer" || len(iters) != 2 || iters[0] != 1 || iters[1] != 2 {
				t.Fatalf("result=%+v iterations=%v", result, iters)
			}
		})
	}
}

func requestTools(t *testing.T, kind string, raw []byte) (string, int, string) {
	t.Helper()
	if kind == "anthropic" {
		var req antRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Error(err)
		}
		name := ""
		if len(req.Tools) > 0 {
			name = req.Tools[0].Name
		}
		return req.System, len(req.Tools), name
	}
	var req oaiRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Error(err)
	}
	system := ""
	if len(req.Messages) > 0 && req.Messages[0].Role == "system" {
		system = req.Messages[0].Content
	}
	name := ""
	if len(req.Tools) > 0 {
		name = req.Tools[0].Function.Name
	}
	return system, len(req.Tools), name
}

func TestRunAgentEmptyModelUsesFreeDefault(t *testing.T) {
	t.Run("openai", func(t *testing.T) {
		var model string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Model string `json:"model"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			model = body.Model
			writeAgentResponse(w, false, "ok")
		}))
		defer srv.Close()
		p := &openAIProvider{baseURL: srv.URL, http: srv.Client(), modelTrial: "trial-x", modelPaid: "paid-y"}
		if _, err := p.RunAgent(context.Background(), AgentRequest{Messages: []AgentMessage{{Role: "user", Content: "Hi"}}}); err != nil {
			t.Fatal(err)
		}
		if model != "trial-x" {
			t.Fatalf("model = %s", model)
		}
	})
	t.Run("anthropic", func(t *testing.T) {
		var model string
		p := &anthropicProvider{http: &http.Client{Transport: agentFixtureTransport{handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Model string `json:"model"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			model = body.Model
			writeAgentFixture(w, "anthropic", "ok")
		})}}}
		if p.ModelForTier(true) != ModelAgentPaidAnthropic {
			t.Fatalf("paid model = %s", p.ModelForTier(true))
		}
		if _, err := p.RunAgent(context.Background(), AgentRequest{Messages: []AgentMessage{{Role: "user", Content: "Hi"}}}); err != nil {
			t.Fatal(err)
		}
		if model != ModelAgentFreeAnthropic || model == p.ModelForTier(true) {
			t.Fatalf("model = %s", model)
		}
	})
}

func openAIFixture(t *testing.T, writes ...func(http.ResponseWriter)) Provider {
	t.Helper()
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writes[n](w)
		if n < len(writes)-1 {
			n++
		}
	}))
	t.Cleanup(srv.Close)
	return &openAIProvider{baseURL: srv.URL, http: srv.Client()}
}

func anthropicFixture(t *testing.T, bodies ...map[string]any) Provider {
	t.Helper()
	n := 0
	return &anthropicProvider{http: &http.Client{Transport: agentFixtureTransport{handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(bodies[n])
		if n < len(bodies)-1 {
			n++
		}
	})}}}
}

func writeSSE(w http.ResponseWriter, chunks ...any) {
	for _, chunk := range chunks {
		raw, _ := json.Marshal(chunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
	}
	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
}
