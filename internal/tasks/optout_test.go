package tasks

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func TestUnsubscribeLinkIsNeverTracked(t *testing.T) {
	body := `<p>Hi <a href="https://acme.com/pricing">pricing</a> and <a href="https://api.example.com/unsubscribe/abc123">unsubscribe</a></p>`
	out, links := WrapLinksForTracking(body, uuid.New(), uuid.New(), "t.example.com")
	if len(links) != 1 || links[0].Destination != "https://acme.com/pricing" {
		t.Fatalf("expected only the pricing link to be ticketed, got %+v", links)
	}
	if !strings.Contains(out, `href="https://api.example.com/unsubscribe/abc123"`) {
		t.Fatalf("unsubscribe link was rewritten: %s", out)
	}
}

func TestOptOutFooter(t *testing.T) {
	text := models.UnsubscribeSettings{Mode: models.UnsubscribeModeText, Text: "Reply and I'll stop."}
	h, p := appendOptOut("<p>Hi</p>", "Hi", text, "")
	if !strings.Contains(h, "Reply and I&#39;ll stop.") || !strings.HasSuffix(p, "Reply and I'll stop.") {
		t.Fatalf("text footer missing: %q / %q", h, p)
	}

	link := models.UnsubscribeSettings{Mode: models.UnsubscribeModeLink, Text: "fallback", LinkIntro: "Not interested?", LinkText: "Unsubscribe"}
	h, p = appendOptOut("<p>Hi</p>", "Hi", link, "https://api.example.com/unsubscribe/tok")
	if !strings.Contains(h, `href="https://api.example.com/unsubscribe/tok"`) || !strings.Contains(p, "Unsubscribe: https://api.example.com/unsubscribe/tok") {
		t.Fatalf("link footer missing: %q / %q", h, p)
	}
	// No link to mint: link mode degrades to the text line, never to nothing.
	h, _ = appendOptOut("<p>Hi</p>", "Hi", link, "")
	if !strings.Contains(h, "fallback") {
		t.Fatalf("link mode without a link should fall back to text: %q", h)
	}
	h, p = appendOptOut("<p>Hi</p>", "Hi", models.UnsubscribeSettings{Mode: models.UnsubscribeModeOff}, "x")
	if h != "<p>Hi</p>" || p != "Hi" {
		t.Fatalf("off mode changed the body: %q / %q", h, p)
	}
}

func TestLinkifyUnsubscribeURL(t *testing.T) {
	const url = "https://api.example.com/unsubscribe/tok123"

	// The variable chip the composer saves: a bare URL in a text node.
	got := linkifyUnsubscribeURL(`<p>Not interested? <span data-var>`+url+`</span></p>`, url, "Unsubscribe")
	want := `<p>Not interested? <span data-var><a href="` + url + `">Unsubscribe</a></span></p>`
	if got != want {
		t.Fatalf("bare token not linkified:\n got %s\nwant %s", got, want)
	}

	// The workspace's own link wording is what the anchor says.
	if got := linkifyUnsubscribeURL("<p>"+url+"</p>", url, "Opt out"); !strings.Contains(got, `>Opt out</a>`) {
		t.Fatalf("link text ignored: %s", got)
	}
	if got := linkifyUnsubscribeURL("<p>"+url+"</p>", url, "  "); !strings.Contains(got, ">Unsubscribe</a>") {
		t.Fatalf("blank link text should fall back to the default: %s", got)
	}
	if got := linkifyUnsubscribeURL("<p>"+url+"</p>", url, `Bill & "Ted"`); !strings.Contains(got, `>Bill &amp; &#34;Ted&#34;</a>`) {
		t.Fatalf("link text not escaped: %s", got)
	}

	// An author's own anchor is left exactly as written: no second link, no
	// rewritten href.
	own := `<p><a href="` + url + `" style="color:red">click here</a></p>`
	if got := linkifyUnsubscribeURL(own, url, "Unsubscribe"); got != own {
		t.Fatalf("author's anchor was rewritten: %s", got)
	}

	// The URL as the text of an existing anchor becomes its label rather than
	// a nested link.
	nested := `<a href="` + url + `">` + url + `</a>`
	if got := linkifyUnsubscribeURL(nested, url, "Unsubscribe"); got != `<a href="`+url+`">Unsubscribe</a>` {
		t.Fatalf("URL inside an anchor should become the label: %s", got)
	}

	// Nothing to do cases.
	if got := linkifyUnsubscribeURL("<p>Hi</p>", url, "Unsubscribe"); got != "<p>Hi</p>" {
		t.Fatalf("body without the link changed: %s", got)
	}
	if got := linkifyUnsubscribeURL("<p>"+url+"</p>", "", "Unsubscribe"); got != "<p>"+url+"</p>" {
		t.Fatalf("no link to mint should be a no-op: %s", got)
	}
	if got := linkifyUnsubscribeURL("", url, "Unsubscribe"); got != "" {
		t.Fatalf("empty body changed: %q", got)
	}

	// A ">" inside a quoted attribute closes nothing. Reading one as the end of
	// the tag turned the href that followed it into a dead link.
	quoted := `<a title="x > y" href="` + url + `">read this</a>`
	if got := linkifyUnsubscribeURL(quoted, url, "Unsubscribe"); got != quoted {
		t.Fatalf("a quoted \">\" broke the tag scan: %s", got)
	}
	if got := linkifyUnsubscribeURL(`<p title="a > b">Bye. `+url+`</p>`, url, "Unsubscribe"); got != `<p title="a > b">Bye. <a href="`+url+`">Unsubscribe</a></p>` {
		t.Fatalf("text after a quoted \">\" should still linkify: %s", got)
	}

	// A malformed body (unterminated tag) is copied through, never truncated.
	if got := linkifyUnsubscribeURL("<p>hi<span "+url, url, "Unsubscribe"); got != "<p>hi<span "+url {
		t.Fatalf("unterminated tag mangled: %s", got)
	}

	// Case-insensitive anchor tracking: </A> closes <A>, so what follows is
	// loose text again.
	mixed := `<A HREF="` + url + `">x</A> or ` + url
	if got := linkifyUnsubscribeURL(mixed, url, "Unsubscribe"); got != `<A HREF="`+url+`">x</A> or <a href="`+url+`">Unsubscribe</a>` {
		t.Fatalf("mixed-case anchors mishandled: %s", got)
	}
}

func TestLinkifiedUnsubscribeLinkSurvivesTracking(t *testing.T) {
	const url = "https://api.example.com/unsubscribe/tok123"
	body := linkifyUnsubscribeURL(`<p>Or `+url+` to stop. <a href="https://acme.com/pricing">pricing</a></p>`, url, "Unsubscribe")
	out, links := WrapLinksForTracking(body, uuid.New(), uuid.New(), "t.example.com")
	if len(links) != 1 || links[0].Destination != "https://acme.com/pricing" {
		t.Fatalf("expected only the pricing link to be ticketed, got %+v", links)
	}
	if !strings.Contains(out, `href="`+url+`">Unsubscribe</a>`) {
		t.Fatalf("unsubscribe anchor was rewritten: %s", out)
	}
}

func TestFinishBodyLinkifiesHTMLAndKeepsThePlainURL(t *testing.T) {
	const url = "https://api.example.com/unsubscribe/tok123"
	off := models.UnsubscribeSettings{Mode: models.UnsubscribeModeOff, LinkText: "Unsubscribe"}
	htmlOut, plainOut := finishBody(`<p>Bye. `+url+`</p>`, "", false, nil, &off, url)
	if !strings.Contains(htmlOut, `<a href="`+url+`">Unsubscribe</a>`) {
		t.Fatalf("html part not linkified: %s", htmlOut)
	}
	// Plain text cannot hide a URL, so it keeps the address itself.
	if !strings.Contains(plainOut, url) {
		t.Fatalf("plain part lost the link: %q", plainOut)
	}
	// No settings at all still linkifies, with the default wording.
	htmlOut, _ = finishBody(`<p>`+url+`</p>`, "x", false, nil, nil, url)
	if !strings.Contains(htmlOut, ">Unsubscribe</a>") {
		t.Fatalf("nil settings should still linkify: %s", htmlOut)
	}
}

// Issue #462: an email written in HTML is a card centred inside a full-width
// page table, and both the mailbox signature and the opt-out footer were
// appended after it. They rendered against the left edge of the window, in
// the page background, styled by nothing. Both belong inside the card.
func TestSignatureAndOptOutLandInsideTheEmailContainer(t *testing.T) {
	body := `<html><body style="background:#f1f5f9">` +
		`<table width="100%"><tr><td align="center">` +
		`<table width="600" bgcolor="#ffffff"><tr><td style="padding:32px 24px">` +
		`<h1>Built by Ayonix Design</h1><p>Explore the work.</p>` +
		`</td></tr></table></td></tr></table></body></html>`

	// In send order: the open-tracking pixel goes on first, at the end of the
	// document. It is a child of <body> from then on, and reading it as
	// layout would stop the descent there and put everything after it back
	// outside the card.
	body = AddOpenTrackingPixel(body, uuid.New(), "t.example.com")

	withSig := AddSignature(body, "<p>Karan Barad</p>", true)
	link := models.UnsubscribeSettings{Mode: models.UnsubscribeModeLink, LinkIntro: "Not the right person, or not interested?", LinkText: "Unsubscribe"}
	out, _ := appendOptOut(withSig, "", link, "https://api.example.com/unsubscribe/tok")

	// The card's own cell, from its padding to the </td> that closes it.
	start, end := strings.Index(out, `padding:32px 24px`), strings.Index(out, "</td></tr></table></td>")
	if start < 0 || end < start {
		t.Fatalf("the card's cell is no longer recognisable:\n%s", out)
	}
	cell := out[start:end]
	for _, want := range []string{"Karan Barad", "Unsubscribe"} {
		if !strings.Contains(cell, want) {
			t.Errorf("%q is outside the email container:\n%s", want, out)
		}
	}
	// In that order: the opt-out is the last line a reader sees.
	if strings.Index(cell, "Karan Barad") > strings.Index(cell, "Unsubscribe") {
		t.Errorf("the footer should follow the signature:\n%s", cell)
	}
}
