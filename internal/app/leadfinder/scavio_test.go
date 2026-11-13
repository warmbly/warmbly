package leadfinder

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

// scavioFixture is two places from a live Scavio Maps search ("dentist" around Austin, TX).
const scavioFixture = `{"local_results": [{"position": 1, "title": "Toothbar", "place_id": "ChIJO0xN0RG1RIYRIDFDZYJkYjA", "data_id": "0x8644b511d14d4c3b:0x3062648265433120", "data_cid": "3486459572745613600", "provider_id": "/g/11hbll8k8x", "gps_coordinates": {"latitude": 30.2671177, "longitude": -97.7527535}, "rating": 4.8, "reviews": 737, "type": "Dentist", "types": ["Dentist", "Cosmetic dentist", "Dental implants provider", "Emergency dental service"], "address": "211 Walter Seaholm Dr LR 160, Austin, TX 78701", "open_state": "Open · Closes 5 PM", "hours": "Open · Closes 5 PM", "operating_hours": {"friday": "Closed", "monday": "8 AM–5 PM", "saturday": "Closed", "sunday": "Closed", "thursday": "8 AM–5 PM", "tuesday": "8 AM–5 PM", "wednesday": "8 AM–5 PM"}, "phone": "(512) 607-4268", "website": "https://www.toothbar.com/"}, {"position": 2, "title": "Breeze Dental", "place_id": "ChIJrTahjN-0RIYRchMKUA6urbg", "data_id": "0x8644b4df8ca136ad:0xb8adae0e500a1372", "data_cid": "13307483850445296498", "provider_id": "/g/11gtyt3hnm", "gps_coordinates": {"latitude": 30.2512668, "longitude": -97.76482299999999}, "rating": 4.8, "reviews": 1002, "type": "Dentist", "types": ["Dentist", "Cosmetic dentist", "Dental clinic", "Dental hygienist", "Dental implants provider", "Dental radiology", "Emergency dental service", "Oral surgeon", "Teeth whitening service"], "address": "1601 S Lamar Blvd, Austin, TX 78704", "open_state": "Open · Closes 4 PM", "hours": "Open · Closes 4 PM", "operating_hours": {"friday": "7 AM–3 PM", "monday": "7 AM–4 PM", "saturday": "Closed", "sunday": "Closed", "thursday": "7 AM–4 PM", "tuesday": "7 AM–4 PM", "wednesday": "7 AM–4 PM"}, "phone": "(512) 745-8825", "website": "https://www.breezeoralcare.com/?utm_campaign=gmb"}]}`

func TestScavioSearchMapsPlacesAndPages(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v2/google/maps/search" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk_test" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		raw, _ := io.ReadAll(r.Body)
		got = map[string]any{}
		_ = json.Unmarshal(raw, &got)
		// Pad the page to a full 20 so a next page is offered.
		var page struct {
			LocalResults []map[string]any `json:"local_results"`
		}
		_ = json.Unmarshal([]byte(scavioFixture), &page)
		for len(page.LocalResults) < scavioPageSize {
			page.LocalResults = append(page.LocalResults, map[string]any{"title": "No place id"})
		}
		_ = json.NewEncoder(w).Encode(page)
	}))
	defer srv.Close()

	p := &scavioProvider{key: "sk_test", base: srv.URL}
	area := models.LeadArea{Bounds: models.LeadBounds{South: 30.22, North: 30.32, West: -97.80, East: -97.70}}
	page, err := p.Search(context.Background(), Query{Text: "dentist", Area: area, Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if got["query"] != "dentist" || got["hl"] != "en" || got["start"] != nil {
		t.Errorf("body = %v", got)
	}
	if ll, _ := got["ll"].(string); !strings.HasPrefix(ll, "@30.270000,-97.750000,") || !strings.HasSuffix(ll, "z") {
		t.Errorf("ll = %v", got["ll"])
	}
	if len(page.Leads) != 2 {
		t.Fatalf("leads = %d, want the 2 with a place id", len(page.Leads))
	}
	l := page.Leads[0]
	if l.Provider != models.LeadProviderScavio || l.ExternalID != "ChIJO0xN0RG1RIYRIDFDZYJkYjA" || l.Name != "Toothbar" {
		t.Errorf("identity = %+v", l)
	}
	if l.Address != "211 Walter Seaholm Dr LR 160, Austin, TX 78701" || l.Phone != "(512) 607-4268" || l.Domain != "toothbar.com" {
		t.Errorf("contact = %q %q %q", l.Address, l.Phone, l.Domain)
	}
	if l.Rating == nil || *l.Rating != 4.8 || l.ReviewCount == nil || *l.ReviewCount != 737 {
		t.Errorf("rating = %v reviews = %v", l.Rating, l.ReviewCount)
	}
	if l.Latitude != 30.2671177 || l.Longitude != -97.7527535 {
		t.Errorf("position = %v,%v", l.Latitude, l.Longitude)
	}
	if l.Category != "Dentist" || len(l.Categories) != 4 || l.BusinessStatus != "operational" {
		t.Errorf("category = %q %v status = %q", l.Category, l.Categories, l.BusinessStatus)
	}
	if len(l.Hours) != 7 || !strings.HasPrefix(l.Hours[0], "monday: ") || !strings.HasPrefix(l.Hours[6], "sunday: ") {
		t.Errorf("hours = %v", l.Hours)
	}
	if !strings.Contains(l.MapsURL, "query_place_id=ChIJO0xN0RG1RIYRIDFDZYJkYjA") {
		t.Errorf("maps url = %q", l.MapsURL)
	}
	if page.NextCursor != "20" {
		t.Errorf("next cursor = %q", page.NextCursor)
	}

	page, err = p.Search(context.Background(), Query{Text: "dentist", Area: area, Cursor: "100"})
	if err != nil {
		t.Fatal(err)
	}
	if got["start"] != float64(100) || page.NextCursor != "" {
		t.Errorf("last page: start = %v next = %q", got["start"], page.NextCursor)
	}
	if _, err := p.Search(context.Background(), Query{Text: "dentist", Area: area, Cursor: "120"}); err == nil {
		t.Error("an offset past the last page should be refused")
	}
}

func TestScavioErrors(t *testing.T) {
	for _, tc := range []struct {
		code int
		body string
		want error
		text string
	}{
		{http.StatusUnauthorized, `{"error":"Invalid API key"}`, ErrProviderKey, ""},
		{http.StatusPaymentRequired, `{"error":"Insufficient credits"}`, ErrProviderQuota, ""},
		{http.StatusTooManyRequests, `{"error":"Rate limit exceeded"}`, ErrProviderQuota, ""},
		{http.StatusBadRequest, `{"error":"query is required"}`, nil, "Scavio refused the search: query is required"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.code)
			_, _ = io.WriteString(w, tc.body)
		}))
		p := &scavioProvider{key: "sk_test", base: srv.URL}
		_, err := p.Search(context.Background(), Query{Text: "dentist", Area: models.LeadArea{Bounds: models.LeadBounds{South: 30, North: 30.1, West: -97.8, East: -97.7}}})
		srv.Close()
		if tc.want != nil && !errors.Is(err, tc.want) {
			t.Errorf("%d: err = %v, want %v", tc.code, err, tc.want)
		}
		if tc.text != "" && (err == nil || err.Error() != tc.text) {
			t.Errorf("%d: err = %v, want %q", tc.code, err, tc.text)
		}
	}
}

func TestScavioZoomFitsTheArea(t *testing.T) {
	city := scavioZoom(models.LeadBounds{South: 30.22, North: 30.32, West: -97.80, East: -97.70})
	block := scavioZoom(models.LeadBounds{South: 30.265, North: 30.270, West: -97.755, East: -97.750})
	state := scavioZoom(models.LeadBounds{South: 26, North: 36, West: -106, East: -94})
	if !(state < city && city < block) || city < 11 || city > 14 || state < 3 || block > 21 {
		t.Errorf("zoom state=%d city=%d block=%d", state, city, block)
	}
}
