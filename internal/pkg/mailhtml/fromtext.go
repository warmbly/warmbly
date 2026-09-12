package mailhtml

import (
	"regexp"
	"strings"

	nethtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// bareURL matches a link a plain-text author typed with no markup around it.
// Trailing sentence punctuation is deliberately outside the match: "see
// https://example.com." ends in a full stop, not in a URL.
var bareURL = regexp.MustCompile(`https?://[^\s<>"']+[^\s<>"'.,;:!?)\]]`)

// FromText renders a plain-text body as the HTML part that ships beside it.
//
// A step written through the API or an agent tool has a plain body and no
// HTML, and the send path puts body_html on the wire as the text/html
// alternative. Every modern client prefers that part, so a step whose HTML was
// still the composer's empty <div></div> placeholder arrived as a blank
// message with the real copy only in the fallback nobody reads.
//
// The output is the shape the dashboard composer produces (one <div> per
// line), so a body derived here opens in the editor unchanged rather than as
// something the author did not write. Bare URLs become anchors because click
// tracking rewrites hrefs: a link left as text is a link that is never
// tracked and never gets a UTM tag.
func FromText(plain string) string {
	// Normalise the line endings first: a CRLF body would otherwise leave a
	// stray carriage return inside every div.
	text := strings.ReplaceAll(strings.ReplaceAll(plain, "\r\n", "\n"), "\r", "\n")
	if strings.TrimSpace(text) == "" {
		return ""
	}
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			// Gmail's own shape for a blank line. An empty <div> collapses to
			// nothing in several clients, which loses the author's spacing.
			b.WriteString("<div><br></div>")
			continue
		}
		b.WriteString("<div>")
		b.WriteString(linkifyEscaped(line))
		b.WriteString("</div>")
	}
	return b.String()
}

// textEscaper escapes the three characters that are markup in element content.
//
// Quotes are deliberately NOT escaped, which is why html.EscapeString is not
// used here: a step body is a Go template, and turning `{{if eq .Company
// "Acme"}}` into `&#34;Acme&#34;` makes it fail to parse, which drops the send
// onto the naive renderer and ships the literal template text to the
// recipient. Nothing this writes can land in an attribute: the URL pattern
// excludes both quote characters, so the href below is always quote-free.
var textEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// linkifyEscaped escapes one line of plain text and wraps its bare URLs in
// anchors. Escaping happens first and the anchor markup is written around the
// escaped text, so nothing the author typed can become markup.
func linkifyEscaped(line string) string {
	escaped := textEscaper.Replace(line)
	return bareURL.ReplaceAllStringFunc(escaped, func(match string) string {
		// A URL carrying a merge field is left as text. The send path renders
		// the body with text/template, which by design performs no escaping
		// (see internal/tasks/template.go), so a contact value containing a
		// quote would break out of the href this would otherwise build. Plain
		// bodies had no anchors at all before, so declining to add one here
		// costs nothing that existed.
		if strings.Contains(match, "{{") {
			return match
		}
		// Otherwise the href is literal text the author typed, already through
		// textEscaper and holding no quote, so it is attribute-safe as it is.
		return `<a href="` + match + `">` + match + `</a>`
	})
}

// contentTags are elements that are content in themselves: an email may
// legitimately be one image, a horizontal rule between two blocks, or a table
// of them, and none of those carry text.
var contentTags = map[atom.Atom]bool{
	atom.Img: true, atom.Video: true, atom.Audio: true, atom.Hr: true,
}

// HasContent reports whether an HTML body would render anything a recipient
// can see: visible text, or an image or rule standing in for it.
//
// It exists because "" is not the only empty body. A step created through the
// API carries the composer's <div></div> placeholder, which is non-empty as a
// string, passes every len() check, and ships as a completely blank email.
func HasContent(bodyHTML string) bool {
	if strings.TrimSpace(bodyHTML) == "" {
		return false
	}
	if strings.TrimSpace(ToPlainText(bodyHTML)) != "" {
		return true
	}
	doc, err := nethtml.Parse(strings.NewReader(bodyHTML))
	if err != nil {
		return false
	}
	root := findElement(doc, atom.Body)
	if root == nil {
		root = doc
	}
	return hasContentNode(root)
}

func hasContentNode(n *nethtml.Node) bool {
	if n.Type == nethtml.ElementNode {
		if textSkip[n.DataAtom] {
			return false
		}
		if contentTags[n.DataAtom] && !isHidden(n) {
			// An <img> with no source renders as nothing (or as a broken-image
			// placeholder), which is not content either.
			if n.DataAtom != atom.Img || strings.TrimSpace(attrOf(n, "src")) != "" {
				return true
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if hasContentNode(c) {
			return true
		}
	}
	return false
}
