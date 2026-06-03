package generation

// Conversation is the structured output of warmup content generation. The
// shape matches how the warmup renderer consumes content: it composes the
// greeting, sign-off and signature itself, so Description is the opening body
// text only (no greeting/signature) and Messages are follow-up question lines.
type Conversation struct {
	Subject     string   `json:"subject" jsonschema:"description=Short lowercase-natural subject line, 2-6 words, never prefixed with Re:"`
	Description string   `json:"description" jsonschema:"description=The opening message body ONLY: a couple of short natural sentences. No greeting, no sign-off, no signature, no subject line."`
	Messages    []string `json:"messages" jsonschema:"description=Short natural follow-up question lines, one sentence each, that could continue the thread. No greeting or sign-off."`
}
