package generation

import (
	"context"
	"encoding/json"
	"errors"
)

// This file defines the provider-agnostic agent-loop contract used by every
// server-side AI feature (dashboard agent, contact research, automation AI
// nodes, inbox agent). Warmbly's hosted product runs on OpenAI; the same
// interface lets a self-hoster point at any OpenAI-compatible endpoint
// (Ollama, vLLM, LocalAI, OpenRouter) via an AI_PROVIDER preset or AI_BASE_URL, or use the
// Anthropic connector, without any caller change.

// RiskClass classifies what a tool can do, which drives the approval policy in
// the agent loop (read auto-runs; write pauses for approval; send always
// requires an explicit per-action human action).
type RiskClass string

const (
	RiskRead  RiskClass = "read"
	RiskWrite RiskClass = "write"
	RiskSend  RiskClass = "send"
)

// ToolDef is the LLM-facing definition of a tool the agent loop can call. It is
// the provider-level projection of a richer aitools.Tool (M2): only what the
// model needs (name, description, JSON-schema input) plus the Go handler the
// loop executes. Permission gating and approval happen in the caller/registry,
// not inside the provider.
type ToolDef struct {
	Name        string
	Description string
	// InputSchema is a JSON Schema object describing the tool's arguments.
	InputSchema map[string]any
	Risk        RiskClass
	// Handler executes the tool for the given JSON args and returns a result
	// string (usually JSON) fed back to the model as the tool result. An error
	// is surfaced to the model as a tool error so it can recover or stop.
	Handler func(ctx context.Context, args json.RawMessage) (string, error)
}

// ToolCall is one tool invocation the model requested, carried in an assistant
// message so the transcript is provider-agnostic and resumable (M3 persists it
// as agent_messages jsonb).
type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

// AgentMessage is one entry in the running transcript. Role is one of
// "user", "assistant", "tool". An assistant turn may carry ToolCalls; a tool
// turn carries ToolCallID + Content (the result).
type AgentMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// AgentEventType enumerates the streamed step kinds surfaced via OnEvent (the
// M3 SSE loop maps these to text deltas / tool_start / tool_result events).
type AgentEventType string

const (
	EventText       AgentEventType = "text"
	EventToolStart  AgentEventType = "tool_start"
	EventToolResult AgentEventType = "tool_result"
	EventIteration  AgentEventType = "iteration"
)

// AgentEvent is a single streamed step from RunAgent.
type AgentEvent struct {
	Type       AgentEventType
	Text       string
	ToolName   string
	ToolArgs   json.RawMessage
	ToolResult string
	Iteration  int
}

// AgentRequest is one RunAgent invocation.
type AgentRequest struct {
	// System is the system prompt.
	System string
	// Messages is the running transcript (may include prior tool turns for a
	// resumed run). At minimum one user message.
	Messages []AgentMessage
	// Tools available this run. May be empty for a plain completion.
	Tools []ToolDef
	// Model is the provider model id; empty means the provider's default for
	// the tier (see ModelForTier).
	Model string
	// MaxIterations bounds tool-use rounds (each model call is one iteration).
	// Zero uses the provider default.
	MaxIterations int
	// MaxTokens caps a single completion. Zero uses the provider default.
	MaxTokens int
	// OnEvent, if set, receives streamed step events. Optional.
	OnEvent func(AgentEvent)
	// Approve, if set, is called before executing a non-read tool. Returning
	// ErrApprovalRequired stops the loop and returns a result whose Pending
	// holds the tool call awaiting approval, so the caller can persist state
	// and resume. Returning any other error aborts the run.
	Approve func(ctx context.Context, tool ToolDef, call ToolCall) error
	// PreIteration, if set, is called at the start of each loop iteration
	// (before the model call). Returning an error stops the loop cleanly with
	// StopReason "stopped" and returns the transcript so far. Used to charge
	// per-iteration credits and enforce a per-run budget; the caller records
	// its own reason (e.g. out-of-credits) before returning.
	PreIteration func(ctx context.Context, iteration int) error
}

// PendingToolCall is the tool awaiting approval when a run stops with
// StopReason "approval_required".
type PendingToolCall struct {
	Call ToolCall
	Risk RiskClass
}

// AgentResult is the final output of a RunAgent run.
type AgentResult struct {
	// Text is the model's final assistant text (empty when the run stopped for
	// approval before producing final text).
	Text string
	// Messages is the full transcript including this run's new turns, so a
	// caller can persist it and resume.
	Messages []AgentMessage
	Model    string
	// TokensUsed is the summed input+output tokens across all iterations.
	TokensUsed int
	Iterations int
	// StopReason is "stop" (final text), "max_iterations", or
	// "approval_required".
	StopReason string
	// Pending is set only when StopReason == "approval_required".
	Pending *PendingToolCall
}

// Compile-time guarantees that both providers satisfy Provider, and that the
// OpenAI provider also serves the writing assistant.
var (
	_ Provider         = (*openAIProvider)(nil)
	_ Provider         = (*anthropicProvider)(nil)
	_ WritingGenerator = (*openAIProvider)(nil)
	_ WritingGenerator = (*AnthropicClient)(nil)
	_ WritingGenerator = (*GenerationClient)(nil)
)

// CompletionRequest is a single, tool-less generation with an explicit system
// prompt. Used by reply drafts (M4) and automation ai_generate (M9).
type CompletionRequest struct {
	System    string
	Prompt    string
	Model     string
	MaxTokens int
	// Temperature optionally pins sampling. nil leaves it to the provider default
	// (creative writing); a pointer to 0 forces deterministic output (the reply
	// classifier's Layer 3 needs a stable single-label verdict).
	Temperature *float64
}

// Deterministic returns a *float64 pointing at 0, for
// CompletionRequest.Temperature when a caller needs a stable, non-sampled result
// (e.g. single-label classification). Each call returns a fresh pointer.
func Deterministic() *float64 {
	z := 0.0
	return &z
}

// Provider is a pluggable LLM backend that can run a tool-use agent loop.
type Provider interface {
	// RunAgent executes the tool-use loop and returns the final result.
	RunAgent(ctx context.Context, req AgentRequest) (*AgentResult, error)
	// Complete runs a single completion (no tools) with an explicit system
	// prompt, returning the text + token usage.
	Complete(ctx context.Context, req CompletionRequest) (*WritingResult, error)
	// ModelForTier returns the provider's model id for the org tier (paid gets
	// the stronger model).
	ModelForTier(paid bool) string
	// Name identifies the provider ("openai" or "anthropic"). Stays stable for
	// accounting even when pointed at a local/self-hosted endpoint.
	Name() string
	// IsLocal reports whether this provider is an explicitly-declared free/local
	// backend (AI_FREE). Callers use it to warn the user and to skip
	// credit charges; it is never inferred from the base URL (OpenRouter/Azure
	// also set one), so it stays a deliberate operator opt-in.
	IsLocal() bool
}

// ErrApprovalRequired is returned by an AgentRequest.Approve hook to pause the
// loop for human approval of a write/send tool. RunAgent turns it into a
// result with StopReason "approval_required".
var ErrApprovalRequired = errors.New("tool approval required")

// ErrProviderNotConfigured is returned by NewProvider when neither an OpenAI
// nor an Anthropic key is configured.
var ErrProviderNotConfigured = errors.New("no LLM provider configured: set AI_PROVIDER and AI_API_KEY")

// Default agent-loop bounds, applied when a request leaves them zero.
const (
	defaultMaxIterations = 12
	defaultAgentTokens   = 2048
)

// ProviderConfig configures provider construction. Only the keys and optional
// overrides that a self-hoster sets; everything else defaults sensibly.
type ProviderConfig struct {
	// OpenAIAPIKey enables the OpenAI provider (the preferred/default backend).
	OpenAIAPIKey string
	// OpenAIBaseURL overrides the OpenAI API base (e.g. an OpenAI-compatible
	// self-hosted endpoint). Empty uses the public OpenAI API.
	OpenAIBaseURL string
	// OpenAIModelTrial / OpenAIModelPaid override the per-tier model ids (so a
	// self-hoster can route to whatever their endpoint serves). Empty uses the
	// built-in gpt-4o-mini / gpt-4o defaults (or the local default when Local).
	OpenAIModelTrial string
	OpenAIModelPaid  string

	// Local marks this as a free/local model backend (AI_FREE). It flips
	// the empty-model default to a local-friendly tag and lets the agent warn
	// the user and skip credit charges. Set deliberately by the operator; never
	// inferred from OpenAIBaseURL.
	Local bool

	// AnthropicAPIKey enables the Anthropic connector (used only when no OpenAI
	// key is set).
	AnthropicAPIKey string

	// Search is the pluggable web-search client used by the search_web tool
	// (M2). Optional; when nil the tool returns a clean not-configured error.
	Search SearchClient
}

// NewProvider selects and constructs the active provider. OpenAI is preferred
// (Warmbly's hosted default and the pluggable self-host path); the Anthropic
// connector is used only when no OpenAI key is present. Returns
// ErrProviderNotConfigured when neither is set.
func NewProvider(cfg ProviderConfig) (Provider, error) {
	if cfg.OpenAIAPIKey != "" {
		return newOpenAIProvider(cfg), nil
	}
	if cfg.AnthropicAPIKey != "" {
		return newAnthropicProvider(cfg), nil
	}
	return nil, ErrProviderNotConfigured
}
