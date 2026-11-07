package leadfinder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/warmbly/warmbly/internal/models"
)

const (
	outscraperSearchURL = "https://api.app.outscraper.com/google-maps-search"
	outscraperPageSize  = 20
	// outscraperMaxSkip is Google Maps' own ceiling on one query's results.
	outscraperMaxSkip = 400
)

type outscraperProvider struct {
	key string
}

// NewOutscraper builds the Outscraper Google Maps provider with an API key.
func NewOutscraper(key string) Provider {
	return &outscraperProvider{key: key}
}

func (p *outscraperProvider) ID() models.LeadProvider { return models.LeadProviderOutscraper }

func (p *outscraperProvider) Search(ctx context.Context, q Query) (*Page, error) {
	text := strings.TrimSpace(q.Text)
	if q.Category != nil {
		text = strings.TrimSpace(text + " " + q.Category.Term)
	}
	if text == "" {
		return nil, errors.New("pick a category or type what to look for")
	}
	skip := 0
	if q.Cursor != "" {
		n, err := strconv.Atoi(q.Cursor)
		if err != nil || n < 0 {
			return nil, errors.New("the next page of this search is no longer available")
		}
		skip = n
	}
	c := Center(q.Area.Bounds)
	payload := map[string]any{
		"query":                      []string{text},
		"organizationsPerQueryLimit": outscraperPageSize,
		"skipPlaces":                 skip,
		"coordinates":                fmt.Sprintf("%.6f,%.6f", c.Lat, c.Lng),
		"dropDuplicates":             true,
		"async":                      false,
		"enrichment":                 []string{"domains_service"},
	}
	if q.Language != "" {
		payload["language"] = q.Language
	}
	rows, err := p.post(ctx, payload)
	if err != nil {
		return nil, err
	}
	out := make([]models.Lead, 0, len(rows))
	for _, r := range rows {
		if l, ok := outscraperLead(r); ok {
			out = append(out, l)
		}
	}
	next := ""
	if len(rows) >= outscraperPageSize && skip+outscraperPageSize < outscraperMaxSkip {
		next = strconv.Itoa(skip + outscraperPageSize)
	}
	return &Page{Leads: out, NextCursor: next}, nil
}

func (p *outscraperProvider) post(ctx context.Context, payload map[string]any) ([]map[string]any, error) {
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, outscraperSearchURL, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", p.key)
	resp, err := providerHTTP.Do(req)
	if err != nil {
		return nil, errors.New("Outscraper did not answer; try again")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, statusErr(resp.StatusCode)
	}
	var body struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&body); err != nil {
		return nil, errors.New("Outscraper sent an answer that could not be read")
	}
	// data is one array per query; tolerate a flat array too.
	var out []map[string]any
	for _, d := range body.Data {
		var group []map[string]any
		if json.Unmarshal(d, &group) == nil {
			out = append(out, group...)
			continue
		}
		var one map[string]any
		if json.Unmarshal(d, &one) == nil {
			out = append(out, one)
		}
	}
	return out, nil
}

func str(m map[string]any, k string) string {
	switch v := m[k].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

func num(m map[string]any, k string) *float64 {
	if v, ok := m[k].(float64); ok {
		return &v
	}
	return nil
}

func outscraperLead(r map[string]any) (models.Lead, bool) {
	id := firstNonEmpty(str(r, "place_id"), str(r, "google_id"))
	lat, lng := num(r, "latitude"), num(r, "longitude")
	if id == "" || lat == nil || lng == nil || str(r, "name") == "" {
		return models.Lead{}, false
	}
	l := models.Lead{
		Provider:    models.LeadProviderOutscraper,
		ExternalID:  id,
		Name:        str(r, "name"),
		Address:     str(r, "full_address"),
		Street:      str(r, "street"),
		City:        str(r, "city"),
		Region:      str(r, "state"),
		Postcode:    str(r, "postal_code"),
		Country:     str(r, "country_code"),
		Latitude:    *lat,
		Longitude:   *lng,
		Phone:       str(r, "phone"),
		Rating:      num(r, "rating"),
		MapsURL:     str(r, "location_link"),
		Description: str(r, "description"),
		Category:    firstNonEmpty(str(r, "type"), str(r, "category")),
		Socials:     map[string]string{},
		Emails:      []models.LeadEmail{},
		Hours:       []string{},
	}
	if v := num(r, "reviews"); v != nil {
		n := int(*v)
		l.ReviewCount = &n
	}
	if rng := str(r, "range"); rng != "" && strings.Trim(rng, "$") == "" {
		n := len(rng)
		l.PriceLevel = &n
	}
	for _, s := range strings.Split(str(r, "subtypes"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			l.Categories = append(l.Categories, s)
		}
	}
	l.Website, l.Domain = NormalizeWebsite(str(r, "site"))
	switch str(r, "business_status") {
	case "CLOSED_PERMANENTLY":
		l.BusinessStatus = "closed"
	case "CLOSED_TEMPORARILY":
		l.BusinessStatus = "temporarily_closed"
	case "OPERATIONAL":
		l.BusinessStatus = "operational"
	}
	if wh, ok := r["working_hours"].(map[string]any); ok {
		days := make([]string, 0, len(wh))
		for d := range wh {
			days = append(days, d)
		}
		sort.Slice(days, func(i, j int) bool { return weekdayIndex(days[i]) < weekdayIndex(days[j]) })
		for _, d := range days {
			switch v := wh[d].(type) {
			case string:
				l.Hours = append(l.Hours, d+": "+v)
			case []any:
				parts := make([]string, 0, len(v))
				for _, x := range v {
					if s, ok := x.(string); ok {
						parts = append(parts, s)
					}
				}
				l.Hours = append(l.Hours, d+": "+strings.Join(parts, ", "))
			}
		}
	}
	for i := 1; i <= 5; i++ {
		e := cleanEmail(str(r, fmt.Sprintf("email_%d", i)))
		if e == "" {
			continue
		}
		l.Emails = append(l.Emails, models.LeadEmail{Email: e, Source: string(models.LeadProviderOutscraper), DomainMatch: emailDomainMatches(e, l.Domain)})
	}
	if len(l.Emails) > 0 {
		l.Email = l.Emails[0].Email
	}
	for _, kind := range []string{"facebook", "instagram", "linkedin", "twitter", "youtube", "tiktok"} {
		if u := socialURL(kind, str(r, kind)); u != "" {
			l.Socials[kind] = u
		}
	}
	return l, true
}

var weekdays = []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}

func weekdayIndex(d string) int {
	d = strings.ToLower(d)
	for i, w := range weekdays {
		if strings.HasPrefix(d, w[:3]) {
			return i
		}
	}
	return len(weekdays)
}

// CheckOutscraperKey proves the key is accepted with a one-result search.
func CheckOutscraperKey(ctx context.Context, key string) error {
	p := &outscraperProvider{key: key}
	_, err := p.post(ctx, map[string]any{"query": []string{"coffee, New York, USA"}, "organizationsPerQueryLimit": 1, "async": false})
	return err
}
