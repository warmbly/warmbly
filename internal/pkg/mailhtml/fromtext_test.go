package mailhtml

import (
	"strings"
	"testing"
)

func TestFromTextKeepsLineStructure(t *testing.T) {
	got := FromText("Hi Ana,\n\nQuick question about your team.")
	want := "<div>Hi Ana,</div><div><br></div><div>Quick question about your team.</div>"
	if got != want {
		t.Errorf("FromText =\n%q\nwant\n%q", got, want)
	}
}

func TestFromTextIsEmptyForEmptyInput(t *testing.T) {
	for _, in := range []string{"", "   ", "\n\n", "\r\n"} {
		if got := FromText(in); got != "" {
			t.Errorf("FromText(%q) = %q, want an empty body so nothing ships an HTML part", in, got)
		}
	}
}

// The plain body is untrusted template text: it may contain angle brackets,
// ampersands, or a merge field someone pasted markup into.
func TestFromTextEscapes(t *testing.T) {
	got := FromText(`Tom & Jerry <script>alert(1)</script>`)
	if strings.Contains(got, "<script>") {
		t.Errorf("FromText did not escape markup: %q", got)
	}
	if !strings.Contains(got, "&amp;") {
		t.Errorf("FromText did not escape the ampersand: %q", got)
	}
}

// Click tracking rewrites hrefs, so a URL left as bare text is never tracked
// and never gets a UTM tag.
func TestFromTextLinkifiesBareURLs(t *testing.T) {
	got := FromText("Book here: https://cal.example.com/ana?ref=a&b=c")
	if !strings.Contains(got, `<a href="https://cal.example.com/ana?ref=a&amp;b=c">`) {
		t.Errorf("FromText did not anchor the URL: %q", got)
	}
}

func TestFromTextLeavesSentencePunctuationOutOfTheLink(t *testing.T) {
	got := FromText("See https://example.com.")
	if !strings.Contains(got, `<a href="https://example.com">https://example.com</a>.`) {
		t.Errorf("FromText swallowed the full stop into the href: %q", got)
	}
}

// A step body is a Go template. Escaping the quotes in a conditional makes it
// fail to parse, which drops the send onto the naive renderer and ships the
// literal {{if ...}} text to the recipient.
func TestFromTextKeepsTemplateSyntaxIntact(t *testing.T) {
	tmpl := `{{if eq .Company "Acme"}}Hi {{.FirstName}}{{end}}`
	got := FromText(tmpl)
	if !strings.Contains(got, tmpl) {
		t.Errorf("FromText mangled the template:\n%q\nwant it to contain\n%q", got, tmpl)
	}
}

func TestHasContent(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"empty string", "", false},
		{"composer placeholder", "<div></div>", false},
		{"nested empty blocks", "<div><p></p><p><br></p></div>", false},
		{"whitespace only", "<div>   </div>", false},
		{"style block only", "<style>.a{color:red}</style>", false},
		{"real copy", "<div>Hi Ana</div>", true},
		{"image only", `<div><img src="https://example.com/a.png"></div>`, true},
		{"image with no source or alt", "<div><img></div>", false},
		{"rule", "<div><hr></div>", true},
	}
	for _, c := range cases {
		if got := HasContent(c.body); got != c.want {
			t.Errorf("%s: HasContent(%q) = %v, want %v", c.name, c.body, got, c.want)
		}
	}
}

// The round trip the derivation relies on: whatever FromText writes for a
// non-empty body must read back as content, or the send-time guard would drop
// the part it just derived.
func TestFromTextOutputHasContent(t *testing.T) {
	for _, in := range []string{"Hello", "a\n\nb", "https://example.com"} {
		if !HasContent(FromText(in)) {
			t.Errorf("FromText(%q) produced a body HasContent calls empty", in)
		}
	}
}

// The send path renders the body with text/template, which escapes nothing by
// design, so an anchor built around a templated URL would let a contact value
// containing a quote break out of the href. Such a URL stays plain text.
func TestFromTextDoesNotAnchorATemplatedURL(t *testing.T) {
	got := FromText("Book here: https://cal.example.com/?ref={{.Company}}")
	if strings.Contains(got, "<a href") {
		t.Errorf("FromText anchored a URL carrying a merge field: %q", got)
	}
	if !strings.Contains(got, "https://cal.example.com/?ref={{.Company}}") {
		t.Errorf("FromText mangled the URL: %q", got)
	}
}
