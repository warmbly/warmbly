package leadfinder

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

func TestSiteCandidates(t *testing.T) {
	got := siteCandidates(&models.Lead{Name: "Moon Creek Dental, PLLC", City: "Boise", Country: "US"})
	want := []string{"mooncreekdental.com", "moon-creek-dental.com", "mooncreekdentalboise.com"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("candidates = %v, want %v", got, want)
	}

	de := siteCandidates(&models.Lead{Name: "Zahnarztpraxis Müller & Söhne GmbH", Country: "DE"})
	if len(de) == 0 || de[0] != "zahnarztpraxismullerandsohne.de" || !containsStr(de, "zahnarztpraxismullersohne.com") {
		t.Fatalf("german candidates = %v", de)
	}
	if uk := siteCandidates(&models.Lead{Name: "The Bakehouse", Country: "GB"}); len(uk) == 0 || uk[0] != "bakehouse.co.uk" {
		t.Fatalf("uk candidates = %v", uk)
	}
	if len(siteCandidates(&models.Lead{Name: "LLC"})) != 0 {
		t.Fatal("a name of only legal words has no domain")
	}
}

func TestConfirmsBusiness(t *testing.T) {
	lead := &models.Lead{Name: "Moon Creek Dental", City: "Boise", Street: "Overland Road", Phone: "+1 208 939 7053"}
	cases := []struct {
		name string
		html string
		want bool
	}{
		{"phone in text", `<html><body><p>Call us (208) 939-7053</p></body></html>`, true},
		{"phone link", `<html><body><a href="tel:+12089397053">Call</a></body></html>`, true},
		{"name and city", `<html><head><title>Moon Creek Dental | Boise dentist</title></head><body></body></html>`, true},
		{"name elsewhere", `<html><head><title>Moon Creek Dental</title></head><body><p>Serving Portland, Oregon</p></body></html>`, false},
		{"parked", `<html><body><h1>mooncreekdental.com is for sale</h1><p>Boise</p></body></html>`, false},
		{"other number", `<html><body><p>Call (208) 555-0199. Boise.</p></body></html>`, false},
	}
	for _, c := range cases {
		if got := confirmsBusiness(parsePage(c.html), lead); got != c.want {
			t.Errorf("%s: confirms = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestFindWebsite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		if strings.HasPrefix(r.Host, "moon-creek-dental.com") {
			_, _ = w.Write([]byte(`<html><body><p>Moon Creek Dental, call 208-939-7053</p></body></html>`))
			return
		}
		_, _ = w.Write([]byte(`<html><body><p>This domain is for sale</p></body></html>`))
	}))
	defer srv.Close()

	c := NewCrawler("test")
	c.client = &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
		},
	}}
	c.lookup = func(_ context.Context, host string) ([]string, error) {
		if host == "mooncreekdental.com" || host == "moon-creek-dental.com" {
			return []string{"192.0.2.1"}, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}

	website, domain := c.FindWebsite(context.Background(), &models.Lead{Name: "Moon Creek Dental", City: "Boise", Country: "US", Phone: "+1 208 939 7053"}, nil)
	if website != "http://moon-creek-dental.com/" || domain != "moon-creek-dental.com" {
		t.Fatalf("found %q %q", website, domain)
	}
	if w, _ := c.FindWebsite(context.Background(), &models.Lead{Name: "Nobody Here", Country: "US"}, nil); w != "" {
		t.Fatalf("found %q for a business with no site", w)
	}
}
