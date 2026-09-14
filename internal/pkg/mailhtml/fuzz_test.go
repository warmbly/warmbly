package mailhtml

import "testing"

// The inliner and the text renderer run on every send that carries a
// stylesheet, on markup the sender pasted from somewhere else. A panic here
// would take down a worker mid-send, so neither may ever do worse than return
// the body it was given.
func FuzzInlineCSS(f *testing.F) {
	f.Add(`<style>.a{color:red}</style><p class="a">hi</p>`)
	f.Add(`<style>@media(){`)
	f.Add(`<style>a{background:url(data:image/png;base64,AA{B;C)}</style><a>x</a>`)
	f.Add(`<style>*{}</style>`)
	f.Add(`<html><head><style>body{margin:0}</style></head><body><p>x</p></body></html>`)
	f.Add(`<style>.a{content:"}"}</style><p class="a">x</p>`)
	f.Add(`<style>@media screen{.a{color:red}}</style>`)
	f.Add("<style>" + `\` + "</style>")
	// The two inputs that found real bugs. Seeds run on every `go test`, so
	// they guard the fixes even where the fuzzer itself is not run: a sheet
	// the parser cannot read must not be deleted, and a body whose case fold
	// changes its length must not move the insertion point.
	f.Add("<style>@weird-at-rule</style><p>hi</p>")
	f.Add("\xa4\xa4\xa4\xa4</BodY>")
	f.Add("<html><body><p>\u0130stanbul</p></body></html>")
	// Table layout, which is what the append pass has to reason about.
	f.Add(`<table width="600"><tr><td>a</td><td>b</td></tr></table>`)
	f.Add(`<body><table><tr><td><table width="600"><tr><td>x`)
	f.Add(`<div style="margin:0 auto;max-width:600px"><p>x</p></div>`)

	f.Fuzz(func(t *testing.T, body string) {
		out := InlineCSS(body)
		if body != "" && out == "" && HasStyleBlock(body) {
			// An empty result is only legitimate when the input was empty.
			t.Errorf("InlineCSS emptied a non-empty body: %q", body)
		}
		_ = ToPlainText(body)
		_ = Lint(body, len(body))
		_ = InsertBeforeBodyEnd(body, "[F]")
		// The append pass may only ever hand back the caller's own bytes with
		// something inserted at one offset: it runs on markup a sender pasted
		// from somewhere else, and a body it rewrote is a message the author
		// did not write.
		if out := AppendToContent(body, "[F]"); !isSplice(body, out) {
			t.Errorf("AppendToContent rewrote the body:\n in %q\nout %q", body, out)
		}
	})
}
