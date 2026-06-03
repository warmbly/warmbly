package generation

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go/v2"
)

var ConversationSchema = GenerateSchema[Conversation]()

// GenerateConversation produces one realistic warmup email thread for a theme.
//
// The prompt is tuned for warmup deliverability: plaintext, a few short
// sentences, one natural question, and explicitly NO links, phone numbers,
// attachments, emoji, or marketing language (all of which raise spam scores).
// model and maxMessages are admin-controlled (empty model → gpt-4o-mini).
func (c *GenerationClient) GenerateConversation(ctx context.Context, theme, model string, maxMessages int) (*Conversation, error) {
	if maxMessages <= 0 {
		maxMessages = 4
	}
	if maxMessages > 5 {
		maxMessages = 5
	}

	systemPrompt := fmt.Sprintf(`Output valid JSON only.

Create content for a short, friendly, realistic warmup email on the topic: %s

Return:
- subject: a short, lowercase-natural subject line, 2-6 words, never prefixed with "Re:".
- description: the OPENING message body only — a couple of short, natural
  sentences a colleague would actually write. Do NOT include a greeting,
  sign-off, signature, or the subject.
- messages: %d short follow-up question lines (one sentence each) that could
  naturally continue the conversation.

Rules: warm, collegial, plain text. NO marketing or sales language, NO links or
URLs, NO phone numbers, NO attachments, NO emoji, NO ALL-CAPS, NO exclamation
spam.`, theme, maxMessages)

	chatModel := openai.ChatModelGPT4oMini
	if model != "" {
		chatModel = openai.ChatModel(model)
	}

	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        "conversation",
		Description: openai.String("Realistic branched email warmup thread"),
		Schema:      ConversationSchema,
		Strict:      openai.Bool(true),
	}

	req := openai.ChatCompletionNewParams{
		Model: chatModel,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage("Generate one complete thread now."),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{JSONSchema: schemaParam},
		},
	}

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
