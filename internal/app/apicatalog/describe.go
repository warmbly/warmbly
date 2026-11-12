package apicatalog

import (
	"encoding/json"
	"strings"
)

// describeBudget bounds a description so one large schema cannot flood the model's context.
const describeBudget = 14000

// Param is one path or query parameter.
type Param struct {
	Name        string `json:"name"`
	In          string `json:"in"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
	Schema      any    `json:"schema,omitempty"`
}

// Description is everything needed to call an operation.
type Description struct {
	Operation
	Description string  `json:"description,omitempty"`
	Params      []Param `json:"params,omitempty"`
	Body        any     `json:"body,omitempty"`
	Response    any     `json:"response,omitempty"`
	// Truncated means deep schemas were cut to fit; nested objects show only their schema name.
	Truncated bool `json:"truncated,omitempty"`
}

// Describe resolves an operation's parameters, request body and success
// response, inlining schema references until the result fits its budget.
func (c *Catalog) Describe(o *Operation) Description {
	var d Description
	for depth := 6; depth >= 1; depth-- {
		d = c.describe(o, depth)
		d.Truncated = depth < 6
		if b, _ := json.Marshal(d); len(b) <= describeBudget {
			return d
		}
	}
	d.Response = nil
	d.Truncated = true
	return d
}

func (c *Catalog) describe(o *Operation, depth int) Description {
	d := Description{Operation: *o, Description: o.Description}
	params, _ := o.raw["parameters"].([]any)
	for _, p := range params {
		pm, _ := c.resolveParam(p).(map[string]any)
		if pm == nil || str(pm["in"]) == "header" {
			continue
		}
		req, _ := pm["required"].(bool)
		d.Params = append(d.Params, Param{
			Name: str(pm["name"]), In: str(pm["in"]), Required: req,
			Description: str(pm["description"]), Schema: c.inline(pm["schema"], depth, nil),
		})
	}
	if body, ok := o.raw["requestBody"].(map[string]any); ok {
		if s := jsonSchema(body); s != nil {
			d.Body = c.inline(s, depth, nil)
		}
	}
	if responses, ok := o.raw["responses"].(map[string]any); ok {
		for _, code := range []string{"200", "201", "202"} {
			r, _ := responses[code].(map[string]any)
			if r == nil {
				continue
			}
			if s := jsonSchema(r); s != nil {
				d.Response = c.inline(s, max(1, depth-2), nil)
			} else if desc := str(r["description"]); desc != "" {
				d.Response = desc
			}
			break
		}
	}
	return d
}

func jsonSchema(holder map[string]any) any {
	content, _ := holder["content"].(map[string]any)
	media, _ := content["application/json"].(map[string]any)
	if media == nil {
		return nil
	}
	return media["schema"]
}

func (c *Catalog) resolveParam(p any) any {
	m, _ := p.(map[string]any)
	if ref := str(m["$ref"]); strings.HasPrefix(ref, "#/components/parameters/") {
		return c.parameters[strings.TrimPrefix(ref, "#/components/parameters/")]
	}
	return p
}

// inline copies a schema with its references expanded to depth, dropping examples.
func (c *Catalog) inline(v any, depth int, stack []string) any {
	switch x := v.(type) {
	case map[string]any:
		if ref := str(x["$ref"]); strings.HasPrefix(ref, "#/components/schemas/") {
			name := strings.TrimPrefix(ref, "#/components/schemas/")
			for _, s := range stack {
				if s == name {
					return map[string]any{"schema": name, "note": "recursive"}
				}
			}
			if depth <= 0 {
				return map[string]any{"schema": name}
			}
			return c.inline(c.schemas[name], depth-1, append(stack, name))
		}
		out := make(map[string]any, len(x))
		for k, e := range x {
			if k == "example" || k == "examples" || strings.HasPrefix(k, "x-") {
				continue
			}
			out[k] = c.inline(e, depth, stack)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = c.inline(e, depth, stack)
		}
		return out
	}
	return v
}
