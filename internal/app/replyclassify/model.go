package replyclassify

import (
	"context"
	"strings"
	"sync"
	"time"
)

// Layer 3: the OPTIONAL model classifier. It rides the platform LLM provider
// (M1) wired in from the app mains via SetModelClassifier, so it uses the same
// OpenAI-first, self-hostable backend as every other AI feature. It is
// platform-paid: this path never charges org credits (it settles only the
// ambiguous sentiment middle the cheap deterministic layers can't). When no
// provider is wired (no AI_PROVIDER) the layer is a pure
// no-op that resolves the middle to "unknown" WITHOUT any network call.
//
// The model is constrained to the three nuanced sentiment classes the cheap
// layers can't separate: positive | negative | neutral. Compliance (unsubscribe)
// and automation (auto_reply / out_of_office) are already settled deterministically
// upstream and are intentionally NOT in the model's output space.

// modelTimeout bounds the single Layer-3 completion.
const modelTimeout = 8 * time.Second

const modelSystemPrompt = "You classify the sentiment of a reply to a cold sales email. " +
	"Reply with exactly one lowercase word and nothing else: positive (interested / wants to talk), " +
	"negative (rejection / not interested), or neutral (a question, deferral, or anything unclear). " +
	"Do not explain."

// ModelClassifyFunc runs one platform LLM completion for Layer 3: given the
// system + user prompt it returns the model's raw text. The app mains adapt
// generation.Provider.Complete to this shape and wire it with SetModelClassifier,
// keeping this low-level package free of a direct provider dependency. nil means
// Layer 3 is disabled (the ambiguous middle resolves to "unknown" offline).
type ModelClassifyFunc func(ctx context.Context, system, user string) (string, error)

var (
	modelMu       sync.RWMutex
	modelClassify ModelClassifyFunc
)

// SetModelClassifier wires (or clears, with nil) the platform provider that
// backs Layer 3. Safe to call once at startup; guarded for concurrent reads.
func SetModelClassifier(fn ModelClassifyFunc) {
	modelMu.Lock()
	modelClassify = fn
	modelMu.Unlock()
}

// classifyModel runs Layer 3 when a provider is wired. Returns (zero, false)
// when unconfigured or on any error, so the caller falls back to "unknown" and
// NEVER hard-errors on a classification miss.
func classifyModel(ctx context.Context, in Input) (Result, bool) {
	modelMu.RLock()
	fn := modelClassify
	modelMu.RUnlock()
	if fn == nil {
		return Result{}, false
	}

	user := strings.TrimSpace("Subject: " + in.Subject + "\n\n" + in.BodyText)
	if user == "" {
		return Result{}, false
	}

	cctx, cancel := context.WithTimeout(ctx, modelTimeout)
	defer cancel()

	out, err := fn(cctx, modelSystemPrompt, user)
	if err != nil {
		return Result{}, false
	}

	switch normalizeModelLabel(out) {
	case ClassPositive:
		return Result{Class: ClassPositive, Confidence: 0.7, Source: SourceModel}, true
	case ClassNegative:
		return Result{Class: ClassNegative, Confidence: 0.7, Source: SourceModel}, true
	case ClassNeutral:
		return Result{Class: ClassNeutral, Confidence: 0.6, Source: SourceModel}, true
	default:
		return Result{}, false
	}
}

// normalizeModelLabel reduces the model's free text to one of the three allowed
// labels, tolerating stray punctuation/whitespace. Anything else is rejected so
// the caller falls back to "unknown".
func normalizeModelLabel(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Trim(s, ".\"' \n\t")
	switch {
	case strings.HasPrefix(s, ClassPositive):
		return ClassPositive
	case strings.HasPrefix(s, ClassNegative):
		return ClassNegative
	case strings.HasPrefix(s, ClassNeutral):
		return ClassNeutral
	default:
		return ""
	}
}
