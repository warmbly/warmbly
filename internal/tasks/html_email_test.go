package tasks

// A designed HTML email is the shape issue #393 reports: a document with its
// own <head>, a class-based stylesheet, a table layout and an Outlook
// conditional comment. Every part of the send path that touches a body has to
// survive it.

import (
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/pkg/mailhtml"
)

// A designed HTML email through the parts of the send path that touch the
// body: plain-text derivation, signature, footer, pixel and CSS inlining.
func TestDesignedEmailThroughTheSendPath(t *testing.T) {
	body := `<!DOCTYPE html><html><head><style>
.wrap{width:600px;margin:0 auto}
.btn{background:#0284c7;color:#fff;padding:12px}
@media (max-width:600px){.wrap{width:100%!important}}
</style></head><body>
<table class="wrap"><tr><td><p>Hi Ana</p>
<p><a class="btn" href="https://x.test/demo">Book a demo</a></p></td></tr></table>
<!--[if mso]></body><![endif]-->
</body></html>`

	plain := ExtractPlainTextFromHTML(body)
	if strings.Contains(plain, "{") || strings.Contains(plain, "margin") {
		t.Errorf("the stylesheet reached the text part:\n%s", plain)
	}
	if !strings.Contains(plain, "Hi Ana") || !strings.Contains(plain, "https://x.test/demo") {
		t.Errorf("the text part lost the message:\n%s", plain)
	}

	withSig := AddSignature(body, `<p>Ana Perez</p>`, true)
	// Inside the laid-out container, not after it: appended at the end of the
	// document it would render flush against the left edge of the window, in
	// the page background, styled by nothing (issue #462).
	if !strings.Contains(withSig, "Ana Perez</p></div></td></tr></table>") {
		t.Errorf("signature did not land inside the email container:\n%s", withSig)
	}
	// The conditional comment carries its own </body>. Reading that one as the
	// document's put the signature inside a comment, where Outlook showed it
	// and every other client did not (issue #393).
	if !strings.Contains(withSig, "<!--[if mso]></body><![endif]-->") {
		t.Errorf("signature landed inside the conditional comment:\n%s", withSig)
	}

	final := mailhtml.InlineCSS(withSig)
	if !strings.Contains(final, `width: 600px`) {
		t.Errorf("the class rule was not inlined onto the table:\n%s", final)
	}
	if !strings.Contains(final, "@media") {
		t.Errorf("the media query was dropped:\n%s", final)
	}
	if !strings.Contains(final, "<!--[if mso]></body><![endif]-->") {
		t.Errorf("the Outlook conditional comment was lost:\n%s", final)
	}
}
