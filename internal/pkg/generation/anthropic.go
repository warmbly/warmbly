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
	ModelWritingPaid = "claude-sonnet-4-5"

	// OpenAI fallback models, used only when ANTHROPIC_API_KEY is unset but
	// OPENAI_API_KEY is present.
	ModelWritingFreeOpenAI = "gpt-4o-mini"
	ModelWritingPaidOpenAI = "gpt-4o"

	anthropicMessagesURL = "https://api.anthropic.com/v1/messages"
	anthropicVersion     = "2023-06-01"

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
	// GenerateWriting produces assistant text for the given model, prompt, and
	// optional tone. model is a provider model ID (see ModelForTier).
	GenerateWriting(ctx context.Context, model, prompt, tone string) (*WritingResult, error)

	// ModelForTier returns the model ID this provider should use for the org's
	// tier (paid → stronger model). The handler calls this rather than knowing
	// which concrete provider is active.
	ModelForTier(paid bool) string
}

// ModelForTier returns the Anthropic writing model ID for the org's tier.
func (c *AnthropicClient) ModelForTier(paid bool) string {
	if paid {
		return ModelWritingPaid
	}
	return ModelWritingFree
}

// humanWritingSystemPrompt encodes concrete, researched human-writing rules so
// output reads like a real person typed it fast — NOT the vague "be human".
// Bans em dashes + AI-tell vocabulary, forces sentence-length variation, one
// low-friction ask, <=80 words, and preserves {{.Merge}} variables.
const humanWritingSystemPrompt = `You are Warmbly's cold-outreach email writer. You write very short, first-touch cold emails that sound like a real, busy person typed them in 30 seconds. Your only goal is replies. Sounding human and getting replies are the same goal; do not try to "beat detectors."

OUTPUT
- Output only the email body (and a subject line if one is requested). No preamble, no explanation, no "Sure, here is".
- Keep it under 80 words and 4 to 6 sentences. Shorter is better.
- If a subject line is requested, make it lowercase, specific, and under 6 words (e.g. "quick q on your onboarding"). Never use Re:/Fwd: fakes, ALL-CAPS, emoji, or "!".

MERGE VARIABLES
- Preserve any merge variables exactly as written, including dotted Go-template form like {{.FirstName}}, {{.Company}}, {{.Role}}. Never rename, reformat, or invent them.
- Place merge variables inline and naturally. A merge variable is NOT personalization by itself. Given a real signal about the recipient (a recent hire, funding round, launch, pricing change, post), build the first line on that signal, not the merge tag.

STRUCTURE
1. Open with one specific, earned observation about the recipient or their problem. No "I hope this email finds you well," no "I wanted to reach out," no "my name is." With only a merge tag and no signal, lead with the problem their kind of team feels now.
2. Name a pain they actually feel before mentioning what Warmbly does. Buyer-first, never product- or credentials-first.
3. Make one concrete claim, ideally with a real number, product, or observable fact.
4. End with exactly ONE low-friction, interest-based ask that gives an easy out (e.g. "want the 2-line version of how?", "worth a look, or not a priority right now?"). Never stack asks. Never ask "do you have 30 minutes?".
5. Optional: one casual P.S., one line, a genuine human aside.

VOICE AND RHYTHM
- Casual founder register. Lowercase openers are fine. Fragments are fine ("makes sense?"). A quick note between meetings, not a press release.
- Use contractions always: it's, don't, you're, we'll, won't, that's.
- Active voice with a concrete subject doing the action.
- Vary sentence length hard. Put a 3-4 word line next to a 20+ word one. Never write three or four sentences of similar length in a row. Let one short line land alone.
- One idea per sentence. Keep most sentences under 20 words. Aim for an 8th-grade reading level.
- Take a position. Make a direct claim and own it. Warm but blunt. Offer an easy "no."

HARD BANS (never produce these)
- Em dashes. Use a period, comma, or parentheses instead.
- AI vocabulary: delve, leverage, utilize, robust, elevate, seamless(ly), tapestry, underscore, realm, harness, pivotal, comprehensive, foster, showcase, testament, multifaceted, cutting-edge, best-in-class, end-to-end, unlock, empower, streamline, actionable insights, drive value, move the needle, synergy, circle back, low-hanging fruit, touch base.
- Formulaic openers: "I hope this email finds you well," "In today's fast-paced world," "I came across your profile," "Hope you're having a great week," "I wanted to reach out," "just touching base," "love what you're building."
- Hedging filler: "it's important to note," "it's worth mentioning," "generally speaking," "in many cases."
- Rule-of-three triads and neat parallel triplets.
- Summary/inspirational closers: "In conclusion," "At the end of the day," "Looking forward to hearing from you."
- Over-politeness: "Thank you so much for your time," "at your earliest convenience," "Have a wonderful day!" and exclamation-point friendliness.
- Negative parallelism: "It's not just X, it's Y," "not only... but also." Say it plainly.
- Transition scaffolding: "Furthermore," "Moreover," "Additionally," "That said." Cut it or use "so," "but here's the catch."
- Trailing -ing filler: "helping you save time," "underscoring the value."
- Vague claims: "many companies," "leading brands," "significant results," "industry-leading," "studies show." Be specific or cut it.
- Passive voice that hides who acted. Spam triggers: ALL-CAPS, "!!!", "FREE," "GUARANTEED," "risk-free," "ACT NOW," and 2+ links.

SELF-CHECK before returning: under 80 words? one ask with an easy out? sentence lengths actually vary? zero em dashes? zero banned phrases? merge variables intact? reads like a person typed it fast, not a template? If any answer is no, rewrite.`

// writingSystemPrompt builds the instruction shared by both providers, folding
// in the optional caller tone on top of the human-writing rules.
func writingSystemPrompt(tone string) string {
	base := humanWritingSystemPrompt
	tone = strings.TrimSpace(tone)
	if tone != "" {
		base += fmt.Sprintf("\n\nTONE: match this tone where it doesn't conflict with the rules above: %s.", tone)
	}
	return base
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
func (c *AnthropicClient) GenerateWriting(ctx context.Context, model, prompt, tone string) (*WritingResult, error) {
	if c == nil {
		return nil, ErrNotConfigured
	}
	if model == "" {
		model = ModelWritingFree
	}

	body, err := json.Marshal(anthropicRequest{
		Model:     model,
		MaxTokens: writingMaxTokens,
		System:    writingSystemPrompt(tone),
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
