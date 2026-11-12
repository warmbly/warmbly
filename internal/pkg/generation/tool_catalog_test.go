package generation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func catalogTestTools(n int) []ToolDef {
	tools := make([]ToolDef, n)
	for i := range tools {
		tools[i] = ToolDef{
			Name:        fmt.Sprintf("tool_%04d", i),
			Description: "Read a workspace record",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
			Risk:        RiskRead,
			Handler:     func(context.Context, json.RawMessage) (string, error) { return `{}`, nil },
		}
	}
	return tools
}

func findCatalogTools(t *testing.T, c *toolCatalog, query string, offset int) toolSearchResult {
	t.Helper()
	args, err := json.Marshal(map[string]any{"query": query, "offset": offset})
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.search.Handler(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	var result toolSearchResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestToolCatalogThreshold(t *testing.T) {
	for _, count := range []int{0, 1, 127, 128, 129, 138, 1024} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			c := newToolCatalog(catalogTestTools(count), nil)
			if count <= 128 {
				if c != nil {
					t.Fatal("a catalog within the limit should be passed through")
				}
				return
			}
			if c == nil || len(c.definitions()) != 1 {
				t.Fatal("oversized catalogs should initially expose only discovery")
			}
		})
	}
}

func TestToolCatalogEveryToolIsReachableWithBoundedPages(t *testing.T) {
	tools := catalogTestTools(1024)
	c := newToolCatalog(tools, nil)
	seen := map[string]bool{}
	for offset := 0; ; {
		result := findCatalogTools(t, c, "", offset)
		if result.TotalMatches != len(tools) {
			t.Fatalf("total = %d, want %d", result.TotalMatches, len(tools))
		}
		defs := c.definitions()
		if len(defs) > maxLoadedTools+1 {
			t.Fatalf("wire catalog size = %d", len(defs))
		}
		onWire := map[string]ToolDef{}
		for _, d := range defs {
			onWire[d.Name] = d
		}
		for _, match := range result.Tools {
			d, ok := onWire[match.Name]
			if seen[match.Name] || !ok || d.InputSchema == nil || d.Handler == nil {
				t.Fatalf("duplicate or unloaded definition: %s", match.Name)
			}
			seen[match.Name] = true
		}
		if result.NextOffset == nil {
			break
		}
		if *result.NextOffset <= offset {
			t.Fatal("pagination did not advance")
		}
		offset = *result.NextOffset
	}
	if len(seen) != len(tools) {
		t.Fatalf("reachable tools = %d, want %d", len(seen), len(tools))
	}
}

func TestToolCatalogSearchAndValidation(t *testing.T) {
	tools := catalogTestTools(138)
	tools[137].Name = "mcp_crm_find_customer"
	tools[137].Description = "Look up a billing subscription"
	c := newToolCatalog(tools, nil)
	for _, query := range []string{"MCP_CRM_FIND_CUSTOMER", "subscription"} {
		result := findCatalogTools(t, c, query, 0)
		if len(result.Tools) != 1 || result.Tools[0].Name != tools[137].Name {
			t.Fatalf("search %q = %+v", query, result)
		}
	}
	if result := findCatalogTools(t, c, "workspace customer", 0); result.Tools[0].Name != tools[137].Name {
		t.Fatal("a name match should rank ahead of a description-only match")
	}
	for _, args := range []string{`{`, `{"query":42}`, `{"offset":-1}`} {
		if _, err := c.find(context.Background(), json.RawMessage(args)); err == nil {
			t.Fatalf("invalid arguments accepted: %s", args)
		}
	}
	for _, tc := range []struct {
		query  string
		offset int
	}{{"no_such_tool", 0}, {"", 9999}} {
		before := len(c.definitions())
		result := findCatalogTools(t, c, tc.query, tc.offset)
		if len(result.Tools) != 0 || result.NextOffset != nil || len(c.definitions()) != before {
			t.Fatalf("an empty search should keep what is loaded: %+v", result)
		}
	}
}

func TestToolCatalogRestoresCurrentPermissionsAndAvoidsNameCollisions(t *testing.T) {
	tools := catalogTestTools(138)
	tools[0].Name = "search_available_tools"
	tools[1].Name = "search_available_tools_2"
	c := newToolCatalog(tools, nil)
	if c.search.Name != "search_available_tools_3" || !strings.Contains(c.instructions(), c.search.Name) {
		t.Fatalf("discovery name = %s", c.search.Name)
	}
	messages := []AgentMessage{
		{Role: "assistant", ToolCalls: []ToolCall{{Name: c.search.Name, Args: json.RawMessage(`{"query":"tool_0137"}`)}}},
		{Role: "tool", Content: `{"tools":[{"name":"forbidden_tool"}]}`},
	}
	restored := newToolCatalog(tools, messages)
	if len(restored.loaded) != 1 || restored.loaded[0].Name != tools[137].Name {
		t.Fatal("discovery was not restored from its arguments")
	}
	messages = append(messages, AgentMessage{Role: "assistant", ToolCalls: []ToolCall{{Name: tools[136].Name}, {Name: "forbidden_tool"}}})
	restored = newToolCatalog(tools, messages)
	if len(restored.loaded) != 2 || restored.loaded[0].Name != tools[136].Name || restored.loaded[1].Name != tools[137].Name {
		t.Fatal("recently used tools were not restored, most recent first")
	}
	tools = tools[:137]
	restored = newToolCatalog(tools, messages[:2])
	if len(restored.loaded) != 0 {
		t.Fatal("a tool removed from the current registry was restored")
	}
}

func TestToolCatalogKeepsCoreToolsAndBoundsDiscovered(t *testing.T) {
	tools := catalogTestTools(300)
	for i := range tools {
		tools[i].Deferred = i >= 10
	}
	c := newToolCatalog(tools, nil)
	if c == nil || len(c.definitions()) != 11 {
		t.Fatal("deferred tools should leave only the core set and discovery on the wire")
	}
	for offset := 0; offset < 300; offset += toolSearchPageSize {
		findCatalogTools(t, c, "", offset)
		defs := c.definitions()
		if len(defs) > 10+maxLoadedTools+1 {
			t.Fatalf("wire catalog grew to %d", len(defs))
		}
		for i := 0; i < 10; i++ {
			if defs[i].Name != tools[i].Name {
				t.Fatal("a core tool left the wire after discovery")
			}
		}
	}
	if len(newToolCatalog(tools[:10], nil).definitionsOrNil()) != 0 {
		t.Fatal("a set with nothing deferred should be passed through")
	}
}

func (c *toolCatalog) definitionsOrNil() []ToolDef {
	if c == nil {
		return nil
	}
	return c.definitions()
}
