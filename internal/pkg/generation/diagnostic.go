package generation

import (
	"fmt"
	"regexp"
	"strings"
)

// DiagnosticScenarioVersion is retired: its rows stay readable for lineage but are never selected again.
const DiagnosticScenarioVersion = "diagnostic-v1"

// WarmupScenarioVersion marks content eligible for new threads.
const WarmupScenarioVersion = "natural-v1"
const CanonicalRenderingVersion = "canonical-v1"

// selfDescribingWording names the mail as generated, simulated or a warmup, which filters and readers both read as synthetic.
var selfDescribingWording = regexp.MustCompile(`(?i)\b(simulated|simulation|diagnostic|hypothetical|fictional|warm-?up|automated|automatically generated|ai-generated|test (email|message|exchange|thread|mailbox))\b`)

// ValidateDiagnostic checks thread structure; semantic review is a separate result.
func ValidateDiagnostic(c Conversation) error {
	if strings.TrimSpace(c.Subject) == "" || strings.ContainsAny(c.Subject, "<>{}\x00\r\n") {
		return fmt.Errorf("missing subject or invalid subject header")
	}
	if len(c.Messages) == 0 || len(c.Messages) > 5 {
		return fmt.Errorf("invalid turn count")
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

// ValidateFreshWarmup admits a thread only when every turn reads as ordinary mail.
func ValidateFreshWarmup(c Conversation) error {
	if err := ValidateDiagnostic(c); err != nil {
		return err
	}
	return ValidateWarmupWording(append([]string{c.Subject, c.Description}, c.Messages...)...)
}

// ValidateWarmupWording applies to every outgoing turn, including queued historical threads.
func ValidateWarmupWording(texts ...string) error {
	for _, text := range texts {
		if selfDescribingWording.MatchString(strings.Join(strings.Fields(text), " ")) {
			return fmt.Errorf("warmup content describes itself as generated, simulated or a warmup")
		}
	}
	return nil
}

// RenderCanonicalTurn signs a turn with the sender; a closing line ending in a comma takes the name directly under it.
func RenderCanonicalTurn(body, sender string) (string, error) {
	if strings.TrimSpace(body) == "" || strings.ContainsAny(body, "<>{}\x00\r") || strings.TrimSpace(sender) == "" || strings.ContainsAny(sender, "\r\n\x00") {
		return "", fmt.Errorf("invalid canonical body or sender identity")
	}
	body = strings.TrimSpace(body)
	if strings.HasSuffix(body, ",") {
		return body + "\n" + sender, nil
	}
	return body + "\n\n" + sender, nil
}

// StripCanonicalSignature removes the sender line RenderCanonicalTurn appended.
func StripCanonicalSignature(rendered, sender string) string {
	if body, ok := strings.CutSuffix(rendered, "\n\n"+sender); ok {
		return body
	}
	if body, ok := strings.CutSuffix(rendered, ",\n"+sender); ok {
		return body + ","
	}
	return rendered
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
