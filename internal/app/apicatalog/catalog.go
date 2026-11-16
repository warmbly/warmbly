// Package apicatalog indexes the public OpenAPI spec so an agent can find,
// read and call any documented endpoint. It decides which operations the
// agent may call and whether a call reads or writes; the route's own
// middleware still authorizes every call as the caller.
package apicatalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/warmbly/warmbly/docs"
)

// Kind is how an operation is treated by an agent.
type Kind string

const (
	// KindRead changes nothing and runs without approval.
	KindRead Kind = "read"
	// KindWrite changes workspace data and needs approval in the assistant.
	KindWrite Kind = "write"
)

// Operation is one documented method and path.
type Operation struct {
	ID          string `json:"operation_id"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Tag         string `json:"tag"`
	Summary     string `json:"summary"`
	Description string `json:"-"`
	Scope       string `json:"scope,omitempty"`
	Kind        Kind   `json:"kind"`
	// Blocked names why an agent cannot call this operation; empty when it can.
	Blocked string `json:"blocked,omitempty"`

	segments []string
	raw      map[string]any
}

// Key is the operation's "METHOD /path" identity.
func (o *Operation) Key() string { return o.Method + " " + o.Path }

// Catalog is the indexed spec.
type Catalog struct {
	ops        []*Operation
	schemas    map[string]any
	parameters map[string]any
}

var (
	defaultOnce sync.Once
	defaultCat  *Catalog
	defaultErr  error
)

// Default is the catalog built from the embedded spec.
func Default() (*Catalog, error) {
	defaultOnce.Do(func() { defaultCat, defaultErr = Parse(docs.OpenAPI) })
	return defaultCat, defaultErr
}

var scopeRe = regexp.MustCompile("Scopes?:?\\s+`?([A-Z][A-Z_]+[A-Z])`?")

var methods = []string{"get", "post", "put", "patch", "delete"}

// Parse indexes an OpenAPI 3 document.
func Parse(spec []byte) (*Catalog, error) {
	var doc struct {
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas    map[string]any `json:"schemas"`
			Parameters map[string]any `json:"parameters"`
		} `json:"components"`
	}
	if err := json.Unmarshal(spec, &doc); err != nil {
		return nil, fmt.Errorf("apicatalog: %w", err)
	}
	c := &Catalog{schemas: doc.Components.Schemas, parameters: doc.Components.Parameters}
	for path, item := range doc.Paths {
		for _, m := range methods {
			rawOp, ok := item[m]
			if !ok {
				continue
			}
			var op map[string]any
			if err := json.Unmarshal(rawOp, &op); err != nil {
				return nil, fmt.Errorf("apicatalog: %s %s: %w", m, path, err)
			}
			o := &Operation{
				Method:   strings.ToUpper(m),
				Path:     path,
				ID:       str(op["operationId"]),
				Summary:  str(op["summary"]),
				raw:      op,
				segments: splitPath(path),
			}
			o.Description = str(op["description"])
			if tags, _ := op["tags"].([]any); len(tags) > 0 {
				o.Tag = str(tags[0])
			}
			if m := scopeRe.FindStringSubmatch(o.Description); m != nil {
				o.Scope = m[1]
			}
			o.Kind, o.Blocked = c.classify(o)
			c.ops = append(c.ops, o)
		}
	}
	sort.Slice(c.ops, func(i, j int) bool {
		if c.ops[i].Path != c.ops[j].Path {
			return c.ops[i].Path < c.ops[j].Path
		}
		return c.ops[i].Method < c.ops[j].Method
	})
	return c, nil
}

// Operations is every indexed operation, sorted by path then method.
func (c *Catalog) Operations() []*Operation { return c.ops }

// Tags lists the areas of the API with their operation counts.
func (c *Catalog) Tags() map[string]int {
	out := map[string]int{}
	for _, o := range c.ops {
		out[o.Tag]++
	}
	return out
}

// SearchResult is one page of matching operations.
type SearchResult struct {
	Operations []*Operation `json:"operations"`
	Total      int          `json:"total"`
	NextOffset *int         `json:"next_offset,omitempty"`
}

// Search ranks operations by how many query terms their path, summary,
// description and tag contain. An empty query lists everything in tag.
func (c *Catalog) Search(query, tag string, offset, limit int) SearchResult {
	if limit <= 0 || limit > 50 {
		limit = 15
	}
	terms := strings.Fields(strings.ToLower(query))
	type hit struct {
		op    *Operation
		score int
	}
	var hits []hit
	for _, o := range c.ops {
		if tag != "" && !strings.EqualFold(o.Tag, tag) {
			continue
		}
		head := strings.ToLower(o.Method + " " + o.Path + " " + o.Summary + " " + o.ID)
		body := strings.ToLower(o.Description + " " + o.Tag)
		score := 0
		for _, t := range terms {
			if strings.Contains(head, t) {
				score += 3
			} else if strings.Contains(body, t) {
				score++
			}
		}
		if len(terms) == 0 || score > 0 {
			hits = append(hits, hit{o, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	offset = max(0, min(offset, len(hits)))
	end := min(offset+limit, len(hits))
	res := SearchResult{Total: len(hits), Operations: make([]*Operation, 0, end-offset)}
	for _, h := range hits[offset:end] {
		res.Operations = append(res.Operations, h.op)
	}
	if end < len(hits) {
		res.NextOffset = &end
	}
	return res
}

// ErrNoOperation means no documented operation has that method and path.
var ErrNoOperation = errors.New("no documented endpoint matches that method and path; use find_api to look it up")

// ErrBadPath means the path is not a plain API path.
var ErrBadPath = errors.New("path must be an API path such as /contacts/123, with no query string, scheme or dot segments")

// Match finds the operation a concrete request path belongs to. A literal
// segment beats a parameter, so /contacts/search wins over /contacts/{id}.
func (c *Catalog) Match(method, path string) (*Operation, error) {
	segs, err := cleanPath(path)
	if err != nil {
		return nil, err
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	var best *Operation
	bestLiterals := -1
	for _, o := range c.ops {
		if o.Method != method || len(o.segments) != len(segs) {
			continue
		}
		literals, ok := 0, true
		for i, s := range o.segments {
			if isParam(s) {
				continue
			}
			if s != segs[i] {
				ok = false
				break
			}
			literals++
		}
		if ok && literals > bestLiterals {
			best, bestLiterals = o, literals
		}
	}
	if best == nil {
		return nil, ErrNoOperation
	}
	return best, nil
}

// Find returns the operation documented at exactly this method and path template.
func (c *Catalog) Find(method, template string) *Operation {
	method = strings.ToUpper(strings.TrimSpace(method))
	template = strings.TrimSpace(template)
	for _, o := range c.ops {
		if o.Method == method && o.Path == template {
			return o
		}
	}
	return nil
}

// cleanPath splits a concrete request path, refusing anything that is not a
// plain path: a query, fragment, scheme, escape, dot segment or empty segment.
func cleanPath(path string) ([]string, error) {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "/v1")
	if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#%\\ \t\r\n") || strings.Contains(path, "//") {
		return nil, ErrBadPath
	}
	segs := splitPath(path)
	for _, s := range segs {
		if s == "" || s == "." || s == ".." {
			return nil, ErrBadPath
		}
		for _, r := range s {
			if r < 0x21 || r == 0x7f {
				return nil, ErrBadPath
			}
		}
	}
	return segs, nil
}

func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

func isParam(seg string) bool { return strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") }

func str(v any) string {
	s, _ := v.(string)
	return s
}
