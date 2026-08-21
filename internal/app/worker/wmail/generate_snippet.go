package wmail

import (
	"strings"
	"unicode/utf8"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/pkg/mailhtml"
)

// snippetMaxRunes bounds the one-line preview shown in conversation lists. It
// is a preview only: the full body lives in object storage and is what the
// thread reader renders.
const snippetMaxRunes = 200

// GenerateSnippet builds the list preview for a message: quoted history and
// signature dropped, whitespace collapsed to a single line, cut to a rune
// boundary so a multi-byte character is never sliced in half.
func GenerateSnippet(bodyPlain, bodyHTML string) string {
	text := bodyPlain
	if strings.TrimSpace(text) == "" && bodyHTML != "" {
		text = bodyHTML
	}
	if mailhtml.LooksLikeHTML(text) {
		// Entity-decoding matters here: the stripped output of an HTML body is
		// escaped text, so "&amp;" would otherwise be shown verbatim. Senders
		// that put markup in their text/plain part get the same treatment.
		text = mailhtml.ToText(text)
	}

	// Line filtering has to run before whitespace collapsing, or there are no
	// lines left to filter.
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, ">") {
			continue // quoted reply history
		}
		if line == "--" {
			break // RFC 3676 signature delimiter (trailing space already trimmed)
		}
		cleaned = append(cleaned, line)
	}

	text = strings.Join(cleaned, " ")
	text = strings.Join(strings.Fields(text), " ")

	if utf8.RuneCountInString(text) > snippetMaxRunes {
		runes := []rune(text)
		text = strings.TrimRight(string(runes[:snippetMaxRunes]), " .,;:-") + "…"
	}
	return text
}

// SearchText renders the message body down to the bounded plain text Postgres
// indexes for search.
func SearchText(bodyPlain, bodyHTML string) string {
	return mailhtml.SearchText(bodyPlain, bodyHTML, config.MaxSearchBodyText)
}
