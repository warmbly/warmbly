package generation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrSearchNotConfigured is returned by the search_web tool path when no search
// backend is configured, so the agent gets a clean "web search is not
// available" instead of an opaque failure.
var ErrSearchNotConfigured = errors.New("web search is not configured")

// SearchResult is one normalized web-search hit, provider-agnostic.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// SearchClient is the pluggable web-search backend the search_web tool (M2)
// calls. Kept an interface so the hosted product and self-hosters can swap
// backends (Serper, SearXNG, DDGS, Jina, Brave, Tavily) without touching the tool.
type SearchClient interface {
	Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
}

// SearchProviders are the web-search backends NewSearchClient speaks.
var SearchProviders = []string{"serper", "searxng", "ddgs", "jina", "brave", "tavily"}

// NewSearchClient builds the configured search backend, or nil when search is
// disabled (empty provider). apiURL overrides the backend endpoint (required for
// the self-hosted searxng and ddgs); apiKey is the backend key (serper, brave
// and tavily need one, jina search does too).
func NewSearchClient(provider, apiURL, apiKey string) SearchClient {
	provider = strings.ToLower(strings.TrimSpace(provider))
	endpoint := strings.TrimRight(strings.TrimSpace(apiURL), "/")
	apiKey = strings.TrimSpace(apiKey)
	hc := &http.Client{Timeout: 15 * time.Second}
	switch provider {
	case "serper":
		if endpoint == "" {
			endpoint = "https://google.serper.dev/search"
		}
		if apiKey == "" {
			return nil
		}
		return &serperClient{endpoint: endpoint, apiKey: apiKey, http: hc}
	case "searxng":
		if endpoint == "" {
			return nil
		}
		return &searxngClient{endpoint: endpoint, http: hc}
	case "ddgs":
		if endpoint == "" {
			return nil
		}
		return &ddgsClient{endpoint: endpoint, http: &http.Client{Timeout: 25 * time.Second}}
	case "jina":
		if endpoint == "" {
			endpoint = "https://s.jina.ai"
		}
		if apiKey == "" {
			return nil
		}
		return &jinaSearchClient{endpoint: endpoint, apiKey: apiKey, http: &http.Client{Timeout: 25 * time.Second}}
	case "brave":
		if endpoint == "" {
			endpoint = "https://api.search.brave.com/res/v1/web/search"
		}
		if apiKey == "" {
			return nil
		}
		return &braveClient{endpoint: endpoint, apiKey: apiKey, http: hc}
	case "tavily":
		if endpoint == "" {
			endpoint = "https://api.tavily.com/search"
		}
		if apiKey == "" {
			return nil
		}
		return &tavilyClient{endpoint: endpoint, apiKey: apiKey, http: hc}
	default:
		return nil
	}
}

// SearchBackend is one configured search provider.
type SearchBackend struct {
	Provider string
	URL      string
	Key      string
}

// NewSearchChain builds the backends in order, skipping any missing what it
// needs; a backend that errors or finds nothing hands the query to the next.
func NewSearchChain(backends []SearchBackend) SearchClient {
	var cs []SearchClient
	for _, b := range backends {
		if c := NewSearchClient(b.Provider, b.URL, b.Key); c != nil {
			cs = append(cs, c)
		}
	}
	switch len(cs) {
	case 0:
		return nil
	case 1:
		return cs[0]
	}
	return searchChain(cs)
}

// SearchLookup reads one setting: an env var, or its AWS parameter or secret.
type SearchLookup func(envKey, awsKey string, secret bool) string

// SearchFromConfig builds web search from SEARCH_PROVIDER, a comma-separated
// list tried in order ("ddgs,jina"). Each provider reads SEARCH_<PROVIDER>_URL
// and SEARCH_<PROVIDER>_API_KEY; the first also falls back to SEARCH_API_URL
// and SEARCH_API_KEY, which is all a single-provider setup ever needed.
func SearchFromConfig(get SearchLookup) SearchClient {
	var backends []SearchBackend
	seen := map[string]bool{}
	for _, p := range strings.Split(get("SEARCH_PROVIDER", "search/provider", false), ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" || seen[p] {
			continue
		}
		up := strings.ToUpper(p)
		b := SearchBackend{
			Provider: p,
			URL:      get("SEARCH_"+up+"_URL", "search/"+p+"_url", false),
			Key:      get("SEARCH_"+up+"_API_KEY", "search/"+p+"_api_key", true),
		}
		if len(seen) == 0 {
			if b.URL == "" {
				b.URL = get("SEARCH_API_URL", "search/api_url", false)
			}
			if b.Key == "" {
				b.Key = get("SEARCH_API_KEY", "search/api_key", true)
			}
		}
		seen[p] = true
		backends = append(backends, b)
	}
	return NewSearchChain(backends)
}

type searchChain []SearchClient

func (c searchChain) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	var errs []error
	for _, s := range c {
		if ctx.Err() != nil {
			break
		}
		res, err := s.Search(ctx, query, limit)
		if err == nil && len(res) > 0 {
			return res, nil
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil, ctx.Err()
	}
	return nil, errors.Join(errs...)
}

func clampSearchLimit(limit int) int {
	if limit <= 0 {
		return 5
	}
	if limit > 10 {
		return 10
	}
	return limit
}

// serperClient calls Serper.dev's Google Search JSON API.
type serperClient struct {
	endpoint string
	apiKey   string
	http     *http.Client
}

func (c *serperClient) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	limit = clampSearchLimit(limit)
	body, _ := json.Marshal(map[string]any{"q": query, "num": limit})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("search: serper status %d", resp.StatusCode)
	}

	var parsed struct {
		Organic []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
		} `json:"organic"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("search: decode serper response: %w", err)
	}
	out := make([]SearchResult, 0, len(parsed.Organic))
	for _, o := range parsed.Organic {
		out = append(out, SearchResult{Title: o.Title, URL: o.Link, Snippet: o.Snippet})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// searxngClient calls a self-hosted SearXNG JSON endpoint. This is the
// preferred fully self-hosted, no-vendor path.
type searxngClient struct {
	endpoint string
	http     *http.Client
}

func (c *searxngClient) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	limit = clampSearchLimit(limit)
	q := url.Values{}
	q.Set("q", query)
	q.Set("format", "json")
	reqURL := c.endpoint + "/search?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("search: searxng status %d", resp.StatusCode)
	}

	var parsed struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("search: decode searxng response: %w", err)
	}
	out := make([]SearchResult, 0, limit)
	for _, r := range parsed.Results {
		out = append(out, SearchResult{Title: r.Title, URL: r.URL, Snippet: r.Content})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// searchJSON sends req and decodes a 2xx JSON answer into out.
func searchJSON(c *http.Client, req *http.Request, name string, out any) error {
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("search: %s status %d", name, resp.StatusCode)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("search: decode %s response: %w", name, err)
	}
	return nil
}

// ddgsClient calls a DDGS API server (`ddgs api`), the keyless metasearch
// most self-hosters already run.
type ddgsClient struct {
	endpoint string
	http     *http.Client
}

func (c *ddgsClient) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	limit = clampSearchLimit(limit)
	q := url.Values{"query": {query}, "max_results": {strconv.Itoa(limit)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/search/text?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	var parsed struct {
		Results []struct {
			Title string `json:"title"`
			Href  string `json:"href"`
			URL   string `json:"url"`
			Body  string `json:"body"`
		} `json:"results"`
	}
	if err := searchJSON(c.http, req, "ddgs", &parsed); err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, limit)
	for _, r := range parsed.Results {
		u := r.Href
		if u == "" {
			u = r.URL
		}
		out = append(out, SearchResult{Title: r.Title, URL: u, Snippet: r.Body})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// jinaSearchClient calls Jina's search endpoint (s.jina.ai).
type jinaSearchClient struct {
	endpoint string
	apiKey   string
	http     *http.Client
}

func (c *jinaSearchClient) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	limit = clampSearchLimit(limit)
	q := url.Values{"q": {query}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	// Titles, links and descriptions only; the pages themselves are not needed.
	req.Header.Set("X-Respond-With", "no-content")
	var parsed struct {
		Data []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
			Content     string `json:"content"`
		} `json:"data"`
	}
	if err := searchJSON(c.http, req, "jina", &parsed); err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, limit)
	for _, r := range parsed.Data {
		snip := r.Description
		if snip == "" {
			snip = r.Content
		}
		out = append(out, SearchResult{Title: r.Title, URL: r.URL, Snippet: snip})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// braveClient calls the Brave Search API.
type braveClient struct {
	endpoint string
	apiKey   string
	http     *http.Client
}

func (c *braveClient) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	limit = clampSearchLimit(limit)
	q := url.Values{"q": {query}, "count": {strconv.Itoa(limit)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", c.apiKey)
	var parsed struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := searchJSON(c.http, req, "brave", &parsed); err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, limit)
	for _, r := range parsed.Web.Results {
		out = append(out, SearchResult{Title: r.Title, URL: r.URL, Snippet: r.Description})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// tavilyClient calls Tavily's search API.
type tavilyClient struct {
	endpoint string
	apiKey   string
	http     *http.Client
}

func (c *tavilyClient) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	limit = clampSearchLimit(limit)
	body, _ := json.Marshal(map[string]any{"query": query, "max_results": limit})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	var parsed struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := searchJSON(c.http, req, "tavily", &parsed); err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, limit)
	for _, r := range parsed.Results {
		out = append(out, SearchResult{Title: r.Title, URL: r.URL, Snippet: r.Content})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// FormatSearchResults renders results as a compact numbered list for feeding
// back to the model as a tool result. Exposed so the search_web tool (M2) and
// the research agent (M5) render identically.
func FormatSearchResults(results []SearchResult) string {
	if len(results) == 0 {
		return "No results."
	}
	var b strings.Builder
	for i, r := range results {
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(". ")
		b.WriteString(r.Title)
		b.WriteString("\n   ")
		b.WriteString(r.URL)
		if r.Snippet != "" {
			b.WriteString("\n   ")
			b.WriteString(r.Snippet)
		}
		b.WriteString("\n")
	}
	return b.String()
}
