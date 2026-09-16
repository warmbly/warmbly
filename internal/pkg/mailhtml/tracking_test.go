package mailhtml

import (
	"strings"
	"testing"
)

func TestSanitizeRemovesWarmblyOpenPixels(t *testing.T) {
	const id = "550e8400-e29b-41d4-a716-446655440000"
	for _, markup := range []string{
		`<img src="https://tracking.customer.test/t/o/` + id + `.png" width="1" height="1" style="display:none;" alt="" />`,
		`<blockquote><p>Quoted reply</p><img src="https://old-domain.test/t/o/` + id + `.png"></blockquote>`,
		`<IMG SRC='http://localhost:3000/t/o/` + id + `'>`,
		`<img src="//custom.test/t/o/` + id + `.png?x=1&amp;y=2">`,
		`<img src="https://custom.test/t/o/` + id + `&#46;png">`,
		`<img src="https://custom.test/t/o/%35` + id[1:] + `.png">`,
	} {
		t.Run(markup, func(t *testing.T) {
			out := Sanitize(`<p>Hello</p>` + markup)
			if strings.Contains(out, "<img") {
				t.Fatalf("displaying the message would request its open pixel: %s", out)
			}
			if !strings.Contains(out, "<p>Hello</p>") {
				t.Fatalf("message content lost: %s", out)
			}
		})
	}
}

func TestSanitizeKeepsOrdinaryImagesAndLinks(t *testing.T) {
	const image = "https://cdn.customer.test/logo.png"
	const link = "https://custom.test/c/550e8400-e29b-41d4-a716-446655440000"
	out := Sanitize(`<p>café &amp; more</p><img src="` + image + `" width="1" height="1">` +
		`<img src="data:image/png;base64,iVBORw0KGgo=">` +
		`<img src="https://cdn.customer.test/t/o/logo.png"><a href="` + link + `">Visit</a>`)
	for _, want := range []string{image, link, "data:image/png;base64,iVBORw0KGgo=", "/t/o/logo.png", "café &amp; more"} {
		if !strings.Contains(out, want) {
			t.Errorf("display content %q lost: %s", want, out)
		}
	}
	if again := Sanitize(out); again != out {
		t.Errorf("sanitizing twice changed the display: %s", again)
	}
}
