package leadfinder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/warmbly/warmbly/internal/models"
)

const (
	foursquareSearchURL = "https://places-api.foursquare.com/places/search"
	foursquareVersion   = "2025-06-17"
	foursquareFields    = "fsq_place_id,name,latitude,longitude,location,categories,chains,tel,website,email," +
		"social_media,rating,stats,price,hours,description,date_closed"
)

type foursquareProvider struct {
	key string
}

// NewFoursquare builds the Foursquare Places provider with a service key.
func NewFoursquare(key string) Provider {
	return &foursquareProvider{key: key}
}

func (p *foursquareProvider) ID() models.LeadProvider { return models.LeadProviderFoursquare }

type foursquarePlace struct {
	ID        string  `json:"fsq_place_id"`
	Name      string  `json:"name"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Location  struct {
		Address          string `json:"address"`
		Locality         string `json:"locality"`
		Region           string `json:"region"`
		Postcode         string `json:"postcode"`
		Country          string `json:"country"`
		FormattedAddress string `json:"formatted_address"`
	} `json:"location"`
	Categories []struct {
		Name string `json:"name"`
	} `json:"categories"`
	Chains []struct {
		Name string `json:"name"`
	} `json:"chains"`
	Tel         string `json:"tel"`
	Website     string `json:"website"`
	Email       string `json:"email"`
	SocialMedia struct {
		FacebookID string `json:"facebook_id"`
		Instagram  string `json:"instagram"`
		Twitter    string `json:"twitter"`
	} `json:"social_media"`
	Rating *float64 `json:"rating"`
	Stats  struct {
		TotalRatings *int `json:"total_ratings"`
	} `json:"stats"`
	Price *int `json:"price"`
	Hours struct {
		Display string `json:"display"`
	} `json:"hours"`
	Description string `json:"description"`
	DateClosed  string `json:"date_closed"`
}

var linkNext = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

func (p *foursquareProvider) Search(ctx context.Context, q Query) (*Page, error) {
	text := strings.TrimSpace(q.Text)
	if q.Category != nil {
		text = strings.TrimSpace(text + " " + q.Category.Term)
	}
	if text == "" {
		return nil, errors.New("pick a category or type what to look for")
	}
	target := q.Cursor
	if target == "" {
		b := q.Area.Bounds
		v := url.Values{
			"query":  {text},
			"ne":     {fmt.Sprintf("%.6f,%.6f", b.North, b.East)},
			"sw":     {fmt.Sprintf("%.6f,%.6f", b.South, b.West)},
			"limit":  {"50"},
			"fields": {foursquareFields},
		}
		target = foursquareSearchURL + "?" + v.Encode()
	} else if u, err := url.Parse(target); err != nil || u.Host != "places-api.foursquare.com" {
		return nil, errors.New("the next page of this search is no longer available")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.key)
	req.Header.Set("X-Places-Api-Version", foursquareVersion)
	req.Header.Set("Accept", "application/json")
	if q.Language != "" {
		req.Header.Set("Accept-Language", q.Language)
	}
	resp, err := providerHTTP.Do(req)
	if err != nil {
		return nil, errors.New("Foursquare did not answer; try again")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, statusErr(resp.StatusCode)
	}
	var body struct {
		Results []foursquarePlace `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&body); err != nil {
		return nil, errors.New("Foursquare sent an answer that could not be read")
	}
	out := make([]models.Lead, 0, len(body.Results))
	for _, pl := range body.Results {
		out = append(out, foursquareLead(pl))
	}
	next := ""
	if m := linkNext.FindStringSubmatch(resp.Header.Get("Link")); m != nil && len(body.Results) > 0 {
		next = m[1]
	}
	return &Page{Leads: out, NextCursor: next}, nil
}

func foursquareLead(pl foursquarePlace) models.Lead {
	l := models.Lead{
		Provider:    models.LeadProviderFoursquare,
		ExternalID:  pl.ID,
		Name:        pl.Name,
		Latitude:    pl.Latitude,
		Longitude:   pl.Longitude,
		Street:      pl.Location.Address,
		City:        pl.Location.Locality,
		Region:      pl.Location.Region,
		Postcode:    pl.Location.Postcode,
		Country:     pl.Location.Country,
		Address:     pl.Location.FormattedAddress,
		Phone:       pl.Tel,
		ReviewCount: pl.Stats.TotalRatings,
		PriceLevel:  pl.Price,
		Description: pl.Description,
		MapsURL:     "https://foursquare.com/v/" + pl.ID,
		Socials:     map[string]string{},
		Emails:      []models.LeadEmail{},
		Hours:       []string{},
	}
	if l.Address == "" {
		l.Address = joinAddress(l.Street, l.City, strings.TrimSpace(l.Region+" "+l.Postcode), l.Country)
	}
	for _, c := range pl.Categories {
		l.Categories = append(l.Categories, c.Name)
	}
	if len(l.Categories) > 0 {
		l.Category = l.Categories[0]
	}
	if len(pl.Chains) > 0 {
		l.Chain = pl.Chains[0].Name
	}
	if pl.Rating != nil {
		// Foursquare rates out of ten; the finder shows stars out of five.
		r := *pl.Rating / 2
		l.Rating = &r
	}
	l.Website, l.Domain = NormalizeWebsite(pl.Website)
	if e := cleanEmail(pl.Email); e != "" {
		l.Email = e
		l.Emails = []models.LeadEmail{{Email: e, Source: string(models.LeadProviderFoursquare), DomainMatch: emailDomainMatches(e, l.Domain)}}
	}
	if pl.SocialMedia.FacebookID != "" {
		l.Socials["facebook"] = "https://www.facebook.com/" + pl.SocialMedia.FacebookID
	}
	if u := socialURL("instagram", pl.SocialMedia.Instagram); u != "" {
		l.Socials["instagram"] = u
	}
	if u := socialURL("twitter", pl.SocialMedia.Twitter); u != "" {
		l.Socials["twitter"] = u
	}
	if pl.Hours.Display != "" {
		for _, part := range strings.Split(pl.Hours.Display, ";") {
			if part = strings.TrimSpace(part); part != "" {
				l.Hours = append(l.Hours, part)
			}
		}
	}
	if pl.DateClosed != "" {
		l.BusinessStatus = "closed"
	}
	return l
}

// CheckFoursquareKey runs one tiny search to prove the key can call Places.
func CheckFoursquareKey(ctx context.Context, key string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, foursquareSearchURL+"?query=coffee&limit=1&fields=fsq_place_id&ll=40.7484,-73.9857", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("X-Places-Api-Version", foursquareVersion)
	resp, err := providerHTTP.Do(req)
	if err != nil {
		return errors.New("Foursquare did not answer; try again")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusErr(resp.StatusCode)
	}
	return nil
}
