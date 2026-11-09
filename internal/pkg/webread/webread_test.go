package webread

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func publicDNS(t *testing.T) {
	t.Helper()
	prev := lookupIP
	lookupIP = func(_ context.Context, host string) ([]net.IPAddr, error) {
		if host == "intranet.example" {
			return []net.IPAddr{{IP: net.ParseIP("10.0.0.5")}}, nil
		}
		return []net.IPAddr{{IP: net.ParseIP("93.184.215.14")}}, nil
	}
	t.Cleanup(func() { lookupIP = prev })
}

const page = `<html><title>Acme</title><body>office@acme.com</body></html>`

func TestEachReaderReturnsThePageHTML(t *testing.T) {
	publicDNS(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/https://acme.com"):
			if r.Header.Get("X-Return-Format") != "html" || r.Header.Get("Authorization") != "Bearer jk" {
				http.Error(w, "headers", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"html": page, "finalUrl": "https://www.acme.com/"}})
		case r.URL.Path == "/crawl":
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"urls":["https://acme.com/"]`) {
				http.Error(w, "body", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "results": []any{map[string]any{"success": true, "html": page, "redirected_url": "https://www.acme.com/", "status_code": 200}}})
		case r.URL.Path == "/extract":
			if r.URL.Query().Get("url") != "https://acme.com/" || r.URL.Query().Get("fmt") != "text" {
				http.Error(w, "query", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"url": "https://acme.com/", "content": page})
		case r.URL.Path == "/read":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(page))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	for _, b := range []Backend{
		{Provider: "jina", URL: srv.URL, Key: "jk"},
		{Provider: "crawl4ai", URL: srv.URL},
		{Provider: "ddgs", URL: srv.URL},
		{Provider: "custom", URL: srv.URL + "/read?u={url_encoded}"},
	} {
		r := New([]Backend{b})
		if r == nil {
			t.Fatalf("%s: not built", b.Provider)
		}
		p, err := r.Read(context.Background(), "https://acme.com/")
		if err != nil {
			t.Fatalf("%s: %v", b.Provider, err)
		}
		if p.HTML != page || p.Via != b.Provider {
			t.Fatalf("%s: page = %+v", b.Provider, p)
		}
	}
}

func TestTheNextReaderAnswersWhenOneFails(t *testing.T) {
	publicDNS(t)
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "busy", http.StatusTooManyRequests)
	}))
	defer down.Close()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(page))
	}))
	defer up.Close()
	r := New([]Backend{{Provider: "crawl4ai", URL: down.URL}, {Provider: "custom", URL: up.URL + "/{url}"}})
	p, err := r.Read(context.Background(), "https://acme.com/")
	if err != nil || p.Via != "custom" {
		t.Fatalf("page %+v, err %v", p, err)
	}
}

func TestAReaderLandingOnAPrivateAddressIsRefused(t *testing.T) {
	publicDNS(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"html": page, "finalUrl": "http://intranet.example/admin"}})
	}))
	defer srv.Close()
	if _, err := New([]Backend{{Provider: "jina", URL: srv.URL}}).Read(context.Background(), "https://acme.com/"); err != ErrNotPublic {
		t.Fatalf("err = %v", err)
	}
}

func TestFromConfigReadsTheOrderedList(t *testing.T) {
	env := map[string]string{
		"WEB_READER":                "crawl4ai, jina",
		"WEB_READER_CRAWL4AI_URL":   "http://crawl4ai:11235",
		"WEB_READER_JINA_API_KEY":   "jk",
		"WEB_READER_DDGS_URL":       "http://ddgs:4479",
		"WEB_READER_CUSTOM_API_KEY": "unused",
	}
	r := FromConfig(func(k, _ string, _ bool) string { return env[k] })
	if r == nil || r.Name() != "crawl4ai,jina" {
		t.Fatalf("reader = %v", r)
	}
	if FromConfig(func(string, string, bool) string { return "" }) != nil {
		t.Fatal("a reader was built with nothing configured")
	}
	if New([]Backend{{Provider: "crawl4ai"}, {Provider: "custom", URL: "http://x/no-placeholder"}}) != nil {
		t.Fatal("a reader was built without the URL it needs")
	}
}

func TestBlockedMeansTurnedAway(t *testing.T) {
	for status, want := range map[int]bool{403: true, 429: true, 503: true, 401: true, 404: false, 500: false, 410: false} {
		if Blocked(status) != want {
			t.Errorf("Blocked(%d) = %v", status, !want)
		}
	}
}

func TestAPrivateOrOddPortPageNeverReachesAReader(t *testing.T) {
	publicDNS(t)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(page))
	}))
	defer srv.Close()
	r := New([]Backend{{Provider: "custom", URL: srv.URL + "/{url}"}})
	for _, target := range []string{"http://intranet.example/", "http://10.0.0.7/", "http://acme.com:6379/"} {
		if _, err := r.Read(context.Background(), target); err != ErrNotPublic {
			t.Errorf("%s: err = %v", target, err)
		}
	}
	if calls != 0 {
		t.Fatalf("the reader was called %d times", calls)
	}
}

func TestAReaderCannotRedirectAwayFromItself(t *testing.T) {
	publicDNS(t)
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(page))
	}))
	defer elsewhere.Close()
	bouncer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/private", http.StatusFound)
	}))
	defer bouncer.Close()
	if _, err := New([]Backend{{Provider: "custom", URL: bouncer.URL + "/{url}"}}).Read(context.Background(), "https://acme.com/"); err == nil {
		t.Fatal("followed a redirect off the reader's host")
	}
}

func TestAnHTTPSReaderCannotRedirectToHTTP(t *testing.T) {
	c := readerClient("https://reader.example")
	via := []*http.Request{{URL: mustURL("https://reader.example/read")}}
	if err := c.CheckRedirect(&http.Request{URL: mustURL("http://reader.example/read")}, via); err == nil {
		t.Fatal("followed an https to http redirect")
	}
	if err := c.CheckRedirect(&http.Request{URL: mustURL("https://reader.example/other")}, via); err != nil {
		t.Fatalf("refused a same-host https redirect: %v", err)
	}
}

func mustURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}
