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

// ErrNotConfigured is returned by WritingGenerator implementations when no
// provider API key is available. Handlers map this to a clear "AI writing
// assistant is not configured" response instead of a generic 500.
var ErrNotConfigured = errors.New("writing assistant not configured: no provider API key")

// Model tiers for the writing assistant. Free orgs are routed to the cheaper,
// faster Haiku model; paid orgs to Sonnet. The strings are Anthropic model IDs;
// callers should use ModelForTier rather than hardcoding.
const (
	ModelWritingFree = "claude-3-5-haiku-latest"
	ModelWritingPaid = "claude-sonnet-4-6"

	// OpenAI models. These are the PRIMARY writing/agent models for Warmbly's
	// hosted product (OpenAI-first) and the default for any OpenAI-compatible
	// self-hosted endpoint.
	ModelWritingFreeOpenAI = "gpt-4o-mini"
	ModelWritingPaidOpenAI = "gpt-4o"

	// Agent-loop models for the Anthropic connector (self-host only): haiku for
	// trial orgs, sonnet for paid.
	ModelAgentFreeAnthropic = "claude-haiku-4-5"
	ModelAgentPaidAnthropic = "claude-sonnet-4-6"

	anthropicMessagesURL = "https://api.anthropic.com/v1/messages"
	anthropicVersion     = "2023-06-01"

	// anthropicWebSearchType is Anthropic's hosted web_search server tool
	// version. Enabled on the connector when a run requests web search.
	anthropicWebSearchType = "web_search_20250305"

	// writingMaxTokens caps a single writing-assistant completion. Generous for
	// an email draft but bounded so a runaway prompt can't burn credits/tokens.
	writingMaxTokens = 1024
)

// WritingResult is the normalized output of a writing-assistant generation,
// regardless of which provider produced it.
type WritingResult struct {
	Text       string
	Model      string
	TokensUsed int
}

// WritingGenerator is the provider-agnostic interface the handler depends on.
// Keeping it behind an interface means a missing API key yields a clear
// ErrNotConfigured at call time, and the provider (Anthropic vs OpenAI
// fallback) can be swapped without touching the handler.
type WritingGenerator interface {
	// GenerateWriting produces assistant text for the given model and prompt,
	// grounded in the org voice profile (product/ICP/house style) plus the
	// caller's optional tone carried in VoiceContext. model is a provider model
	// ID (see ModelForTier).
	GenerateWriting(ctx context.Context, model, prompt string, voice VoiceContext) (*WritingResult, error)

	// ModelForTier returns the model ID this provider should use for the org's
	// tier (paid → stronger model). The handler calls this rather than knowing
	// which concrete provider is active.
	ModelForTier(paid bool) string

	// IsLocal reports whether this is an explicitly free/local backend
	// (AI_FREE). The writing surfaces skip credit charges when true.
	IsLocal() bool
}

// IsLocal is always false for the Anthropic writing client (hosted API).
func (c *AnthropicClient) IsLocal() bool { return false }

// ModelForTier returns the Anthropic writing model ID for the org's tier.
func (c *AnthropicClient) ModelForTier(paid bool) string {
	if paid {
		return ModelWritingPaid
	}
	return ModelWritingFree
}

// AnthropicClient calls the Anthropic Messages API over plain HTTP. We use the
// stdlib rather than adding an SDK dependency so the build stays lean; the
// request shape is the documented v1/messages contract.
type AnthropicClient struct {
	apiKey string
	http   *http.Client
}

// NewAnthropicClient returns a client, or nil if apiKey is empty so callers can
// fall back to another provider.
func NewAnthropicClient(apiKey string) *AnthropicClient {
	if strings.TrimSpace(apiKey) == "" {
		return nil
	}
	return &AnthropicClient{
		apiKey: apiKey,
		http:   &http.Client{Timeout: 60 * time.Second},
	}
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// GenerateWriting implements WritingGenerator against the Anthropic API.
func (c *AnthropicClient) GenerateWriting(ctx context.Context, model, prompt string, voice VoiceContext) (*WritingResult, error) {
	if c == nil {
		return nil, ErrNotConfigured
	}
	if model == "" {
		model = ModelWritingFree
	}

	body, err := json.Marshal(anthropicRequest{
		Model:     model,
		MaxTokens: writingMaxTokens,
		System:    BuildVoiceRules(voice),
		Messages: []anthropicMessage{
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicMessagesURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	var parsed anthropicResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("anthropic: decode response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if parsed.Error != nil {
			return nil, fmt.Errorf("anthropic: %s: %s", parsed.Error.Type, parsed.Error.Message)
		}
		return nil, fmt.Errorf("anthropic: unexpected status %d", resp.StatusCode)
	}

	var sb strings.Builder
	for _, block := range parsed.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}
	text := strings.TrimSpace(sb.String())
	if text == "" {
		return nil, errors.New("anthropic: empty completion")
	}

	return &WritingResult{
		Text:       text,
		Model:      model,
		TokensUsed: parsed.Usage.InputTokens + parsed.Usage.OutputTokens,
	}, nil
}
