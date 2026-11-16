package generation

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	// maxWireTools is OpenAI's per-request tool limit, held for every provider.
	maxWireTools       = 128
	toolSearchPageSize = 20
	// maxLoadedTools bounds the discovered tools kept on the wire beside the core set.
	maxLoadedTools = 40
)

// toolCatalog keeps deferred tools off the wire until the model searches for
// them, without changing how any tool executes or is approved.
type toolCatalog struct {
	all    []ToolDef
	core   []ToolDef
	loaded []ToolDef // most recently found or used first
	search ToolDef
}

// newToolCatalog returns nil when every tool can be sent as is. Tools marked
// Deferred wait for discovery; an unmarked set over the wire limit defers all of them.
func newToolCatalog(tools []ToolDef, messages []AgentMessage) *toolCatalog {
	deferred := false
	for _, t := range tools {
		deferred = deferred || t.Deferred
	}
	if !deferred && len(tools) <= maxWireTools {
		return nil
	}
	c := &toolCatalog{all: tools}
	byName := make(map[string]ToolDef, len(tools))
	for _, t := range tools {
		byName[t.Name] = t
		if deferred && !t.Deferred {
			c.core = append(c.core, t)
		}
	}
	if len(c.core) > maxWireTools-maxLoadedTools-1 {
		c.core = c.core[:maxWireTools-maxLoadedTools-1]
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
		Description: "Find more tools by keywords in their names or descriptions, and load up to 20 matches for your next turn. Use it whenever the tools you can see do not cover the task: there are many more for contacts, campaigns, sequences, mailboxes, warmup, the inbox, CRM, segments, forms, automations, settings and connected apps. An empty query browses everything; next_offset pages on. Discovery does not run anything.",
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

	// Replay discovery and use in order, so a resumed run sees the tools it had, limited to what is permitted now.
	for _, m := range messages {
		for _, call := range m.ToolCalls {
			if call.Name == c.search.Name {
				_, _ = c.find(context.Background(), call.Args)
			} else if tool, ok := byName[call.Name]; ok {
				c.load(tool)
			}
		}
	}
	return c
}

// load puts a deferred tool at the front of the loaded set, evicting the least recent past the bound.
func (c *toolCatalog) load(tool ToolDef) {
	for _, t := range c.core {
		if t.Name == tool.Name {
			return
		}
	}
	kept := []ToolDef{tool}
	for _, t := range c.loaded {
		if t.Name != tool.Name && len(kept) < maxLoadedTools {
			kept = append(kept, t)
		}
	}
	c.loaded = kept
}

func (c *toolCatalog) definitions() []ToolDef {
	out := make([]ToolDef, 0, len(c.core)+len(c.loaded)+1)
	out = append(out, c.core...)
	out = append(out, c.loaded...)
	return append(out, c.search)
}

func (c *toolCatalog) instructions() string {
	return fmt.Sprintf("\n\nOnly some of your tools are listed; the rest load through %s. If a needed tool is not listed, search for it before acting or saying something cannot be done. Search by short keywords, or browse with an empty query and pagination. Found tools stay loaded for the following turns; call them by their names with their documented arguments.", c.search.Name)
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

// find returns matching tools with their argument schemas and loads them for the next turn.
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
	page := matches[start:end]
	for i := len(page) - 1; i >= 0; i-- {
		c.load(page[i].tool)
	}
	for _, match := range page {
		tool := match.tool
		result.Tools = append(result.Tools, toolSearchMatch{Name: tool.Name, Description: tool.Description, Risk: tool.Risk, InputSchema: tool.InputSchema})
	}
	out, err := json.Marshal(result)
	return string(out), err
}
