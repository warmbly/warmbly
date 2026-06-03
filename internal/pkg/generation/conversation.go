package generation

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go/v2"
)

var ConversationSchema = GenerateSchema[Conversation]()

// normalizeMaxMessages clamps the admin-controlled follow-up count into the
// range the warmup prompt is tuned for. Shared so the sync and batch paths
// produce identical thread shapes.
func normalizeMaxMessages(maxMessages int) int {
	if maxMessages <= 0 {
		return 4
	}
	if maxMessages > 5 {
		return 5
	}
	return maxMessages
}

// warmupSystemPrompt builds the deliverability-tuned system prompt for one
// theme. Extracted so the sync path (GenerateConversation) and the Batch API
// path (SubmitBatch) send the exact same instruction, keeping the two
// generation modes byte-for-byte identical.
func warmupSystemPrompt(theme string, maxMessages int) string {
	// The prompt is grounded in the ACTUAL signals AI-text detectors key on
	// (burstiness, contractions, AI-accent vocabulary, stock openers, symmetric
	// templates), not a vague "sound human". A deterministic post-processing
	// pass (internal/pkg/humanlint) then strips residual tells before the
	// content is cached, so this is the first of two coordinated layers.
	return fmt.Sprintf(`Output valid JSON only.

Write a short, real message a busy colleague would actually send to someone they
already know, about: %s. You are that person, NOT an assistant.

Return:
- subject: 2-6 words, lowercase-natural, never prefixed with "Re:" or "Fwd:".
- description: the OPENING body only (no greeting, sign-off, signature, or the
  subject) — 1 to 3 sentences.
- messages: %d short follow-up lines (one sentence each) that could continue it.

Write like a person, by following how people actually write — not by trying to
"sound human":
- Use contractions naturally (I'm, don't, you're, it's, can't, I'll, that's,
  we've, didn't). Don't contract every single one; leave a couple expanded.
- Vary sentence length hard. Put at least one very short line or fragment
  (2-5 words, e.g. "Makes sense." / "No rush.") next to a longer one. Never let
  every sentence land at the same length.
- Include exactly one concrete, specific detail a stranger couldn't guess — a
  day ("Tuesday"), a time ("after lunch"), a named thing ("the second draft",
  "the deck"), or a small number.
- Start with the actual point or a bare "hey" — never a stock opener ("I hope
  this email finds you well", "I wanted to reach out", "I just wanted to").
- No intro-body-conclusion shape, no restating, no "in conclusion".

Hard bans (these are the strongest AI tells):
- NO AI-accent words: delve, leverage, utilize, harness, robust, seamless,
  underscore, showcase, foster, streamline, elevate, pivotal, comprehensive,
  testament, tapestry, realm, synergy, paradigm, furthermore, moreover,
  additionally. Use plain words (use, show, solid, smooth, key, full, also).
- NO "not only X but also Y", NO "it's not X, it's Y", NO rule-of-three lists
  ("fast, reliable, and affordable"). Use at most one dash; prefer none.
- NO hedging/corporate filler: "it's worth noting", "in order to", "circle
  back", "touch base", "at the end of the day". It's fine to start with "And"
  or "But".
- Plain text only. No links, URLs, phone numbers, attachments, emoji, ALL-CAPS,
  or marketing/sales language. At most one "!", prefer zero.

Vary the shape across messages — sometimes a fragment plus a question, sometimes
one longer line — so a batch isn't uniform.`, theme, maxMessages)
}

// conversationResponseFormat returns the strict JSON-schema response format
// shared by sync and batch generation.
func conversationResponseFormat() openai.ChatCompletionNewParamsResponseFormatUnion {
	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        "conversation",
		Description: openai.String("Realistic branched email warmup thread"),
		Schema:      ConversationSchema,
		Strict:      openai.Bool(true),
	}
	return openai.ChatCompletionNewParamsResponseFormatUnion{
		OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{JSONSchema: schemaParam},
	}
}

// buildConversationParams assembles the chat-completion request for one warmup
// thread. Both GenerateConversation (sync) and SubmitBatch (Batch API) use this
// so the request bodies are identical; the only difference is the transport.
func buildConversationParams(theme, model string, maxMessages int) openai.ChatCompletionNewParams {
	chatModel := openai.ChatModelGPT4oMini
	if model != "" {
		chatModel = openai.ChatModel(model)
	}
	return openai.ChatCompletionNewParams{
		Model: chatModel,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(warmupSystemPrompt(theme, maxMessages)),
			openai.UserMessage("Generate one complete thread now."),
		},
		ResponseFormat: conversationResponseFormat(),
	}
}

// GenerateConversation produces one realistic warmup email thread for a theme.
//
// The prompt is tuned for warmup deliverability: plaintext, a few short
// sentences, one natural question, and explicitly NO links, phone numbers,
// attachments, emoji, or marketing language (all of which raise spam scores).
// model and maxMessages are admin-controlled (empty model → gpt-4o-mini).
func (c *GenerationClient) GenerateConversation(ctx context.Context, theme, model string, maxMessages int) (*Conversation, error) {
	req := buildConversationParams(theme, model, normalizeMaxMessages(maxMessages))

	resp, err := c.client.Chat.Completions.New(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("generation returned no choices")
	}

	var parsed Conversation
	if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &parsed); err != nil {
		return nil, err
	}

	return &parsed, nil
}
