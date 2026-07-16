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
// backends (Serper, SearXNG, or a custom endpoint) without touching the tool.
type SearchClient interface {
	Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
}

// NewSearchClient builds the configured search backend, or nil when search is
// disabled (empty provider). provider is one of "serper" or "searxng"; apiURL
// overrides the backend endpoint (optional for serper, required for searxng);
// apiKey is the backend key (serper).
func NewSearchClient(provider, apiURL, apiKey string) SearchClient {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case "serper":
		endpoint := strings.TrimSpace(apiURL)
		if endpoint == "" {
			endpoint = "https://google.serper.dev/search"
		}
		if apiKey == "" {
			return nil
		}
		return &serperClient{endpoint: endpoint, apiKey: apiKey, http: &http.Client{Timeout: 15 * time.Second}}
	case "searxng":
		endpoint := strings.TrimSpace(apiURL)
		if endpoint == "" {
			return nil
		}
		return &searxngClient{endpoint: strings.TrimRight(endpoint, "/"), http: &http.Client{Timeout: 15 * time.Second}}
	default:
		return nil
	}
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
