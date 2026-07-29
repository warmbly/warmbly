package generation

import (
	"context"
	"errors"
	"strings"

	"github.com/openai/openai-go/v2"
)

// ModelForTier returns the OpenAI fallback writing model ID for the org's tier.
func (c *GenerationClient) ModelForTier(paid bool) string {
	if paid {
		return ModelWritingPaidOpenAI
	}
	return ModelWritingFreeOpenAI
}

// IsLocal is false: GenerationClient targets the hosted OpenAI API (batch /
// conversation paths); a local OpenAI-compatible endpoint uses openAIProvider as
// the WritingGenerator instead.
func (c *GenerationClient) IsLocal() bool { return false }

// GenerateWriting implements WritingGenerator using the existing OpenAI client.
// The handler depends on the WritingGenerator interface, so it never needs to
// know which provider is active.
func (c *GenerationClient) GenerateWriting(ctx context.Context, model, prompt string, voice VoiceContext) (*WritingResult, error) {
	if c == nil {
		return nil, ErrNotConfigured
	}
	chatModel := openai.ChatModel(ModelWritingFreeOpenAI)
	if model != "" {
		chatModel = openai.ChatModel(model)
	}

	resp, err := c.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: chatModel,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(BuildVoiceRules(voice)),
			openai.UserMessage(prompt),
		},
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, errors.New("openai: empty completion")
	}
	text := strings.TrimSpace(resp.Choices[0].Message.Content)
	if text == "" {
		return nil, errors.New("openai: empty completion")
	}

	return &WritingResult{
		Text:       text,
		Model:      string(chatModel),
		TokensUsed: int(resp.Usage.TotalTokens),
	}, nil
}
