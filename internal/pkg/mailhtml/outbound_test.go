package mailhtml

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// A designed email: a stylesheet with classes, a media query, a :hover rule,
// a hidden preheader and a table layout. This is the shape issue #393 reports.
const designed = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Newsletter</title>
<style>
  /* brand */
  body { margin:0; background-color:#f4f4f4; font-family: Helvetica, Arial, sans-serif; }
  .wrap { width:600px; margin:0 auto; }
  .btn { background:#0284c7; color:#ffffff !important; padding:12px 20px; }
  td.cell { padding: 16px; }
  a:hover { text-decoration: underline; }
  @media only screen and (max-width:600px) { .wrap { width:100% !important; } }
</style></head>
<body>
<div style="display:none;max-height:0">Quick question about your pipeline</div>
<table class="wrap"><tr><td class="cell">
  <h1>Hi Ana</h1>
  <p>We help teams like <b>Acme</b> book more meetings.</p>
  <ul><li>Deliverability first</li><li>Real warmup</li></ul>
  <p><a class="btn" href="https://example.com/demo" style="color:#eeeeee">Book a demo</a></p>
</td></tr></table>
</body></html>`

func TestInlineCSSMovesRulesOntoElements(t *testing.T) {
	out := InlineCSS(designed)

	for _, want := range []string{
		`style="margin: 0; background-color: #f4f4f4`, // body
		`width: 600px; margin: 0 auto`,                // .wrap on the table
		`style="padding: 16px"`,                       // td.cell
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected inlined %q in:\n%s", want, out)
		}
	}
}

func TestInlineCSSHonoursTheCascade(t *testing.T) {
	out := InlineCSS(designed)
	// .btn's color is !important, so it beats the anchor's own style attribute.
	if !strings.Contains(out, "color: #ffffff !important") {
		t.Errorf("!important sheet rule should beat an inline one:\n%s", out)
	}
	if strings.Contains(out, "#eeeeee") {
		t.Errorf("the losing inline color should not survive:\n%s", out)
	}

	// Without !important the author's own inline value wins.
	plain := InlineCSS(`<style>p{color:red}</style><p style="color:blue">x</p>`)
	if !strings.Contains(plain, "color: blue") || strings.Contains(plain, "red") {
		t.Errorf("inline style should beat a sheet rule: %q", plain)
	}
}

func TestInlineCSSKeepsWhatItCannotInline(t *testing.T) {
	out := InlineCSS(designed)
	if !strings.Contains(out, "@media") {
		t.Errorf("media queries must stay in the <style> block:\n%s", out)
	}
	if !strings.Contains(out, "a:hover") {
		t.Errorf(":hover must stay in the <style> block:\n%s", out)
	}
	// The inlined rules are gone from the block, or the payload carries the
	// stylesheet twice and Gmail's clipping limit arrives sooner.
	if strings.Contains(out, "td.cell") {
		t.Errorf("an inlined rule should not remain in the block:\n%s", out)
	}
}

func TestInlineCSSSpecificityOrder(t *testing.T) {
	out := InlineCSS(`<style>p{color:red}.x{color:green}#y{color:blue}</style><p id="y" class="x">t</p>`)
	if !strings.Contains(out, "color: blue") {
		t.Errorf("the id selector should win: %q", out)
	}
}

func TestInlineCSSShorthandPrecedesLonghand(t *testing.T) {
	out := InlineCSS(`<style>td{margin:0}</style><table><tr><td style="margin-top:8px">c</td></tr></table>`)
	m, mt := strings.Index(out, "margin: 0"), strings.Index(out, "margin-top: 8px")
	if m < 0 || mt < 0 || m > mt {
		t.Errorf("shorthand must be written before the longhand that narrows it: %q", out)
	}
}

func TestInlineCSSLeavesAFragmentAFragment(t *testing.T) {
	out := InlineCSS(`<style>.x{color:red}</style><p class="x">hi</p>`)
	if strings.Contains(out, "<html") || strings.Contains(out, "<body") {
		t.Errorf("a fragment body must not gain a document wrapper: %q", out)
	}
	if !strings.Contains(out, "color: red") {
		t.Errorf("fragment rules should still inline: %q", out)
	}
}

func TestInlineCSSKeepsUninlinableRulesOfAFragment(t *testing.T) {
	// The parser hoists a leading <style> into the head; dropping the head on
	// the way out would lose the media query with it.
	out := InlineCSS(`<style>@media (max-width:600px){.x{width:100%}}</style><p class="x">hi</p>`)
	if !strings.Contains(out, "@media") {
		t.Errorf("a fragment's media query must survive: %q", out)
	}
}

func TestInlineCSSIsANoOpWithoutAStylesheet(t *testing.T) {
	body := `<p>Hi {{.FirstName}}, <a href="https://x.test">link</a></p>`
	if got := InlineCSS(body); got != body {
		t.Errorf("a body with no <style> must ship byte for byte:\n got %q\nwant %q", got, body)
	}
}

func TestInlineCSSRespectsTheAuthorsOptOut(t *testing.T) {
	body := `<style data-warmbly-inline="false">.x{color:red}</style><p class="x">hi</p>`
	out := InlineCSS(body)
	if strings.Contains(out, `style="color`) {
		t.Errorf("an opted-out stylesheet must not be inlined: %q", out)
	}
	if !strings.Contains(out, ".x{color:red}") {
		t.Errorf("an opted-out stylesheet must survive verbatim: %q", out)
	}
}

func TestInlineCSSSkipsAPrintStylesheet(t *testing.T) {
	out := InlineCSS(`<style media="print">.x{color:red}</style><p class="x">hi</p>`)
	if strings.Contains(out, `style="color`) {
		t.Errorf("a print stylesheet is not what the reader sees: %q", out)
	}
}

func TestInlineCSSSurvivesBrokenCSS(t *testing.T) {
	// Unterminated blocks, a comment inside a value, a url() holding braces
	// and semicolons: none of it may panic or lose the document.
	for _, css := range []string{
		`.a{color:red`,
		`.a{background:url(data:image/png;base64,AA{BB)}`,
		`.a{color:/* c */red}`,
		`@media{`,
		`.a{content:"}"}`,
	} {
		out := InlineCSS(`<style>` + css + `</style><p class="a">hi</p>`)
		if !strings.Contains(out, "hi") {
			t.Errorf("CSS %q lost the body: %q", css, out)
		}
	}
}

func TestInsertBeforeBodyEndIgnoresAConditionalComment(t *testing.T) {
	// Outlook conditional comments carry their own </body>. Inserting there
	// put the signature and the opt-out footer inside a comment.
	body := `<html><body><p>a</p><!--[if mso]></body><![endif]--></body></html>`
	out := InsertBeforeBodyEnd(body, "[F]")
	if !strings.Contains(out, "[F]</body></html>") {
		t.Errorf("insert landed in the conditional comment: %q", out)
	}
}

func TestInsertBeforeBodyEndVariants(t *testing.T) {
	cases := map[string]string{
		"<p>a</p>":                          "<p>a</p>[F]",
		"<html><body>x</BODY ></html>":      "<html><body>x[F]</BODY ></html>",
		"<html><p>x</p></html>":             "<html><p>x</p>[F]</html>",
		"<body>x</body><body>y</body>":      "<body>x</body><body>y[F]</body>",
		"<p>a</p><style>i{}</style></body>": "<p>a</p><style>i{}</style>[F]</body>",
	}
	for in, want := range cases {
		if got := InsertBeforeBodyEnd(in, "[F]"); got != want {
			t.Errorf("InsertBeforeBodyEnd(%q):\n got %q\nwant %q", in, got, want)
		}
	}
}

func TestToPlainTextKeepsStructureAndDropsCSS(t *testing.T) {
	got := ToPlainText(designed)

	if strings.Contains(got, "font-family") || strings.Contains(got, "@media") || strings.Contains(got, "{") {
		t.Errorf("the stylesheet must not reach the text part:\n%s", got)
	}
	if strings.Contains(got, "Quick question about your pipeline") {
		t.Errorf("a hidden preheader is written to be read once:\n%s", got)
	}
	if !strings.Contains(got, "We help teams like Acme book more meetings.") {
		t.Errorf("inline elements must not run their words together:\n%s", got)
	}
	if !strings.Contains(got, "- Deliverability first\n- Real warmup") {
		t.Errorf("list items should be bulleted lines:\n%s", got)
	}
	if !strings.Contains(got, "Book a demo (https://example.com/demo)") {
		t.Errorf("a link should keep its destination:\n%s", got)
	}
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("no more than one blank line between blocks:\n%s", got)
	}
}

func TestToPlainTextLists(t *testing.T) {
	got := ToPlainText(`<ol><li>one</li><li>two</li></ol><ul><li>a</li></ul>`)
	if !strings.Contains(got, "1. one") || !strings.Contains(got, "2. two") {
		t.Errorf("ordered items should be numbered: %q", got)
	}
	if !strings.Contains(got, "- a") {
		t.Errorf("unordered items should be dashed: %q", got)
	}
	// A <ul> inside an <ol> must not inherit the numbering.
	nested := ToPlainText(`<ol><li>one<ul><li>inner</li></ul></li></ol>`)
	if !strings.Contains(nested, "- inner") {
		t.Errorf("a nested ul keeps its bullets: %q", nested)
	}
}

func TestToPlainTextTableRows(t *testing.T) {
	got := ToPlainText(`<table><tr><td>a</td><td>b</td></tr><tr><td>c</td></tr></table>`)
	if !strings.Contains(got, "a b") {
		t.Errorf("cells are columns of a row: %q", got)
	}
	if !strings.Contains(got, "\nc") {
		t.Errorf("rows are lines: %q", got)
	}
}

func TestToPlainTextLinkThatIsItsOwnLabel(t *testing.T) {
	got := ToPlainText(`<a href="https://x.test/a">https://x.test/a</a>`)
	if got != "https://x.test/a" {
		t.Errorf("a bare URL should not be repeated: %q", got)
	}
}

// A button and a linked image are nothing but their destination once the
// markup is gone, so the text/plain half has to carry it (issue #433).
func TestToPlainTextKeepsAButtonAndALinkedImage(t *testing.T) {
	button := ToPlainText(`<table data-warmbly-button=""><tbody><tr><td>` +
		`<a href="https://cal.test/me" style="display:inline-block">Book a call</a></td></tr></tbody></table>`)
	if button != "Book a call (https://cal.test/me)" {
		t.Errorf("a button is its label and where it goes: %q", button)
	}

	image := ToPlainText(`<a href="https://x.test/demo"><img src="https://x.test/a.png" alt="Watch the demo"></a>`)
	if image != "[Watch the demo] (https://x.test/demo)" {
		t.Errorf("a linked image stands in as its alt text: %q", image)
	}
}

func TestToPlainTextEntitiesAndBreaks(t *testing.T) {
	got := ToPlainText(`<p>Tom &amp; Jerry<br>next line</p>`)
	if got != "Tom & Jerry\nnext line" {
		t.Errorf("entities decode and <br> breaks: %q", got)
	}
}

func TestLintNamesWhatClientsDo(t *testing.T) {
	codes := func(fs []Finding) map[string]bool {
		m := map[string]bool{}
		for _, f := range fs {
			m[f.Code] = true
		}
		return m
	}

	got := codes(Lint(`<link rel="stylesheet" href="https://x.test/a.css">`+
		`<script>alert(1)</script><div style="position:absolute">x</div>`+
		`<img src="https://x.test/a.png">`, 1000))

	for _, want := range []string{"external_stylesheet", "script_stripped", "outlook_unsupported_css", "image_missing_alt"} {
		if !got[want] {
			t.Errorf("expected finding %q, got %v", want, got)
		}
	}

	// background-position is not position; a false alarm here fires on almost
	// every real template.
	quiet := codes(Lint(`<div style="background-position:center">x</div>`, 100))
	if quiet["outlook_unsupported_css"] {
		t.Error("background-position must not read as position")
	}

	big := codes(Lint("<p>x</p>", 200*1024))
	if !big["gmail_clipping"] {
		t.Error("a message past 102 KB is clipped by Gmail")
	}

	media := codes(Lint(`<style>@media (max-width:600px){.x{width:100%}}</style>`, 100))
	if !media["media_query_needs_important"] {
		t.Error("a media query with no !important cannot override an inline style")
	}
}

func TestLintIsQuietOnAnOrdinaryBody(t *testing.T) {
	if f := Lint(`<p>Hi Ana, do you have ten minutes on Thursday?</p>`, 400); len(f) != 0 {
		t.Errorf("a plain cold email should raise nothing, got %v", f)
	}
}

// Lowercasing a copy of the body and indexing into the original is off by
// however much the case fold changed the length. Turkish "İ" grows a byte and
// the Kelvin sign shrinks two, so a body that merely says "İstanbul" put the
// signature at the wrong offset, inside a character or past the end.
func TestInsertBeforeBodyEndWithCaseFoldingCharacters(t *testing.T) {
	for _, body := range []string{
		"<html><body><p>İstanbul</p></body></html>",
		"<html><body><p>İİİİİİ</p></body></html>",
		"<html><body><p>2 K of it</p></body></html>",
		"<html><body><p>ẞ</p></body></html>",
	} {
		out := InsertBeforeBodyEnd(body, "[F]")
		if !strings.HasSuffix(out, "[F]</body></html>") {
			t.Errorf("insert landed at the wrong offset for %q:\n%s", body, out)
		}
		if !utf8.ValidString(out) {
			t.Errorf("insert split a character in %q:\n%q", body, out)
		}
	}
}

// A body is valid UTF-8 coming out of Postgres, but nothing downstream should
// be able to crash a send if that ever stops being true.
func TestInsertBeforeBodyEndSurvivesInvalidUTF8(t *testing.T) {
	out := InsertBeforeBodyEnd("\xa4\xa4\xa4\xa4</BodY>", "[F]")
	if !strings.Contains(out, "[F]") {
		t.Errorf("fragment was dropped: %q", out)
	}
}

// CSS the parser cannot read is passed through, never rewritten from the
// fraction of it that was understood.
func TestInlineCSSKeepsAStylesheetItCannotParse(t *testing.T) {
	body := `<style>@weird-at-rule-we-do-not-know</style><p>hi</p>`
	out := InlineCSS(body)
	if !strings.Contains(out, "@weird-at-rule-we-do-not-know") {
		t.Errorf("unparsed CSS was deleted:\n%s", out)
	}
	if !strings.Contains(out, "hi") {
		t.Errorf("the body was lost:\n%s", out)
	}
}

// A selector list is only partly ours: the half that inlines must not take the
// declaration the other half still needs with it.
func TestInlineCSSKeepsTheHalfOfAListItCannotInline(t *testing.T) {
	out := InlineCSS(`<style>.btn, .other:hover { color: red }</style><p class="btn">x</p>`)
	if !strings.Contains(out, ".other:hover") {
		t.Errorf("the stateful half of the list was dropped:\n%s", out)
	}
	if !strings.Contains(out, `style="color: red"`) {
		t.Errorf("the half that could inline did not:\n%s", out)
	}
}

// Commas inside an attribute value or :is() belong to the selector. Splitting
// on them and rejoining would write a selector that matches something else, so
// a rule with a piece we cannot read is left exactly as the author wrote it.
func TestInlineCSSDoesNotRewriteASelectorItCannotSplit(t *testing.T) {
	out := InlineCSS(`<style>.btn, [title="a,b"] { color: red }</style><p class="btn">x</p>`)
	if !strings.Contains(out, `[title="a,b"]`) {
		t.Errorf("the selector was rewritten or dropped:\n%s", out)
	}
}

// A rule matching nothing today is still the author's; it stays in the sheet.
func TestInlineCSSKeepsARuleThatMatchesNothing(t *testing.T) {
	out := InlineCSS(`<style>.absent { color: red }</style><p>x</p>`)
	if !strings.Contains(out, ".absent") {
		t.Errorf("a rule matching nothing was deleted:\n%s", out)
	}
}

// The sheet is rewritten from parsed items as soon as one rule inlines, so
// anything the parser could not read has to survive as an item of its own or
// it is deleted from the author's stylesheet by an unrelated rule matching.
func TestInlineCSSKeepsUnreadableTextBesideARuleThatInlines(t *testing.T) {
	out := InlineCSS("<style>.a{color:red}\n@weird-at-rule-we-do-not-know</style><p class=\"a\">hi</p>")
	if !strings.Contains(out, "@weird-at-rule-we-do-not-know") {
		t.Errorf("unreadable text was deleted once another rule inlined:\n%s", out)
	}
	if !strings.Contains(out, `style="color: red"`) {
		t.Errorf("the readable rule did not inline:\n%s", out)
	}
}

// The editor promises inlining from this finding, so it must not claim it for
// a sheet InlineCSS will leave exactly as written.
func TestLintPromisesInliningOnlyForSheetsThatGetIt(t *testing.T) {
	has := func(html string) bool {
		for _, f := range Lint(html, 500) {
			if f.Code == "stylesheet_inlined" {
				return true
			}
		}
		return false
	}
	if !has(`<style>.a{color:red}</style><p class="a">x</p>`) {
		t.Error("an ordinary stylesheet is inlined and should say so")
	}
	if has(`<style media="print">.a{color:red}</style><p class="a">x</p>`) {
		t.Error("a print stylesheet is never inlined")
	}
	if has(`<style data-warmbly-inline="false">.a{color:red}</style><p class="a">x</p>`) {
		t.Error("an opted-out stylesheet is never inlined")
	}
}
