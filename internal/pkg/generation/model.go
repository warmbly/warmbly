package generation

// Conversation is the structured output of warmup content generation. The
// shape matches how the warmup renderer consumes content: it composes the
// greeting, sign-off and signature itself, so Description is the opening body
// text only (no greeting/signature) and Messages are ordered reply turns.
type Conversation struct {
	Subject     string   `json:"subject" jsonschema:"description=Short lowercase-natural subject line, 2-6 words, never prefixed with Re:"`
	Description string   `json:"description" jsonschema:"description=The opening message body ONLY: a couple of short natural sentences. No greeting, no sign-off, no signature, no subject line."`
	Messages    []string `json:"messages" jsonschema:"description=Ordered natural reply bodies that alternate between participants and continue the thread. One or two sentences each, with no greeting or sign-off."`
}
