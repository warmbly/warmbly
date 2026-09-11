package tasks

import (
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

// finishBody is what the preview and the test send share with the campaign
// send: the parts land in send order (body, signature, opt-out) and a
// plain-text campaign loses its HTML part before the signature is added.
func TestFinishBodyMatchesSendOrder(t *testing.T) {
	account := &models.Email{SignatureSync: true, SignatureHTML: "<p>Ana</p>", SignaturePlain: "Ana"}
	optOut := &models.UnsubscribeSettings{Mode: models.UnsubscribeModeText, Text: "Reply stop to opt out."}

	h, p := finishBody("<p>Hi</p>", "", false, account, optOut, "")
	sig, foot := strings.Index(h, "<p>Ana</p>"), strings.Index(h, "Reply stop to opt out.")
	if sig < 0 || foot < 0 || sig > foot {
		t.Fatalf("html parts out of send order: %q", h)
	}
	if !strings.HasPrefix(p, "Hi") || strings.Index(p, "Ana") > strings.Index(p, "Reply stop") {
		t.Fatalf("plain part not derived from html or out of order: %q", p)
	}

	h, p = finishBody("<p>Hi</p>", "", true, account, optOut, "")
	if h != "" {
		t.Fatalf("plain-text campaign kept an html part: %q", h)
	}
	if !strings.Contains(p, "Ana") || !strings.Contains(p, "Reply stop") {
		t.Fatalf("plain-text campaign lost signature or footer: %q", p)
	}

	// No mailbox and no campaign: templates only, nothing appended.
	h, p = finishBody("<p>Hi</p>", "Hi", false, nil, nil, "")
	if h != "<p>Hi</p>" || p != "Hi" {
		t.Fatalf("bare preview was decorated: %q / %q", h, p)
	}

	// Signature sync off leaves the body alone even with a signature stored.
	off := &models.Email{SignatureSync: false, SignatureHTML: "<p>Ana</p>"}
	if h, _ = finishBody("<p>Hi</p>", "Hi", false, off, nil, ""); h != "<p>Hi</p>" {
		t.Fatalf("signature added while sync is off: %q", h)
	}
}

func TestPreviewTemplatesWithUsesTheGivenLink(t *testing.T) {
	p := previewTemplatesWith("s", "<a href=\"{{.UnsubscribeLink}}\">x</a>", "", models.Contact{}, "https://api.example.com/unsubscribe/tok")
	if !strings.Contains(p.BodyHTML, "https://api.example.com/unsubscribe/tok") {
		t.Fatalf("link variable did not resolve to the given link: %q", p.BodyHTML)
	}
	if q := PreviewTemplates("s", "{{.UnsubscribeLink}}", "", models.Contact{}); q.BodyHTML != PreviewUnsubscribeLink {
		t.Fatalf("default preview link changed: %q", q.BodyHTML)
	}
}

// A step created through the API or an agent tool carries the composer's empty
// <div></div> under body_html. It is not an empty string, so it shipped as the
// text/html alternative and the recipient opened a blank message with the real
// copy only in the fallback part.
func TestFinishBodyDropsATextlessHTMLPart(t *testing.T) {
	h, p := finishBody("<div></div>", "Hi Ana, quick question.", false, nil, nil, "")
	if h != "" {
		t.Errorf("HTML part = %q, want it dropped so the plain part is what ships", h)
	}
	if p != "Hi Ana, quick question." {
		t.Errorf("plain part = %q, want the step's own text", p)
	}
}

// The guard must not touch a body that renders something, including one whose
// only content is an image.
func TestFinishBodyKeepsABodyThatRenders(t *testing.T) {
	if h, _ := finishBody("<div>Hi</div>", "Hi", false, nil, nil, ""); h != "<div>Hi</div>" {
		t.Errorf("HTML part = %q, want it kept", h)
	}
	img := `<div><img src="https://example.com/a.png"></div>`
	if h, _ := finishBody(img, "see the image", false, nil, nil, ""); h == "" {
		t.Error("an image-only body was dropped; it renders for the recipient")
	}
}

// With no plain part either there is nothing to fall back to, so dropping the
// HTML would send an empty message. The start-time guard is what stops that
// campaign existing; here the body is left alone.
func TestFinishBodyKeepsAnEmptyHTMLPartWhenThereIsNoPlainText(t *testing.T) {
	if h, _ := finishBody("<div></div>", "", false, nil, nil, ""); h == "" {
		t.Error("dropped the only part the message had")
	}
}
