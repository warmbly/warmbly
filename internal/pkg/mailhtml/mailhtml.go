// Package mailhtml renders untrusted email bodies safely.
//
// Two directions: Sanitize produces display-safe HTML for the dashboard (the
// message body arrives from the sender's mail client and is fully untrusted),
// and ToText flattens an HTML body into readable plain text for snippets and
// any other text-only consumer.
package mailhtml

import (
	"html"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/microcosm-cc/bluemonday"
)

// Elements whose text content must never survive stripping. A <style> block's
// CSS is text to the tokenizer, so without this the whole stylesheet of a
// marketing email renders as body copy.
var skipContent = []string{
	"style", "script", "head", "title", "noscript",
	"iframe", "object", "embed", "applet", "svg", "math",
}

var (
	displayPolicy = newDisplayPolicy()
	textPolicy    = bluemonday.StrictPolicy().SkipElementsContent(skipContent...)
)

// Number-ish legacy table attributes ("600", "100%", "0").
var lengthAttr = regexp.MustCompile(`(?i)^[0-9]+(\.[0-9]+)?(%|px)?$`)

// newDisplayPolicy builds the sanitizer used for rendering a received message.
// It is deliberately more permissive than UGCPolicy on layout: real email is
// table-based, inline-styled, and often still uses <font>/<center>. Anything
// that can execute (script, event handlers, iframes, object/embed) is dropped
// along with its content.
func newDisplayPolicy() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()

	p.SkipElementsContent(skipContent...)

	// Legacy presentational markup that HTML mail still ships.
	p.AllowElements("center", "font", "big", "tt")
	p.AllowAttrs("color", "face", "size").OnElements("font")

	// Table layout attributes. UGCPolicy's AllowTables covers colspan/rowspan
	// but not the sizing/alignment attributes every email template uses.
	tableEls := []string{"table", "thead", "tbody", "tfoot", "tr", "td", "th"}
	p.AllowAttrs("width", "height").Matching(lengthAttr).OnElements(tableEls...)
	p.AllowAttrs("cellpadding", "cellspacing", "border").Matching(lengthAttr).OnElements("table")
	p.AllowAttrs("align").Matching(regexp.MustCompile(`(?i)^(left|right|center|justify|char)$`)).OnElements(append(tableEls, "div", "p", "img")...)
	p.AllowAttrs("valign").Matching(regexp.MustCompile(`(?i)^(top|middle|bottom|baseline)$`)).OnElements(tableEls...)
	p.AllowAttrs("bgcolor").Matching(regexp.MustCompile(`(?i)^(#[0-9a-f]{3,8}|[a-z]+|rgba?\([0-9,.\s%]+\))$`)).OnElements(append(tableEls, "body")...)

	// Inline CSS, property-allowlisted and value-validated by bluemonday's own
	// CSS parser. Layout emails are unreadable without it.
	p.AllowStyles(
		"color", "background", "background-color", "background-image",
		"font", "font-family", "font-size", "font-style", "font-weight",
		"letter-spacing", "line-height", "text-align", "text-decoration",
		"text-transform", "vertical-align", "white-space", "direction",
		"margin", "margin-top", "margin-right", "margin-bottom", "margin-left",
		"padding", "padding-top", "padding-right", "padding-bottom", "padding-left",
		"border", "border-top", "border-right", "border-bottom", "border-left",
		"border-color", "border-radius", "border-style", "border-width",
		"width", "min-width", "max-width", "height", "min-height", "max-height",
		"display", "float", "clear", "list-style-type", "opacity", "table-layout",
	).Globally()

	// Images: remote (http/https) and inline data: URIs. cid: references point
	// at MIME parts we do not serve, so they stay blocked rather than render
	// as broken-image icons.
	p.AllowDataURIImages()
	p.AllowAttrs("width", "height").Matching(lengthAttr).OnElements("img")

	// Every surviving link leaves the dashboard in a new tab and carries no
	// referrer or opener back to us.
	p.RequireNoFollowOnLinks(true)
	p.RequireNoReferrerOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)
	p.AllowURLSchemes("http", "https", "mailto", "tel")

	return p
}

// Sanitize returns display-safe HTML for an email body. The result is a
// fragment: any <html>/<head>/<body> wrapper is stripped, so callers supply
// their own document shell.
func Sanitize(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	return displayPolicy.Sanitize(raw)
}

// tagPattern matches an HTML tag opener. A plain-text body that merely
// mentions an address in angle brackets ("<ana@example.com>") does not match,
// because a tag name has to be followed by whitespace or a bracket.
var tagPattern = regexp.MustCompile(`(?i)<(!doctype|/?[a-z][a-z0-9]*)(\s|>|/>)`)

// LooksLikeHTML reports whether a body is actually markup.
//
// Mail synced before the IMAP reader fetched per-part sections stored the
// plain-text body under the HTML field as well. Rendering that as HTML is
// exactly the reported bug (line breaks collapsed, "&" shown as an entity,
// anything in angle brackets swallowed), so a stored "HTML" body with no tag
// in it is treated as the plain text it really is.
func LooksLikeHTML(body string) bool {
	return tagPattern.MatchString(body)
}

// blockBreak marks the end of a block-level element so flattened text keeps its
// line structure instead of running every paragraph together.
var blockBreak = regexp.MustCompile(`(?i)<(br|/p|/div|/tr|/li|/h[1-6]|/blockquote|/table)\s*/?>`)

// ToText flattens an HTML body to plain text: tags removed, block boundaries
// turned into newlines, entities decoded back to the characters they stand for.
func ToText(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	withBreaks := blockBreak.ReplaceAllString(raw, "\n$0")
	// Sanitize escapes its text output ("&" -> "&amp;"), so a body that was
	// already entity-encoded would otherwise surface as literal "&amp;".
	return html.UnescapeString(textPolicy.Sanitize(withBreaks))
}

// SearchText renders a message body down to the bounded plain text that gets
// indexed for search. Unlike a preview snippet it keeps quoted history, because
// searching for a phrase someone quoted back at you should still find the
// conversation, and it flattens HTML so the indexed words are the words the
// reader sees. maxRunes of 0 or less means no limit.
func SearchText(bodyPlain, bodyHTML string, maxRunes int) string {
	text := bodyPlain
	if strings.TrimSpace(text) == "" && bodyHTML != "" {
		text = bodyHTML
	}
	if LooksLikeHTML(text) {
		text = ToText(text)
	}
	text = strings.Join(strings.Fields(text), " ")

	if maxRunes > 0 && utf8.RuneCountInString(text) > maxRunes {
		text = string([]rune(text)[:maxRunes])
	}
	return text
}
