package leadfinder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

const (
	osmMaxResults = 1000
	// osmMaxAreaKM2 keeps a query inside what a public Overpass answers in time.
	osmMaxAreaKM2 = 2500
	// osmHedgeAfter is how long a silent mirror gets before the next one is asked too.
	osmHedgeAfter = 10 * time.Second
)

// DefaultOverpassURLs are public Overpass mirrors, asked in order, the next as soon as one is busy or slow. They are
// shared and often busy; operators set LEAD_FINDER_OVERPASS_URL to their own.
var DefaultOverpassURLs = []string{
	"https://overpass-api.de/api/interpreter",
	"https://maps.mail.ru/osm/tools/overpass/api/interpreter",
	"https://overpass.private.coffee/api/interpreter",
}

// errOverpassBusy means a mirror is overloaded; the next one is worth trying.
var errOverpassBusy = errors.New("OpenStreetMap's servers are busy right now; try again in a minute or use another provider")

type osmProvider struct {
	endpoints []string
	userAgent string
}

// NewOSM builds the OpenStreetMap provider over Overpass endpoints, given as a
// comma-separated list; empty means the public mirrors.
func NewOSM(endpoints, userAgent string) Provider {
	var list []string
	for _, e := range strings.Split(endpoints, ",") {
		if e = strings.TrimSpace(e); e != "" {
			list = append(list, e)
		}
	}
	if len(list) == 0 {
		list = DefaultOverpassURLs
	}
	return &osmProvider{endpoints: list, userAgent: userAgent}
}

func (p *osmProvider) ID() models.LeadProvider { return models.LeadProviderOSM }

// overpassQuery renders the search as Overpass QL.
func overpassQuery(q Query) (string, error) {
	b := q.Area.Bounds
	if b.West > b.East {
		return "", errors.New("OpenStreetMap cannot search across the date line; move the map")
	}
	selectors := genericOSM
	if q.Category != nil {
		selectors = q.Category.OSM
	}
	name := ""
	if t := strings.TrimSpace(q.Text); t != "" {
		name = `["name"~"` + overpassString(regexEscape(t)) + `",i]`
	} else if q.Category == nil {
		return "", errors.New("pick a category or type what to look for")
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "[out:json][timeout:25][bbox:%.6f,%.6f,%.6f,%.6f];\n(\n", b.South, b.West, b.North, b.East)
	for _, sel := range selectors {
		fmt.Fprintf(&sb, "  nwr%s[\"name\"]%s;\n", sel, name)
	}
	fmt.Fprintf(&sb, ");\nout center tags %d;\n", osmMaxResults)
	return sb.String(), nil
}

func regexEscape(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if strings.ContainsRune(`\.^$*+?()[]{}|`, r) {
			sb.WriteByte('\\')
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

func overpassString(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", " ").Replace(s)
}

type overpassElement struct {
	Type   string                      `json:"type"`
	ID     int64                       `json:"id"`
	Lat    float64                     `json:"lat"`
	Lon    float64                     `json:"lon"`
	Center *struct{ Lat, Lon float64 } `json:"center"`
	Tags   map[string]string           `json:"tags"`
}

func (p *osmProvider) Search(ctx context.Context, q Query) (*Page, error) {
	if AreaKM2(q.Area.Bounds) > osmMaxAreaKM2 {
		return nil, ErrAreaTooLarge
	}
	ql, err := overpassQuery(q)
	if err != nil {
		return nil, err
	}
	elements, err := p.hedged(ctx, ql)
	if err != nil {
		return nil, err
	}
	out := make([]models.Lead, 0, len(elements))
	for _, el := range elements {
		if l, ok := osmLead(el, q.Category); ok {
			out = append(out, l)
		}
	}
	return &Page{Leads: out}, nil
}

// hedged asks the mirrors in order, starting the next as soon as one is busy
// or has been silent for osmHedgeAfter, and keeps the first answer.
func (p *osmProvider) hedged(ctx context.Context, ql string) ([]overpassElement, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		elements []overpassElement
		err      error
	}
	results := make(chan result, len(p.endpoints))
	next, pending := 0, 0
	launch := func() {
		endpoint := p.endpoints[next]
		next++
		pending++
		go func() {
			els, err := p.query(ctx, endpoint, ql)
			results <- result{els, err}
		}()
	}
	launch()
	lastErr := errOverpassBusy
	for pending > 0 {
		timer := time.NewTimer(osmHedgeAfter)
		select {
		case r := <-results:
			timer.Stop()
			pending--
			if r.err == nil {
				return r.elements, nil
			}
			if !errors.Is(r.err, errOverpassBusy) {
				return nil, r.err
			}
			lastErr = r.err
			if next < len(p.endpoints) {
				launch()
			}
		case <-timer.C:
			if next < len(p.endpoints) {
				launch()
			}
		case <-ctx.Done():
			timer.Stop()
			return nil, errOverpassBusy
		}
	}
	return nil, lastErr
}

// query runs the search on one mirror. An overloaded or silent mirror is
// errOverpassBusy so the caller moves on; anything else is final.
func (p *osmProvider) query(ctx context.Context, endpoint, ql string) ([]overpassElement, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(url.Values{"data": {ql}}.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", p.userAgent)
	resp, err := providerHTTP.Do(req)
	if err != nil {
		return nil, errOverpassBusy
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return nil, errOverpassBusy
	case http.StatusBadRequest:
		return nil, errors.New("OpenStreetMap could not run this search; try simpler words")
	default:
		return nil, statusErr(resp.StatusCode)
	}
	var body struct {
		Elements []overpassElement `json:"elements"`
		Remark   string            `json:"remark"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&body); err != nil {
		return nil, errOverpassBusy
	}
	if len(body.Elements) == 0 && strings.Contains(body.Remark, "timed out") {
		return nil, errors.New("OpenStreetMap took too long; zoom in and search a smaller area")
	}
	return body.Elements, nil
}

// osmKinds are the tags whose value says what a place is, most specific first.
var osmKinds = []string{"healthcare", "craft", "office", "shop", "amenity", "tourism", "leisure", "sport"}

func osmLead(el overpassElement, c *Category) (models.Lead, bool) {
	t := el.Tags
	name := strings.TrimSpace(t["name"])
	if name == "" {
		return models.Lead{}, false
	}
	lat, lng := el.Lat, el.Lon
	if el.Center != nil {
		lat, lng = el.Center.Lat, el.Center.Lon
	}
	if lat == 0 && lng == 0 {
		return models.Lead{}, false
	}
	l := models.Lead{
		Provider:    models.LeadProviderOSM,
		ExternalID:  fmt.Sprintf("%s/%d", el.Type, el.ID),
		Name:        name,
		Latitude:    lat,
		Longitude:   lng,
		Description: t["description"],
		Chain:       firstNonEmpty(t["brand"], t["operator:wikidata"]),
		MapsURL:     fmt.Sprintf("https://www.openstreetmap.org/%s/%d", el.Type, el.ID),
		Socials:     map[string]string{},
		Emails:      []models.LeadEmail{},
		Hours:       []string{},
	}
	if l.Chain == "" && t["brand:wikidata"] != "" {
		l.Chain = name
	}
	for _, k := range osmKinds {
		if v := t[k]; v != "" && v != "yes" {
			l.Categories = append(l.Categories, Humanize(v))
		}
	}
	if cu := t["cuisine"]; cu != "" {
		for _, v := range strings.Split(cu, ";") {
			l.Categories = append(l.Categories, Humanize(v))
		}
	}
	if len(l.Categories) > 0 {
		l.Category = l.Categories[0]
	} else if c != nil {
		l.Category = c.Label
	}
	l.Street = strings.TrimSpace(t["addr:housenumber"] + " " + t["addr:street"])
	l.City = firstNonEmpty(t["addr:city"], t["addr:town"], t["addr:village"], t["addr:suburb"])
	l.Region = firstNonEmpty(t["addr:state"], t["addr:province"], t["addr:county"])
	l.Postcode = t["addr:postcode"]
	l.Country = t["addr:country"]
	l.Address = joinAddress(l.Street, l.City, strings.TrimSpace(l.Region+" "+l.Postcode), l.Country)
	l.Phone = strings.TrimSpace(strings.Split(firstNonEmpty(t["phone"], t["contact:phone"], t["contact:mobile"], t["mobile"]), ";")[0])
	l.Website, l.Domain = NormalizeWebsite(firstNonEmpty(t["website"], t["contact:website"], t["url"]))
	if e := cleanEmail(strings.Split(firstNonEmpty(t["email"], t["contact:email"]), ";")[0]); e != "" {
		l.Email = e
		l.Emails = []models.LeadEmail{{Email: e, Source: string(models.LeadProviderOSM), DomainMatch: emailDomainMatches(e, l.Domain)}}
	}
	for _, kind := range []string{"facebook", "instagram", "linkedin", "twitter", "tiktok", "youtube"} {
		if v := firstNonEmpty(t["contact:"+kind], t[kind]); v != "" {
			if u := socialURL(kind, v); u != "" {
				l.Socials[kind] = u
			}
		}
	}
	if oh := t["opening_hours"]; oh != "" {
		for _, part := range strings.Split(oh, ";") {
			if part = strings.TrimSpace(part); part != "" {
				l.Hours = append(l.Hours, part)
			}
		}
	}
	if t["disused:shop"] != "" || t["disused:amenity"] != "" {
		l.BusinessStatus = "closed"
	}
	return l, true
}

func emailDomainMatches(email, domain string) bool {
	if domain == "" {
		return false
	}
	at := strings.LastIndexByte(email, '@')
	if at < 0 {
		return false
	}
	host := email[at+1:]
	return host == domain || strings.HasSuffix(host, "."+domain)
}
