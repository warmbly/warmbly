package mcp

import (
	"encoding/json"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
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
	if got := parseSearchText(text); len(got) != 1 || got[0].URL != "https://acme.com/contact" || got[0].Snippet == "" {
		t.Fatalf("text: %+v", got)
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
