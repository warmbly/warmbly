package leadfinder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/warmbly/warmbly/internal/models"
)

const (
	scavioBaseURL  = "https://api.scavio.dev"
	scavioPageSize = 20
	// scavioMaxStart is the last offset Scavio's Maps search pages to.
	scavioMaxStart = 100
)

type scavioProvider struct {
	key  string
	base string
}

// NewScavio builds the Scavio Google Maps provider with an API key.
func NewScavio(key string) Provider {
	return &scavioProvider{key: key, base: scavioBaseURL}
}

func (p *scavioProvider) ID() models.LeadProvider { return models.LeadProviderScavio }

type scavioPlace struct {
	Title          string `json:"title"`
	PlaceID        string `json:"place_id"`
	DataCID        string `json:"data_cid"`
	GPSCoordinates *struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	} `json:"gps_coordinates"`
	Rating         *float64          `json:"rating"`
	Reviews        *int              `json:"reviews"`
	Type           string            `json:"type"`
	Types          []string          `json:"types"`
	Address        string            `json:"address"`
	OpenState      string            `json:"open_state"`
	OperatingHours map[string]string `json:"operating_hours"`
	Phone          string            `json:"phone"`
	Website        string            `json:"website"`
	Price          string            `json:"price"`
	Description    string            `json:"description"`
}

func (p *scavioProvider) Search(ctx context.Context, q Query) (*Page, error) {
	text := strings.TrimSpace(q.Text)
	if q.Category != nil {
		text = strings.TrimSpace(text + " " + q.Category.Term)
	}
	if text == "" {
		return nil, errors.New("pick a category or type what to look for")
	}
	start := 0
	if q.Cursor != "" {
		n, err := strconv.Atoi(q.Cursor)
		if err != nil || n < 0 || n > scavioMaxStart || n%scavioPageSize != 0 {
			return nil, errors.New("the next page of this search is no longer available")
		}
		start = n
	}
	c := Center(q.Area.Bounds)
	body := map[string]any{
		"query": text,
		"ll":    fmt.Sprintf("@%.6f,%.6f,%dz", c.Lat, c.Lng, scavioZoom(q.Area.Bounds)),
	}
	if start > 0 {
		body["start"] = start
	}
	if q.Language != "" && len(q.Language) >= 2 && len(q.Language) <= 5 {
		body["hl"] = q.Language
	}
	var out struct {
		LocalResults []scavioPlace `json:"local_results"`
	}
	if err := p.do(ctx, http.MethodPost, "/api/v2/google/maps/search", body, &out); err != nil {
		return nil, err
	}
	leads := make([]models.Lead, 0, len(out.LocalResults))
	for _, pl := range out.LocalResults {
		if l, ok := scavioLead(pl); ok {
			leads = append(leads, l)
		}
	}
	next := ""
	if len(out.LocalResults) >= scavioPageSize && start+scavioPageSize <= scavioMaxStart {
		next = strconv.Itoa(start + scavioPageSize)
	}
	return &Page{Leads: leads, NextCursor: next}, nil
}

// scavioZoom fits a ~1000px view inside the bounds, since only places in the area are kept.
func scavioZoom(b models.LeadBounds) int {
	const viewPx = 1024.0
	lat := (b.South + b.North) / 2 * math.Pi / 180
	z := 21.0
	if span := lngSpan(b); span > 0 {
		z = math.Min(z, math.Log2(360*viewPx/(256*span)))
	}
	if span := b.North - b.South; span > 0 {
		z = math.Min(z, math.Log2(360*viewPx*math.Cos(lat)/(256*span)))
	}
	return int(math.Max(3, math.Min(21, math.Ceil(z))))
}

func (p *scavioProvider) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.base+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+p.key)
	resp, err := providerHTTP.Do(req)
	if err != nil {
		return errors.New("Scavio did not answer; try again")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if resp.StatusCode == http.StatusBadRequest {
			var e struct {
				Error string `json:"error"`
			}
			_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&e)
			if e.Error != "" {
				return errors.New("Scavio refused the search: " + e.Error)
			}
		}
		return statusErr(resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out); err != nil {
		return errors.New("Scavio sent an answer that could not be read")
	}
	return nil
}

func scavioLead(pl scavioPlace) (models.Lead, bool) {
	id := firstNonEmpty(strings.TrimSpace(pl.PlaceID), strings.TrimSpace(pl.DataCID))
	name := strings.TrimSpace(pl.Title)
	if id == "" || name == "" || pl.GPSCoordinates == nil {
		return models.Lead{}, false
	}
	l := models.Lead{
		Provider:    models.LeadProviderScavio,
		ExternalID:  id,
		Name:        name,
		Address:     strings.TrimSpace(pl.Address),
		Latitude:    pl.GPSCoordinates.Latitude,
		Longitude:   pl.GPSCoordinates.Longitude,
		Phone:       strings.TrimSpace(pl.Phone),
		Rating:      pl.Rating,
		ReviewCount: pl.Reviews,
		Category:    strings.TrimSpace(pl.Type),
		Description: strings.TrimSpace(pl.Description),
		Socials:     map[string]string{},
		Emails:      []models.LeadEmail{},
		Hours:       []string{},
	}
	if pl.PlaceID != "" {
		l.MapsURL = "https://www.google.com/maps/search/?api=1&query=" + url.QueryEscape(name) + "&query_place_id=" + url.QueryEscape(pl.PlaceID)
	}
	for _, t := range pl.Types {
		if t = strings.TrimSpace(t); t != "" {
			l.Categories = append(l.Categories, t)
		}
	}
	if l.Category == "" && len(l.Categories) > 0 {
		l.Category = l.Categories[0]
	}
	l.Website, l.Domain = NormalizeWebsite(pl.Website)
	if rng := strings.TrimSpace(pl.Price); rng != "" && strings.Trim(rng, "$") == "" {
		n := len(rng)
		l.PriceLevel = &n
	}
	switch state := strings.ToLower(pl.OpenState); {
	case strings.Contains(state, "permanently closed"):
		l.BusinessStatus = "closed"
	case strings.Contains(state, "temporarily closed"):
		l.BusinessStatus = "temporarily_closed"
	case state != "":
		l.BusinessStatus = "operational"
	}
	days := make([]string, 0, len(pl.OperatingHours))
	for d := range pl.OperatingHours {
		days = append(days, d)
	}
	sort.Slice(days, func(i, j int) bool { return weekdayIndex(days[i]) < weekdayIndex(days[j]) })
	for _, d := range days {
		l.Hours = append(l.Hours, d+": "+pl.OperatingHours[d])
	}
	return l, true
}

// CheckScavioKey proves the key is accepted by reading the free usage endpoint.
func CheckScavioKey(ctx context.Context, key string) error {
	p := &scavioProvider{key: key, base: scavioBaseURL}
	return p.do(ctx, http.MethodGet, "/api/v1/usage", nil, nil)
}
