package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

func tool(name, schema string) models.MCPTool {
	return models.MCPTool{Name: name, InputSchema: json.RawMessage(schema)}
}

func TestSearchArgsFindsTheQueryAndLimit(t *testing.T) {
	cases := []struct {
		schema       string
		query, limit string
		ok           bool
	}{
		// The DDGS MCP server's search_text.
		{`{"type":"object","properties":{"query":{"type":"string"},"region":{"type":"string"},"max_results":{"type":"integer"}},"required":["query"]}`, "query", "max_results", true},
		{`{"properties":{"q":{"type":"string"},"count":{"type":"number"}}}`, "q", "count", true},
		// One required string is the query whatever it is called.
		{`{"properties":{"input":{"type":"string"}},"required":["input"]}`, "input", "", true},
		{`{"properties":{"url":{"type":"string"},"selector":{"type":"string"}},"required":["url","selector"]}`, "", "", false},
		// Something else required that a search cannot fill in.
		{`{"properties":{"query":{"type":"string"},"index":{"type":"string"}},"required":["query","index"]}`, "", "", false},
	}
	for _, c := range cases {
		q, l, ok := searchArgs(tool("t", c.schema))
		if q != c.query || l != c.limit || ok != c.ok {
			t.Errorf("%s: got %q %q %v", c.schema, q, l, ok)
		}
	}
}

func TestParseSearchTextReadsCommonShapes(t *testing.T) {
	ddgs := `[{"title":"Acme Plumbing","href":"https://acme.com/","body":"Boise plumbers"},{"title":"Acme on Yelp","href":"https://yelp.com/biz/acme","body":"Reviews"}]`
	if got := parseSearchText(ddgs); len(got) != 2 || got[0].URL != "https://acme.com/" || got[0].Snippet != "Boise plumbers" || got[0].Title != "Acme Plumbing" {
		t.Fatalf("ddgs: %+v", got)
	}
	nested := `{"data":{"results":[{"name":"Acme","url":"https://acme.com/","description":"x"}]}}`
	if got := parseSearchText(nested); len(got) != 1 || got[0].Title != "Acme" {
		t.Fatalf("nested: %+v", got)
	}
	text := "1. Acme Plumbing - https://acme.com/contact.\n2. Write to office@acme.com, see https://acme.com/contact"
	if got := parseSearchText(text); len(got) != 1 || got[0].URL != "https://acme.com/contact" || !strings.Contains(got[0].Snippet, "office@acme.com") {
		t.Fatalf("a duplicate link dropped its text: %+v", got)
	}
	multi := "Acme Plumbing\nhttps://acme.com/\nEmail: office@acme.com\n\nOther result https://other.example/"
	if got := parseSearchText(multi); len(got) != 2 || !strings.Contains(got[0].Snippet, "office@acme.com") {
		t.Fatalf("multiline: %+v", got)
	}
	envelope := `{"url":"https://search.example/?q=acme","results":[{"title":"Acme","url":"https://acme.com/","snippet":"office@acme.com"}]}`
	if got := parseSearchText(envelope); len(got) != 1 || got[0].URL != "https://acme.com/" {
		t.Fatalf("envelope: %+v", got)
	}
}

func TestSearchCallKeepsTheCountInRange(t *testing.T) {
	_, limit, counts, ok := searchCall(tool("t", `{"properties":{"query":{"type":"string"},"max_results":{"type":"integer","minimum":5.5,"maximum":20}},"required":["query"]}`))
	if !ok || limit != "max_results" || counts.clamp(5) != 6 || counts.clamp(50) != 20 {
		t.Fatalf("limit %q counts %+v ok %v", limit, counts, ok)
	}
	_, _, small, _ := searchCall(tool("t", `{"properties":{"q":{"type":"string"},"limit":{"type":"integer","maximum":3}}}`))
	if small.clamp(5) != 3 {
		t.Fatalf("a tool allowing three got %d", small.clamp(5))
	}
}

func TestEnvelopesAndHitsAreToldApart(t *testing.T) {
	wrapped := `{"url":"https://search.example/?q=acme","data":{"results":[{"url":"https://acme.com/","snippet":"office@acme.com"}]}}`
	if got := parseSearchText(wrapped); len(got) != 1 || got[0].URL != "https://acme.com/" {
		t.Fatalf("wrapped: %+v", got)
	}
	related := `[{"url":"https://acme.com/","title":"Acme","snippet":"office@acme.com","related":[{"url":"https://yelp.com/biz/acme"}]}]`
	if got := parseSearchText(related); len(got) != 1 || got[0].URL != "https://acme.com/" || got[0].Snippet != "office@acme.com" {
		t.Fatalf("related: %+v", got)
	}
}

func TestTextSnippetsJoinMatchingStructuredResults(t *testing.T) {
	structured := parseSearchJSON([]byte(`{"results":[{"url":"https://acme.com/","title":"Acme"}]}`))
	text := parseSearchText("Acme https://acme.com/ office@acme.com\nUnrelated https://other.example/")
	got := dedupe(append(structured, keepMatching(text, structured)...))
	if len(got) != 1 || !strings.Contains(got[0].Snippet, "office@acme.com") {
		t.Fatalf("merged: %+v", got)
	}
}

func TestOnlyAToolThatTakesAQueryCanBeTheSearch(t *testing.T) {
	sv := &models.MCPServer{DiscoveredTools: []models.MCPTool{
		tool("search_text", `{"properties":{"query":{"type":"string"}},"required":["query"]}`),
		tool("extract_content", `{"properties":{"url":{"type":"string"},"format":{"type":"string"}},"required":["url","format"]}`),
	}}
	if err := validSearchTool(sv, "search_text"); err != nil {
		t.Fatal(err)
	}
	if validSearchTool(sv, "extract_content") == nil || validSearchTool(sv, "missing") == nil {
		t.Fatal("a tool that cannot search was accepted")
	}
}

func TestDuplicateLinksKeepEveryTitle(t *testing.T) {
	got := dedupe([]generation.SearchResult{
		{URL: "https://acme.com/", Title: "Acme Plumbing"},
		{URL: "https://acme.com/", Title: "Contact office@acme.com"},
	})
	if len(got) != 1 || got[0].Title != "Acme Plumbing" || !strings.Contains(got[0].Snippet, "office@acme.com") {
		t.Fatalf("got %+v", got)
	}
}
