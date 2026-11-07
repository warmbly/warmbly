package leadfinder

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/warmbly/warmbly/internal/models"
)

// DefaultGeocoderURL is Komoot's public Photon instance; operators can point
// LEAD_FINDER_GEOCODER_URL at their own.
const DefaultGeocoderURL = "https://photon.komoot.io/api"

// GeocodeResult is one place a location search matched.
type GeocodeResult struct {
	Label  string             `json:"label"`
	Detail string             `json:"detail"`
	Kind   string             `json:"kind"`
	Center models.LatLng      `json:"center"`
	Bounds *models.LeadBounds `json:"bounds,omitempty"`
}

// Geocoder turns a typed location into points on the map.
type Geocoder struct {
	endpoint  string
	userAgent string
}

// NewGeocoder builds a Photon-compatible geocoder.
func NewGeocoder(endpoint, userAgent string) *Geocoder {
	if endpoint == "" {
		endpoint = DefaultGeocoderURL
	}
	return &Geocoder{endpoint: endpoint, userAgent: userAgent}
}

// Search returns up to limit places matching q, nearest bias first when given.
func (g *Geocoder) Search(ctx context.Context, q string, near *models.LatLng, limit int, lang string) ([]GeocodeResult, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []GeocodeResult{}, nil
	}
	v := url.Values{"q": {q}, "limit": {strconv.Itoa(limit)}}
	if near != nil {
		v.Set("lat", strconv.FormatFloat(near.Lat, 'f', 5, 64))
		v.Set("lon", strconv.FormatFloat(near.Lng, 'f', 5, 64))
	}
	if lang = strings.ToLower(strings.TrimSpace(lang)); lang == "en" || lang == "de" || lang == "fr" {
		v.Set("lang", lang)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.endpoint+"?"+v.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", g.userAgent)
	resp, err := providerHTTP.Do(req)
	if err != nil {
		return nil, errors.New("the location search did not answer; try again")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, statusErr(resp.StatusCode)
	}
	var body struct {
		Features []struct {
			Geometry struct {
				Coordinates []float64 `json:"coordinates"`
			} `json:"geometry"`
			Properties struct {
				Name        string    `json:"name"`
				Street      string    `json:"street"`
				HouseNumber string    `json:"housenumber"`
				City        string    `json:"city"`
				County      string    `json:"county"`
				State       string    `json:"state"`
				Country     string    `json:"country"`
				Postcode    string    `json:"postcode"`
				Type        string    `json:"type"`
				OSMValue    string    `json:"osm_value"`
				Extent      []float64 `json:"extent"`
			} `json:"properties"`
		} `json:"features"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&body); err != nil {
		return nil, errors.New("the location search sent an answer that could not be read")
	}
	out := make([]GeocodeResult, 0, len(body.Features))
	seen := map[string]bool{}
	for _, f := range body.Features {
		if len(f.Geometry.Coordinates) < 2 {
			continue
		}
		p := f.Properties
		label := firstNonEmpty(p.Name, strings.TrimSpace(p.HouseNumber+" "+p.Street), p.City)
		if label == "" {
			continue
		}
		detailParts := []string{}
		for _, part := range []string{p.City, p.State, p.Country} {
			if part != "" && part != label {
				detailParts = append(detailParts, part)
			}
		}
		r := GeocodeResult{
			Label:  label,
			Detail: strings.Join(detailParts, ", "),
			Kind:   firstNonEmpty(p.Type, p.OSMValue),
			Center: models.LatLng{Lat: f.Geometry.Coordinates[1], Lng: f.Geometry.Coordinates[0]},
		}
		if len(p.Extent) == 4 {
			r.Bounds = &models.LeadBounds{West: p.Extent[0], North: p.Extent[1], East: p.Extent[2], South: p.Extent[3]}
		}
		key := r.Label + "|" + r.Detail
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	// Someone picking where to search means a place far more often than a street.
	sort.SliceStable(out, func(i, j int) bool { return placeRank(out[i].Kind) < placeRank(out[j].Kind) })
	return out, nil
}

func placeRank(kind string) int {
	switch kind {
	case "city", "town", "village":
		return 0
	case "district", "locality", "suburb", "neighbourhood":
		return 1
	case "county", "state", "country", "region":
		return 2
	case "street":
		return 4
	case "house":
		return 5
	}
	return 3
}
