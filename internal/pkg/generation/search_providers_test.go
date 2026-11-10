package generation

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEachSearchProviderReadsItsAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/ddgs/search/text":
			if r.URL.Query().Get("query") != "acme boise" || r.URL.Query().Get("max_results") != "3" {
				http.Error(w, "query", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"results":[{"title":"Acme","href":"https://acme.com/","body":"Plumbing"}]}`))
		case "/jina/":
			if r.URL.Query().Get("q") != "acme boise" || r.Header.Get("Authorization") != "Bearer k" {
				http.Error(w, "auth", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"code":200,"data":[{"title":"Acme","url":"https://acme.com/","description":"Plumbing"}]}`))
		case "/brave":
			if r.Header.Get("X-Subscription-Token") != "k" || r.URL.Query().Get("count") != "3" {
				http.Error(w, "auth", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"web":{"results":[{"title":"Acme","url":"https://acme.com/","description":"Plumbing"}]}}`))
		case "/tavily":
			body, _ := io.ReadAll(r.Body)
			var in map[string]any
			_ = json.Unmarshal(body, &in)
			if r.Header.Get("Authorization") != "Bearer k" || in["query"] != "acme boise" {
				http.Error(w, "auth", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"results":[{"title":"Acme","url":"https://acme.com/","content":"Plumbing"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	for _, b := range []SearchBackend{
		{Provider: "ddgs", URL: srv.URL + "/ddgs"},
		{Provider: "jina", URL: srv.URL + "/jina", Key: "k"},
		{Provider: "brave", URL: srv.URL + "/brave", Key: "k"},
		{Provider: "tavily", URL: srv.URL + "/tavily", Key: "k"},
	} {
		c := NewSearchClient(b.Provider, b.URL, b.Key)
		if c == nil {
			t.Fatalf("%s: not built", b.Provider)
		}
		res, err := c.Search(context.Background(), "acme boise", 3)
		if err != nil {
			t.Fatalf("%s: %v", b.Provider, err)
		}
		if len(res) != 1 || res[0] != (SearchResult{Title: "Acme", URL: "https://acme.com/", Snippet: "Plumbing"}) {
			t.Fatalf("%s: %+v", b.Provider, res)
		}
	}
}

func TestSearchFallsBackDownTheList(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "ratelimited", http.StatusTooManyRequests)
	}))
	defer down.Close()
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer empty.Close()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"title":"Acme","url":"https://acme.com/","content":"x"}]}`))
	}))
	defer up.Close()
	env := map[string]string{
		"SEARCH_PROVIDER":       "ddgs, searxng,jina",
		"SEARCH_API_URL":        down.URL,
		"SEARCH_SEARXNG_URL":    empty.URL,
		"SEARCH_JINA_API_KEY":   "",
		"SEARCH_TAVILY_API_KEY": "unused",
	}
	// jina has no key, so the chain is ddgs then searxng; neither answers.
	c := SearchFromConfig(func(k, _ string, _ bool) string { return env[k] })
	if _, err := c.Search(context.Background(), "acme", 3); err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("err = %v", err)
	}
	env["SEARCH_PROVIDER"] = "ddgs,searxng"
	env["SEARCH_SEARXNG_URL"] = up.URL
	res, err := SearchFromConfig(func(k, _ string, _ bool) string { return env[k] }).Search(context.Background(), "acme", 3)
	if err != nil || len(res) != 1 {
		t.Fatalf("res %v, err %v", res, err)
	}
}

func TestSingleProviderSettingsStillWork(t *testing.T) {
	env := map[string]string{"SEARCH_PROVIDER": "serper", "SEARCH_API_KEY": "k"}
	if c, ok := SearchFromConfig(func(k, _ string, _ bool) string { return env[k] }).(*serperClient); !ok || c.apiKey != "k" {
		t.Fatalf("serper from the old settings: %#v", c)
	}
}
