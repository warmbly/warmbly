package tasks

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/mailhtml"
)

func TestOpenTrackingIsRemovedOnlyFromDisplayCopy(t *testing.T) {
	const original = `<html><body><p>Hello</p><img src="https://example.com/logo.png"></body></html>`
	for _, host := range []string{"t.warmbly.com", "custom.example.com", "localhost:3000"} {
		t.Run(host, func(t *testing.T) {
			delivered := AddOpenTrackingPixel(original, uuid.New(), host)
			displayed := mailhtml.Sanitize(delivered)
			if strings.Contains(displayed, "/t/o/") {
				t.Fatalf("reading the sent copy would track an open: %s", displayed)
			}
			if !strings.Contains(delivered, "/t/o/") {
				t.Fatalf("recipient copy lost tracking: %s", delivered)
			}
			if !strings.Contains(displayed, "https://example.com/logo.png") {
				t.Fatalf("ordinary image lost: %s", displayed)
			}
		})
	}
}

func TestAddOpenTrackingPixelUsesTheConfiguredHost(t *testing.T) {
	html := "<html><body>hi</body></html>"
	out := AddOpenTrackingPixel(html, uuid.New(), "t.acme.com")
	if !strings.Contains(out, `src="https://t.acme.com/t/o/`) {
		t.Fatalf("pixel did not use the host: %s", out)
	}
}

// A local tracking service does not serve TLS, so an https pixel there is one
// no mail client can load.
func TestAddOpenTrackingPixelUsesHTTPForLocalHosts(t *testing.T) {
	out := AddOpenTrackingPixel("<html><body>hi</body></html>", uuid.New(), "localhost:3000")
	if !strings.Contains(out, `src="http://localhost:3000/t/o/`) {
		t.Fatalf("expected an http pixel, got: %s", out)
	}
}

// Without a tracking host there is nowhere to record an open. The pixel used to
// fall back to a hardcoded warmbly.com name, which on any other install meant
// mailing recipients a beacon to a third party that recorded nothing locally.
func TestAddOpenTrackingPixelSkippedWithoutAHost(t *testing.T) {
	html := "<html><body>hi</body></html>"
	if out := AddOpenTrackingPixel(html, uuid.New(), ""); out != html {
		t.Fatalf("expected the body untouched, got: %s", out)
	}
}

func TestWrapLinksForTrackingMintsTickets(t *testing.T) {
	html := `<a href="https://example.com/pricing">pricing</a>`
	out, links := WrapLinksForTracking(html, uuid.New(), uuid.New(), "t.acme.com")
	if len(links) != 1 {
		t.Fatalf("expected one ticket, got %d", len(links))
	}
	if links[0].Destination != "https://example.com/pricing" {
		t.Fatalf("destination not preserved: %q", links[0].Destination)
	}
	if !strings.Contains(out, "https://t.acme.com/c/"+links[0].ID.String()) {
		t.Fatalf("link not rewritten to the ticket: %s", out)
	}
}

// The worst outcome of a missing tracking host: every link in a real campaign
// email rewritten to a host that does not exist. Ship the originals instead.
func TestWrapLinksForTrackingLeavesLinksAloneWithoutAHost(t *testing.T) {
	html := `<a href="https://example.com/pricing">pricing</a>`
	out, links := WrapLinksForTracking(html, uuid.New(), uuid.New(), "")
	if out != html || links != nil {
		t.Fatalf("expected the original links, got %s / %v", out, links)
	}
}

func TestWrapLinksForTrackingSkipsItsOwnHostAndNonHTTP(t *testing.T) {
	html := `<a href="https://t.acme.com/c/abc">x</a><a href="mailto:a@b.com">m</a><a href="#top">t</a>`
	out, links := WrapLinksForTracking(html, uuid.New(), uuid.New(), "t.acme.com")
	if len(links) != 0 {
		t.Fatalf("expected no tickets, got %d", len(links))
	}
	if out != html {
		t.Fatalf("body should be unchanged: %s", out)
	}
}

func TestTrackLinksReadsTheAnchorTextAsTheLabel(t *testing.T) {
	html := `<p>See <a href="https://example.com/pricing"><b>our</b> Pricing &amp; plans</a> or <a href="https://example.com/x"><img src="cid:logo" alt="Logo"></a></p>`
	_, links := TrackLinks(html, LinkTracking{TaskID: uuid.New(), CampaignID: uuid.New(), TrackingDomain: "t.acme.com", Wrap: true})
	if len(links) != 2 {
		t.Fatalf("expected two tickets, got %d", len(links))
	}
	if links[0].Label != "our Pricing & plans" {
		t.Fatalf("label not read from the anchor text: %q", links[0].Label)
	}
	if links[1].Label != "Logo" {
		t.Fatalf("image link should fall back to alt text: %q", links[1].Label)
	}
}

func TestTrackLinksTagsUTMAndStoresTheTaggedDestination(t *testing.T) {
	html := `<a href="https://example.com/pricing?ref=1#top">Pricing page</a>`
	utm := &UTMParams{Source: "warmbly", Medium: "email", Campaign: "q3_outbound"}
	out, links := TrackLinks(html, LinkTracking{TaskID: uuid.New(), CampaignID: uuid.New(), TrackingDomain: "t.acme.com", Wrap: true, UTM: utm})
	if len(links) != 1 {
		t.Fatalf("expected one ticket, got %d", len(links))
	}
	want := "https://example.com/pricing?ref=1&utm_source=warmbly&utm_medium=email&utm_campaign=q3_outbound&utm_content=pricing_page#top"
	if links[0].Destination != want {
		t.Fatalf("destination = %q, want %q", links[0].Destination, want)
	}
	if strings.Contains(out, "utm_") {
		t.Fatalf("the email must carry only the ticket, got: %s", out)
	}
}

func TestTrackLinksTagsUTMWithoutWrappingWhenLinkTrackingIsOff(t *testing.T) {
	html := `<a href='https://example.com/a?x=1&amp;y=2'>Go</a>`
	out, links := TrackLinks(html, LinkTracking{UTM: &UTMParams{Source: "s", Medium: "m", Campaign: "c"}})
	if links != nil {
		t.Fatalf("no tickets expected, got %v", links)
	}
	want := `<a href="https://example.com/a?x=1&amp;y=2&amp;utm_source=s&amp;utm_medium=m&amp;utm_campaign=c&amp;utm_content=go">Go</a>`
	if out != want {
		t.Fatalf("got %s\nwant %s", out, want)
	}
}

func TestTrackLinksKeepsHandWrittenUTMValues(t *testing.T) {
	html := `<a href="https://example.com/?utm_source=newsletter&utm_content=hero">Hero</a>`
	_, links := TrackLinks(html, LinkTracking{TaskID: uuid.New(), CampaignID: uuid.New(), TrackingDomain: "t.acme.com", Wrap: true, UTM: &UTMParams{Source: "warmbly", Medium: "email", Campaign: "c"}})
	got := links[0].Destination
	if strings.Count(got, "utm_source=") != 1 || !strings.Contains(got, "utm_source=newsletter") || !strings.Contains(got, "utm_content=hero") {
		t.Fatalf("hand-written values must win: %s", got)
	}
	if !strings.Contains(got, "utm_medium=email") || !strings.Contains(got, "utm_campaign=c") {
		t.Fatalf("missing values must still be added: %s", got)
	}
}

func TestTrackLinksNumbersLinksWithoutText(t *testing.T) {
	html := `<a href="https://example.com/1"><img src="x"></a><a href="https://example.com/2"></a>`
	_, links := TrackLinks(html, LinkTracking{TaskID: uuid.New(), CampaignID: uuid.New(), TrackingDomain: "t.acme.com", Wrap: true, UTM: &UTMParams{Source: "s", Medium: "m", Campaign: "c"}})
	if !strings.HasSuffix(links[0].Destination, "utm_content=link_1") || !strings.HasSuffix(links[1].Destination, "utm_content=link_2") {
		t.Fatalf("unlabelled links should be numbered: %s / %s", links[0].Destination, links[1].Destination)
	}
}

// A stylesheet or preload href in the head is not a link anyone clicks;
// redirecting it through a ticket would break it.
func TestTrackLinksOnlyTouchesAnchors(t *testing.T) {
	html := `<link rel="stylesheet" href="https://example.com/a.css"><a href="https://example.com/p">p</a>`
	out, links := WrapLinksForTracking(html, uuid.New(), uuid.New(), "t.acme.com")
	if len(links) != 1 || links[0].Destination != "https://example.com/p" {
		t.Fatalf("expected only the anchor wrapped: %v", links)
	}
	if !strings.Contains(out, `href="https://example.com/a.css"`) {
		t.Fatalf("stylesheet href must be untouched: %s", out)
	}
}

func TestCampaignUTMDefaults(t *testing.T) {
	c := &models.Campaign{Name: "Q3 Outbound: Fintech!", UTMTracking: true}
	p := CampaignUTM(c)
	if p == nil || p.Source != "warmbly" || p.Medium != "email" || p.Campaign != "q3_outbound_fintech" {
		t.Fatalf("unexpected defaults: %+v", p)
	}
	c.UTMSource, c.UTMCampaign = " acme ", "launch"
	p = CampaignUTM(c)
	if p.Source != "acme" || p.Campaign != "launch" {
		t.Fatalf("overrides not honoured: %+v", p)
	}
	if CampaignUTM(&models.Campaign{Name: "x"}) != nil {
		t.Fatal("utm tagging off must yield nil")
	}
}

func TestTrackLinksReadsBareHrefAndIgnoresDataHref(t *testing.T) {
	html := `<a data-href="https://tracker.example/x" href=https://example.com/bare target="_blank">Bare</a>`
	out, links := WrapLinksForTracking(html, uuid.New(), uuid.New(), "t.acme.com")
	if len(links) != 1 || links[0].Destination != "https://example.com/bare" || links[0].Label != "Bare" {
		t.Fatalf("expected the bare href wrapped: %+v", links)
	}
	if !strings.Contains(out, `data-href="https://tracker.example/x"`) || !strings.Contains(out, `target="_blank"`) {
		t.Fatalf("other attributes must survive: %s", out)
	}
}

func TestTrackableURLComparesTheHost(t *testing.T) {
	if trackableURL("https://t.acme.com/c/abc", "t.acme.com") {
		t.Fatal("a link already on the tracking host must be left alone")
	}
	if !trackableURL("https://example.com/?next=t.acme.com", "t.acme.com") {
		t.Fatal("a URL merely mentioning the host is a normal link")
	}
	if !trackableURL("https://not-t.acme.com/", "t.acme.com") {
		t.Fatal("a different host that ends with the tracking host is a normal link")
	}
}

func TestTagPlainTextLinks(t *testing.T) {
	body := "See https://example.com/pricing. Docs: https://example.com/docs?x=1\nSkip https://t.acme.com/c/abc"
	out := TagPlainTextLinks(body, &UTMParams{Source: "s", Medium: "m", Campaign: "c"}, "t.acme.com")
	want := "See https://example.com/pricing?utm_source=s&utm_medium=m&utm_campaign=c&utm_content=link_1. Docs: https://example.com/docs?x=1&utm_source=s&utm_medium=m&utm_campaign=c&utm_content=link_2\nSkip https://t.acme.com/c/abc"
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}
