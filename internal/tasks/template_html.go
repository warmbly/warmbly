package tasks

import (
	"html"
	htmltemplate "html/template"
	"regexp"
	"strconv"
	"strings"

	"github.com/warmbly/warmbly/internal/pkg/tmplfuncs"
)

// htmlBody is a compiled HTML body plus the comments shielded from the escaper,
// which would otherwise strip them (Outlook's conditional comments among them).
type htmlBody struct {
	t        *htmltemplate.Template
	comments []string
}

var htmlTmplCache tmplfuncs.TypedCache[*htmlBody]

var htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)

// Placeholders hold NUL, which the escaper never emits and the source is cleared of.
const (
	shieldOpen  = "\x00wo\x00"
	shieldClose = "\x00wc\x00"
	shieldBang  = "\x00wb\x00"
)

var shieldToken = regexp.MustCompile("\x00w(o|c|b|[0-9]+)\x00")

// shieldComments swaps each comment for a placeholder; a comment holding an
// action keeps its body in place, so that action is escaped like any other.
func shieldComments(src string) (string, []string) {
	var comments []string
	out := htmlComment.ReplaceAllStringFunc(src, func(c string) string {
		if strings.Contains(c, "{{") {
			return shieldOpen + strings.ReplaceAll(c[4:len(c)-3], "<![", shieldBang) + shieldClose
		}
		comments = append(comments, c)
		return "\x00w" + strconv.Itoa(len(comments)-1) + "\x00"
	})
	return out, comments
}

func (h *htmlBody) restore(out string) string {
	out = strings.ReplaceAll(out, "&lt;no value&gt;", "")
	if !strings.Contains(out, "\x00w") {
		return out
	}
	return shieldToken.ReplaceAllStringFunc(out, func(tok string) string {
		switch tok[2] {
		case 'o':
			return "<!--"
		case 'c':
			return "-->"
		case 'b':
			return "<!["
		}
		i, err := strconv.Atoi(tok[2 : len(tok)-1])
		if err != nil || i >= len(h.comments) {
			return ""
		}
		return h.comments[i]
	})
}

// compiledHTMLTemplate returns the cached HTML body for tmpl, nil when it does not compile.
func compiledHTMLTemplate(tmpl string) *htmlBody {
	if body, ok := htmlTmplCache.Load(tmpl); ok {
		return body
	}
	src, comments := shieldComments(strings.ReplaceAll(tmpl, "\x00", ""))
	t, err := tmplfuncs.CompileHTML("body", normalizeTemplate(src))
	if err != nil {
		htmlTmplCache.Store(tmpl, nil)
		return nil
	}
	body := &htmlBody{t: t, comments: comments}
	htmlTmplCache.Store(tmpl, body)
	return body
}

type htmlState int

const (
	inText htmlState = iota
	inComment
	inTag
	inQuotedValue
	inUnquotedValue
	inRawText
)

// htmlScanner tracks where a fallback render is in the document, so each value
// it inserts is escaped for that place.
type htmlScanner struct {
	state   htmlState
	quote   byte
	name    strings.Builder
	attr    string
	tag     string
	started bool
}

// htmlPosition is where one value lands.
type htmlPosition struct {
	state   htmlState
	attr    string
	tag     string
	started bool
}

func (s *htmlScanner) position() htmlPosition {
	return htmlPosition{state: s.state, attr: s.attr, tag: s.tag, started: s.started}
}

// sawValue records that a value was written into the current attribute.
func (s *htmlScanner) sawValue() {
	if s.state == inQuotedValue {
		s.started = true
	}
}

func isLetter(c byte) bool { return c|0x20 >= 'a' && c|0x20 <= 'z' }

func (s *htmlScanner) feed(text string) {
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch s.state {
		case inText:
			switch {
			case strings.HasPrefix(text[i:], "<!--"):
				s.state, i = inComment, i+3
			case c == '<' && i+1 < len(text) && (isLetter(text[i+1]) || text[i+1] == '/' && i+2 < len(text) && isLetter(text[i+2])):
				j := i + 1
				if text[j] == '/' {
					j++
				}
				k := j
				for k < len(text) && (isLetter(text[k]) || text[k] >= '0' && text[k] <= '9') {
					k++
				}
				s.tag = strings.ToLower(text[j:k])
				if text[i+1] == '/' {
					s.tag = ""
				}
				s.state, s.attr, i = inTag, "", k-1
				s.name.Reset()
			}
		case inComment:
			if strings.HasPrefix(text[i:], "-->") {
				s.state, i = inText, i+2
			}
		case inTag:
			switch {
			case c == '>':
				s.state = inText
				if s.tag == "script" || s.tag == "style" || s.tag == "textarea" || s.tag == "title" {
					s.state = inRawText
				}
			case c == '=':
				s.attr = strings.ToLower(strings.TrimSpace(s.name.String()))
				s.name.Reset()
				j := i + 1
				for j < len(text) && (text[j] == ' ' || text[j] == '\t' || text[j] == '\n' || text[j] == '\r') {
					j++
				}
				if j < len(text) && (text[j] == '"' || text[j] == '\'') {
					s.state, s.quote, s.started, i = inQuotedValue, text[j], false, j
				} else {
					s.state, i = inUnquotedValue, j-1
				}
			case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '/':
				s.name.Reset()
			default:
				s.name.WriteByte(c)
			}
		case inQuotedValue:
			if c == s.quote {
				s.state, s.attr = inTag, ""
			} else if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
				s.started = true
			}
		case inUnquotedValue:
			switch c {
			case ' ', '\t', '\n', '\r':
				s.state, s.attr = inTag, ""
			case '>':
				s.state, s.attr = inTag, ""
				i--
			}
		case inRawText:
			if c == '<' && strings.HasPrefix(strings.ToLower(text[i:]), "</"+s.tag) {
				s.state, s.tag, i = inTag, "", i+1+len(s.tag)
				s.name.Reset()
			}
		}
	}
}

var urlAttrs = map[string]bool{
	"href": true, "src": true, "action": true, "formaction": true, "background": true,
	"poster": true, "cite": true, "data": true, "codebase": true, "longdesc": true,
	"usemap": true, "srcset": true, "xlink:href": true, "lowsrc": true, "dynsrc": true,
}

// escape writes v for its position: text, titles and plain attributes are
// HTML-escaped, a URL attribute refuses any scheme but http, https, mailto and
// tel, and a value inside a tag, a script, a style or an event handler is dropped.
func (p htmlPosition) escape(v string) string {
	switch p.state {
	case inText, inComment:
		return html.EscapeString(v)
	case inQuotedValue:
		switch {
		case strings.HasPrefix(p.attr, "on"), p.attr == "style", p.attr == "srcdoc":
			return ""
		case urlAttrs[p.attr] && !p.started:
			return html.EscapeString(safeURL(v))
		case urlAttrs[p.attr]:
			return escapeURLPart(v)
		}
		return html.EscapeString(v)
	case inRawText:
		if p.tag == "title" || p.tag == "textarea" {
			return html.EscapeString(v)
		}
	}
	return ""
}

// safeURL keeps a URL whose scheme is http, https, mailto or tel, or that has
// none; anything else becomes the inert fragment html/template uses.
func safeURL(v string) string {
	u := strings.TrimSpace(v)
	end := strings.IndexAny(u, "/?#")
	if end < 0 {
		end = len(u)
	}
	colon := strings.IndexByte(u[:end], ':')
	if colon < 0 {
		return u
	}
	switch strings.ToLower(u[:colon]) {
	case "http", "https", "mailto", "tel":
		return u
	}
	return "#ZgotmplZ"
}

// escapeURLPart percent-encodes everything but unreserved characters, so a
// value placed after the start of a URL can add no scheme, query or markup.
func escapeURLPart(v string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		c := v[i]
		if isLetter(c) || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&15])
	}
	return b.String()
}
