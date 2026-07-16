package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/app/credits"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"

	"github.com/google/uuid"
)

// AI action nodes (M9). One LLM step runs over the event data and merges its
// output back into the data map (exactly like set_variables), so downstream
// condition nodes can branch on it. Kinds:
//
//   - ai_classify: pick one of the configured labels -> variable "ai_class"
//     (or the node's output_key).
//   - ai_extract:  pull the configured output_keys as string values.
//   - ai_generate: write text from the instruction -> variable "ai_text"
//     (or the node's output_key).
//
// Each node costs one credit, charged to the org, refunded if the provider call
// fails. Out-of-credits fails only that node (the run continues down the normal
// edge); a flow whose org is persistently out of credits auto-pauses after
// aiCreditFailurePauseAt consecutive misses so it stops hammering a hard wall.
const (
	aiNodeCredits          = 1
	aiNodeMaxTokens        = 512
	aiNodeTimeout          = 20 * time.Second
	aiCreditFailurePauseAt = 20

	aiClassVar         = "ai_class"
	aiTextVar          = "ai_text"
	automationRunIDKey = "_run_id"
)

// aiActionConfig is the per-node config for an AI action node.
type aiActionConfig struct {
	// Instruction is the user's prompt, Go-templated against the event data so
	// it can reference {{.subject}}, {{.snippet}}, etc.
	Instruction string `json:"instruction"`
	// Labels is the closed set ai_classify must choose from.
	Labels []string `json:"labels"`
	// OutputKeys are the variable names ai_extract fills from the text.
	OutputKeys []string `json:"output_keys"`
	// OutputKey optionally overrides the default target variable for
	// ai_classify (ai_class) / ai_generate (ai_text) so two AI nodes in one flow
	// don't collide.
	OutputKey string `json:"output_key"`
}

func parseAIConfig(raw json.RawMessage) aiActionConfig {
	var c aiActionConfig
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &c)
	}
	return c
}

// aiActionLabel is a short run-history label for an AI node.
func aiActionLabel(a models.IntegrationAction) string {
	switch a {
	case models.IntegrationActionAIClassify:
		return "AI classify"
	case models.IntegrationActionAIExtract:
		return "AI extract"
	case models.IntegrationActionAIGenerate:
		return "AI generate"
	default:
		return string(a)
	}
}

// execAIAction charges one credit, runs the LLM step under a 20s ceiling, and
// merges the result into data. feedPause is true for live runs (a credit miss
// advances the auto-pause counter) and false for dry-runs (a test must never
// disable the flow, though it is still charged and still shows real output).
func (s *service) execAIAction(ctx context.Context, a models.Automation, n models.AutomationNode, data map[string]any, feedPause bool) error {
	if s.aiProvider == nil || s.credits == nil {
		return errors.New("AI steps are not available on this deployment")
	}
	cfg := parseAIConfig(n.Config)
	instruction := strings.TrimSpace(renderTemplate(cfg.Instruction, data))
	if instruction == "" {
		return errors.New("this AI step has no instruction")
	}

	// gate -> consume(Idempotency-Key) -> call -> refund-on-failure. The key is
	// scoped to this run + node so a retried walk never double-charges. A
	// free/local model (AI_FREE) runs un-metered, so skip the charge.
	model := s.aiProvider.ModelForTier(false)
	idemKey := "auto_ai:" + stringFromMap(data, automationRunIDKey) + ":" + n.ID
	if !s.aiProvider.IsLocal() {
		if _, cerr := s.credits.Consume(ctx, a.OrganizationID, aiNodeCredits, "automation_ai", model, 0, idemKey); cerr != nil {
			switch {
			case errors.Is(cerr, credits.ErrInsufficientCredits):
				if feedPause {
					s.noteAICreditFailure(ctx, a)
				}
				return fmt.Errorf("out of AI credits: this step needs %d credit", aiNodeCredits)
			case errors.Is(cerr, credits.ErrCapExceeded):
				return errors.New("AI usage cap reached; try again later")
			default:
				return cerr
			}
		}
	}

	// Per-node ceiling well under the 30s graph budget.
	cctx, cancel := context.WithTimeout(ctx, aiNodeTimeout)
	defer cancel()

	system, prompt := buildAIPrompt(n.Action, cfg, instruction, data)
	res, gerr := s.aiProvider.Complete(cctx, generation.CompletionRequest{
		System:      system,
		Prompt:      prompt,
		Model:       model,
		MaxTokens:   aiNodeMaxTokens,
		Temperature: aiTemperature(n.Action),
	})
	if gerr != nil || res == nil {
		// The org paid for a step the provider couldn't complete: refund it.
		_, _ = s.credits.Grant(ctx, a.OrganizationID, aiNodeCredits, "automation_ai_refund")
		if gerr != nil {
			return fmt.Errorf("AI step failed: %w", gerr)
		}
		return errors.New("AI step returned no output")
	}

	mergeAIOutput(n.Action, cfg, res.Text, data)
	// A successful LIVE AI node clears the consecutive out-of-credits counter.
	// A dry-run (feedPause=false) must not touch the auto-pause lifecycle: it is
	// a test, not a live delivery, so it neither advances nor resets the counter.
	if feedPause {
		_ = s.repo.ResetAutomationAICreditFailures(ctx, a.ID)
	}
	return nil
}

// noteAICreditFailure advances the consecutive out-of-credits counter and pauses
// the flow once it reaches the threshold (best-effort; the run continues either
// way). Auto-pausing publishes a run event so the builder reflects the change.
func (s *service) noteAICreditFailure(ctx context.Context, a models.Automation) {
	n, err := s.repo.BumpAutomationAICreditFailures(ctx, a.ID)
	if err != nil {
		return
	}
	if n >= aiCreditFailurePauseAt {
		_ = s.repo.DisableAutomationForCredits(ctx, a.ID)
		if s.publisher != nil {
			s.publisher.PublishAutomationEvent(ctx, a.OrganizationID, uuid.Nil, pubsub.EventAutomationRun, a.ID.String(), a.Name)
		}
	}
}

// buildAIPrompt builds the system + user prompt for one AI node. The event data
// (minus internal keys) is included so the model can read the fields the
// instruction references.
func buildAIPrompt(action models.IntegrationAction, cfg aiActionConfig, instruction string, data map[string]any) (system, prompt string) {
	eventCtx := aiEventContext(data)
	switch action {
	case models.IntegrationActionAIClassify:
		labels := nonEmptyStrings(cfg.Labels)
		system = "You label an automation event. Follow the instruction, read the event data, and reply with EXACTLY one of these labels and nothing else: " + strings.Join(labels, ", ") + "."
		prompt = "Instruction: " + instruction + "\n\nEvent data:\n" + eventCtx
	case models.IntegrationActionAIExtract:
		keys := nonEmptyStrings(cfg.OutputKeys)
		system = "You extract structured fields from an automation event. Reply with ONLY a JSON object whose keys are exactly: " + strings.Join(keys, ", ") + ". Each value must be a string; use an empty string when the field is not present. No prose, no code fences."
		prompt = "Instruction: " + instruction + "\n\nEvent data:\n" + eventCtx
	default: // ai_generate
		system = "You write short, useful text for an automation step based on the instruction and event data. Reply with only the text, no preamble, no quotes."
		prompt = "Instruction: " + instruction + "\n\nEvent data:\n" + eventCtx
	}
	return system, prompt
}

// aiEventContext renders the public (non-underscore) event fields as bounded
// "key: value" lines so the model has the data the instruction refers to.
func aiEventContext(data map[string]any) string {
	var b strings.Builder
	n := 0
	for k, v := range data {
		if strings.HasPrefix(k, "_") {
			continue
		}
		if n >= 40 {
			break
		}
		val := truncate(valueString(v), 400)
		if val == "" {
			continue
		}
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(val)
		b.WriteString("\n")
		n++
	}
	if b.Len() == 0 {
		return "(no fields)"
	}
	return b.String()
}

// mergeAIOutput writes the model result into the event data under the node's
// output variable(s), matching set_variables semantics so downstream conditions
// can branch on it.
func mergeAIOutput(action models.IntegrationAction, cfg aiActionConfig, text string, data map[string]any) {
	text = strings.TrimSpace(text)
	switch action {
	case models.IntegrationActionAIClassify:
		data[classifyOutputKey(cfg)] = matchLabel(text, nonEmptyStrings(cfg.Labels))
	case models.IntegrationActionAIExtract:
		vals := parseExtractJSON(text)
		for _, k := range nonEmptyStrings(cfg.OutputKeys) {
			data[k] = truncate(valueString(vals[k]), 2000)
		}
	default: // ai_generate
		data[generateOutputKey(cfg)] = text
	}
}

// aiOutputKeys names the variables an AI node writes, for the run-history preview.
func aiOutputKeys(action models.IntegrationAction, cfg aiActionConfig) []string {
	switch action {
	case models.IntegrationActionAIClassify:
		return []string{classifyOutputKey(cfg)}
	case models.IntegrationActionAIExtract:
		return nonEmptyStrings(cfg.OutputKeys)
	default:
		return []string{generateOutputKey(cfg)}
	}
}

// aiTemperature pins classify/extract to deterministic sampling (stable label /
// structured output); generate keeps the provider default so writing stays
// natural.
func aiTemperature(a models.IntegrationAction) *float64 {
	switch a {
	case models.IntegrationActionAIClassify, models.IntegrationActionAIExtract:
		return generation.Deterministic()
	default:
		return nil
	}
}

func classifyOutputKey(cfg aiActionConfig) string {
	return firstNonEmpty(strings.TrimSpace(cfg.OutputKey), aiClassVar)
}

func generateOutputKey(cfg aiActionConfig) string {
	return firstNonEmpty(strings.TrimSpace(cfg.OutputKey), aiTextVar)
}

// matchLabel maps the model's answer to one of the allowed labels
// (case-insensitive exact, then prefix, then substring). If nothing matches it
// returns the normalized answer so the miss is still visible in the trace and a
// loosely-typed condition can still test it.
func matchLabel(text string, labels []string) string {
	got := strings.ToLower(strings.Trim(strings.TrimSpace(text), ".\"' \n\t"))
	if got == "" {
		return ""
	}
	for _, l := range labels {
		if strings.EqualFold(strings.TrimSpace(l), got) {
			return l
		}
	}
	for _, l := range labels {
		if strings.HasPrefix(got, strings.ToLower(strings.TrimSpace(l))) {
			return l
		}
	}
	for _, l := range labels {
		if strings.Contains(got, strings.ToLower(strings.TrimSpace(l))) {
			return l
		}
	}
	return got
}

// parseExtractJSON parses the model's JSON object, tolerating a ```json fence.
// A parse failure yields an empty map so every requested key falls back to "".
func parseExtractJSON(text string) map[string]any {
	text = strings.TrimSpace(text)
	if i := strings.Index(text, "{"); i >= 0 {
		if j := strings.LastIndex(text, "}"); j >= i {
			text = text[i : j+1]
		}
	}
	out := map[string]any{}
	_ = json.Unmarshal([]byte(text), &out)
	return out
}

// nonEmptyStrings trims and drops empty entries from a string slice.
func nonEmptyStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
