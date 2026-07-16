package generation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// anthropicProvider is the optional self-host connector: it drives the same
// tool-use agent loop over Anthropic's Messages API. Warmbly's hosted product
// runs on OpenAI; this exists so a self-hoster who prefers Anthropic can set
// AI_PROVIDER=anthropic and get the identical Provider
// behavior.
type anthropicProvider struct {
	apiKey string
	http   *http.Client
}

func newAnthropicProvider(cfg ProviderConfig) *anthropicProvider {
	return &anthropicProvider{
		apiKey: cfg.AnthropicAPIKey,
		http:   &http.Client{Timeout: 90 * time.Second},
	}
}

func (p *anthropicProvider) Name() string { return "anthropic" }

// IsLocal is always false: the Anthropic connector targets Anthropic's hosted
// API, which is never a free/local backend.
func (p *anthropicProvider) IsLocal() bool { return false }

func (p *anthropicProvider) ModelForTier(paid bool) string {
	if paid {
		return ModelAgentPaidAnthropic
	}
	return ModelAgentFreeAnthropic
}

// --- Anthropic Messages wire types (tool-use) ---

type antTool struct {
	Type        string         `json:"type,omitempty"` // set for hosted server tools (web_search)
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

type antContentBlock struct {
	Type string `json:"type"`
	// text block
	Text string `json:"text,omitempty"`
	// tool_use block
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result block
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
}

type antMessage struct {
	Role    string            `json:"role"`
	Content []antContentBlock `json:"content"`
}

type antRequest struct {
	Model       string       `json:"model"`
	MaxTokens   int          `json:"max_tokens"`
	System      string       `json:"system,omitempty"`
	Messages    []antMessage `json:"messages"`
	Tools       []antTool    `json:"tools,omitempty"`
	Temperature *float64     `json:"temperature,omitempty"`
}

type antResponse struct {
	Content []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// transcriptToAnthropic converts the provider-agnostic transcript into
// Anthropic messages, grouping consecutive tool results into a single user
// message (Anthropic requires all tool_results for a turn in one user turn).
func transcriptToAnthropic(msgs []AgentMessage) []antMessage {
	var out []antMessage
	var pendingToolResults []antContentBlock

	flush := func() {
		if len(pendingToolResults) > 0 {
			out = append(out, antMessage{Role: "user", Content: pendingToolResults})
			pendingToolResults = nil
		}
	}

	for _, m := range msgs {
		switch m.Role {
		case "tool":
			pendingToolResults = append(pendingToolResults, antContentBlock{
				Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Content,
			})
		case "assistant":
			flush()
			blocks := make([]antContentBlock, 0, 1+len(m.ToolCalls))
			if strings.TrimSpace(m.Content) != "" {
				blocks = append(blocks, antContentBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, antContentBlock{Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: tc.Args})
			}
			out = append(out, antMessage{Role: "assistant", Content: blocks})
		default: // user
			flush()
			out = append(out, antMessage{Role: "user", Content: []antContentBlock{{Type: "text", Text: m.Content}}})
		}
	}
	flush()
	return out
}

func antToolsFromDefs(tools []ToolDef, enableWebSearch bool) []antTool {
	out := make([]antTool, 0, len(tools)+1)
	for _, t := range tools {
		out = append(out, antTool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	if enableWebSearch {
		out = append(out, antTool{Type: anthropicWebSearchType, Name: "web_search"})
	}
	return out
}

func (p *anthropicProvider) complete(ctx context.Context, model string, maxTokens int, system string, msgs []antMessage, tools []antTool, temperature *float64) (*antResponse, error) {
	body, err := json.Marshal(antRequest{Model: model, MaxTokens: maxTokens, System: system, Messages: msgs, Tools: tools, Temperature: temperature})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicMessagesURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var parsed antResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("anthropic: decode response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if parsed.Error != nil {
			return nil, fmt.Errorf("anthropic: %s: %s", parsed.Error.Type, parsed.Error.Message)
		}
		return nil, fmt.Errorf("anthropic: unexpected status %d", resp.StatusCode)
	}
	return &parsed, nil
}

// Complete runs a single tool-less completion with an explicit system prompt.
func (p *anthropicProvider) Complete(ctx context.Context, req CompletionRequest) (*WritingResult, error) {
	if p == nil {
		return nil, ErrNotConfigured
	}
	model := req.Model
	if model == "" {
		model = ModelAgentFreeAnthropic
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultAgentTokens
	}
	msgs := []antMessage{{Role: "user", Content: []antContentBlock{{Type: "text", Text: req.Prompt}}}}
	resp, err := p.complete(ctx, model, maxTokens, req.System, msgs, nil, req.Temperature)
	if err != nil {
		return nil, err
	}
	var sb strings.Builder
	for _, b := range resp.Content {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	text := strings.TrimSpace(sb.String())
	if text == "" {
		return nil, errors.New("anthropic: empty completion")
	}
	return &WritingResult{Text: text, Model: model, TokensUsed: resp.Usage.InputTokens + resp.Usage.OutputTokens}, nil
}

// RunAgent implements the tool-use loop against Anthropic. Same approval/resume
// contract as the OpenAI provider (see provider.go).
func (p *anthropicProvider) RunAgent(ctx context.Context, req AgentRequest) (*AgentResult, error) {
	model := req.Model
	if model == "" {
		model = ModelAgentFreeAnthropic
	}
	maxIter := req.MaxIterations
	if maxIter <= 0 {
		maxIter = defaultMaxIterations
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultAgentTokens
	}

	byName := make(map[string]ToolDef, len(req.Tools))
	for _, t := range req.Tools {
		byName[t.Name] = t
	}
	tools := antToolsFromDefs(req.Tools, false)

	messages := append([]AgentMessage(nil), req.Messages...)
	result := &AgentResult{Model: model}

	for iter := 0; iter < maxIter; iter++ {
		if req.PreIteration != nil {
			if err := req.PreIteration(ctx, iter+1); err != nil {
				result.Messages = messages
				result.StopReason = "stopped"
				return result, nil
			}
		}
		result.Iterations++
		if req.OnEvent != nil {
			req.OnEvent(AgentEvent{Type: EventIteration, Iteration: result.Iterations})
		}

		resp, err := p.complete(ctx, model, maxTokens, req.System, transcriptToAnthropic(messages), tools, nil)
		if err != nil {
			return nil, err
		}
		result.TokensUsed += resp.Usage.InputTokens + resp.Usage.OutputTokens

		// Split the response into text and tool_use calls.
		var text strings.Builder
		var calls []ToolCall
		for _, b := range resp.Content {
			switch b.Type {
			case "text":
				text.WriteString(b.Text)
			case "tool_use":
				calls = append(calls, ToolCall{ID: b.ID, Name: b.Name, Args: b.Input})
			}
		}

		if len(calls) == 0 {
			final := strings.TrimSpace(text.String())
			messages = append(messages, AgentMessage{Role: "assistant", Content: final})
			if req.OnEvent != nil && final != "" {
				req.OnEvent(AgentEvent{Type: EventText, Text: final})
			}
			result.Text = final
			result.Messages = messages
			result.StopReason = "stop"
			return result, nil
		}

		// Approval gate before executing any tool (see OpenAI provider).
		if req.Approve != nil {
			for _, call := range calls {
				tool, ok := byName[call.Name]
				if !ok || tool.Risk == RiskRead {
					continue
				}
				if err := req.Approve(ctx, tool, call); err != nil {
					if errors.Is(err, ErrApprovalRequired) {
						result.Messages = messages
						result.StopReason = "approval_required"
						result.Pending = &PendingToolCall{Call: call, Risk: tool.Risk}
						return result, nil
					}
					return nil, err
				}
			}
		}

		messages = append(messages, AgentMessage{Role: "assistant", Content: text.String(), ToolCalls: calls})
		for _, call := range calls {
			if req.OnEvent != nil {
				req.OnEvent(AgentEvent{Type: EventToolStart, ToolName: call.Name, ToolArgs: call.Args})
			}
			out := execToolShared(ctx, byName, call)
			if req.OnEvent != nil {
				req.OnEvent(AgentEvent{Type: EventToolResult, ToolName: call.Name, ToolResult: out})
			}
			messages = append(messages, AgentMessage{Role: "tool", ToolCallID: call.ID, Content: out})
		}
	}

	result.Messages = messages
	result.StopReason = "max_iterations"
	return result, nil
}

// execToolShared runs one tool handler, returning the result string (or a JSON
// error string). Shared shape with the OpenAI provider's execTool.
func execToolShared(ctx context.Context, byName map[string]ToolDef, call ToolCall) string {
	tool, ok := byName[call.Name]
	if !ok {
		return fmt.Sprintf(`{"error":"unknown tool %q"}`, call.Name)
	}
	out, err := tool.Handler(ctx, call.Args)
	if err != nil {
		b, _ := json.Marshal(map[string]string{"error": err.Error()})
		return string(b)
	}
	return out
}
