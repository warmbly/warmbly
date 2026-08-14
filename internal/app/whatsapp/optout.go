package whatsapp

import (
	"strings"
	"unicode"
)

// OptOutMatch describes an automatic opt-out phrase detection result.
type OptOutMatch struct {
	Matched   bool
	Category  string // opt_out | do_not_contact | ambiguous | none
	Phrase    string
	Confident bool // true only for unequivocal phrases
}

// unequivocal Portuguese / Spanish / English opt-out phrases.
// Keep conservative: ambiguous text routes to human review.
var unequivocalPhrases = []string{
	"não tenho interesse",
	"nao tenho interesse",
	"não me chame mais",
	"nao me chame mais",
	"não me ligue mais",
	"retire meu número",
	"retire meu numero",
	"não envie mensagens",
	"nao envie mensagens",
	"não envie mais",
	"nao envie mais",
	"pare de me enviar",
	"remover meu número",
	"remover meu numero",
	"do not contact",
	"don't contact me",
	"dont contact me",
	"stop messaging me",
	"remove my number",
	"unsubscribe",
	"opt out",
	"opt-out",
	"no me contacten",
	"no me escriban más",
	"no me escriban mas",
}

// standaloneOptOutTokens only match as whole-message or whole-word intent.
var standaloneOptOutTokens = []string{
	"parar",
	"sair",
	"stop",
	"cancelar",
	"remover",
}

// DetectOptOut scans inbound free text for unequivocal opt-out intent.
// On doubt returns Confident=false so the operator reviews.
func DetectOptOut(body string) OptOutMatch {
	norm := normalizeOptOutText(body)
	if norm == "" {
		return OptOutMatch{Category: "none"}
	}

	for _, p := range unequivocalPhrases {
		if strings.Contains(norm, p) {
			return OptOutMatch{
				Matched:   true,
				Category:  "opt_out",
				Phrase:    p,
				Confident: true,
			}
		}
	}

	// Whole-message short tokens only (avoid "não posso parar agora").
	trimmed := strings.TrimSpace(norm)
	for _, t := range standaloneOptOutTokens {
		if trimmed == t {
			return OptOutMatch{
				Matched:   true,
				Category:  "opt_out",
				Phrase:    t,
				Confident: true,
			}
		}
	}

	// Soft signals → human review, not auto block.
	soft := []string{"sem interesse", "agora não", "agora nao", "talvez depois", "não é o momento", "nao e o momento"}
	for _, p := range soft {
		if strings.Contains(norm, p) {
			return OptOutMatch{
				Matched:   true,
				Category:  "ambiguous",
				Phrase:    p,
				Confident: false,
			}
		}
	}

	return OptOutMatch{Category: "none"}
}

func normalizeOptOutText(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevSpace = false
			continue
		}
		if unicode.IsSpace(r) || r == '-' || r == '_' {
			if !prevSpace && b.Len() > 0 {
				b.WriteByte(' ')
				prevSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}
