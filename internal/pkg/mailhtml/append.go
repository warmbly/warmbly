package mailhtml

import (
	"regexp"
	"strconv"
	"strings"
)

// Elements that wrap other elements rather than carry copy of their own. Only
// these are followed when looking for the container an email was laid out in.
var layoutElements = map[string]bool{
	"table": true, "tbody": true, "thead": true, "tfoot": true, "tr": true,
	"td": true, "th": true, "div": true, "center": true,
	"section": true, "article": true, "main": true,
}

// Table sections a row can be appended to.
var tableSections = map[string]bool{
	"table": true, "tbody": true, "thead": true, "tfoot": true,
}

// Elements that hold no layout of their own: metadata, and a line break,
// which is a plausible thing to find trailing a container and should not be
// read as a second one.
//
// <noscript> is deliberately NOT here. Mail clients run no scripts, so its
// fallback content is content the reader sees, and skipping one that follows
// the layout would put the opt-out line above copy instead of last.
var nonLayoutElements = map[string]bool{
	"head": true, "meta": true, "link": true, "style": true, "script": true,
	"title": true, "base": true, "br": true, "wbr": true,
}

// maxContainerDepth bounds the descent. Real mail nests four or five wrappers
// deep; anything past this is markup we should not be reasoning about.
const maxContainerDepth = 32

// AppendToContent places a fragment at the end of a message's visible content:
// inside the container the email was laid out in, not after it.
//
// A designed email is a centred box inside a full-width table, and
// InsertBeforeBodyEnd puts the signature and the opt-out footer after that
// box: flush against the left edge of the window, in the page's own
// background, styled by nothing (issue #462). The container is the deepest
// element that still holds all of the message, so appending inside it is what
// makes the footer read as the last line of the email rather than as debris
// under it.
//
// Anything this cannot place with certainty falls back to InsertBeforeBodyEnd,
// and nothing here ever reparses or re-renders the body: the result is always
// the caller's own bytes with the fragment spliced into one offset.
func AppendToContent(body, fragment string) string {
	if fragment == "" {
		return body
	}
	// Past this a message is already unsendable (Gmail clips at ~102 KB), so
	// it is not worth the scan.
	if body == "" || len(body) > maxInlineBytes {
		return InsertBeforeBodyEnd(body, fragment)
	}

	root := outline(body)
	target := contentContainer(root)
	// A row reached on its own means the table has several cells across; the
	// footer belongs under them, as a row of its own.
	if target != nil && target.name == "tr" {
		if target.parent != nil && tableSections[target.parent.name] {
			target = target.parent
		} else {
			target = nil
		}
	}
	if target != nil && !layoutElements[target.name] {
		target = nil
	}

	// The insertion point is inside the content column for a simply laid out
	// email, and is still the full width of the page for a builder export,
	// whose rows each centre a card of their own. In the second case the
	// footer has to centre itself or it renders hard left all the same.
	scope := target
	if scope == nil {
		scope = root
	}
	if !widthConstrained(target) {
		if w := contentWidth(scope); w > 0 {
			fragment = centredBlock(w, fragment)
		}
	}

	if target == nil {
		return InsertBeforeBodyEnd(body, fragment)
	}
	at := target.contentEnd
	if at < 0 || at > len(body) {
		return InsertBeforeBodyEnd(body, fragment)
	}
	if tableSections[target.name] {
		// A <p> written straight into a table is hoisted back out of it by
		// every HTML parser, which lands it exactly where the bug put it.
		fragment = tableRow(target, fragment)
	}
	return body[:at] + fragment + body[at:]
}

// contentContainer returns the deepest element that still contains the whole
// message: from the body down, while there is exactly one layout element
// holding everything and no copy written beside it. nil when that is the body
// itself, which is what InsertBeforeBodyEnd already does.
func contentContainer(root *outlineNode) *outlineNode {
	cur := root
	if b := firstNamed(root, "body"); b != nil {
		cur = b
	} else if h := firstNamed(root, "html"); h != nil {
		cur = h
	}
	for depth := 0; depth < maxContainerDepth; depth++ {
		// Copy written directly here means this element is the message, not a
		// wrapper around it.
		if cur.hasText {
			break
		}
		kids := layoutChildren(cur)
		if len(kids) != 1 || !layoutElements[kids[0].name] {
			break
		}
		cur = kids[0]
	}
	if cur == root || cur.name == "body" || cur.name == "html" {
		return nil
	}
	return cur
}

// layoutChildren lists the children that take up room on the page. Metadata is
// not layout, and neither is anything the author hid: the tracking pixel and
// the preheader line are both children of <body> in a designed email, and
// counting either would stop the descent at the body and leave the footer
// exactly where it is today.
func layoutChildren(n *outlineNode) []*outlineNode {
	var out []*outlineNode
	for _, c := range n.children {
		if nonLayoutElements[c.name] || takesNoSpace(c) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// takesNoSpace reports whether an element is written to take up no room: the
// preheader ("display:none", or the max-height:0 / mso-hide variants every
// template uses), a hidden attribute, or a 1x1 tracking pixel.
func takesNoSpace(n *outlineNode) bool {
	if _, ok := n.attr("hidden"); ok {
		return true
	}
	if n.name == "img" {
		w, _ := n.attr("width")
		h, _ := n.attr("height")
		if w == "1" && h == "1" {
			return true
		}
	}
	style := n.style()
	switch {
	case strings.Contains(style, "display:none"), strings.Contains(style, "mso-hide:all"):
		return true
	case strings.Contains(style, "max-height:0") && strings.Contains(style, "overflow:hidden"):
		return true
	}
	return false
}

// firstNamed finds <body> or <html>, which sit within a few levels of the
// root in any document that has them. The depth bound keeps a deeply nested
// body (or an adversarial one) from being walked in full to learn nothing.
func firstNamed(n *outlineNode, name string) *outlineNode {
	if n.depth > 6 {
		return nil
	}
	for _, c := range n.children {
		if c.name == name {
			return c
		}
		if found := firstNamed(c, name); found != nil {
			return found
		}
	}
	return nil
}

// tableRow wraps a fragment as the last row of a table section, spanning every
// column and picking up the side padding of the rows above it so the footer
// lines up with the copy rather than sitting against the edge of the box.
func tableRow(section *outlineNode, fragment string) string {
	cols, cell := 0, (*outlineNode)(nil)
	for _, row := range section.children {
		if row.name != "tr" {
			continue
		}
		n := 0
		for _, c := range row.children {
			if c.name != "td" && c.name != "th" {
				continue
			}
			if n == 0 {
				cell = c
			}
			// A table is as wide as its grid, not as its cells: counting a
			// <td colspan="2"> as one column made the appended row stop short
			// of the copy above it.
			n += cellSpan(c)
		}
		if n > cols {
			cols = n
		}
	}
	td := "<td"
	if cols > 1 {
		td += ` colspan="` + strconv.Itoa(cols) + `"`
	}
	if pad := sidePadding(cell); pad != "" {
		td += ` style="` + pad + `"`
	}
	return "<tr>" + td + ">" + fragment + "</td></tr>"
}

// cellSpan is how many grid columns a cell occupies. HTML caps colspan at
// 1000, and anything unreadable counts as the one column it is written as.
func cellSpan(cell *outlineNode) int {
	raw, ok := cell.attr("colspan")
	if !ok {
		return 1
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 || n > 1000 {
		return 1
	}
	return n
}

// sidePadding returns the left/right padding of a cell as a style declaration,
// read from padding-left/padding-right or from the padding shorthand.
func sidePadding(cell *outlineNode) string {
	if cell == nil {
		return ""
	}
	style := cell.style()
	left, right := declValue(style, "padding-left"), declValue(style, "padding-right")
	if left == "" || right == "" {
		// The shorthand is top / right / bottom / left, and the sides only
		// mirror each other until it names all four: reading the left from
		// the second value put the footer under a four-value cell out of line
		// with the copy in it.
		if parts := strings.Fields(declValue(style, "padding")); len(parts) > 0 {
			l, r := parts[0], parts[0]
			if len(parts) > 1 {
				l, r = parts[1], parts[1]
			}
			if len(parts) > 3 {
				l = parts[3]
			}
			if left == "" {
				left = l
			}
			if right == "" {
				right = r
			}
		}
	}
	// Only a real length is copied. The value is whatever the author wrote,
	// and writing one that is not back out unquoted produced a style
	// attribute that closed itself early and broke the cell.
	if !cssLength.MatchString(left) {
		left = ""
	}
	if !cssLength.MatchString(right) {
		right = ""
	}
	if left == "" && right == "" {
		return ""
	}
	var out []string
	if left != "" {
		out = append(out, "padding-left:"+left)
	}
	if right != "" {
		out = append(out, "padding-right:"+right)
	}
	return strings.Join(out, ";")
}

// cssLength is a length we are willing to copy into markup of our own.
var cssLength = regexp.MustCompile(`(?i)^[0-9]+(\.[0-9]+)?(px|pt|em|rem|%)?$`)

// declValue reads one property out of a normalised style attribute (see
// outlineNode.style). It matches on a declaration boundary, so "padding" does
// not answer with the value of "padding-left".
func declValue(style, prop string) string {
	for i := 0; i < len(style); {
		end := strings.IndexByte(style[i:], ';')
		if end < 0 {
			end = len(style)
		} else {
			end += i
		}
		decl := style[i:end]
		if colon := strings.IndexByte(decl, ':'); colon > 0 && decl[:colon] == prop {
			return decl[colon+1:]
		}
		i = end + 1
	}
	return ""
}

// A content column is between these widths. Narrower is a call-to-action
// button or a spacer, wider is the page rather than the column the copy sits
// in. The floor is deliberately above button territory: the narrowest real
// template is around 480px, and a centred 300px button in a card whose own
// width lives in a stylesheet was otherwise the only candidate in sight.
const (
	minContentWidth = 400
	maxContentWidth = 1000
)

// widthConstrained reports whether an insertion point already sits inside the
// email's content column, i.e. some element around it declares a pixel width.
// A footer added there is as wide as the copy above it and needs nothing.
func widthConstrained(n *outlineNode) bool {
	for ; n != nil; n = n.parent {
		if w := declaredWidth(n); w > 0 && w < maxContentWidth {
			return true
		}
	}
	return false
}

// contentWidth returns the width of the centred card an email was built
// around, or 0 when it has none.
//
// A builder export (Unlayer, Beefree, Stripo) is a stack of full-width rows
// each holding its own centred 600px table, so the deepest element that still
// contains the whole message is the full-width cell those rows sit in.
// Appending there is inside the document but still against the left edge of
// the window, which is the bug. Knowing the card's width lets the footer be
// centred on it instead.
//
// Only an element that is itself centred counts, so a coloured box that
// happens to declare a width in a left-aligned email is never mistaken for
// the card.
func contentWidth(root *outlineNode) int {
	counts := map[int]int{}
	var walk func(*outlineNode, int)
	seen := 0
	walk = func(n *outlineNode, depth int) {
		for _, c := range n.children {
			if seen++; seen > 2000 || depth > maxContainerDepth {
				return
			}
			if (c.name == "table" || c.name == "div") && len(c.children) > 0 && isCentred(c) {
				if w := declaredWidth(c); w >= minContentWidth && w < maxContentWidth {
					// The outermost centred box is the card. Whatever is
					// centred inside it is a module of the card, not another
					// candidate to be counted against it.
					counts[w]++
					continue
				}
			}
			walk(c, depth+1)
		}
	}
	walk(root, 0)

	best, bestCount := 0, 0
	for w, n := range counts {
		if n > bestCount || (n == bestCount && w > best) {
			best, bestCount = w, n
		}
	}
	return best
}

// isCentred reports whether an element is centred on the page: by its own
// align attribute, by auto side margins, or by the cell it sits in.
func isCentred(n *outlineNode) bool {
	if align, _ := n.attr("align"); strings.EqualFold(align, "center") {
		return true
	}
	if marginAuto(n.style()) {
		return true
	}
	if p := n.parent; p != nil {
		if align, _ := p.attr("align"); strings.EqualFold(align, "center") {
			return true
		}
	}
	return false
}

// marginAuto reports whether a style centres its element with auto side
// margins. The shorthand has to be read rather than matched as text: MJML
// writes "margin:0px auto" and a hand-written template writes "margin:0 auto",
// and a check for either spelling misses the other one.
func marginAuto(style string) bool {
	if declValue(style, "margin-left") == "auto" && declValue(style, "margin-right") == "auto" {
		return true
	}
	parts := strings.Fields(declValue(style, "margin"))
	switch len(parts) {
	case 2, 3:
		return parts[1] == "auto"
	case 4:
		return parts[1] == "auto" && parts[3] == "auto"
	}
	return false
}

// declaredWidth reads an element's width in pixels from its width attribute or
// its style, or 0 for a percentage or no width at all.
func declaredWidth(n *outlineNode) int {
	if n.name == "" {
		return 0
	}
	if v, ok := n.attr("width"); ok {
		if w := pixels(v); w > 0 {
			return w
		}
	}
	style := n.style()
	for _, prop := range []string{"width", "max-width"} {
		if w := pixels(declValue(style, prop)); w > 0 {
			return w
		}
	}
	return 0
}

// pixels reads "600" or "600px" as 600, and anything else (a percentage, a
// calc(), an em) as 0.
func pixels(v string) int {
	v = strings.TrimSpace(strings.ToLower(v))
	v = strings.TrimSuffix(v, "px")
	n := 0
	if v == "" {
		return 0
	}
	for i := 0; i < len(v); i++ {
		if v[i] < '0' || v[i] > '9' {
			return 0
		}
		n = n*10 + int(v[i]-'0')
		if n > 1<<20 {
			return 0
		}
	}
	return n
}

// centredBlock puts a fragment in a table centred on the email's own content
// width. A table, not a max-width div: Outlook's Word engine ignores
// max-width and auto margins, and a footer it renders full width and hard
// left is the bug this is here to fix.
func centredBlock(width int, fragment string) string {
	w := strconv.Itoa(width)
	return `<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="width:100%;border-collapse:collapse"><tr>` +
		`<td align="center" style="padding:0">` +
		`<table role="presentation" width="` + w + `" cellpadding="0" cellspacing="0" border="0" align="center" style="width:` + w + `px;max-width:100%;border-collapse:collapse"><tr>` +
		`<td align="left" style="padding:0 16px">` + fragment + `</td></tr></table>` +
		`</td></tr></table>`
}
