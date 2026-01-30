package generation

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go/v2"
)

var ConversationSchema = GenerateSchema[Conversation]()

func (c *GenerationClient) GenerateConversation(ctx context.Context, theme string) (*Conversation, error) {
	systemPrompt := fmt.Sprintf(`
Output valid JSON only.

Positive realistic warmup email thread for theme: %s

- Friendly colleague tone, occasional 😊👍, always ask questions to continue
- Use {{.FirstName}} {{.LastName}} {{.Email}} {{.Company}} {{.Signature}} naturally
- Every body = only text sender writes + {{.Signature}} at end (no headers, no quotes, no "On ...")
- Aim for 10–14 messages total across branches
- Include at least 4 branch points with 2–4 variants each (short/long/different questions)
- Keep replies natural & positive
`, theme)

	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        "conversation",
		Description: openai.String("Realistic branched email warmup thread"),
		Schema:      ConversationSchema,
		Strict:      openai.Bool(true),
	}

	req := openai.ChatCompletionNewParams{
		Model: openai.ChatModelGPT4oMini,
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

	var parsed Conversation
	if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &parsed); err != nil {
		return nil, err
	}

	return &parsed, nil
}
