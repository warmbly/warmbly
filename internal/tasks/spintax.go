package tasks

import (
	"math/rand"
	"regexp"
	"strings"
)

// spintaxAlt matches an innermost {a|b|…} group that actually contains a '|'.
var spintaxAlt = regexp.MustCompile(`\{([^{}]*\|[^{}]*)\}`)

// ShortestSpintax resolves each alternation group expandSpintax would expand to
// its shortest option: the reading every recipient's copy is at least as long as.
func ShortestSpintax(s string) string {
	return ResolveSpintax(s, func(opts []string) string {
		best := opts[0]
		for _, o := range opts[1:] {
			if len(o) < len(best) {
				best = o
			}
		}
		return best
	})
}

// ResolveSpintax resolves each alternation group expandSpintax would expand,
// innermost first, to the option pick chooses.
func ResolveSpintax(s string, pick func(opts []string) string) string {
	if !strings.Contains(s, "|") {
		return s
	}
	for i := 0; i < 20 && spintaxAlt.MatchString(s); i++ {
		s = spintaxAlt.ReplaceAllStringFunc(s, func(m string) string {
			return pick(strings.Split(m[1:len(m)-1], "|"))
		})
	}
	return s
}

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
