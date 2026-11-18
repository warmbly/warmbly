package repository

import (
	"strings"
	"testing"
	"unicode"
)

// The SQL blank check and strings.TrimSpace agree on which runes are blank.
func TestBlankRunesMatchUnicodeSpace(t *testing.T) {
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if unicode.IsSpace(r) != strings.ContainsRune(blankRunes, r) {
			t.Errorf("rune %U: unicode.IsSpace %v, in blankRunes %v", r, unicode.IsSpace(r), !unicode.IsSpace(r))
		}
	}
}
