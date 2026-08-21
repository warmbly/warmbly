package mailhtml

import "strings"

import "testing"

func TestSanitizeDropsScriptAndStyleContent(t *testing.T) {
	in := `<html><head><style>.a{color:red}</style></head><body>
	<script>alert(1)</script><p onclick="steal()">Hello <b>world</b></p></body></html>`
	out := Sanitize(in)
	for _, banned := range []string{"alert(1)", "color:red", "onclick", "<script", "<style"} {
		if strings.Contains(out, banned) {
			t.Fatalf("sanitized output leaked %q: %s", banned, out)
		}
	}
	if !strings.Contains(out, "<b>world</b>") {
		t.Fatalf("formatting dropped: %s", out)
	}
}

func TestSanitizeKeepsLayoutMarkup(t *testing.T) {
	in := `<table width="600" cellpadding="0" bgcolor="#ffffff"><tr><td align="center" style="font-size:14px;color:#111">` +
		`<a href="https://example.com">Link</a><img src="https://example.com/a.png" width="20"></td></tr></table>`
	out := Sanitize(in)
	for _, want := range []string{`width="600"`, `bgcolor="#ffffff"`, `align="center"`, "font-size", `href="https://example.com"`, `target="_blank"`, "<img"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output: %s", want, out)
		}
	}
}

func TestSanitizeDropsUnsafeURLSchemes(t *testing.T) {
	out := Sanitize(`<a href="javascript:alert(1)">x</a><a href="data:text/html,<b>x</b>">y</a>`)
	if strings.Contains(out, "javascript:") || strings.Contains(out, "data:text/html") {
		t.Fatalf("unsafe scheme survived: %s", out)
	}
}

func TestToTextDecodesEntitiesAndKeepsLineStructure(t *testing.T) {
	in := `<style>.x{color:red}</style><p>Hello,</p><p>Terms &amp; conditions &#169; caf&eacute;</p>`
	out := ToText(in)
	if strings.Contains(out, "color:red") {
		t.Fatalf("stylesheet text leaked: %q", out)
	}
	if !strings.Contains(out, "Terms & conditions © café") {
		t.Fatalf("entities not decoded: %q", out)
	}
	if !strings.Contains(out, "\n") {
		t.Fatalf("block structure lost: %q", out)
	}
}

func TestSanitizeAllowsInlineAndRemoteImages(t *testing.T) {
	out := Sanitize(`<img src="data:image/png;base64,iVBORw0KGgo=" alt="inline">` +
		`<img src="https://cdn.example.com/logo.png" alt="remote">` +
		`<img src="cid:part1@mail" alt="attachment">`)
	if !strings.Contains(out, "data:image/png;base64") {
		t.Fatalf("inline image dropped: %s", out)
	}
	if !strings.Contains(out, "https://cdn.example.com/logo.png") {
		t.Fatalf("remote image dropped: %s", out)
	}
	if strings.Contains(out, "cid:") {
		t.Fatalf("cid image should not survive: %s", out)
	}
}

func TestSanitizeLeavesUnicodeIntact(t *testing.T) {
	out := Sanitize(`<p>café ™ 🎉 &amp; more</p>`)
	if !strings.Contains(out, "café ™ 🎉") {
		t.Fatalf("unicode mangled: %s", out)
	}
	// "&" must stay escaped exactly once so the browser renders one ampersand.
	if !strings.Contains(out, "&amp; more") || strings.Contains(out, "&amp;amp;") {
		t.Fatalf("entity handling wrong: %s", out)
	}
}

func TestLooksLikeHTML(t *testing.T) {
	html := []string{
		"<p>hi</p>", "<!DOCTYPE html><html><body>x</body></html>",
		"line<br>break", `<div class="x">y</div>`, "<TABLE><TR><TD>a</TD></TR></TABLE>",
	}
	for _, in := range html {
		if !LooksLikeHTML(in) {
			t.Fatalf("expected HTML: %q", in)
		}
	}
	plain := []string{
		"Hello,\n\nSend it to <ana@example.com> when ready.",
		"a < b and c > d",
		"> quoted line\nreply text",
		"",
	}
	for _, in := range plain {
		if LooksLikeHTML(in) {
			t.Fatalf("expected plain text: %q", in)
		}
	}
}

func TestSearchTextKeepsQuotedHistoryAndFlattensHTML(t *testing.T) {
	got := SearchText("", `<p>Happy to help.</p><blockquote>&gt; what is the pricing?</blockquote>`, 0)
	if !strings.Contains(got, "Happy to help.") || !strings.Contains(got, "what is the pricing?") {
		t.Fatalf("search text lost content: %q", got)
	}
	if strings.Contains(got, "<") || strings.Contains(got, "&gt;") {
		t.Fatalf("markup or entities survived: %q", got)
	}
}

func TestSearchTextPrefersPlainAndRespectsLimit(t *testing.T) {
	if got := SearchText("plain wins", "<p>html loses</p>", 0); got != "plain wins" {
		t.Fatalf("got %q", got)
	}
	got := SearchText(strings.Repeat("é", 100), "", 10)
	if len([]rune(got)) != 10 {
		t.Fatalf("limit not applied on rune boundaries: %q", got)
	}
}
