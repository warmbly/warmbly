package leadfinder

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

func TestParsePageFindsAddressesTheWayTheyArePublished(t *testing.T) {
	src := `<html><head><title>Bright Smiles Dental</title>
		<meta name="description" content="Family dentist in Austin">
		<meta name="viewport" content="width=device-width"></head>
		<body>
		<a href="mailto:Hello@BrightSmiles.com?subject=Hi">Email us</a>
		<p>Billing: billing [at] brightsmiles [dot] com</p>
		<span class="__cf_email__" data-cfemail="` + cfEncode("front@brightsmiles.com", 0x42) + `">[email protected]</span>
		<img src="logo@2x.png">
		<p>Sentry: 9f8e7d6c5b4a39281706f5e4d3c2b1a0@sentry.io</p>
		<a href="tel:+1 (512) 555-0100">Call</a>
		<a href="/contact-us">Contact</a>
		<a href="https://www.facebook.com/brightsmiles">fb</a>
		<script>var x = "nope@script.com";</script>
		</body></html>`
	p := parsePage(src)
	want := map[string]bool{"hello@brightsmiles.com": true, "billing@brightsmiles.com": true, "front@brightsmiles.com": true}
	got := map[string]bool{}
	for _, e := range p.emails {
		got[e] = true
	}
	for e := range want {
		if !got[e] {
			t.Errorf("missing %s in %v", e, p.emails)
		}
	}
	for e := range got {
		if !want[e] {
			t.Errorf("picked up %s, which is not the business's address", e)
		}
	}
	if p.title != "Bright Smiles Dental" || p.description != "Family dentist in Austin" || !p.hasViewport {
		t.Errorf("page meta = %q %q %v", p.title, p.description, p.hasViewport)
	}
	if len(p.phones) != 1 || p.phones[0] != "+15125550100" {
		t.Errorf("phones = %v", p.phones)
	}
	var contact bool
	for _, l := range p.links {
		if l.href == "/contact-us" && l.text == "Contact" {
			contact = true
		}
	}
	if !contact {
		t.Errorf("contact link not kept with its text: %+v", p.links)
	}
}

func cfEncode(email string, key byte) string {
	const hexdigits = "0123456789abcdef"
	out := []byte{hexdigits[key>>4], hexdigits[key&15]}
	for i := 0; i < len(email); i++ {
		b := email[i] ^ key
		out = append(out, hexdigits[b>>4], hexdigits[b&15])
	}
	return string(out)
}

func TestDetectSiteNamesGaps(t *testing.T) {
	p := parsePage(`<html><head><meta name="generator" content="WordPress 6.1"></head><body>© 2018 Joe's Plumbing</body></html>`)
	p.url, _ = url.Parse("http://joesplumbing.com/")
	platform, signals := detectSite(p, time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC))
	if platform != "WordPress" {
		t.Errorf("platform = %q", platform)
	}
	joined := strings.Join(signals, "|")
	for _, want := range []string{"No HTTPS", "Not mobile friendly", "Outdated footer (2018)", "No analytics"} {
		if !strings.Contains(joined, want) {
			t.Errorf("signals %v missing %q", signals, want)
		}
	}
}

func TestRobotsRules(t *testing.T) {
	rules := parseRobots(`
User-agent: *
Disallow: /private
Allow: /private/contact

User-agent: WarmblyBot
Disallow: /team$
`, "warmblybot")
	cases := map[string]bool{"/": true, "/team": false, "/team/jane": true, "/private": true}
	for path, want := range cases {
		u, _ := url.Parse("https://example.org" + path)
		if got := rules.allowed(u); got != want {
			t.Errorf("own group: %s allowed = %v, want %v", path, got, want)
		}
	}
	star := parseRobots("User-agent: *\nDisallow: /private\nAllow: /private/contact\n", "warmblybot")
	for path, want := range map[string]bool{"/private/x": false, "/private/contact": true, "/about": true} {
		u, _ := url.Parse("https://example.org" + path)
		if got := star.allowed(u); got != want {
			t.Errorf("star group: %s allowed = %v, want %v", path, got, want)
		}
	}
}

func TestOverpassQueryEscapesTypedText(t *testing.T) {
	q, err := overpassQuery(Query{
		Text:     `Joe's "best" (pizza).*`,
		Category: CategoryByID("restaurant"),
		Area:     models.LeadArea{Kind: models.LeadAreaBBox, Bounds: models.LeadBounds{South: 30.2, West: -97.8, North: 30.3, East: -97.7}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q, `["name"~"Joe's \"best\" \\(pizza\\)\\.\\*",i]`) {
		t.Errorf("text not escaped for both regex and string:\n%s", q)
	}
	if !strings.Contains(q, `nwr["amenity"="restaurant"]["name"]`) || !strings.Contains(q, "[bbox:30.200000,-97.800000,30.300000,-97.700000]") {
		t.Errorf("query = %s", q)
	}
	if _, err := overpassQuery(Query{Area: models.LeadArea{Bounds: models.LeadBounds{South: 1, West: 1, North: 2, East: 2}}}); err == nil {
		t.Error("an empty search must be refused, not sent as every named place")
	}
}

func TestAreaShapes(t *testing.T) {
	circle, err := NormalizeArea(models.LeadArea{Kind: models.LeadAreaCircle, Center: &models.LatLng{Lat: 47.5, Lng: 19.05}, RadiusM: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if !Contains(circle, models.LatLng{Lat: 47.5045, Lng: 19.05}) || Contains(circle, models.LatLng{Lat: 47.51, Lng: 19.05}) {
		t.Error("circle containment is off")
	}
	if circle.Bounds.North <= 47.5 || circle.Bounds.South >= 47.5 {
		t.Errorf("circle bounds = %+v", circle.Bounds)
	}
	poly, err := NormalizeArea(models.LeadArea{Kind: models.LeadAreaPolygon, Polygon: []models.LatLng{{Lat: 0, Lng: 0}, {Lat: 0, Lng: 2}, {Lat: 2, Lng: 2}, {Lat: 2, Lng: 0}}})
	if err != nil {
		t.Fatal(err)
	}
	if !Contains(poly, models.LatLng{Lat: 1, Lng: 1}) || Contains(poly, models.LatLng{Lat: 3, Lng: 1}) {
		t.Error("polygon containment is off")
	}
	if _, err := NormalizeArea(models.LeadArea{Kind: models.LeadAreaCircle, Center: &models.LatLng{Lat: 1, Lng: 1}, RadiusM: 900000}); err == nil {
		t.Error("an oversized circle must be refused")
	}
}

func TestEmailRankPrefersTheBusinessFrontDesk(t *testing.T) {
	es := []models.LeadEmail{
		{Email: "jane@gmail.com", Status: "valid"},
		{Email: "noreply@shop.com", Status: "valid", DomainMatch: true},
		{Email: "info@shop.com", Status: "valid", DomainMatch: true},
		{Email: "owner@shop.com", Status: "invalid", DomainMatch: true},
		{Email: "sales@shop.com", Status: "suppressed", DomainMatch: true},
	}
	best := es[0]
	for _, e := range es[1:] {
		if emailRank(e) < emailRank(best) {
			best = e
		}
	}
	if best.Email != "info@shop.com" {
		t.Errorf("best = %s", best.Email)
	}
}

func TestNormalizeWebsite(t *testing.T) {
	cases := map[string][2]string{
		"brightsmiles.com":                {"https://brightsmiles.com", "brightsmiles.com"},
		"http://www.Shop.co.uk/home#top":  {"http://www.shop.co.uk/home", "shop.co.uk"},
		"https://facebook.com/brightshop": {"", ""},
		"not a site":                      {"", ""},
	}
	for in, want := range cases {
		site, domain := NormalizeWebsite(in)
		if site != want[0] || domain != want[1] {
			t.Errorf("NormalizeWebsite(%q) = %q, %q; want %q, %q", in, site, domain, want[0], want[1])
		}
	}
}

func TestOSMFallsThroughBusyMirrors(t *testing.T) {
	busy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer busy.Close()
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"elements":[{"type":"node","id":7,"lat":47.5,"lon":19.05,"tags":{"name":"Corner Cafe","amenity":"cafe","website":"cornercafe.hu"}}]}`))
	}))
	defer ok.Close()
	refused := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer refused.Close()

	q := Query{Category: CategoryByID("cafe"), Area: models.LeadArea{Kind: models.LeadAreaBBox, Bounds: models.LeadBounds{South: 47.4, West: 19, North: 47.6, East: 19.1}}}
	page, err := NewOSM(busy.URL+","+ok.URL, "test").Search(context.Background(), q)
	if err != nil || len(page.Leads) != 1 || page.Leads[0].Domain != "cornercafe.hu" {
		t.Fatalf("page = %+v, err = %v", page, err)
	}
	if _, err := NewOSM(refused.URL+","+ok.URL, "test").Search(context.Background(), q); err == nil || errors.Is(err, errOverpassBusy) {
		t.Fatalf("a refused query must not fall through to the next mirror: %v", err)
	}
	if _, err := NewOSM(busy.URL, "test").Search(context.Background(), q); !errors.Is(err, errOverpassBusy) {
		t.Fatalf("every mirror busy = %v", err)
	}
}

func TestCellSafe(t *testing.T) {
	cases := map[string]string{
		"+36 30 364 0342":         "+36 30 364 0342",
		"+1 (512) 555-0100":       "+1 (512) 555-0100",
		"-12.5":                   "-12.5",
		"=HYPERLINK(\"x\",\"y\")": "'=HYPERLINK(\"x\",\"y\")",
		"+cmd|' /C calc'!A0":      "'+cmd|' /C calc'!A0",
		"@SUM(A1)":                "'@SUM(A1)",
		"Bright Smiles":           "Bright Smiles",
	}
	for in, want := range cases {
		if got := cellSafe(in); got != want {
			t.Errorf("cellSafe(%q) = %q, want %q", in, got, want)
		}
	}
}
