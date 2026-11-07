package leadfinder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/warmbly/warmbly/internal/models"
)

const googleSearchURL = "https://places.googleapis.com/v1/places:searchText"

// googleFieldMask asks for what a lead shows and nothing billed beyond it.
const googleFieldMask = "places.id,places.displayName,places.formattedAddress,places.addressComponents," +
	"places.location,places.types,places.primaryType,places.primaryTypeDisplayName,places.businessStatus," +
	"places.googleMapsUri,places.nationalPhoneNumber,places.internationalPhoneNumber,places.websiteUri," +
	"places.rating,places.userRatingCount,places.priceLevel,places.regularOpeningHours,places.editorialSummary,nextPageToken"

type googleProvider struct {
	key      string
	endpoint string
}

// NewGooglePlaces builds the Google Places (New) provider with an API key.
func NewGooglePlaces(key string) Provider {
	return &googleProvider{key: key, endpoint: googleSearchURL}
}

func (p *googleProvider) ID() models.LeadProvider { return models.LeadProviderGooglePlaces }

type googleText struct {
	Text string `json:"text"`
}

type googlePlace struct {
	ID                string     `json:"id"`
	DisplayName       googleText `json:"displayName"`
	FormattedAddress  string     `json:"formattedAddress"`
	AddressComponents []struct {
		LongText  string   `json:"longText"`
		ShortText string   `json:"shortText"`
		Types     []string `json:"types"`
	} `json:"addressComponents"`
	Location struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	} `json:"location"`
	Types                  []string   `json:"types"`
	PrimaryType            string     `json:"primaryType"`
	PrimaryTypeDisplayName googleText `json:"primaryTypeDisplayName"`
	BusinessStatus         string     `json:"businessStatus"`
	GoogleMapsURI          string     `json:"googleMapsUri"`
	NationalPhone          string     `json:"nationalPhoneNumber"`
	InternationalPhone     string     `json:"internationalPhoneNumber"`
	WebsiteURI             string     `json:"websiteUri"`
	Rating                 *float64   `json:"rating"`
	UserRatingCount        *int       `json:"userRatingCount"`
	PriceLevel             string     `json:"priceLevel"`
	RegularOpeningHours    *struct {
		WeekdayDescriptions []string `json:"weekdayDescriptions"`
	} `json:"regularOpeningHours"`
	EditorialSummary googleText `json:"editorialSummary"`
}

func googleBody(q Query, withType bool) map[string]any {
	text := strings.TrimSpace(q.Text)
	if q.Category != nil {
		text = strings.TrimSpace(text + " " + q.Category.Term)
	}
	b := q.Area.Bounds
	body := map[string]any{
		"textQuery": text,
		"pageSize":  20,
		"locationRestriction": map[string]any{"rectangle": map[string]any{
			"low":  map[string]float64{"latitude": b.South, "longitude": b.West},
			"high": map[string]float64{"latitude": b.North, "longitude": b.East},
		}},
	}
	if withType && q.Category != nil && q.Category.GoogleType != "" {
		body["includedType"] = q.Category.GoogleType
		body["strictTypeFiltering"] = true
	}
	if q.Language != "" {
		body["languageCode"] = q.Language
	}
	if q.Cursor != "" {
		body["pageToken"] = q.Cursor
	}
	return body
}

func (p *googleProvider) post(ctx context.Context, body map[string]any) ([]googlePlace, string, int, error) {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Goog-Api-Key", p.key)
	req.Header.Set("X-Goog-FieldMask", googleFieldMask)
	resp, err := providerHTTP.Do(req)
	if err != nil {
		return nil, "", 0, errors.New("Google Places did not answer; try again")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error struct {
				Message string `json:"message"`
				Status  string `json:"status"`
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&e)
		if resp.StatusCode == http.StatusBadRequest && strings.Contains(strings.ToLower(e.Error.Message), "api key") {
			return nil, "", resp.StatusCode, ErrProviderKey
		}
		if resp.StatusCode == http.StatusBadRequest {
			return nil, "", resp.StatusCode, errors.New("Google Places refused the search: " + e.Error.Message)
		}
		return nil, "", resp.StatusCode, statusErr(resp.StatusCode)
	}
	var out struct {
		Places        []googlePlace `json:"places"`
		NextPageToken string        `json:"nextPageToken"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&out); err != nil {
		return nil, "", resp.StatusCode, errors.New("Google Places sent an answer that could not be read")
	}
	return out.Places, out.NextPageToken, resp.StatusCode, nil
}

func (p *googleProvider) Search(ctx context.Context, q Query) (*Page, error) {
	if strings.TrimSpace(q.Text) == "" && q.Category == nil {
		return nil, errors.New("pick a category or type what to look for")
	}
	if q.Area.Bounds.West > q.Area.Bounds.East {
		return nil, errors.New("Google Places cannot search across the date line; move the map")
	}
	places, next, code, err := p.post(ctx, googleBody(q, true))
	if err != nil && code == http.StatusBadRequest && !errors.Is(err, ErrProviderKey) && q.Category != nil && q.Category.GoogleType != "" {
		// A type Google no longer knows falls back to the plain phrase.
		places, next, _, err = p.post(ctx, googleBody(q, false))
	}
	if err != nil {
		return nil, err
	}
	out := make([]models.Lead, 0, len(places))
	for _, pl := range places {
		out = append(out, googleLead(pl))
	}
	return &Page{Leads: out, NextCursor: next}, nil
}

func googleLead(pl googlePlace) models.Lead {
	l := models.Lead{
		Provider:    models.LeadProviderGooglePlaces,
		ExternalID:  pl.ID,
		Name:        pl.DisplayName.Text,
		Address:     pl.FormattedAddress,
		Latitude:    pl.Location.Latitude,
		Longitude:   pl.Location.Longitude,
		Phone:       firstNonEmpty(pl.InternationalPhone, pl.NationalPhone),
		Rating:      pl.Rating,
		ReviewCount: pl.UserRatingCount,
		MapsURL:     pl.GoogleMapsURI,
		Description: pl.EditorialSummary.Text,
		Socials:     map[string]string{},
		Emails:      []models.LeadEmail{},
		Hours:       []string{},
	}
	l.Website, l.Domain = NormalizeWebsite(pl.WebsiteURI)
	l.Category = firstNonEmpty(pl.PrimaryTypeDisplayName.Text, Humanize(pl.PrimaryType))
	for _, t := range pl.Types {
		if t != "point_of_interest" && t != "establishment" {
			l.Categories = append(l.Categories, Humanize(t))
		}
	}
	var number, route string
	for _, c := range pl.AddressComponents {
		for _, t := range c.Types {
			switch t {
			case "street_number":
				number = c.LongText
			case "route":
				route = c.LongText
			case "locality", "postal_town":
				if l.City == "" {
					l.City = c.LongText
				}
			case "administrative_area_level_1":
				l.Region = c.ShortText
			case "postal_code":
				l.Postcode = c.LongText
			case "country":
				l.Country = c.ShortText
			}
		}
	}
	l.Street = strings.TrimSpace(number + " " + route)
	switch pl.BusinessStatus {
	case "CLOSED_PERMANENTLY":
		l.BusinessStatus = "closed"
	case "CLOSED_TEMPORARILY":
		l.BusinessStatus = "temporarily_closed"
	case "OPERATIONAL":
		l.BusinessStatus = "operational"
	}
	if lvl := googlePriceLevels[pl.PriceLevel]; lvl > 0 {
		l.PriceLevel = &lvl
	}
	if pl.RegularOpeningHours != nil {
		l.Hours = append(l.Hours, pl.RegularOpeningHours.WeekdayDescriptions...)
	}
	return l
}

var googlePriceLevels = map[string]int{
	"PRICE_LEVEL_INEXPENSIVE": 1, "PRICE_LEVEL_MODERATE": 2, "PRICE_LEVEL_EXPENSIVE": 3, "PRICE_LEVEL_VERY_EXPENSIVE": 4,
}

// CheckGoogleKey runs one tiny search to prove the key can call Places (New).
func CheckGoogleKey(ctx context.Context, key string) error {
	p := &googleProvider{key: key, endpoint: googleSearchURL}
	_, _, _, err := p.post(ctx, map[string]any{"textQuery": "coffee", "pageSize": 1})
	return err
}
