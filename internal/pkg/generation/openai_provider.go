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
	"sync/atomic"
	"time"
)

// openAIProvider is the primary provider: it drives the tool-use agent loop and
// the writing assistant over the OpenAI (or any OpenAI-compatible) chat
// completions API via a lean HTTP client. Using plain HTTP (rather than the SDK
// used for the warmup batch path) keeps the tool-call JSON explicit and lets a
// self-hoster retarget the whole loop with an AI_PROVIDER preset or AI_BASE_URL.
type openAIProvider struct {
	apiKey     string
	baseURL    string
	modelTrial string
	modelPaid  string
	search     SearchClient
	local      bool
	http       *http.Client

	// Sticky parameter-compatibility flags. Newer OpenAI models (gpt-5.x,
	// o-series) reject the legacy max_tokens param (they want
	// max_completion_tokens) and any non-default temperature, while most
	// OpenAI-compatible backends (Ollama, Groq, OpenRouter) only know the
	// legacy shape. Instead of a model-family table that goes stale, the first
	// such 400 flips the flag and the call retries adapted; every later call
	// uses the adapted shape directly.
	useMaxCompletionTokens atomic.Bool
	omitTemperature        atomic.Bool
}

// defaultOpenAIBaseURL is the public OpenAI API. Overridable for
// OpenAI-compatible self-hosted endpoints (Ollama, vLLM, LocalAI, OpenRouter).
const defaultOpenAIBaseURL = "https://api.openai.com/v1"

// defaultLocalModel is the fall-back model tag for a free/local backend
// (AI_FREE) when no explicit model id is set, so a dev who forgets
// AI_MODEL does not 404 on "gpt-4o-mini". llama3.1 has reliable
// tool-calling in Ollama; override with AI_MODEL (or AI_MODEL_TRIAL / _PAID).
const defaultLocalModel = "llama3.1"

func newOpenAIProvider(cfg ProviderConfig) *openAIProvider {
	base := strings.TrimRight(strings.TrimSpace(cfg.OpenAIBaseURL), "/")
	if base == "" {
		base = defaultOpenAIBaseURL
	}
	// Empty model ids default to the hosted OpenAI models, or a local-friendly
	// tag when this is an explicit free/local backend.
	fallbackTrial, fallbackPaid := ModelWritingFreeOpenAI, ModelWritingPaidOpenAI
	if cfg.Local {
		fallbackTrial, fallbackPaid = defaultLocalModel, defaultLocalModel
	}
	trial := cfg.OpenAIModelTrial
	if trial == "" {
		trial = fallbackTrial
	}
	paid := cfg.OpenAIModelPaid
	if paid == "" {
		paid = fallbackPaid
	}
	return &openAIProvider{
		apiKey:     cfg.OpenAIAPIKey,
		baseURL:    base,
		modelTrial: trial,
		modelPaid:  paid,
		search:     cfg.Search,
		local:      cfg.Local,
		http:       &http.Client{Timeout: 90 * time.Second},
	}
}

func (p *openAIProvider) Name() string { return "openai" }

func (p *openAIProvider) IsLocal() bool { return p.local }

func (p *openAIProvider) ModelForTier(paid bool) string {
	if paid {
		return p.modelPaid
	}
	return p.modelTrial
}

// SearchClient exposes the configured web-search backend so the search_web tool
// (M2) can reach it through the provider. Nil when search is not configured.
func (p *openAIProvider) SearchClient() SearchClient { return p.search }

// --- chat-completions wire types (OpenAI-compatible) ---

type oaiTool struct {
	Type     string      `json:"type"`
	Function oaiToolFunc `json:"function"`
}

type oaiToolFunc struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type oaiToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type oaiMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content,omitempty"`
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type oaiRequest struct {
	Model string `json:"model"`
	// Exactly one of MaxTokens / MaxCompletionTokens is set per call, driven
	// by the provider's useMaxCompletionTokens compatibility flag.
	MaxTokens           int          `json:"max_tokens,omitempty"`
	MaxCompletionTokens int          `json:"max_completion_tokens,omitempty"`
	Messages            []oaiMessage `json:"messages"`
	Tools               []oaiTool    `json:"tools,omitempty"`
	ToolChoice          string       `json:"tool_choice,omitempty"`
	Temperature         *float64     `json:"temperature,omitempty"`
}

type oaiResponse struct {
	Choices []struct {
		Message struct {
			Content   string        `json:"content"`
			ToolCalls []oaiToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *oaiError `json:"error"`
}

type oaiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param"`
	Code    string `json:"code"`
}

// toolDefsToWire converts the provider-agnostic tool defs to OpenAI tools.
func toolDefsToWire(tools []ToolDef) []oaiTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]oaiTool, 0, len(tools))
	for _, t := range tools {
		out = append(out, oaiTool{
			Type: "function",
			Function: oaiToolFunc{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}
	return out
}

// transcriptToWire renders the running transcript into OpenAI messages, with
// the system prompt prepended.
func transcriptToWire(system string, msgs []AgentMessage) []oaiMessage {
	out := make([]oaiMessage, 0, len(msgs)+1)
	if strings.TrimSpace(system) != "" {
		out = append(out, oaiMessage{Role: "system", Content: system})
	}
	for _, m := range msgs {
		wm := oaiMessage{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			var call oaiToolCall
			call.ID = tc.ID
			call.Type = "function"
			call.Function.Name = tc.Name
			call.Function.Arguments = string(tc.Args)
			wm.ToolCalls = append(wm.ToolCalls, call)
		}
		out = append(out, wm)
	}
	return out
}

// complete performs one chat-completion call, retrying once per parameter when
// the backend rejects the legacy request shape (see the compatibility flags on
// openAIProvider).
func (p *openAIProvider) complete(ctx context.Context, model string, maxTokens int, msgs []oaiMessage, tools []oaiTool, temperature *float64) (*oaiResponse, error) {
	for attempt := 0; ; attempt++ {
		reqBody := oaiRequest{Model: model, Messages: msgs}
		if p.useMaxCompletionTokens.Load() {
			reqBody.MaxCompletionTokens = maxTokens
		} else {
			reqBody.MaxTokens = maxTokens
		}
		if !p.omitTemperature.Load() {
			reqBody.Temperature = temperature
		}
		if len(tools) > 0 {
			reqBody.Tools = tools
			reqBody.ToolChoice = "auto"
		}
		body, err := json.Marshal(reqBody)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+p.apiKey)

		resp, err := p.http.Do(req)
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		var parsed oaiResponse
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, fmt.Errorf("openai: decode response: %w", err)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			if resp.StatusCode == http.StatusBadRequest && attempt < 2 && p.adaptParams(parsed.Error) {
				continue
			}
			if parsed.Error != nil {
				return nil, fmt.Errorf("openai: %s: %s", parsed.Error.Type, parsed.Error.Message)
			}
			return nil, fmt.Errorf("openai: unexpected status %d", resp.StatusCode)
		}
		if len(parsed.Choices) == 0 {
			return nil, errors.New("openai: empty completion")
		}
		return &parsed, nil
	}
}

// adaptParams flips the sticky compatibility flag matching a 400 that names a
// parameter this backend rejects. Returns true when a retry makes sense.
func (p *openAIProvider) adaptParams(e *oaiError) bool {
	if e == nil {
		return false
	}
	switch {
	case e.Param == "max_tokens" || strings.Contains(e.Message, "max_completion_tokens"):
		if p.useMaxCompletionTokens.Load() {
			return false
		}
		p.useMaxCompletionTokens.Store(true)
		return true
	case e.Param == "temperature":
		if p.omitTemperature.Load() {
			return false
		}
		p.omitTemperature.Store(true)
		return true
	}
	return false
}

// RunAgent implements the provider-agnostic tool-use loop. See provider.go for
// the approval/resume contract.
func (p *openAIProvider) RunAgent(ctx context.Context, req AgentRequest) (*AgentResult, error) {
	model := req.Model
	if model == "" {
		model = p.modelTrial
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
	wireTools := toolDefsToWire(req.Tools)

	// messages is the durable transcript; we mutate a copy so the caller's
	// slice is not aliased.
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

		resp, err := p.complete(ctx, model, maxTokens, transcriptToWire(req.System, messages), wireTools, nil)
		if err != nil {
			return nil, err
		}
		result.TokensUsed += resp.Usage.TotalTokens
		choice := resp.Choices[0]

		// No tool calls: final answer.
		if len(choice.Message.ToolCalls) == 0 {
			text := strings.TrimSpace(choice.Message.Content)
			messages = append(messages, AgentMessage{Role: "assistant", Content: text})
			if req.OnEvent != nil && text != "" {
				req.OnEvent(AgentEvent{Type: EventText, Text: text})
			}
			result.Text = text
			result.Messages = messages
			result.StopReason = "stop"
			return result, nil
		}

		// Approval gate: check every requested tool BEFORE executing any, so we
		// never leave a partial assistant turn (which the API rejects). On a
		// pause, return the transcript as it was at loop entry (without this
		// assistant turn) so a resumed run re-issues the call cleanly.
		calls := make([]ToolCall, 0, len(choice.Message.ToolCalls))
		for _, tc := range choice.Message.ToolCalls {
			calls = append(calls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Args: json.RawMessage(tc.Function.Arguments)})
		}
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

		// Execute all tool calls and append the assistant turn + results.
		assistant := AgentMessage{Role: "assistant", Content: choice.Message.Content, ToolCalls: calls}
		messages = append(messages, assistant)
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

	// Exhausted iterations without a final answer.
	result.Messages = messages
	result.StopReason = "max_iterations"
	return result, nil
}

// Complete runs a single tool-less completion with an explicit system prompt.
func (p *openAIProvider) Complete(ctx context.Context, req CompletionRequest) (*WritingResult, error) {
	if p == nil {
		return nil, ErrNotConfigured
	}
	model := req.Model
	if model == "" {
		model = p.modelTrial
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultAgentTokens
	}
	resp, err := p.complete(ctx, model, maxTokens, []oaiMessage{
		{Role: "system", Content: req.System},
		{Role: "user", Content: req.Prompt},
	}, nil, req.Temperature)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(resp.Choices[0].Message.Content)
	if text == "" {
		return nil, errors.New("openai: empty completion")
	}
	return &WritingResult{Text: text, Model: model, TokensUsed: resp.Usage.TotalTokens}, nil
}

// --- WritingGenerator port ---

// GenerateWriting implements WritingGenerator over the chat-completions API,
// behaviorally identical to the prior OpenAI writing path (voice system prompt,
// single completion, writingMaxTokens cap).
func (p *openAIProvider) GenerateWriting(ctx context.Context, model, prompt string, voice VoiceContext) (*WritingResult, error) {
	if p == nil {
		return nil, ErrNotConfigured
	}
	if model == "" {
		model = p.modelTrial
	}
	resp, err := p.complete(ctx, model, writingMaxTokens, []oaiMessage{
		{Role: "system", Content: BuildVoiceRules(voice)},
		{Role: "user", Content: prompt},
	}, nil, nil)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(resp.Choices[0].Message.Content)
	if text == "" {
		return nil, errors.New("openai: empty completion")
	}
	return &WritingResult{Text: text, Model: model, TokensUsed: resp.Usage.TotalTokens}, nil
}
