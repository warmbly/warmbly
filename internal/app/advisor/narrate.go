package advisor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// Narrator rewrites a finding's card copy from the evidence the detector
// already computed.
//
// It is strictly a rewriter. It cannot create, suppress, reclassify, or
// re-severity a finding, and it never sees anything except the detector's own
// description and evidence map. That boundary is what makes it safe to leave on
// by default: the worst a bad completion can do is produce a clumsy sentence,
// never wrong advice.
//
// Narration is free to the org. Detection is pure Go and always runs, the
// results are cached per (detector, evidence shape), and every finding ships
// with complete deterministic copy already, so an unconfigured provider, an
// empty credit balance, or a provider outage all degrade to "the cards read
// slightly plainer" rather than "the Advisor stopped working".
type Narrator struct {
	provider generation.Provider
	// about grounds each rewrite in what the detector actually looks for, so
	// the copy explains the underlying rule instead of restating the numbers.
	about map[string]string
	// voice is the org's writing grounding (product, ICP, house voice), used
	// only so the advice sounds like it belongs in their workspace.
	voice VoiceSource
	cache NarrationCache
	// paid selects the model tier, matching every other AI surface.
	tier TierSource
}

// VoiceSource renders the org's voice grounding. Optional.
type VoiceSource interface {
	VoiceInstructions(ctx context.Context, orgID uuid.UUID) string
}

// TierSource reports whether an org is on a paid plan, which picks the model.
type TierSource interface {
	IsPaid(ctx context.Context, orgID uuid.UUID) bool
}

// NarrationCache is the persistence the narrator reuses across runs and orgs
// with the same evidence shape.
type NarrationCache interface {
	GetNarration(ctx context.Context, orgID uuid.UUID, cacheKey string) (title, detail, remedy string, ok bool)
	PutNarration(ctx context.Context, orgID uuid.UUID, cacheKey, title, detail, remedy, model string) error
}

// NewNarrator wires a narrator. A nil provider yields a narrator that is a
// no-op, which is the correct behaviour for a self-hosted install with no LLM
// configured.
func NewNarrator(provider generation.Provider, voice VoiceSource, tier TierSource, cache NarrationCache) *Narrator {
	return &Narrator{
		provider: provider,
		about:    DetectorAbout(),
		voice:    voice,
		tier:     tier,
		cache:    cache,
	}
}

// Enabled reports whether narration can run at all.
func (n *Narrator) Enabled() bool { return n != nil && n.provider != nil }

// narrationSystemPrompt is intentionally strict. The failure mode we are
// guarding against is not a refusal, it is enthusiasm: a model that turns a
// measured finding into marketing copy, invents a cause the evidence does not
// support, or hedges a critical finding into a suggestion.
const narrationSystemPrompt = `You rewrite one diagnostic finding for a cold-email platform's dashboard so it reads like a knowledgeable colleague explaining the problem, not like a linter.

You are given: what the check looks for and why it matters, and the exact evidence that made it fire. That is all you know. You may not introduce any fact, number, cause, or consequence that is not in the evidence or the check description.

Return ONLY a JSON object with exactly these keys:
{"title": "...", "detail": "...", "remedy": "..."}

title: one short line naming the specific thing that is wrong, including the mailbox, campaign, or step it concerns. Under 70 characters. No trailing period.
detail: two or three sentences. State what is happening with the actual numbers from the evidence, then why it matters to this sender. The reader knows what cold email is; do not explain the basics.
remedy: two or three sentences on what to do, concretely. If the evidence implies a specific number to change, say the number.

Rules:
- Write plainly. No marketing voice, no "unlock", "supercharge", "best practices", "pro tip", no exclamation marks.
- Do not use em dashes. Use a period, comma, colon, or parentheses.
- Do not open with "It looks like", "We noticed", "Great news", or any similar throat-clearing. Start with the substance.
- Do not soften a serious finding into a suggestion, and do not inflate a minor one into an emergency. Match the severity you are given.
- Do not tell the reader to "consider" or "try" something. Say what to do.
- Never mention that you are an AI, and never refer to "the system" or "the platform" in the third person.
- Never invent a cause. If the evidence does not say why something is happening, describe what is happening and what to do about it.`

// narrationInput is the exact payload the model sees.
type narrationInput struct {
	Check    string         `json:"what_this_check_looks_for"`
	Severity string         `json:"severity"`
	Subject  string         `json:"subject,omitempty"`
	Evidence map[string]any `json:"evidence"`
}

type narrationOutput struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Remedy string `json:"remedy"`
}

// cacheKey identifies a reusable narration: same detector, same severity, same
// evidence shape. Two mailboxes with the same problem at the same magnitude
// legitimately get the same words, which is why one org's daily run is a
// handful of completions rather than one per finding.
func cacheKey(f *models.AdvisorFinding) string {
	h := sha256.New()
	h.Write([]byte(f.DetectorKey))
	h.Write([]byte{0})
	h.Write([]byte(f.Severity))
	h.Write([]byte{0})
	h.Write(f.Evidence)
	return hex.EncodeToString(h.Sum(nil)[:12])
}

// Narrate rewrites the finding in place. It returns true when the copy was
// replaced. Any failure leaves the deterministic fallback untouched, which is
// why the caller can ignore the error.
func (n *Narrator) Narrate(ctx context.Context, orgID uuid.UUID, f *models.AdvisorFinding) bool {
	if !n.Enabled() || f == nil || f.Narrated {
		return false
	}

	key := cacheKey(f)
	if n.cache != nil {
		if title, detail, remedy, ok := n.cache.GetNarration(ctx, orgID, key); ok {
			f.Title, f.Detail, f.Remedy, f.Narrated = title, detail, remedy, true
			return true
		}
	}

	var evidence map[string]any
	if err := json.Unmarshal(f.Evidence, &evidence); err != nil {
		evidence = map[string]any{}
	}
	payload, err := json.Marshal(narrationInput{
		Check:    n.about[f.DetectorKey],
		Severity: string(f.Severity),
		Subject:  f.EntityLabel,
		Evidence: evidence,
	})
	if err != nil {
		return false
	}

	system := narrationSystemPrompt
	if n.voice != nil {
		if v := strings.TrimSpace(n.voice.VoiceInstructions(ctx, orgID)); v != "" {
			// The org's voice grounding shapes tone only. It is appended after
			// the hard rules so it can never override them.
			system += "\n\nThis workspace's context, for tone and vocabulary only. It does not change any rule above:\n" + v
		}
	}

	paid := false
	if n.tier != nil {
		paid = n.tier.IsPaid(ctx, orgID)
	}

	res, err := n.provider.Complete(ctx, generation.CompletionRequest{
		System: system,
		Prompt: string(payload),
		Model:  n.provider.ModelForTier(paid),
		// Deterministic: the same finding should not get different wording on
		// every run, and this is exposition, not creative writing.
		Temperature: generation.Deterministic(),
		MaxTokens:   400,
	})
	if err != nil || res == nil {
		return false
	}

	out, ok := parseNarration(res.Text)
	if !ok {
		return false
	}

	f.Title, f.Detail, f.Remedy, f.Narrated = out.Title, out.Detail, out.Remedy, true
	if n.cache != nil {
		model := ""
		if res != nil {
			model = n.provider.Name()
		}
		_ = n.cache.PutNarration(ctx, orgID, key, out.Title, out.Detail, out.Remedy, model)
	}
	return true
}

// parseNarration extracts the JSON object, tolerating a model that wraps it in
// a fenced code block, and rejects any result that is not complete. A partial
// rewrite (good title, empty remedy) is worse than the fallback, so it is
// discarded wholesale.
func parseNarration(text string) (narrationOutput, bool) {
	var out narrationOutput
	trimmed := strings.TrimSpace(text)
	if i := strings.Index(trimmed, "{"); i >= 0 {
		if j := strings.LastIndex(trimmed, "}"); j > i {
			trimmed = trimmed[i : j+1]
		}
	}
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return out, false
	}

	out.Title = sanitize(out.Title)
	out.Detail = sanitize(out.Detail)
	out.Remedy = sanitize(out.Remedy)
	if out.Title == "" || out.Detail == "" || out.Remedy == "" {
		return out, false
	}
	// A title that ran away is a sign the model ignored the format; the
	// fallback is better than a card that breaks the layout.
	if len(out.Title) > 120 || len(out.Detail) > 700 || len(out.Remedy) > 700 {
		return out, false
	}
	return out, true
}

// sanitize enforces the house style the prompt asks for, because a prompt rule
// is a request and this is the guarantee. Em dashes in particular read as
// machine-written, which is exactly the impression this feature must not give.
func sanitize(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "—", ", ")
	s = strings.ReplaceAll(s, " – ", ", ")
	s = strings.ReplaceAll(s, "  ", " ")
	return strings.TrimSpace(s)
}

// String renders a finding for logs.
func describe(f *models.AdvisorFinding) string {
	return fmt.Sprintf("%s/%s(%s)", f.DetectorKey, f.Severity, f.EntityLabel)
}
