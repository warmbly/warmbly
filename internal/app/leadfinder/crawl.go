package leadfinder

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/publicsuffix"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/safehttp"
	"github.com/warmbly/warmbly/internal/pkg/webread"
)

const (
	crawlMaxPages    = 7
	crawlMaxBytes    = 1536 << 10
	crawlPageTimeout = 12 * time.Second
	crawlMaxEmails   = 8
	// crawlMaxAddrs bounds every address read, personal ones included.
	crawlMaxAddrs = 60
	// crawlMaxReaderPages bounds the reader requests one site makes, failed ones included.
	crawlMaxReaderPages = 4
)

// FoundEmail is one address read off a page.
type FoundEmail struct {
	Email string
	URL   string
	// Source is where it was found; empty means the business's own site.
	Source string
}

// CrawlResult is everything reading one business's website found.
type CrawlResult struct {
	Emails  []FoundEmail
	Socials map[string]string
	Site    models.LeadSite
	// People are those the site names, with any address it published for them.
	People []FoundPerson
	// Domain is the site's own registrable domain, after redirects.
	Domain string
}

// Crawler reads a business's own public pages, the way a visitor would: the
// home page and the few pages that list contact details, nothing robots.txt
// keeps out.
type Crawler struct {
	client    *http.Client
	userAgent string
	now       func() time.Time
	lookup    func(ctx context.Context, host string) ([]string, error)
	// fallback reads a page the site turned a direct visit away from.
	fallback webread.Reader
	// readerLeft is how many reader requests the current site may still make.
	readerLeft *int
}

// withFallback is the crawler reading blocked pages through r.
func (c *Crawler) withFallback(r webread.Reader) *Crawler {
	cp := *c
	cp.fallback = r
	return &cp
}

// NewCrawler builds a crawler over the SSRF-hardened transport. Plain-http
// sites are common among small businesses, so port 80 is allowed.
func NewCrawler(userAgent string) *Crawler {
	return &Crawler{
		userAgent: userAgent,
		now:       time.Now,
		lookup:    defaultLookup,
		client: &http.Client{
			Timeout:   crawlPageTimeout,
			Transport: safehttp.NewTransport("80", "443"),
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("too many redirects")
				}
				if (req.URL.Scheme != "http" && req.URL.Scheme != "https") || req.URL.User != nil {
					return errors.New("redirect is not allowed")
				}
				return nil
			},
		},
	}
}

// contactPath spots links worth following for contact details, in the
// languages the finder is most used in.
var contactPath = regexp.MustCompile(`(?i)(contact|kontakt|contacto|contatti|contato|about|uber-uns|ueber-uns|about-us|team|staff|people|leadership|management|meet|doctors|dentists|attorneys|lawyers|providers|physicians|agents|founders|who-we-are|mitarbeiter|equipo|equipe|squadra|impressum|imprint|legal|mentions|aviso-legal|location|find-us|reach)`)

// Crawl reads the site at website. A site that cannot be reached is a result,
// not an error: Site.Reachable is false and Site.Error says why.
func (c *Crawler) Crawl(ctx context.Context, website string) (*CrawlResult, error) {
	start, err := url.Parse(website)
	if err != nil || start.Hostname() == "" {
		return nil, errors.New("the website address is not valid")
	}
	if c.fallback != nil {
		cp, left := *c, crawlMaxReaderPages
		cp.readerLeft = &left
		c = &cp
	}
	now := c.now()
	res := &CrawlResult{Socials: map[string]string{}, Site: models.LeadSite{CheckedAt: &now}}
	rules := c.robots(ctx, start)

	home, err := c.fetch(ctx, start.String(), rules, false)
	if err != nil && start.Scheme == "https" {
		alt := *start
		alt.Scheme = "http"
		// The http origin answers for itself, here and for every page read after.
		altRules := c.robots(ctx, &alt)
		if h, e := c.httpRetry(err).fetch(ctx, alt.String(), altRules, false); e == nil {
			home, err, rules = h, nil, altRules
		}
	}
	if err != nil {
		res.Site.Error = err.Error()
		res.Site.Unread = unreadReason(err)
		return res, nil
	}
	res.Site.Reachable = true
	res.Site.FinalURL = home.url.String()
	res.Site.HTTPS = home.url.Scheme == "https"
	res.Site.Title = home.title
	res.Site.Description = home.description
	res.Site.Platform, res.Site.Signals = detectSite(home, now)
	res.Site.ReadVia = home.via
	pages := []*page{home}

	baseDomain := registrable(home.url.Hostname())
	var follow []string
	seen := map[string]bool{normURL(home.url): true}
	for _, l := range home.links {
		u, err := home.url.Parse(l.href)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || registrable(u.Hostname()) != baseDomain {
			continue
		}
		u.Fragment = ""
		key := normURL(u)
		if seen[key] || !(contactPath.MatchString(u.Path) || contactPath.MatchString(l.text)) {
			continue
		}
		seen[key] = true
		follow = append(follow, u.String())
	}
	// Contact pages first, then about, then the rest.
	sort.SliceStable(follow, func(i, j int) bool { return contactRank(follow[i]) < contactRank(follow[j]) })
	// A site that turned the home page away sends every page to the reader.
	viaReader := home.via != ""
	limit := crawlMaxPages
	if viaReader {
		limit = crawlMaxReaderPages
	}
	for _, target := range follow {
		if len(pages) >= limit || ctx.Err() != nil {
			break
		}
		if p, err := c.fetch(ctx, target, rules, viaReader); err == nil {
			pages = append(pages, p)
		}
	}
	res.Site.Pages = len(pages)
	res.Domain = baseDomain

	emailSeen := map[string]bool{}
	phoneSeen := map[string]bool{}
	var addrs []FoundEmail
	var found []FoundPerson
	personal := ""
	for _, p := range pages {
		for _, e := range p.emails {
			if !emailSeen[e] && len(addrs) < crawlMaxAddrs {
				emailSeen[e] = true
				addrs = append(addrs, FoundEmail{Email: e, URL: p.url.String()})
			}
		}
		found = append(found, extractPeople(p)...)
		for _, ph := range p.phones {
			if !phoneSeen[ph] && len(res.Site.Phones) < 3 {
				phoneSeen[ph] = true
				res.Site.Phones = append(res.Site.Phones, ph)
			}
		}
		for _, l := range p.links {
			u, err := p.url.Parse(l.href)
			if err != nil {
				continue
			}
			if kind := socialKind(u.String()); kind != "" && res.Socials[kind] == "" {
				u.RawQuery, u.Fragment = "", ""
				// A profile on a team page is a person's, not the business's.
				if kind == "linkedin" && strings.HasPrefix(strings.Trim(u.Path, "/"), "in/") {
					if personal == "" {
						personal = u.String()
					}
					continue
				}
				res.Socials[kind] = u.String()
			}
		}
	}
	if res.Socials["linkedin"] == "" && personal != "" {
		res.Socials["linkedin"] = personal
	}
	people, pattern := attachEmails(mergePeople(found), addrs, baseDomain)
	res.People = mergePeople(people)
	res.Site.EmailPattern = pattern
	// The business's own list leads with the addresses no one person owns.
	owned := map[string]bool{}
	for _, p := range res.People {
		owned[p.Email] = true
	}
	for _, own := range []bool{false, true} {
		for _, e := range addrs {
			if len(res.Emails) < crawlMaxEmails && owned[e.Email] == own {
				res.Emails = append(res.Emails, e)
			}
		}
	}
	return res, nil
}

// teamPath names the pages that list the people who work somewhere.
var teamPath = regexp.MustCompile(`(?i)(team|staff|people|leadership|management|meet|doctors|dentists|attorneys|lawyers|providers|physicians|agents|founders|who-we-are|mitarbeiter|equipo|equipe|squadra)`)

func contactRank(u string) int {
	l := strings.ToLower(u)
	switch {
	case strings.Contains(l, "contact") || strings.Contains(l, "kontakt") || strings.Contains(l, "contato"):
		return 0
	case teamPath.MatchString(l):
		return 1
	case imprintURL(l):
		return 2
	case strings.Contains(l, "about"):
		return 3
	}
	return 4
}

func imprintURL(u string) bool {
	l := strings.ToLower(u)
	return strings.Contains(l, "impressum") || strings.Contains(l, "imprint") || strings.Contains(l, "legal") || strings.Contains(l, "mentions")
}

func registrable(host string) string {
	host = strings.ToLower(host)
	d, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return strings.TrimPrefix(host, "www.")
	}
	return d
}

func normURL(u *url.URL) string {
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.") + strings.TrimSuffix(u.EscapedPath(), "/") + "?" + u.RawQuery
}

type link struct {
	href string
	text string
}

type page struct {
	url         *url.URL
	title       string
	description string
	generator   string
	raw         string
	links       []link
	emails      []string
	phones      []string
	hasViewport bool
	// segments is the visible text, one entry per element, in page order.
	segments []string
	jsonld   []string
	// via names the reader service that fetched the page, empty when direct.
	via string
}

// errRobots is a page the site's robots.txt keeps crawlers out of.
var errRobots = errors.New("the site's robots.txt asks crawlers not to read this page")

// unreadReason classifies why a site's home page could not be read.
func unreadReason(err error) models.LeadSiteUnread {
	var blocked *blockedError
	switch {
	case errors.As(err, &blocked):
		return models.LeadSiteBlocked
	case errors.Is(err, errRobots):
		return models.LeadSiteRobots
	}
	return models.LeadSiteUnreachable
}

// blockedError is a site turning the finder away, after any reader tried too.
type blockedError struct {
	status int
	reader bool
}

func (e *blockedError) Error() string {
	msg := "the website answered " + strconv.Itoa(e.status)
	if e.reader {
		msg += ", and the reader service could not get past it either"
	}
	return msg
}

// inlineTags format text without starting a new piece of it.
var inlineTags = map[string]bool{"em": true, "i": true, "small": true, "sup": true, "sub": true, "abbr": true, "mark": true, "u": true}

// fetch reads one page, straight through the reader when viaReader is set or
// when the site turns the direct visit away. robots.txt binds both paths.
func (c *Crawler) fetch(ctx context.Context, target string, rules *robotsRules, viaReader bool) (*page, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	if !rules.allowed(u) {
		return nil, errRobots
	}
	if viaReader && c.fallback != nil {
		return c.read(ctx, target, http.StatusForbidden)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5")
	resp, err := c.client.Do(req)
	if err != nil {
		if errors.Is(err, safehttp.ErrBlockedAddress) {
			return nil, errors.New("the website points at a private address")
		}
		return nil, errors.New("the website did not answer")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		if webread.Blocked(resp.StatusCode) {
			if c.fallback != nil {
				return c.read(ctx, target, resp.StatusCode)
			}
			return nil, &blockedError{status: resp.StatusCode}
		}
		return nil, errors.New("the website answered " + strconv.Itoa(resp.StatusCode))
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		mt, _, _ := mime.ParseMediaType(ct)
		if mt != "text/html" && mt != "application/xhtml+xml" {
			return nil, errors.New("the website did not send a web page")
		}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, crawlMaxBytes))
	if err != nil {
		return nil, errors.New("the website stopped mid-page")
	}
	p := parsePage(string(body))
	p.url = resp.Request.URL
	return p, nil
}

// httpRetry is the crawler to try a site's http origin with after its https
// one failed: one that already went to the reader is not sent there twice.
func (c *Crawler) httpRetry(err error) *Crawler {
	var blocked *blockedError
	if errors.As(err, &blocked) && blocked.reader {
		return c.withFallback(nil)
	}
	return c
}

// read fetches a page through the reader service after the site answered status.
func (c *Crawler) read(ctx context.Context, target string, status int) (*page, error) {
	if c.readerLeft != nil {
		if *c.readerLeft <= 0 {
			return nil, &blockedError{status: status, reader: true}
		}
		*c.readerLeft--
	}
	rp, err := c.fallback.Read(ctx, target)
	if err != nil {
		return nil, &blockedError{status: status, reader: true}
	}
	final, err := url.Parse(rp.URL)
	if err != nil || final.Hostname() == "" {
		final, _ = url.Parse(target)
	}
	src := rp.HTML
	if len(src) > crawlMaxBytes {
		src = src[:crawlMaxBytes]
	}
	p := parsePage(src)
	p.url = final
	p.via = rp.Via
	return p, nil
}

var (
	emailInText = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9\-]+(?:\.[a-zA-Z0-9\-]+)*\.[a-zA-Z]{2,24}`)
	obfuscAt    = regexp.MustCompile(`(?i)\s*[\[\(\{<]\s*(?:at|@)\s*[\]\)\}>]\s*`)
	obfuscDot   = regexp.MustCompile(`(?i)\s*[\[\(\{<]\s*(?:dot|\.)\s*[\]\)\}>]\s*`)
	telDigits   = regexp.MustCompile(`[^0-9+]`)
)

// junkEmailDomains are hosts that show up in page source but never belong to
// the business: tracking, builders and placeholders.
var junkEmailDomains = map[string]bool{
	"sentry.io": true, "wixpress.com": true, "sentry.wixpress.com": true, "sentry-next.wixpress.com": true,
	"example.com": true, "example.org": true, "domain.com": true, "yourdomain.com": true, "yoursite.com": true,
	"email.com": true, "website.com": true, "company.com": true, "mysite.com": true, "test.com": true,
	"godaddy.com": true, "squarespace.com": true, "wix.com": true, "shopify.com": true, "wordpress.com": true,
	"sentry.zendesk.com": true, "addresshere.com": true,
}

var junkEmailSuffix = []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".css", ".js", ".ico", ".avif"}

var hexLocal = regexp.MustCompile(`^[0-9a-f]{16,}$`)

// acceptEmail filters what the page extraction found down to real addresses.
func acceptEmail(raw string) string {
	e := cleanEmail(raw)
	if e == "" {
		return ""
	}
	for _, s := range junkEmailSuffix {
		if strings.HasSuffix(e, s) {
			return ""
		}
	}
	at := strings.LastIndexByte(e, '@')
	local, host := e[:at], e[at+1:]
	if junkEmailDomains[host] || hexLocal.MatchString(local) || strings.Contains(local, "..") {
		return ""
	}
	switch local {
	case "you", "your", "name", "yourname", "email", "user", "username", "someone", "john.doe", "johndoe", "firstname.lastname":
		return ""
	}
	return e
}

// decodeCloudflareEmail reverses Cloudflare's email obfuscation.
func decodeCloudflareEmail(enc string) string {
	b, err := hex.DecodeString(enc)
	if err != nil || len(b) < 2 {
		return ""
	}
	key := b[0]
	out := make([]byte, len(b)-1)
	for i := 1; i < len(b); i++ {
		out[i-1] = b[i] ^ key
	}
	return string(out)
}

func parsePage(src string) *page {
	p := &page{raw: strings.ToLower(src)}
	z := html.NewTokenizer(strings.NewReader(src))
	var text, seg, ld strings.Builder
	var inTitle, skip, inLD bool
	cur := -1
	flush := func() {
		t := strings.Join(strings.Fields(seg.String()), " ")
		seg.Reset()
		if t != "" && len(p.segments) < segmentsMax && utf8.RuneCountInString(t) <= segmentMax {
			p.segments = append(p.segments, t)
		}
	}
	emails := map[string]bool{}
	addEmail := func(raw string) {
		if e := acceptEmail(raw); e != "" && !emails[e] {
			emails[e] = true
			p.emails = append(p.emails, e)
		}
	}
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			if !inlineTags[tag] {
				flush()
			}
			attrs := map[string]string{}
			for hasAttr {
				var k, v []byte
				k, v, hasAttr = z.TagAttr()
				attrs[string(k)] = string(v)
			}
			if enc := attrs["data-cfemail"]; enc != "" {
				addEmail(decodeCloudflareEmail(enc))
			}
			switch tag {
			case "title":
				inTitle = true
			case "script", "style", "noscript", "svg":
				skip = tt == html.StartTagToken
				inLD = skip && tag == "script" && strings.Contains(strings.ToLower(attrs["type"]), "ld+json")
			case "meta":
				switch strings.ToLower(attrs["name"]) {
				case "description":
					p.description = strings.TrimSpace(attrs["content"])
				case "generator":
					p.generator = strings.TrimSpace(attrs["content"])
				case "viewport":
					p.hasViewport = true
				}
				if strings.EqualFold(attrs["property"], "og:description") && p.description == "" {
					p.description = strings.TrimSpace(attrs["content"])
				}
			case "a":
				href := strings.TrimSpace(attrs["href"])
				lower := strings.ToLower(href)
				switch {
				case strings.HasPrefix(lower, "mailto:"):
					addr, _ := url.QueryUnescape(href[len("mailto:"):])
					for _, part := range strings.Split(addr, ",") {
						addEmail(part)
					}
					// Kept as a link too: its text may name the person it reaches.
					p.links = append(p.links, link{href: href})
					if tt == html.StartTagToken {
						cur = len(p.links) - 1
					}
				case strings.HasPrefix(lower, "tel:"):
					if d := telDigits.ReplaceAllString(href[4:], ""); len(d) >= 7 {
						p.phones = append(p.phones, d)
					}
				case strings.Contains(lower, "/cdn-cgi/l/email-protection#"):
					// Sliced at the fragment of href itself: lowercasing can change byte lengths.
					if i := strings.IndexByte(href, '#'); i >= 0 {
						addEmail(decodeCloudflareEmail(href[i+1:]))
					}
				case href != "" && !strings.HasPrefix(lower, "javascript:"):
					p.links = append(p.links, link{href: href})
					if tt == html.StartTagToken {
						cur = len(p.links) - 1
					}
				}
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			if !inlineTags[string(name)] {
				flush()
			}
			switch string(name) {
			case "title":
				inTitle = false
			case "script", "style", "noscript", "svg":
				if inLD && ld.Len() > 0 && len(p.jsonld) < 20 {
					p.jsonld = append(p.jsonld, ld.String())
				}
				ld.Reset()
				skip, inLD = false, false
			case "a":
				cur = -1
			}
		case html.TextToken:
			if inLD {
				if ld.Len() < 256<<10 {
					ld.Write(z.Text())
				}
				continue
			}
			if skip {
				continue
			}
			t := string(z.Text())
			if inTitle && p.title == "" {
				p.title = strings.TrimSpace(t)
			}
			if cur >= 0 && len(p.links[cur].text) < 80 {
				p.links[cur].text += strings.TrimSpace(t)
			}
			text.WriteString(t)
			text.WriteByte(' ')
			seg.WriteString(t)
			seg.WriteByte(' ')
		}
	}
	flush()
	plain := obfuscDot.ReplaceAllString(obfuscAt.ReplaceAllString(text.String(), "@"), ".")
	for _, m := range emailInText.FindAllString(plain, 50) {
		addEmail(m)
	}
	if len(p.title) > 200 {
		p.title = p.title[:200]
	}
	if len(p.description) > 400 {
		p.description = p.description[:400]
	}
	return p
}

type signature struct {
	name    string
	needles []string
}

var platforms = []signature{
	{"Shopify", []string{"cdn.shopify.com", "myshopify.com"}},
	{"Wix", []string{"static.wixstatic.com", "wix.com website builder"}},
	{"Squarespace", []string{"static1.squarespace.com", "squarespace.com"}},
	{"Webflow", []string{"data-wf-site", "webflow.com"}},
	{"Framer", []string{"framerusercontent.com"}},
	{"Duda", []string{"multiscreensite.com", "dudaone"}},
	{"GoDaddy Website Builder", []string{"img1.wsimg.com"}},
	{"Weebly", []string{"weebly.com"}},
	{"HubSpot CMS", []string{"hs-sites.com", "hubspot cms"}},
	{"WordPress", []string{"wp-content/", "wp-includes/"}},
	{"Joomla", []string{"/media/jui/", "joomla"}},
	{"Drupal", []string{"drupal-settings-json", "/sites/default/files/"}},
}

var signals = []signature{
	{"Google Analytics", []string{"google-analytics.com/", "gtag('config'", "gtag(\"config\""}},
	{"Google Tag Manager", []string{"googletagmanager.com/gtm.js"}},
	{"Meta Pixel", []string{"connect.facebook.net", "fbq('init'"}},
	{"Online booking", []string{"calendly.com", "acuityscheduling.com", "booksy.com", "fresha.com", "opentable.com", "resy.com", "mindbodyonline.com", "squareup.com/appointments", "zocdoc.com", "setmore.com", "simplybook"}},
	{"Live chat", []string{"widget.intercom.io", "js.driftt.com", "embed.tawk.to", "client.crisp.chat", "livechatinc.com", "static.zdassets.com", "tidio"}},
	{"Online store", []string{"cdn.shopify.com", "woocommerce", "ecwid.com", "bigcommerce.com"}},
	{"Contact form", []string{"wpcf7", "gform_wrapper", "type=\"email\"", "hs-form", "formspree.io"}},
}

var copyrightYear = regexp.MustCompile(`(?:©|&copy;|copyright)\s*(?:\d{4}\s*[-–]\s*)?((?:19|20)\d{2})`)

// detectSite names the platform a site runs on and the signals a prospector
// reads from it, digital gaps included.
func detectSite(p *page, now time.Time) (string, []string) {
	platform := ""
	gen := strings.ToLower(p.generator)
	for _, s := range platforms {
		if strings.Contains(gen, strings.ToLower(s.name)) {
			platform = s.name
			break
		}
		for _, n := range s.needles {
			if strings.Contains(p.raw, n) {
				platform = s.name
				break
			}
		}
		if platform != "" {
			break
		}
	}
	var out []string
	for _, s := range signals {
		for _, n := range s.needles {
			if strings.Contains(p.raw, n) {
				out = append(out, s.name)
				break
			}
		}
	}
	if p.url != nil && p.url.Scheme != "https" {
		out = append(out, "No HTTPS")
	}
	if !p.hasViewport {
		out = append(out, "Not mobile friendly")
	}
	if m := copyrightYear.FindAllStringSubmatch(p.raw, -1); len(m) > 0 {
		latest := 0
		for _, g := range m {
			if y, _ := strconv.Atoi(g[1]); y > latest && y <= now.Year() {
				latest = y
			}
		}
		if latest > 0 && latest < now.Year()-2 {
			out = append(out, "Outdated footer ("+strconv.Itoa(latest)+")")
		}
	}
	if !containsStr(out, "Google Analytics") && !containsStr(out, "Google Tag Manager") && !containsStr(out, "Meta Pixel") {
		out = append(out, "No analytics")
	}
	return platform, out
}

func containsStr(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// --- robots.txt -------------------------------------------------------------

type robotsRule struct {
	allow bool
	path  string
}

type robotsRules struct {
	rules []robotsRule
}

// robots reads the site's robots.txt for our agent, falling back to "*". A
// missing or unreadable file allows everything, as crawlers conventionally do.
func (c *Crawler) robots(ctx context.Context, site *url.URL) *robotsRules {
	u := url.URL{Scheme: site.Scheme, Host: site.Host, Path: "/robots.txt"}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return &robotsRules{}
	}
	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.client.Do(req)
	if err != nil {
		return &robotsRules{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &robotsRules{}
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	return parseRobots(string(body), "warmblybot")
}

func parseRobots(src, agent string) *robotsRules {
	groups := map[string][]robotsRule{}
	var current []string
	lastWasAgent := false
	for _, line := range strings.Split(src, "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		switch k {
		case "user-agent":
			if !lastWasAgent {
				current = nil
			}
			current = append(current, strings.ToLower(v))
			lastWasAgent = true
		case "allow", "disallow":
			lastWasAgent = false
			if v == "" {
				continue
			}
			for _, a := range current {
				groups[a] = append(groups[a], robotsRule{allow: k == "allow", path: v})
			}
		default:
			lastWasAgent = false
		}
	}
	if r, ok := groups[agent]; ok {
		return &robotsRules{rules: r}
	}
	return &robotsRules{rules: groups["*"]}
}

// allowed applies the longest matching rule; allow wins a tie.
func (r *robotsRules) allowed(u *url.URL) bool {
	if r == nil {
		return true
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	best, allow := -1, true
	for _, rule := range r.rules {
		if robotsMatch(rule.path, path) {
			n := len(rule.path)
			if n > best || (n == best && rule.allow) {
				best, allow = n, rule.allow
			}
		}
	}
	return allow
}

func robotsMatch(pattern, path string) bool {
	anchored := strings.HasSuffix(pattern, "$")
	pattern = strings.TrimSuffix(pattern, "$")
	parts := strings.Split(pattern, "*")
	pos := 0
	for i, part := range parts {
		idx := strings.Index(path[pos:], part)
		if idx < 0 || (i == 0 && idx != 0) {
			return false
		}
		pos += idx + len(part)
	}
	return !anchored || pos == len(path) || strings.HasSuffix(pattern, "*")
}
