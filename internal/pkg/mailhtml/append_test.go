package mailhtml

import (
	"strings"
	"testing"
)

// The shape issue #462 reports: an HTML email laid out as a card centred
// inside a full-width page table. Appended at the end of the document, the
// signature and the opt-out footer render against the left edge of the
// window, in the page background, styled by nothing.
const centredCard = `<html><body style="background:#f1f5f9">` +
	`<table width="100%" cellpadding="0"><tr><td align="center">` +
	`<table width="600" bgcolor="#ffffff"><tr><td style="padding:32px 24px">` +
	`<h1>Hi</h1><p>Body</p>` +
	`</td></tr></table></td></tr></table></body></html>`

func TestAppendToContentLandsInsideTheCard(t *testing.T) {
	out := AppendToContent(centredCard, "[F]")
	if !strings.Contains(out, "<p>Body</p>[F]</td></tr></table>") {
		t.Fatalf("footer did not land in the card's own cell:\n%s", out)
	}
}

// A preheader and a tracking pixel are both children of <body> in a designed
// email. Counting either as layout stops the descent at the body and leaves
// the footer exactly where the bug put it.
func TestAppendToContentIgnoresThePreheaderAndThePixel(t *testing.T) {
	body := `<body><div style="display:none;max-height:0;overflow:hidden">Quick question</div>` +
		`<table width="600"><tr><td style="padding:24px">Hi</td></tr></table>` +
		`<img src="https://t.test/t/o/x.png" width="1" height="1" style="display:none;" alt="" /></body>`
	out := AppendToContent(body, "[F]")
	if !strings.Contains(out, "Hi[F]</td>") {
		t.Fatalf("hidden siblings stopped the descent:\n%s", out)
	}
}

// A <p> written straight into a table is hoisted back out of it by every HTML
// parser, which lands it exactly where the bug put it. A table with rows of
// its own gets a row instead, spanning every column and picking up the side
// padding of the rows above so the footer lines up with the copy.
func TestAppendToContentAddsARowToATable(t *testing.T) {
	body := `<table width="600"><tbody>` +
		`<tr><td>Banner</td><td>Logo</td></tr>` +
		`<tr><td style="padding:20px 30px">Copy</td><td>Aside</td></tr>` +
		`</tbody></table>`
	out := AppendToContent(body, "[F]")
	want := `<tr><td colspan="2" style="padding-left:30px;padding-right:30px">[F]</td></tr></tbody>`
	if !strings.Contains(out, want) {
		t.Fatalf("expected %s in:\n%s", want, out)
	}
}

// A builder export (Unlayer, Beefree, Stripo) is a stack of full-width rows
// each centring a card of its own, so the deepest element holding the whole
// message is the full-width cell. Appending there is inside the document but
// still hard left; the footer has to centre itself on the card's width.
func TestAppendToContentCentresItselfInABuilderExport(t *testing.T) {
	row := `<div class="u-row-container" style="padding:0px"><div class="u-row" style="margin:0 auto;min-width:320px;max-width:600px;background-color:#ffffff">` +
		`<div style="display:table;width:100%"><div class="u-col" style="display:table-cell"><div style="padding:20px"><p>Copy</p></div></div></div>` +
		`</div></div>`
	body := `<body><table id="u_body" style="width:100%;margin:0 auto"><tbody><tr><td>` + row + row + `</td></tr></tbody></table></body>`

	out := AppendToContent(body, "[F]")
	if !strings.Contains(out, `width="600"`) || !strings.Contains(out, `align="center"`) {
		t.Fatalf("footer was not centred on the card's width:\n%s", out)
	}
	if !strings.Contains(out, "[F]</td></tr></table></td></tr></table></td></tr></tbody></table></body>") {
		t.Fatalf("footer did not land at the end of the content:\n%s", out)
	}
	// The same card, written as one container rather than a stack of rows,
	// needs no wrapper: the insertion point is already the content column.
	if out := AppendToContent(centredCard, "[F]"); strings.Contains(out, "role=\"presentation\"") {
		t.Fatalf("a width-constrained insertion point should not be wrapped:\n%s", out)
	}
}

// An email with no layout is every campaign written in the visual editor.
// Nothing about those changes: the footer goes at the end, unwrapped.
func TestAppendToContentLeavesAPlainBodyAlone(t *testing.T) {
	for in, want := range map[string]string{
		"<p>a</p>":                          "<p>a</p>[F]",
		"<p>a</p><p>b</p>":                  "<p>a</p><p>b</p>[F]",
		"<html><p>x</p></html>":             "<html><p>x</p>[F]</html>",
		"<html><body>x</body></html>":       "<html><body>x[F]</body></html>",
		"<p>a</p><style>i{}</style></body>": "<p>a</p><style>i{}</style>[F]</body>",
	} {
		if got := AppendToContent(in, "[F]"); got != want {
			t.Errorf("AppendToContent(%q):\n got %q\nwant %q", in, got, want)
		}
	}
	if got := AppendToContent("<p>a</p>", ""); got != "<p>a</p>" {
		t.Errorf("an empty fragment changed the body: %q", got)
	}
}

// Outlook conditional comments carry their own </body> and their own tables.
// Reading either as the document's puts the footer inside a comment, where
// Outlook shows it and every other client does not (issue #393).
func TestAppendToContentIgnoresConditionalComments(t *testing.T) {
	body := `<html><body>` +
		`<!--[if mso]><table width="600"><tr><td><![endif]-->` +
		`<table width="600"><tr><td style="padding:16px"><p>Hi</p></td></tr></table>` +
		`<!--[if mso]></td></tr></table></body><![endif]-->` +
		`</body></html>`
	out := AppendToContent(body, "[F]")
	if !strings.Contains(out, "<p>Hi</p>[F]</td>") {
		t.Fatalf("footer did not land in the real table's cell:\n%s", out)
	}
	if !strings.Contains(out, `<!--[if mso]></td></tr></table></body><![endif]-->`) {
		t.Fatalf("footer landed inside a conditional comment:\n%s", out)
	}
}

// Whatever the shape, the body is the caller's own bytes with something
// inserted at exactly one offset. Nothing here reparses or re-renders: a
// message that ships as its author wrote it is the whole point.
func TestAppendToContentOnlySplices(t *testing.T) {
	for _, body := range []string{
		centredCard, designed,
		`<table width="600"><tr><td>a</td><td>b</td></tr></table>`,
		`<div style="max-width:600px;margin:0 auto"><p>Hi</p><p>Bye</p></div>`,
		`<p>a</p>`,
		`<td>orphan cell`,
		`<div><style>td{}</style><p>x</p>`,
		"\xa4\xa4\xa4\xa4</BodY>",
	} {
		out := AppendToContent(body, "[F]")
		if !strings.Contains(out, "[F]") {
			t.Errorf("fragment was dropped from %q: %q", body, out)
		}
		if !isSplice(body, out) {
			t.Errorf("body was rewritten, not spliced:\n in %q\nout %q", body, out)
		}
	}
}

// isSplice reports whether out is body with one run of text inserted: the
// common prefix and the common suffix together have to account for all of it.
func isSplice(body, out string) bool {
	p := 0
	for p < len(body) && p < len(out) && body[p] == out[p] {
		p++
	}
	s := 0
	for s < len(body)-p && s < len(out)-p && body[len(body)-1-s] == out[len(out)-1-s] {
		s++
	}
	return p+s >= len(body)
}

// HTML mail leaves end tags off constantly, and the outline has to close the
// same elements a browser would or the shape it reports is not the shape the
// reader sees.
func TestAppendToContentReadsMarkupWithOmittedEndTags(t *testing.T) {
	// An unclosed <p> before the next cell: the second cell is a sibling of
	// the first, not something nested inside it, so the table is two columns
	// across and the appended row has to span both.
	body := `<table width="600"><tbody><tr><td><p>One<td><p>Two<tr><td>Three<td>Four</tbody></table>`
	if out := AppendToContent(body, "[F]"); !strings.Contains(out, `<tr><td colspan="2">[F]</td></tr></tbody>`) {
		t.Errorf("omitted end tags mis-read the table:\n%s", out)
	}

	// A trailing slash inside an unquoted attribute value is part of the
	// value, not a self-closing tag. Read as one, the element never opens and
	// everything written inside it becomes a sibling, which moves the end of
	// the content somewhere the reader does not see it.
	unquoted := `<table width="600"><tr><td background=/bg.gif/><p>a</p><p>b</p></td></tr></table>`
	if out := AppendToContent(unquoted, "[F]"); !strings.Contains(out, "<p>b</p>[F]</td>") {
		t.Errorf("an unquoted path was read as a self-closing tag:\n%s", out)
	}
}

// The padding shorthand is top / right / bottom / left, and the sides only
// mirror each other until it names all four.
func TestAppendedRowMatchesAFourValuePadding(t *testing.T) {
	body := `<table width="600"><tr><td>a</td></tr><tr><td style="padding:10px 20px 10px 40px">b</td></tr></table>`
	if out := AppendToContent(body, "[F]"); !strings.Contains(out, `style="padding-left:40px;padding-right:20px"`) {
		t.Errorf("the appended row did not line up with the copy above it:\n%s", out)
	}
}

// mjmlSection is one MJML row: a centred 600px div wrapping a full-width
// table, between the Outlook conditional comments MJML emits around it.
func mjmlSection(text string) string {
	return `<!--[if mso | IE]><table align="center" border="0" width="600"><tr><td><![endif]-->` +
		`<div style="margin:0px auto;max-width:600px;">` +
		`<table align="center" border="0" cellpadding="0" cellspacing="0" role="presentation" style="width:100%;"><tbody><tr>` +
		`<td style="direction:ltr;font-size:0px;padding:20px 0;text-align:center;">` +
		`<div class="mj-column-per-100" style="font-size:0px;text-align:left;display:inline-block;vertical-align:top;width:100%;">` +
		`<table border="0" cellpadding="0" cellspacing="0" role="presentation" width="100%"><tbody><tr>` +
		`<td style="font-size:0px;padding:10px 25px;word-break:break-word;">` +
		`<div style="font-family:Ubuntu;font-size:13px;line-height:1;text-align:left;color:#000000;">` + text + `</div>` +
		`</td></tr></tbody></table></div></td></tr></tbody></table></div>` +
		`<!--[if mso | IE]></td></tr></table><![endif]-->`
}

// MJML is the most common way a designed email reaches us, and it writes its
// centring as "margin:0px auto". Matching the hand-written "margin:0 auto" as
// text missed it, and a multi-section export went out hard left.
func TestAppendToContentHandlesMJML(t *testing.T) {
	page := func(sections ...string) string {
		return `<body style="background-color:#F4F4F4;"><div style="background-color:#F4F4F4;">` +
			strings.Join(sections, "") + `</div></body>`
	}

	// One section is a single container all the way down, so the line goes
	// inside the last text block, padded like the copy around it.
	one := AppendToContent(page(mjmlSection("Hello")), "[F]")
	if !strings.Contains(one, "Hello[F]</div>") {
		t.Errorf("a single-section export did not land in its content block:\n%s", one)
	}
	if strings.Contains(one, `<table role="presentation" width="600"`) {
		t.Errorf("a container that already holds the copy needs no wrapper:\n%s", one)
	}

	// Several sections have no single container to sit in, so the line
	// centres itself on the width the sections share.
	multi := AppendToContent(page(mjmlSection("Hello"), mjmlSection("Second")), "[F]")
	if !strings.Contains(multi, `<table role="presentation" width="600"`) {
		t.Errorf("a multi-section export was not centred on the section width:\n%s", multi)
	}
	if !strings.Contains(multi, "[F]</td></tr></table></td></tr></table></div></body>") {
		t.Errorf("the line did not land at the end of the content:\n%s", multi)
	}
}

// A card whose width lives in a stylesheet declares none inline, so the only
// centred element with a pixel width can be the call-to-action button. Sizing
// the opt-out line on that squeezed it to a third of the copy above it.
func TestAppendToContentIsNotSizedByAButton(t *testing.T) {
	body := `<body><table class="body"><tr><td align="center"><center>` +
		`<table align="center" class="container"><tbody><tr><td class="wrapper">` +
		`<p>Hello</p><p>More copy</p>` +
		`<table align="center" width="300" class="button"><tr><td>Book a demo</td></tr></table>` +
		`</td></tr></tbody></table></center></td></tr></table></body>`
	out := AppendToContent(body, "[F]")
	if strings.Contains(out, `role="presentation"`) {
		t.Errorf("the line was sized on the button rather than the copy:\n%s", out)
	}
	if !strings.Contains(out, "</table>[F]</td>") {
		t.Errorf("the line did not land at the end of the card's cell:\n%s", out)
	}
}

// The padding copied onto the appended row is whatever the author wrote in
// their own style attribute. Writing a value that is not a length back out
// unquoted produced a style attribute that closed itself early and broke the
// cell, so anything that is not a length is dropped instead.
func TestAppendedRowOnlyCopiesARealLength(t *testing.T) {
	garbled := `<table width="600"><tr><td>a</td></tr><tr><td style='padding:0 "x'>b</td></tr></table>`
	if out := AppendToContent(garbled, "[F]"); !strings.Contains(out, "<tr><td>[F]</td></tr>") {
		t.Errorf("a value that is not a length was copied into the markup:\n%s", out)
	}
	ems := `<table width="600"><tr><td>a</td></tr><tr><td style="padding:1em 2em">b</td></tr></table>`
	if out := AppendToContent(ems, "[F]"); !strings.Contains(out, `style="padding-left:2em;padding-right:2em"`) {
		t.Errorf("a length in em should still line the row up:\n%s", out)
	}
}

// Mail clients run no scripts, so a <noscript> in the body is copy the reader
// sees. Skipping one that follows the layout put the opt-out line above it
// instead of last.
func TestAppendToContentDoesNotSkipNoscriptContent(t *testing.T) {
	body := `<body><table width="600"><tr><td>Copy</td></tr></table>` +
		`<noscript><p>Enable images to see this</p></noscript></body>`
	out := AppendToContent(body, "[F]")
	if strings.Index(out, "[F]") < strings.Index(out, "<noscript>") {
		t.Errorf("the line landed above visible noscript copy:\n%s", out)
	}
}

// A table is as wide as its grid, not as its cells.
func TestAppendedRowSpansColspan(t *testing.T) {
	body := `<table width="600"><tbody>` +
		`<tr><td colspan="3">Banner</td></tr>` +
		`<tr><td colspan="2">Copy</td><td>Aside</td></tr>` +
		`</tbody></table>`
	if out := AppendToContent(body, "[F]"); !strings.Contains(out, `<tr><td colspan="3">[F]</td></tr>`) {
		t.Errorf("the appended row stopped short of the grid:\n%s", out)
	}
	// Anything unreadable counts as the one column it is written as.
	odd := `<table width="600"><tr><td colspan="nope">a</td><td>b</td></tr><tr><td>c</td><td>d</td></tr></table>`
	if out := AppendToContent(odd, "[F]"); !strings.Contains(out, `<tr><td colspan="2">[F]</td></tr>`) {
		t.Errorf("an unreadable colspan should count as one column:\n%s", out)
	}
}

// Every ancestor walk in the outline runs from an element to the root, so the
// nesting limit is what keeps the whole scan linear. Without it, a body that
// opens thousands of inline elements and never closes them makes every later
// start tag rescan the entire open chain: 250 KB of it took four seconds of a
// send's time, and 2 MB took minutes.
//
// Asserted on the depth rather than on the clock. The bound is what makes the
// walk cheap, and a wall-clock threshold measures the machine instead: under
// the race detector CI runs with, the same input takes a hundred times longer
// while being just as bounded.
func TestOutlineNestingIsBounded(t *testing.T) {
	const k = 20000
	body := strings.Repeat("<span>", k) + strings.Repeat("<p></p>", k)
	if d := deepest(outline(body)); d > maxOutlineDepth {
		t.Errorf("nesting reached %d, past the %d bound: every ancestor walk is quadratic again", d, maxOutlineDepth)
	}

	// Nesting that is deep but properly closed, and within the descent's own
	// limit, still reads correctly.
	deep := strings.Repeat("<div>", 20) + "<p>a</p><p>b</p>" + strings.Repeat("</div>", 20)
	if out := AppendToContent(deep, "[F]"); !strings.Contains(out, "<p>b</p>[F]</div>") {
		t.Errorf("deep but valid nesting was mis-read:\n%s", out)
	}
}

// deepest returns the deepest element in an outline.
func deepest(n *outlineNode) int {
	d := n.depth
	for _, c := range n.children {
		if cd := deepest(c); cd > d {
			d = cd
		}
	}
	return d
}
