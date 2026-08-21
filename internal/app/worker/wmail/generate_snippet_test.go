package wmail

import (
	"strings"
	"testing"
)

func TestGenerateSnippetPlainCollapsesAndDropsQuotes(t *testing.T) {
	body := "Hello,\n\nThis is the first paragraph.\n\n> quoted line\n\n--\nAna, Sunrise Labs"
	got := GenerateSnippet(body, "")
	want := "Hello, This is the first paragraph."
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestGenerateSnippetFromHTMLDecodesEntities(t *testing.T) {
	got := GenerateSnippet("", `<style>.a{color:red}</style><p>Terms &amp; conditions</p><p>caf&eacute; &#169;</p>`)
	if strings.Contains(got, "&amp;") || strings.Contains(got, "color:red") {
		t.Fatalf("unexpected snippet %q", got)
	}
	if got != "Terms & conditions café ©" {
		t.Fatalf("got %q", got)
	}
}

func TestGenerateSnippetTruncatesOnRuneBoundary(t *testing.T) {
	got := GenerateSnippet(strings.Repeat("é", 400), "")
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected ellipsis, got %q", got)
	}
	if strings.ContainsRune(got, '�') {
		t.Fatalf("snippet sliced a rune in half: %q", got)
	}
}
