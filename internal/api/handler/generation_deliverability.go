package handler

import (
	"context"
	"slices"
	"strings"

	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/pkg/warmlint"
)

// rewordFlagged gives generated copy one fix pass when the editor's content
// score would flag it (spam-trigger terms, stacked punctuation), so a draft
// lands clean. Anything already in the author's own source text is theirs and
// left alone. It keeps the original unless the pass flags less and every
// template token survives, and returns the input and output tokens the pass
// used. onPass, when set, hears that the pass is starting.
func (h *Handler) rewordFlagged(ctx context.Context, model string, voice generation.VoiceContext, text, source string, onPass func()) (string, int, int) {
	terms, stacked := warmlint.Rewordable(text)
	theirs, theirsStacked := warmlint.Rewordable(source)
	terms = slices.DeleteFunc(terms, func(t string) bool { return slices.Contains(theirs, t) })
	stacked = stacked && !theirsStacked
	if (len(terms) == 0 && !stacked) || h.AIProvider == nil {
		return text, 0, 0
	}
	if onPass != nil {
		onPass()
	}
	var asks []string
	if len(terms) > 0 {
		asks = append(asks, "replace these words, which spam filters flag, with plain wording that means the same thing: "+strings.Join(terms, ", "))
	}
	if stacked {
		asks = append(asks, "use one punctuation mark where it stacks them (!!, ?!)")
	}
	instruction := strings.Join(asks, "; ") + ". Change nothing else. If the passage starts with a \"Subject:\" line, keep that line and apply this to it too."
	res, err := h.AIProvider.Complete(ctx, generation.CompletionRequest{
		System:    generation.BuildEditRules(voice),
		Prompt:    buildEditPrompt(generationEditRequest{Text: text, Instruction: instruction}),
		Model:     model,
		MaxTokens: editMaxTokens,
	})
	if err != nil || res == nil {
		return text, 0, 0
	}
	fixed := stripEditFences(res.Text)
	after, stillStacked := warmlint.Rewordable(fixed)
	after = slices.DeleteFunc(after, func(t string) bool { return slices.Contains(theirs, t) })
	if fixed == "" || !sameTemplateTokens(text, fixed) || len(after) >= len(terms) && (stillStacked && !theirsStacked) == stacked {
		return text, res.InputTokens, res.OutputTokens
	}
	return fixed, res.InputTokens, res.OutputTokens
}

// sameTemplateTokens reports whether b keeps every merge token, condition,
// spintax set and AI block of a, so a reword can never drop personalization.
func sameTemplateTokens(a, b string) bool {
	for _, marker := range []string{"{{", "}}", "[[ai:", "{", "|"} {
		if strings.Count(a, marker) != strings.Count(b, marker) {
			return false
		}
	}
	return true
}
