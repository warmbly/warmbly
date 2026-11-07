package generation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type agentFixtureTransport struct{ handler http.Handler }

// RoundTrip routes provider requests to deterministic fixtures without external network access.
func (tr agentFixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	tr.handler.ServeHTTP(w, req)
	return w.Result(), nil
}

// agentFixtureProvider exercises each provider adapter against the same local HTTP contract.
func agentFixtureProvider(kind string, handler http.Handler) Provider {
	client := &http.Client{Transport: agentFixtureTransport{handler: handler}}
	if kind == "anthropic" {
		return &anthropicProvider{http: client}
	}
	return &openAIProvider{baseURL: "http://provider.test", http: client}
}

// writeAgentFixture emits equivalent tool and text responses in each provider's wire format.
func writeAgentFixture(w http.ResponseWriter, kind, text string, calls ...ToolCall) {
	if kind != "anthropic" {
		writeAgentResponse(w, kind == "openai-stream", text, calls...)
		return
	}
	blocks := []antContentBlock{{Type: "text", Text: text}}
	for _, call := range calls {
		blocks = append(blocks, antContentBlock{Type: "tool_use", ID: call.ID, Name: call.Name, Input: call.Args})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"content": blocks, "usage": map[string]int{"input_tokens": 6, "output_tokens": 4},
	})
}

// TestAgentReservesFinalResponseOnlyWhenRequested keeps chat answers inside the caller's turn and credit budget.
func TestAgentReservesFinalResponseOnlyWhenRequested(t *testing.T) {
	for _, kind := range []string{"openai", "openai-stream", "anthropic"} {
		for _, reserve := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reserve=%t", kind, reserve), func(t *testing.T) {
				requests, executions := 0, 0
				var iterations []int
				var finalText string
				provider := agentFixtureProvider(kind, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var req struct {
						Tools    []json.RawMessage `json:"tools"`
						Messages json.RawMessage   `json:"messages"`
						System   string            `json:"system"`
					}
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						t.Error(err)
					}
					requests++
					if reserve && requests == 2 {
						if len(req.Tools) != 0 || !strings.Contains(string(req.Messages), "verified evidence") {
							t.Error("final request must keep tool evidence and omit tools")
						}
						if !strings.Contains(req.System+string(req.Messages), "Do not invent findings") {
							t.Error("final request lacks instructions to ground the answer")
						}
						writeAgentFixture(w, kind, "I could not verify the sending cause.")
						return
					}
					writeAgentFixture(w, kind, "", ToolCall{ID: fmt.Sprint(requests), Name: "read", Args: json.RawMessage(`{}`)})
				}))
				req := AgentRequest{
					Messages: []AgentMessage{{Role: "user", Content: "Why did sending stop?"}},
					Tools: []ToolDef{{Name: "read", Risk: RiskRead, Handler: func(context.Context, json.RawMessage) (string, error) {
						executions++
						return "verified evidence", nil
					}}},
					MaxIterations: 2, ReserveFinalResponse: reserve,
					PreIteration: func(_ context.Context, iter int) error { iterations = append(iterations, iter); return nil },
					OnEvent: func(ev AgentEvent) {
						if ev.Type == EventText {
							finalText = ev.Text
						}
					},
				}
				if kind == "openai" {
					req.OnEvent = nil
				}
				result, err := provider.RunAgent(context.Background(), req)
				if err != nil {
					t.Fatal(err)
				}
				if requests != 2 || result.Iterations != 2 || result.TokensUsed != 20 || !reflect.DeepEqual(iterations, []int{1, 2}) || result.StopReason != "max_iterations" {
					t.Fatalf("result=%+v requests=%d charged iterations=%v", result, requests, iterations)
				}
				if reserve {
					last := result.Messages[len(result.Messages)-1]
					if executions != 1 || result.Text != "I could not verify the sending cause." || kind != "openai" && finalText != result.Text || last.Role != "assistant" || last.Content != result.Text || len(last.ToolCalls) != 0 {
						t.Fatalf("missing final answer: result=%+v executions=%d event=%q", result, executions, finalText)
					}
				} else if executions != 2 || result.Text != "" {
					t.Fatalf("tool-only caller behavior changed: result=%+v executions=%d", result, executions)
				}
			})
		}
	}
}

// TestAgentReturnsEarlyAnswerWithoutSpendingReservedTurn avoids charging another turn after an answer arrives.
func TestAgentReturnsEarlyAnswerWithoutSpendingReservedTurn(t *testing.T) {
	for _, kind := range []string{"openai", "openai-stream", "anthropic"} {
		t.Run(kind, func(t *testing.T) {
			requests, charges := 0, 0
			provider := agentFixtureProvider(kind, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				writeAgentFixture(w, kind, "The mailbox is paused.")
			}))
			result, err := provider.RunAgent(context.Background(), AgentRequest{
				Messages:             []AgentMessage{{Role: "user", Content: "Why did sending stop?"}},
				ReserveFinalResponse: true,
				PreIteration:         func(context.Context, int) error { charges++; return nil },
				OnEvent:              func(AgentEvent) {},
			})
			if err != nil {
				t.Fatal(err)
			}
			if requests != 1 || charges != 1 || result.Iterations != 1 || result.StopReason != "stop" || result.Text != "The mailbox is paused." {
				t.Fatalf("early answer spent extra turns: requests=%d charges=%d result=%+v", requests, charges, result)
			}
		})
	}
}

// TestAgentRejectsEmptyAnswer requires visible text for a successful chat completion.
func TestAgentRejectsEmptyAnswer(t *testing.T) {
	for _, kind := range []string{"openai", "openai-stream", "anthropic"} {
		t.Run(kind, func(t *testing.T) {
			provider := agentFixtureProvider(kind, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeAgentFixture(w, kind, "  ")
			}))
			req := AgentRequest{
				Messages: []AgentMessage{{Role: "user", Content: "Explain the sending status"}}, ReserveFinalResponse: true,
			}
			if kind == "openai-stream" {
				req.OnEvent = func(AgentEvent) {}
			}
			result, err := provider.RunAgent(context.Background(), req)
			if err == nil {
				t.Fatalf("empty answer returned as success: %+v", result)
			}
		})
	}
}

// TestAgentAllowsEmptyStopForToolOnlyCallers preserves research flows that report findings through tools.
func TestAgentAllowsEmptyStopForToolOnlyCallers(t *testing.T) {
	for _, kind := range []string{"openai", "openai-stream", "anthropic"} {
		t.Run(kind, func(t *testing.T) {
			provider := agentFixtureProvider(kind, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeAgentFixture(w, kind, "")
			}))
			result, err := provider.RunAgent(context.Background(), AgentRequest{
				Messages: []AgentMessage{
					{Role: "user", Content: "Save the findings and stop"},
					{Role: "assistant", ToolCalls: []ToolCall{{ID: "save-1", Name: "save_research", Args: json.RawMessage(`{}`)}}},
					{Role: "tool", ToolCallID: "save-1", Content: `{"saved":true}`},
				},
				OnEvent: func(AgentEvent) {},
			})
			if err != nil || result.StopReason != "stop" || result.Text != "" || len(result.Messages) != 4 {
				t.Fatalf("tool-only completion changed: result=%+v err=%v", result, err)
			}
		})
	}
}

// TestAgentHonorsStopBeforeReservedFinalResponse keeps the final answer behind the caller's credit gate.
func TestAgentHonorsStopBeforeReservedFinalResponse(t *testing.T) {
	for _, kind := range []string{"openai", "openai-stream", "anthropic"} {
		t.Run(kind, func(t *testing.T) {
			requests := 0
			provider := agentFixtureProvider(kind, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				writeAgentFixture(w, kind, "", ToolCall{ID: "read-1", Name: "read", Args: json.RawMessage(`{}`)})
			}))
			result, err := provider.RunAgent(context.Background(), AgentRequest{
				Messages: []AgentMessage{{Role: "user", Content: "Inspect sending"}},
				Tools: []ToolDef{{Name: "read", Risk: RiskRead, Handler: func(context.Context, json.RawMessage) (string, error) {
					return `{}`, nil
				}}},
				MaxIterations: 2, ReserveFinalResponse: true,
				OnEvent: func(AgentEvent) {},
				PreIteration: func(_ context.Context, iter int) error {
					if iter == 2 {
						return errors.New("no remaining credits")
					}
					return nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if requests != 1 || result.Iterations != 1 || result.StopReason != "stopped" || result.Messages[len(result.Messages)-1].Role != "tool" {
				t.Fatalf("stop hook was bypassed: result=%+v requests=%d", result, requests)
			}
		})
	}
}

// TestAgentNeverExecutesToolsInReservedResponse refuses tool execution after the tool budget closes.
func TestAgentNeverExecutesToolsInReservedResponse(t *testing.T) {
	for _, kind := range []string{"openai", "openai-stream", "anthropic"} {
		t.Run(kind, func(t *testing.T) {
			provider := agentFixtureProvider(kind, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeAgentFixture(w, kind, "", ToolCall{ID: "write-1", Name: "write", Args: json.RawMessage(`{}`)})
			}))
			executions, approvals := 0, 0
			_, err := provider.RunAgent(context.Background(), AgentRequest{
				Messages: []AgentMessage{{Role: "user", Content: "Inspect sending"}},
				Tools: []ToolDef{{Name: "write", Risk: RiskWrite, Handler: func(context.Context, json.RawMessage) (string, error) {
					executions++
					return `{}`, nil
				}}},
				MaxIterations: 1, ReserveFinalResponse: true,
				OnEvent: func(AgentEvent) {},
				Approve: func(context.Context, ToolDef, ToolCall) error { approvals++; return nil },
			})
			if err == nil || executions != 0 || approvals != 0 {
				t.Fatalf("unexpected final tool call: err=%v executions=%d approvals=%d", err, executions, approvals)
			}
		})
	}
}

// TestAgentPreservesCompletedToolsWhenFinalResponseFails retains tool evidence alongside later provider errors.
func TestAgentPreservesCompletedToolsWhenFinalResponseFails(t *testing.T) {
	for _, kind := range []string{"openai", "openai-stream", "anthropic"} {
		for _, failure := range []string{"empty", "unexpected_tool", "provider_error"} {
			t.Run(kind+"/"+failure, func(t *testing.T) {
				requests, executions := 0, 0
				provider := agentFixtureProvider(kind, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests++
					if requests == 1 || failure == "unexpected_tool" {
						writeAgentFixture(w, kind, "", ToolCall{ID: fmt.Sprint(requests), Name: "read", Args: json.RawMessage(`{}`)})
					} else if failure == "provider_error" {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusBadGateway)
						_, _ = w.Write([]byte(`{"error":{"message":"fixture upstream unavailable"}}`))
					} else {
						writeAgentFixture(w, kind, "")
					}
				}))
				result, err := provider.RunAgent(context.Background(), AgentRequest{
					Messages: []AgentMessage{{Role: "user", Content: "Inspect sending"}},
					Tools: []ToolDef{{Name: "read", Risk: RiskRead, Handler: func(context.Context, json.RawMessage) (string, error) {
						executions++
						return "verified evidence", nil
					}}},
					MaxIterations: 2, ReserveFinalResponse: true, OnEvent: func(AgentEvent) {},
				})
				if err == nil || result == nil {
					t.Fatalf("expected partial result with error: result=%+v err=%v", result, err)
				}
				if requests != 2 || executions != 1 || len(result.Messages) != 3 || result.Messages[2].Content != "verified evidence" || result.StopReason != "error" {
					t.Fatalf("completed turns were lost: result=%+v requests=%d executions=%d", result, requests, executions)
				}
			})
		}
	}
}
