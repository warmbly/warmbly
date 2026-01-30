package generation

type Conversation struct {
	Title       string                `json:"title" jsonschema:"description=Short descriptive title"`
	Description string                `json:"description" jsonschema:"description=1-2 sentence summary"`
	Subject     string                `json:"subject" jsonschema:"description=Initial subject line (no Re:)"`
	Messages    []ConversationMessage `json:"messages" jsonschema:"minItems=1"`
}

type ConversationMessage struct {
	Body     string                `json:"body" jsonschema:"description=Plaintext content the sender actually types. Greeting + message + sign-off + {{.Signature}} at the very bottom. No email headers, no quoting, no 'On ... wrote:'. Keep positive and natural."`
	Messages []ConversationMessage `json:"messages" jsonschema:"description=Alternative replies (0-4). Use to create branches with different tones/lengths/questions. Aim for deep threads."`
}
