package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// queryArgs and limitArgs are the argument names search tools take, best first.
var (
	queryArgs = []string{"query", "q", "search_query", "search", "keywords", "term", "text", "prompt"}
	limitArgs = []string{"max_results", "limit", "count", "num_results", "num", "maxResults", "numResults", "top_k", "n"}
)

// searchArgs works out how to call a tool as a web search: the argument that
// takes the query and, when it has one, the argument that bounds the results.
func searchArgs(t models.MCPTool) (query, limit string, ok bool) {
	query, limit, _, ok = searchCall(t)
	return query, limit, ok
}

// countRange is the result counts a tool accepts; zero means unbounded.
type countRange struct{ min, max int }

// clamp fits n into the range.
func (r countRange) clamp(n int) int {
	if r.min > 0 && n < r.min {
		n = r.min
	}
	if r.max > 0 && n > r.max {
		n = r.max
	}
	return n
}

// searchCall is searchArgs plus the result counts the tool accepts.
func searchCall(t models.MCPTool) (query, limit string, counts countRange, ok bool) {
	var schema struct {
		Properties map[string]struct {
			Type    any      `json:"type"`
			Minimum *float64 `json:"minimum"`
			Maximum *float64 `json:"maximum"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if len(t.InputSchema) > 0 {
		_ = json.Unmarshal(t.InputSchema, &schema)
	}
	isType := func(name, want string) bool {
		p, ok := schema.Properties[name]
		if !ok {
			return false
		}
		switch v := p.Type.(type) {
		case string:
			return v == want
		case []any:
			for _, x := range v {
				if s, _ := x.(string); s == want {
					return true
				}
			}
		case nil:
			return true
		}
		return false
	}
	for _, name := range queryArgs {
		if isType(name, "string") {
			query = name
			break
		}
	}
	if query == "" {
		// A tool whose only required argument is a string takes the query there.
		var strs []string
		for _, r := range schema.Required {
			if isType(r, "string") {
				strs = append(strs, r)
			}
		}
		if len(strs) != 1 {
			return "", "", countRange{}, false
		}
		query = strs[0]
	}
	for _, name := range limitArgs {
		if isType(name, "integer") || isType(name, "number") {
			limit = name
			if m := schema.Properties[name].Minimum; m != nil && *m > 0 {
				counts.min = int(math.Ceil(*m))
			}
			if m := schema.Properties[name].Maximum; m != nil && *m >= 1 {
				counts.max = int(math.Floor(*m))
			}
			break
		}
	}
	for _, r := range schema.Required {
		if r != query && r != limit {
			// Something else is required that a search cannot supply.
			return "", "", countRange{}, false
		}
	}
	return query, limit, counts, true
}

// markSearchable flags the server's tools that can be its web search.
func markSearchable(sv *models.MCPServer) {
	for i := range sv.DiscoveredTools {
		_, _, sv.DiscoveredTools[i].SearchCapable = searchArgs(sv.DiscoveredTools[i])
	}
}

// validSearchTool checks a server can serve tool as a web search.
func validSearchTool(sv *models.MCPServer, tool string) error {
	for _, t := range sv.DiscoveredTools {
		if t.Name != tool {
			continue
		}
		if _, _, ok := searchArgs(t); !ok {
			return errors.New("that tool does not take a search query")
		}
		return nil
	}
	return errors.New("the server has no tool by that name")
}

// WebSearch is the workspace's web search through the tools its enabled MCP
// servers offer as one, nil when none does.
func (s *service) WebSearch(ctx context.Context, orgID uuid.UUID) generation.SearchClient {
	servers, err := s.repo.List(ctx, orgID, true)
	if err != nil {
		return nil
	}
	var backends []generation.SearchClient
	for i := range servers {
		sv := servers[i]
		if sv.SearchTool == "" {
			continue
		}
		for _, t := range sv.DiscoveredTools {
			if t.Name != sv.SearchTool {
				continue
			}
			if q, l, m, ok := searchCall(t); ok {
				backends = append(backends, &toolSearch{s: s, server: sv, tool: t.Name, queryArg: q, limitArg: l, counts: m})
			}
		}
	}
	switch len(backends) {
	case 0:
		return nil
	case 1:
		return backends[0]
	}
	return toolSearches(backends)
}

type toolSearches []generation.SearchClient

func (ts toolSearches) Search(ctx context.Context, query string, limit int) ([]generation.SearchResult, error) {
	var errs []error
	for _, t := range ts {
		res, err := t.Search(ctx, query, limit)
		if err == nil && len(res) > 0 {
			return res, nil
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return nil, errors.Join(errs...)
}

// toolSearch calls one MCP tool as a web search.
type toolSearch struct {
	s        *service
	server   models.MCPServer
	tool     string
	queryArg string
	limitArg string
	counts   countRange
}

func (t *toolSearch) Search(ctx context.Context, query string, limit int) ([]generation.SearchResult, error) {
	if limit <= 0 || limit > 10 {
		limit = 5
	}
	args := map[string]any{t.queryArg: query}
	if t.limitArg != "" {
		args[t.limitArg] = t.counts.clamp(limit)
	}
	raw, _ := json.Marshal(args)
	token, err := t.s.open(ctx, t.server.OrgID, t.server.CredentialsEncrypted)
	if err != nil {
		return nil, errors.New("mcp: the server's credentials could not be opened")
	}
	out, err := t.s.client.CallToolResult(ctx, t.server.URL, token, t.tool, raw)
	if err != nil {
		return nil, err
	}
	var res []generation.SearchResult
	if len(out.Structured) > 0 {
		res = parseSearchJSON(out.Structured)
	}
	if text := parseSearchText(out.Text); len(res) == 0 {
		res = text
	} else {
		// The text may carry what the structured results leave out, an address say.
		res = dedupe(append(res, keepMatching(text, res)...))
	}
	if len(res) > limit {
		res = res[:limit]
	}
	return res, nil
}

var (
	urlInText  = regexp.MustCompile(`https?://[^\s<>"'\)\]]+`)
	urlKeys    = []string{"url", "href", "link", "uri"}
	titleKeys  = []string{"title", "name"}
	snippetKey = []string{"body", "snippet", "description", "content", "text", "summary"}
)

// parseSearchText reads a search tool's answer: JSON results in any of the
// usual shapes, or else every link in the text with the text around it.
func parseSearchText(text string) []generation.SearchResult {
	if res := parseSearchJSON([]byte(strings.TrimSpace(text))); len(res) > 0 {
		return res
	}
	// Each link takes the text from its line up to the next link's line, at most three lines.
	lines := strings.Split(text, "\n")
	type hit struct{ url, line string }
	var hits []hit
	var starts []int
	for i, line := range lines {
		for _, u := range urlInText.FindAllString(line, -1) {
			hits = append(hits, hit{strings.TrimRight(u, ".,;:"), line})
			starts = append(starts, i)
		}
	}
	var out []generation.SearchResult
	for k, h := range hits {
		end := len(lines)
		if k+1 < len(hits) && starts[k+1] > starts[k] {
			end = starts[k+1]
		} else if k+1 < len(hits) {
			end = starts[k] + 1
		}
		end = min(end, starts[k]+3)
		block := strings.Join(lines[starts[k]:end], " ")
		out = append(out, generation.SearchResult{URL: h.url, Snippet: strings.Join(strings.Fields(strings.ReplaceAll(block, h.url, "")), " ")})
	}
	return dedupe(out)
}

// keepMatching is the results in from that share a link with one in to.
func keepMatching(from, to []generation.SearchResult) []generation.SearchResult {
	have := map[string]bool{}
	for _, r := range to {
		have[r.URL] = true
	}
	var out []generation.SearchResult
	for _, r := range from {
		if have[r.URL] {
			out = append(out, r)
		}
	}
	return out
}

// resultKeys name the lists an envelope keeps its hits under, best first.
var resultKeys = []string{"results", "items", "organic", "hits", "data", "web", "search_results", "value"}

// parseSearchJSON reads results out of JSON in any of the usual shapes.
func parseSearchJSON(raw []byte) []generation.SearchResult {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	var out []generation.SearchResult
	collectResults(v, &out, 0)
	return dedupe(out)
}

func collectResults(v any, out *[]generation.SearchResult, depth int) {
	if depth > 6 || len(*out) >= 50 {
		return
	}
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			collectResults(e, out, depth+1)
		}
	case map[string]any:
		// An envelope's result collection wins over the envelope's own link.
		before := len(*out)
		for _, k := range resultKeys {
			if e, ok := x[k]; ok {
				collectResults(e, out, depth+1)
			}
		}
		if len(*out) > before {
			return
		}
		// A hit with a link is one result; its related links are not.
		if u := pick(x, urlKeys); strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
			*out = append(*out, generation.SearchResult{URL: u, Title: pick(x, titleKeys), Snippet: pick(x, snippetKey)})
			return
		}
		for _, e := range x {
			collectResults(e, out, depth+1)
		}
	}
}

func pick(m map[string]any, keys []string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// dedupe keeps one result per link, keeping the text every copy carried.
func dedupe(in []generation.SearchResult) []generation.SearchResult {
	at := map[string]int{}
	var out []generation.SearchResult
	for _, r := range in {
		i, seen := at[r.URL]
		if !seen {
			at[r.URL] = len(out)
			out = append(out, r)
			continue
		}
		if out[i].Title == "" {
			out[i].Title = r.Title
		} else if r.Title != "" && r.Title != out[i].Title && !strings.Contains(out[i].Snippet, r.Title) {
			// A different title can carry what the kept one does not, an address say.
			out[i].Snippet = strings.TrimSpace(out[i].Snippet + " " + r.Title)
		}
		if r.Snippet != "" && !strings.Contains(out[i].Snippet, r.Snippet) {
			out[i].Snippet = strings.TrimSpace(out[i].Snippet + " " + r.Snippet)
		}
	}
	return out
}
