package wmail

import (
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

func GenerateSnippet(bodyPlain, bodyHTML string) string {
	text := bodyPlain
	if text == "" && bodyHTML != "" {
		// Strip all HTML tags
		policy := bluemonday.StrictPolicy()
		text = policy.Sanitize(bodyHTML)
	}

	// Remove excessive hitespace
	text = strings.TrimSpace(text)
	text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")

	// Optionally remove quoted text (lines starting with '>') and signatures
	lines := strings.Split(text, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, ">") {
			continue // skip quoted lines
		}
		if strings.HasPrefix(line, "--") {
			break // stop at signature delimiter
		}
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}
	text = strings.Join(cleaned, " ")

	// Limit to 100 characters
	if len(text) > 100 {
		text = text[:100]
		text = strings.TrimRight(text, " .,;:-") + "…"
	}
	return text
}
