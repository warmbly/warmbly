package mailhtml

import "strings"

// An outline is the element tree of a body with the source offsets that make
// appending to it a splice instead of a reparse.
//
// html.Parse would give a better tree, but it can only give it back by
// re-rendering, and re-rendering a hand-written email rewrites markup its
// author never asked us to touch. Everything here therefore works in offsets
// into the caller's own string: the only edit anyone makes from it is
// body[:at] + fragment + body[at:].
type outlineNode struct {
	name  string // lowercased tag name; "" for the root
	attrs string // the start tag's raw attribute text
	// contentEnd is where this element's content stops: the '<' of its end
	// tag, the '<' of whatever closed it implicitly, or the end of the body.
	contentEnd int
	children   []*outlineNode
	parent     *outlineNode
	// depth is the element's nesting level, 0 for the root.
	depth int
	// hasText records non-whitespace text written directly inside this
	// element. An element holding copy of its own is where the message is,
	// not a wrapper around where the message is.
	hasText bool
	// styleOnce memoises style(), which several passes ask each element for.
	styleOnce *string
}

// maxOutlineDepth bounds how deeply elements may nest. Every ancestor walk in
// here is bounded by it, which is what keeps the scan linear: without it, a
// body that opens thousands of inline elements and never closes them makes
// every later start tag rescan the whole open chain, and 260 KB of
// "<span>...<p></p>..." took four seconds of a send's time.
//
// Real mail nests around a dozen deep and MJML about twenty, so anything past
// this is markup no reader is going to see laid out the way it was written.
const maxOutlineDepth = 64

// Elements that never contain anything, so they never open a scope.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}

// Elements whose content is text, not markup: a "<td>" inside one of these is
// characters the reader sees or CSS, never an element.
var rawTextElements = map[string]bool{
	"script": true, "style": true, "textarea": true, "title": true,
}

// Elements a start tag closes implicitly, keyed by the tag being opened. HTML
// mail omits these end tags constantly, and an outline that believed a <td>
// stayed open until </table> would nest every later cell inside the first.
var impliedClose = map[string][]string{
	"li":     {"li"},
	"dt":     {"dt", "dd"},
	"dd":     {"dt", "dd"},
	"option": {"option"},
	"td":     {"td", "th"},
	"th":     {"td", "th"},
	"tr":     {"td", "th", "tr"},
	"tbody":  {"td", "th", "tr", "tbody", "thead", "tfoot"},
	"thead":  {"td", "th", "tr", "tbody", "thead", "tfoot"},
	"tfoot":  {"td", "th", "tr", "tbody", "thead", "tfoot"},
}

// Block-level elements, which close an open <p>.
var blockElements = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true,
	"center": true, "details": true, "div": true, "dl": true, "fieldset": true,
	"figure": true, "footer": true, "form": true, "h1": true, "h2": true,
	"h3": true, "h4": true, "h5": true, "h6": true, "header": true, "hr": true,
	"li": true, "main": true, "menu": true, "nav": true, "ol": true, "p": true,
	"pre": true, "section": true, "table": true, "ul": true,
}

// outline builds the element tree of body. It never fails: markup it cannot
// make sense of leaves the tree shallower, which only means an append lands
// where it lands today.
func outline(body string) *outlineNode {
	root := &outlineNode{contentEnd: len(body)}
	cur := root

	// closeTo ends n and everything still open inside it. n has to be cur or
	// one of its ancestors; both callers find it by walking that chain.
	closeTo := func(n *outlineNode, at int) {
		for c := cur; c != root; c = c.parent {
			c.contentEnd = at
			if c == n {
				cur = n.parent
				return
			}
		}
	}
	// closeNamed closes the outermost run of elements a start tag ends
	// implicitly. It walks through anything the author was entitled to leave
	// open, so neither "<td><b>x<td>" nor "<td><p>x<td>" nests the second
	// cell inside the first.
	closeNamed := func(names []string, at int) {
		var outermost *outlineNode
		for n := cur; n != nil && n != root; n = n.parent {
			if contains(names, n.name) {
				outermost = n
				continue
			}
			if !inlineElements[n.name] && !optionalEndTag[n.name] {
				break
			}
		}
		if outermost != nil {
			closeTo(outermost, at)
		}
	}

	for i := 0; i < len(body); {
		lt := strings.IndexByte(body[i:], '<')
		if lt < 0 {
			if strings.TrimSpace(body[i:]) != "" {
				cur.hasText = true
			}
			break
		}
		if strings.TrimSpace(body[i:i+lt]) != "" {
			cur.hasText = true
		}
		i += lt

		if hasPrefixFold(body[i:], "<!--") {
			end := strings.Index(body[i+4:], "-->")
			if end < 0 {
				break
			}
			i += 4 + end + 3
			continue
		}
		end := TagEnd(body[i:])
		if end < 0 {
			break
		}
		tag := body[i : i+end+1]
		next := i + end + 1

		if len(tag) > 1 && (tag[1] == '!' || tag[1] == '?') {
			i = next // doctype, processing instruction, bogus comment
			continue
		}
		closing := len(tag) > 1 && tag[1] == '/'
		name, attrs, selfClosing := splitTag(tag)
		if name == "" {
			// Not markup after all ("a < b"): the '<' is text.
			cur.hasText = true
			i++
			continue
		}

		switch {
		case closing:
			if !voidElements[name] {
				if n := openAncestor(cur, root, name); n != nil {
					closeTo(n, i)
				}
			}
		case voidElements[name]:
			cur.children = append(cur.children, &outlineNode{name: name, attrs: attrs, parent: cur, depth: cur.depth + 1, contentEnd: i})
		default:
			if names, ok := impliedClose[name]; ok {
				closeNamed(names, i)
			} else if blockElements[name] {
				closeNamed([]string{"p"}, i)
			}
			n := &outlineNode{name: name, attrs: attrs, parent: cur, depth: cur.depth + 1, contentEnd: len(body)}
			cur.children = append(cur.children, n)
			if !selfClosing && n.depth < maxOutlineDepth {
				cur = n
				if rawTextElements[name] {
					// The content is text; skip it whole so a "<td>" written
					// in a stylesheet is not read as an element. Resume ON the
					// closing tag, which closes n on the next pass.
					closer := "</" + name
					if at := skipRawText(body, next, closer); at < len(body) {
						next = at - len(closer)
					} else {
						next = len(body)
					}
				}
			}
		}
		i = next
	}
	for c := cur; c != root; c = c.parent {
		c.contentEnd = len(body)
	}
	return root
}

// openAncestor returns the nearest open element named name, or nil when the
// end tag closes nothing (a stray </div>, which is ignored rather than
// unwinding the whole tree).
func openAncestor(cur, root *outlineNode, name string) *outlineNode {
	for n := cur; n != nil && n != root; n = n.parent {
		if n.name == name {
			return n
		}
	}
	return nil
}

func contains(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// splitTag reads a full "<...>" slice into its lowercased tag name, the raw
// attribute text after it, and whether it closed itself. An empty name means
// the '<' did not start a tag.
func splitTag(tag string) (name, attrs string, selfClosing bool) {
	i := 1
	if i < len(tag) && tag[i] == '/' {
		i++
	}
	start := i
	if i >= len(tag) || !isLetter(tag[i]) {
		return "", "", false
	}
	for i < len(tag) {
		c := tag[i]
		if isLetter(c) || c >= '0' && c <= '9' || c == ':' || c == '-' || c == '_' {
			i++
			continue
		}
		break
	}
	// A name has to be followed by whitespace or the end of the tag; "<3" and
	// "<b@d" are text, not markup.
	if i < len(tag)-1 && !isSpaceByte(tag[i]) && tag[i] != '/' {
		return "", "", false
	}
	rest := strings.TrimRight(tag[i:len(tag)-1], " \t\r\n\f")
	// A trailing '/' only closes the tag when it is not the last character of
	// an unquoted attribute value: "<a href=/promo/>" is a link to a path,
	// and reading it as self-closing left the anchor's content a sibling.
	if strings.HasSuffix(rest, "/") {
		if before := len(rest) - 2; before < 0 || isSpaceByte(rest[before]) || rest[before] == '"' || rest[before] == '\'' {
			selfClosing = true
			rest = rest[:len(rest)-1]
		}
	}
	return strings.ToLower(tag[start:i]), strings.TrimSpace(rest), selfClosing
}

// Elements whose end tag HTML makes optional, so an author leaves them open
// on purpose and the markup after one is not inside it.
var optionalEndTag = map[string]bool{
	"p": true, "li": true, "dt": true, "dd": true, "option": true,
}

// Inline elements, which an author may leave open without meaning to nest
// everything after them inside.
var inlineElements = map[string]bool{
	"a": true, "abbr": true, "b": true, "big": true, "cite": true, "code": true,
	"em": true, "font": true, "i": true, "label": true, "s": true, "small": true,
	"span": true, "strike": true, "strong": true, "sub": true, "sup": true,
	"tt": true, "u": true,
}

func isLetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

// attr returns one attribute's value, and whether it was written at all: a
// bare "hidden" has no value but is still present.
func (n *outlineNode) attr(name string) (string, bool) {
	attrs := n.attrs
	for i := 0; i < len(attrs); {
		for i < len(attrs) && isSpaceByte(attrs[i]) {
			i++
		}
		start := i
		for i < len(attrs) && !isSpaceByte(attrs[i]) && attrs[i] != '=' {
			i++
		}
		key := attrs[start:i]
		if key == "" {
			i++
			continue
		}
		for i < len(attrs) && isSpaceByte(attrs[i]) {
			i++
		}
		if i >= len(attrs) || attrs[i] != '=' {
			if strings.EqualFold(key, name) {
				return "", true
			}
			continue
		}
		i++
		for i < len(attrs) && isSpaceByte(attrs[i]) {
			i++
		}
		var val string
		switch {
		case i < len(attrs) && (attrs[i] == '"' || attrs[i] == '\''):
			quote := attrs[i]
			i++
			if j := strings.IndexByte(attrs[i:], quote); j >= 0 {
				val, i = attrs[i:i+j], i+j+1
			} else {
				val, i = attrs[i:], len(attrs)
			}
		default:
			vs := i
			for i < len(attrs) && !isSpaceByte(attrs[i]) {
				i++
			}
			val = attrs[vs:i]
		}
		if strings.EqualFold(key, name) {
			return val, true
		}
	}
	return "", false
}

// style returns the element's style attribute folded to lower case and
// normalised around its punctuation, so "display : NONE !important" can be
// matched as "display:none". The spaces INSIDE a value are kept: stripping
// them all turned the shorthand "padding: 20px 30px" into one unreadable
// length. Containment only: no offset is ever taken from it.
func (n *outlineNode) style() string {
	if n.styleOnce != nil {
		return *n.styleOnce
	}
	raw, _ := n.attr("style")
	v := strings.ToLower(strings.Join(strings.Fields(raw), " "))
	for _, pair := range [][2]string{{" :", ":"}, {": ", ":"}, {" ;", ";"}, {"; ", ";"}} {
		v = strings.ReplaceAll(v, pair[0], pair[1])
	}
	n.styleOnce = &v
	return v
}

// TagEnd returns the index of the '>' that closes the tag starting at s[0], or
// -1 when there is none. A '>' inside a quoted attribute value does not close
// anything: reading one as the end split `<a title="x > y" href="URL">` into a
// tag and a run of text, and the href in that "text" was then rewritten into a
// dead link.
func TagEnd(s string) int {
	var quote byte
	for i := 1; i < len(s); i++ {
		switch c := s[i]; {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '>':
			return i
		}
	}
	return -1
}
