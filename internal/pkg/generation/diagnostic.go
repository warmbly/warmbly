package generation

import (
	"fmt"
	"strings"
)

const DiagnosticScenarioVersion = "diagnostic-v1"
const CanonicalRenderingVersion = "canonical-v1"

// ValidateDiagnostic checks structure; semantic review is a separate result.
func ValidateDiagnostic(c Conversation) error {
	if !strings.HasPrefix(c.Subject, "Simulated diagnostic:") || strings.ContainsAny(c.Subject, "<>{}\x00\r\n") {
		return fmt.Errorf("missing diagnostic subject or invalid subject header")
	}
	if !strings.HasPrefix(c.Description, "Simulated diagnostic.") || len(c.Messages) == 0 || len(c.Messages) > 5 {
		return fmt.Errorf("missing diagnostic disclosure or invalid turn count")
	}
	turns := append([]string{c.Description}, c.Messages...)
	for _, text := range turns {
		if strings.TrimSpace(text) == "" || strings.ContainsAny(text, "<>{}") || strings.ContainsAny(text, "\x00\r") {
			return fmt.Errorf("empty turn or unsupported markup, placeholders or control characters")
		}
	}
	if strings.Contains(c.Messages[len(c.Messages)-1], "?") {
		return fmt.Errorf("final turn must close without another question")
	}
	return nil
}

func RenderCanonicalTurn(body, sender string) (string, error) {
	if strings.TrimSpace(body) == "" || strings.ContainsAny(body, "<>{}\x00\r") || strings.TrimSpace(sender) == "" || strings.ContainsAny(sender, "\r\n\x00") {
		return "", fmt.Errorf("invalid canonical body or sender identity")
	}
	return strings.TrimSpace(body) + "\n\nSimulated diagnostic.\n" + sender, nil
}

// RenderDiagnostic alternates actual sender identities without rewriting bodies.
func RenderDiagnostic(c Conversation, senders [2]string) (Conversation, error) {
	if err := ValidateDiagnostic(c); err != nil {
		return Conversation{}, err
	}
	out := c
	var err error
	out.Description, err = RenderCanonicalTurn(c.Description, senders[0])
	if err != nil {
		return Conversation{}, err
	}
	out.Messages = make([]string, len(c.Messages))
	for i, message := range c.Messages {
		out.Messages[i], err = RenderCanonicalTurn(message, senders[(i+1)%2])
		if err != nil {
			return Conversation{}, err
		}
	}
	return out, nil
}
