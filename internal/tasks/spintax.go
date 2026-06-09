package tasks

import (
	"math/rand"
	"regexp"
	"strings"
)

// spintaxGroup matches an innermost {a|b|c} alternation group (no nested braces
// inside). Nested groups are resolved by repeated passes, innermost first.
var spintaxGroup = regexp.MustCompile(`\{([^{}]*)\}`)

// spin expands {a|b|c} spintax: each alternation group is replaced by one of
// its options chosen at random. This is the body-level analogue of the subject
// synthesiser — it multiplies the number of distinct rendered bodies so the
// warmup corpus isn't a small fixed set that filters can fingerprint. Text
// containing no braces is returned unchanged. A group with no '|' is treated as
// literal text with the braces stripped.
func spin(s string) string {
	// Bounded loop: each pass resolves the current innermost groups; the cap
	// guards against pathological/malformed input that never fully resolves.
	for i := 0; i < 20 && strings.Contains(s, "{"); i++ {
		s = spintaxGroup.ReplaceAllStringFunc(s, func(m string) string {
			inner := m[1 : len(m)-1]
			opts := strings.Split(inner, "|")
			return opts[rand.Intn(len(opts))]
		})
	}
	return s
}

// spinClean expands spintax and tidies whitespace introduced by optional
// fragments (e.g. an empty option leaving a double space).
func spinClean(s string) string {
	out := spin(s)
	out = strings.ReplaceAll(out, "  ", " ")
	return strings.TrimSpace(out)
}

// spintaxAlt matches an innermost {a|b|…} group that actually contains a '|'.
var spintaxAlt = regexp.MustCompile(`\{([^{}]*\|[^{}]*)\}`)

// expandSpintax expands ONLY real alternation groups — those containing a '|' —
// choosing one option at random per group. Brace runs without a '|' (CSS in an
// HTML body, JSON, an unrelated "{...}") are left verbatim, so this is safe to
// run over campaign HTML/plaintext. Repeated passes resolve nesting
// innermost-first. Use this (not spin) for user-authored campaign bodies.
func expandSpintax(s string) string {
	if !strings.Contains(s, "|") {
		return s
	}
	for i := 0; i < 20 && spintaxAlt.MatchString(s); i++ {
		s = spintaxAlt.ReplaceAllStringFunc(s, func(m string) string {
			opts := strings.Split(m[1:len(m)-1], "|")
			return opts[rand.Intn(len(opts))]
		})
	}
	return s
}
