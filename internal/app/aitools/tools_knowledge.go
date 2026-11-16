package aitools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/warmbly/warmbly/internal/app/apicatalog"
	"github.com/warmbly/warmbly/internal/app/docsearch"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// apiResultBudget bounds an API response fed back to the model.
const apiResultBudget = 24000

// registerKnowledgeTools adds the docs and API bridge tools: how the product
// works, and every documented endpoint the caller may use.
func (d Deps) registerKnowledgeTools(r *Registry) {
	r.Register(Tool{
		Name: "search_docs",
		Description: "Search Warmbly's documentation for how a feature works: settings, limits, defaults, health bands, warmup, " +
			"deliverability, sequences, the inbox, CRM, integrations, billing, the API and self-hosting. Returns matching " +
			"sections with a snippet; read one in full with read_doc. Use it before explaining product behavior you are not sure about.",
		InputSchema: objectSchema(map[string]any{
			"query": strProp("What to look up, in a few keywords."),
			"area":  enumProp("Limit to one part of the docs.", "guides", "learn", "api", "development"),
			"limit": intProp("Results to return, default 8, max 20."),
		}, "query"),
		Risk:    generation.RiskRead,
		Handler: searchDocs,
	})
	r.Register(Tool{
		Name:        "read_doc",
		Description: "Read a Warmbly docs page found with search_docs, whole or one section. Long pages come in chunks; pass next_offset to continue.",
		InputSchema: objectSchema(map[string]any{
			"page":    strProp("Page path from search_docs, such as guides/warmup, or its docs.warmbly.com URL."),
			"section": strProp("Optional: only the sections whose heading contains this text."),
			"offset":  intProp("Character offset from a previous next_offset."),
		}, "page"),
		Risk:    generation.RiskRead,
		Handler: readDoc,
	})
	r.Register(Tool{
		Name: "find_api",
		Description: "Find Warmbly API endpoints by keywords, covering everything the dashboard can do: mailboxes, warmup, domains, " +
			"campaigns, A/B variants, sequences, contacts, imports, leads, segments, CRM, the inbox, templates, forms, " +
			"integrations, automations, placement, analytics and more. Use it when no dedicated tool covers a task, then " +
			"describe_api for the parameters and body, then api_read or api_write. With no query it lists the API's areas.",
		InputSchema: objectSchema(map[string]any{
			"query":  strProp("Keywords, such as 'reply template' or 'pause lead'. Empty lists areas."),
			"area":   strProp("Optional area (tag) from the area list, such as campaigns or mailboxes."),
			"offset": intProp("Result offset from a previous next_offset."),
		}),
		Risk:    generation.RiskRead,
		Handler: findAPI,
	})
	r.Register(Tool{
		Name:        "describe_api",
		Description: "Describe one API endpoint found with find_api: what it does, its path and query parameters, its JSON body schema and what it returns.",
		InputSchema: objectSchema(map[string]any{
			"method": enumProp("HTTP method.", "GET", "POST", "PUT", "PATCH", "DELETE"),
			"path":   strProp("The endpoint path, as a template (/campaigns/{id}) or concrete (/campaigns/7f9c...)."),
		}, "method", "path"),
		Risk:    generation.RiskRead,
		Handler: describeAPI,
	})
	r.Register(Tool{
		Name: "api_read",
		Description: "Call a read-only Warmbly API endpoint (any GET, or a POST that only searches or previews) as the current user, " +
			"with their permissions. Look the endpoint up with find_api and describe_api first. Returns the JSON response.",
		InputSchema: objectSchema(map[string]any{
			"method": enumProp("GET, or POST for a search or preview endpoint.", "GET", "POST"),
			"path":   strProp("Concrete path with ids filled in, such as /campaigns/7f9c.../steps. No query string."),
			"query":  queryProp(),
			"body":   bodyProp(),
		}, "path"),
		Risk:     generation.RiskRead,
		NeedsAPI: true,
		Handler:  callAPI(apicatalog.KindRead),
	})
	r.Register(Tool{
		Name: "api_write",
		Description: "Call a Warmbly API endpoint that changes data (POST, PUT, PATCH or DELETE) as the current user, with their " +
			"permissions. Prefer a dedicated tool when one exists. Look the endpoint up with find_api and describe_api first " +
			"and send exactly the documented body. Sending mail and starting campaigns are not available here; use their own tools.",
		InputSchema: objectSchema(map[string]any{
			"method": enumProp("HTTP method.", "POST", "PUT", "PATCH", "DELETE"),
			"path":   strProp("Concrete path with ids filled in, such as /campaigns/7f9c.../steps. No query string."),
			"query":  queryProp(),
			"body":   bodyProp(),
		}, "method", "path"),
		Risk:      generation.RiskWrite,
		AlwaysAsk: true,
		NeedsAPI:  true,
		Handler:   callAPI(apicatalog.KindWrite),
	})
}

func queryProp() map[string]any {
	return map[string]any{
		"type":                 "object",
		"description":          "Query parameters by name, such as {\"limit\": 20}.",
		"additionalProperties": map[string]any{"type": []any{"string", "number", "boolean"}},
	}
}

func bodyProp() map[string]any {
	return map[string]any{"type": []any{"object", "array"}, "description": "The JSON request body, as describe_api documents it."}
}

func searchDocs(_ context.Context, _ Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		Query string `json:"query"`
		Area  string `json:"area"`
		Limit int    `json:"limit"`
	}](args)
	if err != nil {
		return "", err
	}
	idx, ierr := docsearch.Default()
	if ierr != nil {
		return "", ierr
	}
	hits := idx.Search(in.Query, in.Area, in.Limit)
	if len(hits) == 0 {
		return jsonResult(map[string]any{"results": []any{}, "hint": "Nothing matched. Try fewer or different keywords, or browse the pages below.", "pages": idx.Pages(in.Area)})
	}
	return jsonResult(map[string]any{"results": hits})
}

func readDoc(_ context.Context, _ Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		Page    string `json:"page"`
		Section string `json:"section"`
		Offset  int    `json:"offset"`
	}](args)
	if err != nil {
		return "", err
	}
	idx, ierr := docsearch.Default()
	if ierr != nil {
		return "", ierr
	}
	page, rerr := idx.Read(in.Page, in.Section, in.Offset)
	if rerr != nil {
		return "", rerr
	}
	return jsonResult(page)
}

func findAPI(_ context.Context, _ Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		Query  string `json:"query"`
		Area   string `json:"area"`
		Offset int    `json:"offset"`
	}](args)
	if err != nil {
		return "", err
	}
	cat, cerr := apicatalog.Default()
	if cerr != nil {
		return "", cerr
	}
	if strings.TrimSpace(in.Query) == "" && in.Area == "" {
		type area struct {
			Area       string `json:"area"`
			Operations int    `json:"operations"`
		}
		var areas []area
		for tag, n := range cat.Tags() {
			areas = append(areas, area{tag, n})
		}
		sort.Slice(areas, func(i, j int) bool { return areas[i].Area < areas[j].Area })
		return jsonResult(map[string]any{"areas": areas, "hint": "Search with keywords, or pass an area to list its endpoints."})
	}
	return jsonResult(cat.Search(in.Query, in.Area, in.Offset, 15))
}

func describeAPI(_ context.Context, _ Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		Method string `json:"method"`
		Path   string `json:"path"`
	}](args)
	if err != nil {
		return "", err
	}
	cat, cerr := apicatalog.Default()
	if cerr != nil {
		return "", cerr
	}
	op := cat.Find(in.Method, in.Path)
	if op == nil {
		var merr error
		if op, merr = cat.Match(in.Method, in.Path); merr != nil {
			return "", merr
		}
	}
	return jsonResult(cat.Describe(op))
}

// callAPI runs one documented operation of the given kind through the caller's own API access.
func callAPI(kind apicatalog.Kind) Handler {
	return func(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
		in, err := decodeArgs[struct {
			Method string          `json:"method"`
			Path   string          `json:"path"`
			Query  map[string]any  `json:"query"`
			Body   json.RawMessage `json:"body"`
		}](args)
		if err != nil {
			return "", err
		}
		if inv.API == nil {
			return "", ErrToolForbidden
		}
		method := strings.ToUpper(strings.TrimSpace(in.Method))
		if method == "" {
			method = "GET"
		}
		cat, cerr := apicatalog.Default()
		if cerr != nil {
			return "", cerr
		}
		op, merr := cat.Match(method, in.Path)
		if merr != nil {
			return "", merr
		}
		if op.Blocked != "" {
			return "", fmt.Errorf("%s is not available to the assistant: %s", op.Key(), op.Blocked)
		}
		if op.Kind != kind {
			if kind == apicatalog.KindRead {
				return "", fmt.Errorf("%s changes data; call it with api_write", op.Key())
			}
			return "", fmt.Errorf("%s only reads; call it with api_read", op.Key())
		}
		query, qerr := queryValues(in.Query)
		if qerr != nil {
			return "", qerr
		}
		var body []byte
		if len(in.Body) > 0 && string(in.Body) != "null" {
			if method == "GET" {
				return "", errors.New("a GET takes no body; pass filters in query")
			}
			body = in.Body
		}
		status, resp, derr := inv.API.Do(ctx, method, strings.TrimPrefix(strings.TrimSpace(in.Path), "/v1"), query, body)
		if derr != nil {
			return "", derr
		}
		return apiResult(status, resp)
	}
}

func queryValues(in map[string]any) (url.Values, error) {
	out := url.Values{}
	for k, v := range in {
		switch x := v.(type) {
		case string:
			out.Set(k, x)
		case bool:
			out.Set(k, fmt.Sprint(x))
		case float64:
			out.Set(k, strconv.FormatFloat(x, 'f', -1, 64))
		case []any:
			for _, e := range x {
				out.Add(k, fmt.Sprint(e))
			}
		case nil:
		default:
			return nil, fmt.Errorf("query parameter %q must be a string, number, boolean or list", k)
		}
	}
	return out, nil
}

// apiResult turns an API response into the tool result: the JSON with
// credential-shaped fields withheld, cut to budget; an error status becomes a tool error.
func apiResult(status int, resp []byte) (string, error) {
	var parsed any
	isJSON := json.Unmarshal(resp, &parsed) == nil
	if status >= 400 {
		msg := strings.TrimSpace(string(resp))
		if m, ok := parsed.(map[string]any); ok {
			code, _ := m["code"].(string)
			text, _ := m["message"].(string)
			if text == "" {
				text, _ = m["error"].(string)
			}
			msg = strings.TrimSpace(code + ": " + text)
		}
		if len(msg) > 600 {
			msg = msg[:600]
		}
		return "", fmt.Errorf("the API answered %d %s", status, msg)
	}
	out := string(resp)
	if isJSON {
		b, _ := json.Marshal(redact(parsed))
		out = string(b)
	}
	if out == "" {
		out = fmt.Sprintf(`{"status":%d}`, status)
	}
	if len(out) > apiResultBudget {
		out = out[:apiResultBudget] + fmt.Sprintf("\n[truncated: %d more bytes; ask for fewer results with limit or filters]", len(out)-apiResultBudget)
	}
	return out, nil
}

// redact withholds credential-shaped fields, so no secret an endpoint returns reaches the model.
func redact(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if apicatalog.SecretField(k) {
				if e != nil && e != "" {
					x[k] = secretPlaceholder
				}
				continue
			}
			x[k] = redact(e)
		}
	case []any:
		for i, e := range x {
			x[i] = redact(e)
		}
	}
	return v
}

const secretPlaceholder = "[withheld]"
