package generation

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	openAIMaxTools     = 128
	toolSearchPageSize = 20
)

// toolCatalog exposes oversized registries in bounded pages without changing execution or approval.
type toolCatalog struct {
	all    []ToolDef
	active []ToolDef
	search ToolDef
}

func newToolCatalog(tools []ToolDef, messages []AgentMessage) *toolCatalog {
	if len(tools) <= openAIMaxTools {
		return nil
	}
	c := &toolCatalog{all: tools}
	byName := make(map[string]ToolDef, len(tools))
	for _, t := range tools {
		byName[t.Name] = t
	}
	name := "search_available_tools"
	for i := 2; ; i++ {
		if _, exists := byName[name]; !exists {
			break
		}
		name = fmt.Sprintf("search_available_tools_%d", i)
	}
	c.search = ToolDef{
		Name:        name,
		Description: "Find available tools by keywords in their names or descriptions. Loads up to 20 matching tools for your next turn, replacing the previous selection. Use an empty query to browse all tools; use next_offset to get the next page. Discovery does not execute the tools.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":  map[string]any{"type": "string", "description": "Tool name or space-separated keywords. Empty lists all tools."},
				"offset": map[string]any{"type": "integer", "minimum": 0, "description": "Result offset, default 0."},
			},
			"required":             []string{"query"},
			"additionalProperties": false,
		},
		Risk:    RiskRead,
		Handler: c.find,
	}

	// Restore only tools still permitted in this invocation, including after an approval pause.
	seen := map[string]bool{}
	for i := len(messages) - 1; i >= 0 && len(c.active) < toolSearchPageSize; i-- {
		for j := len(messages[i].ToolCalls) - 1; j >= 0 && len(c.active) < toolSearchPageSize; j-- {
			call := messages[i].ToolCalls[j]
			if call.Name == c.search.Name {
				if len(c.active) == 0 {
					_, _ = c.find(context.Background(), call.Args)
				}
				return c
			}
			if tool, ok := byName[call.Name]; ok && !seen[call.Name] {
				c.active = append(c.active, tool)
				seen[call.Name] = true
			}
		}
	}
	return c
}

func (c *toolCatalog) definitions() []ToolDef {
	return append(append([]ToolDef(nil), c.active...), c.search)
}

func (c *toolCatalog) instructions() string {
	return fmt.Sprintf("\n\nThe full tool catalog is available through %s. If a needed tool is not currently listed, search for it before acting or saying it is unavailable. Search by short keywords, or browse with an empty query and pagination. Each search loads a new page of tools for the next turn; call those tools by their original names with their documented arguments.", c.search.Name)
}

type toolSearchMatch struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Risk        RiskClass      `json:"risk"`
	InputSchema map[string]any `json:"input_schema"`
}

type toolSearchResult struct {
	Tools        []toolSearchMatch `json:"tools"`
	TotalMatches int               `json:"total_matches"`
	NextOffset   *int              `json:"next_offset,omitempty"`
}

// find preserves matching argument schemas in the transcript while replacing the active tool page.
func (c *toolCatalog) find(_ context.Context, args json.RawMessage) (string, error) {
	var input struct {
		Query  string `json:"query"`
		Offset int    `json:"offset"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("invalid tool search arguments: %w", err)
	}
	if input.Offset < 0 {
		return "", fmt.Errorf("tool search offset must be non-negative")
	}
	type rankedTool struct {
		tool  ToolDef
		score int
	}
	terms := strings.Fields(strings.ToLower(input.Query))
	var matches []rankedTool
	for _, tool := range c.all {
		name := strings.ToLower(tool.Name)
		text := name + " " + strings.ToLower(tool.Description)
		score := 0
		for _, term := range terms {
			if strings.Contains(text, term) {
				score++
			}
			if strings.Contains(name, term) {
				score++
			}
		}
		if len(terms) == 0 || score > 0 {
			matches = append(matches, rankedTool{tool: tool, score: score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	start := min(input.Offset, len(matches))
	end := start + min(toolSearchPageSize, len(matches)-start)
	result := toolSearchResult{Tools: make([]toolSearchMatch, 0, end-start), TotalMatches: len(matches)}
	if end < len(matches) {
		result.NextOffset = &end
	}
	c.active = nil
	for _, match := range matches[start:end] {
		tool := match.tool
		c.active = append(c.active, tool)
		result.Tools = append(result.Tools, toolSearchMatch{Name: tool.Name, Description: tool.Description, Risk: tool.Risk, InputSchema: tool.InputSchema})
	}
	out, err := json.Marshal(result)
	return string(out), err
}
