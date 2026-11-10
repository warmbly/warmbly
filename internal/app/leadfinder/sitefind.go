package leadfinder

import (
	"context"
	"net"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

const (
	siteFindMaxCandidates = 8
	siteFindMaxReads      = 3
	// siteFindMaxSearched is how many hosts from a web search get a read of their own.
	siteFindMaxSearched   = 3
	siteFindLookupTimeout = 3 * time.Second
	siteFindTimeout       = 20 * time.Second
)

// legalWords say what kind of company a name is, never what its domain is.
var legalWords = map[string]bool{
	"llc": true, "inc": true, "ltd": true, "limited": true, "gmbh": true, "co": true, "corp": true, "pllc": true, "pc": true,
	"pa": true, "plc": true, "kft": true, "bt": true, "zrt": true, "sarl": true, "sas": true, "srl": true, "spa": true, "bv": true,
	"ag": true, "kg": true, "ohg": true, "ug": true, "dds": true, "dmd": true, "md": true, "cpa": true, "esq": true,
}

// countryTLDs are the endings a business in that country is most likely on, before .com.
var countryTLDs = map[string][]string{
	"GB": {"co.uk"}, "UK": {"co.uk"}, "AU": {"com.au"}, "NZ": {"co.nz"}, "BR": {"com.br"}, "MX": {"com.mx"},
	"AR": {"com.ar"}, "ZA": {"co.za"}, "JP": {"co.jp"}, "IN": {"in", "co.in"},
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// nameTokens folds a business name to the words a domain is built from.
func nameTokens(name string) []string {
	s := strings.ReplaceAll(asciiFold(name), "&", " and ")
	var out []string
	for i, w := range strings.Fields(nonAlnum.ReplaceAllString(s, " ")) {
		if legalWords[w] || (i == 0 && w == "the") {
			continue
		}
		out = append(out, w)
	}
	return out
}

// folded is text reduced to lowercase ASCII words separated by single spaces.
func folded(s string) string {
	return strings.Join(strings.Fields(nonAlnum.ReplaceAllString(asciiFold(s), " ")), " ")
}

// siteCandidates are the domains a business with this name most likely owns, best first.
func siteCandidates(l *models.Lead) []string {
	tokens := nameTokens(l.Name)
	if len(tokens) == 0 {
		return nil
	}
	var labels []string
	add := func(s string) {
		if len(s) >= 4 && len(s) <= 40 && !containsStr(labels, s) {
			labels = append(labels, s)
		}
	}
	add(strings.Join(tokens, ""))
	if len(tokens) > 1 {
		add(strings.Join(tokens, "-"))
	}
	var plain []string
	for _, t := range tokens {
		if t != "and" && t != "of" {
			plain = append(plain, t)
		}
	}
	add(strings.Join(plain, ""))
	if city := strings.ReplaceAll(folded(l.City), " ", ""); city != "" && len(tokens) <= 3 {
		add(strings.Join(tokens, "") + city)
	}

	country := strings.ToUpper(strings.TrimSpace(l.Country))
	tlds := append([]string(nil), countryTLDs[country]...)
	if len(tlds) == 0 && len(country) == 2 && country != "US" {
		tlds = []string{strings.ToLower(country)}
	}
	tlds = append(tlds, "com")

	var out []string
	for _, tld := range tlds {
		for _, label := range labels {
			if len(out) < siteFindMaxCandidates {
				out = append(out, label+"."+tld)
			}
		}
	}
	return out
}

// FindWebsite works out the site of a business whose listing has none, trying
// the hosts a web search turned up before the ones built from its name. A
// candidate counts only when its home page carries the business's phone
// number, or its full name together with its town, street or postcode.
func (c *Crawler) FindWebsite(ctx context.Context, l *models.Lead, searched []string) (string, string) {
	ctx, cancel := context.WithTimeout(ctx, siteFindTimeout)
	defer cancel()
	var cands []string
	for _, h := range append(append([]string(nil), searched...), siteCandidates(l)...) {
		if h = strings.ToLower(h); h != "" && !containsStr(cands, h) {
			cands = append(cands, h)
		}
	}
	maxReads := siteFindMaxReads + min(len(searched), siteFindMaxSearched)
	live := make([]bool, len(cands))
	var wg sync.WaitGroup
	for i, host := range cands {
		wg.Add(1)
		go func(i int, host string) {
			defer wg.Done()
			lctx, cancel := context.WithTimeout(ctx, siteFindLookupTimeout)
			defer cancel()
			addrs, err := c.lookup(lctx, host)
			live[i] = err == nil && len(addrs) > 0
		}(i, host)
	}
	wg.Wait()

	reads := 0
	for i, host := range cands {
		if !live[i] || reads >= maxReads || ctx.Err() != nil {
			continue
		}
		reads++
		start := &url.URL{Scheme: "https", Host: host, Path: "/"}
		rules := c.robots(ctx, start)
		p, err := c.fetch(ctx, start.String(), rules, false)
		if err != nil {
			start.Scheme = "http"
			p, err = c.httpRetry(err).fetch(ctx, start.String(), c.robots(ctx, start), false)
		}
		if err != nil || !confirmsBusiness(p, l) {
			continue
		}
		home := url.URL{Scheme: p.url.Scheme, Host: p.url.Host, Path: "/"}
		if website, domain := NormalizeWebsite(home.String()); website != "" {
			return website, domain
		}
	}
	return "", ""
}

// confirmsBusiness says whether a page is the business's own.
func confirmsBusiness(p *page, l *models.Lead) bool {
	if want := phoneTail(l.Phone); want != "" {
		for _, ph := range p.phones {
			if phoneTail(ph) == want {
				return true
			}
		}
		for _, seg := range p.segments {
			for _, run := range phoneRun.FindAllString(seg, -1) {
				if phoneTail(run) == want {
					return true
				}
			}
		}
	}
	name := strings.Join(nameTokens(l.Name), " ")
	if len(name) < 4 {
		return false
	}
	text := " " + folded(p.title+" "+strings.Join(p.segments, " ")) + " "
	if !strings.Contains(text, " "+name+" ") {
		return false
	}
	for _, place := range []string{l.City, l.Street, l.Postcode} {
		if f := folded(place); len(f) >= 3 && strings.Contains(text, " "+f+" ") {
			return true
		}
	}
	return false
}

var phoneRun = regexp.MustCompile(`\+?\d[\d\s().\-/]{6,}\d`)

// phoneTail is the part of a number that stays the same however it is
// written: its last nine digits, or all of a seven or eight digit one.
func phoneTail(s string) string {
	d := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
	switch {
	case len(d) >= 9:
		return d[len(d)-9:]
	case len(d) >= 7:
		return d
	}
	return ""
}

func defaultLookup(ctx context.Context, host string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, host)
}
