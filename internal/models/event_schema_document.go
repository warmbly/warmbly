package models

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/iskorotkov/avro/v2"
)

// SchemaDocument is the JSON to hand a schema registry for one of the derived
// schemas. It is not json.Marshal(schema), because the library's MarshalJSON
// writes two defaults in a form the spec forbids and no parser accepts, while
// Schema.String() drops every default (#583, #586):
//
//   - a fixed field's default is written as the byte array it was validated
//     into, where the spec wants a string of code points 0-255. Every uint64
//     on the bus is a fixed
//   - inside a record's default, a nested optional field's null is stored as
//     the library's internal sentinel and comes out as {}
//
// So the marshalled document is re-read and every default is normalised
// against the type it belongs to, then parsed back before it is used: the
// failure this guards against is exactly a document that marshals fine and
// parses never.
func SchemaDocument(s avro.Schema) ([]byte, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	(&schemaDefaults{named: map[string]any{}}).walk(doc, "")
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	if _, err := avro.Parse(string(out)); err != nil {
		return nil, fmt.Errorf("schema document does not round-trip: %w", err)
	}
	return out, nil
}

// schemaDefaults normalises defaults in a parsed schema document. It walks in
// definition order and remembers every named type, because Avro defines a
// named type at its first appearance and refers to it by name after that, so
// a field typed by name can only be normalised once its definition was seen.
type schemaDefaults struct {
	named map[string]any
}

func (d *schemaDefaults) walk(node any, namespace string) {
	switch v := node.(type) {
	case []any:
		for _, b := range v {
			d.walk(b, namespace)
		}
	case map[string]any:
		if ns, ok := v["namespace"].(string); ok && ns != "" {
			namespace = ns
		}
		switch v["type"] {
		case "fixed", "enum":
			d.named[fullName(v["name"], namespace)] = v
		case "record":
			d.named[fullName(v["name"], namespace)] = v
			fields, _ := v["fields"].([]any)
			for _, f := range fields {
				field, ok := f.(map[string]any)
				if !ok {
					continue
				}
				d.walk(field["type"], namespace)
				if def, ok := field["default"]; ok {
					field["default"] = d.normalize(field["type"], def, namespace)
				}
			}
		case "array":
			// A record can be defined for the first time inside an array's
			// items (a []Mailbox), and everything referring to it by name
			// afterwards depends on it having been seen here.
			d.walk(v["items"], namespace)
		case "map":
			d.walk(v["values"], namespace)
		default:
			d.walk(v["type"], namespace)
		}
	}
}

// normalize returns the spec's form of a default for a value of type typ.
func (d *schemaDefaults) normalize(typ, def any, namespace string) any {
	switch t := typ.(type) {
	case string:
		// A primitive name, or a reference to a named type seen earlier.
		if named, ok := d.named[fullName(t, namespace)]; ok {
			return d.normalize(named, def, namespace)
		}
		return def
	case []any:
		// A union default is written against its first branch. When that is
		// null, the library's sentinel arrives here as an empty object.
		if len(t) == 0 {
			return def
		}
		if t[0] == "null" {
			if m, ok := def.(map[string]any); ok && len(m) == 0 {
				return nil
			}
			return def
		}
		return d.normalize(t[0], def, namespace)
	case map[string]any:
		if ns, ok := t["namespace"].(string); ok && ns != "" {
			namespace = ns
		}
		switch t["type"] {
		case "fixed":
			if bytes, ok := def.([]any); ok {
				return bytesToDefaultString(bytes)
			}
		case "record":
			m, ok := def.(map[string]any)
			if !ok {
				return def
			}
			fields, _ := t["fields"].([]any)
			for _, f := range fields {
				field, ok := f.(map[string]any)
				if !ok {
					continue
				}
				name, _ := field["name"].(string)
				if v, ok := m[name]; ok {
					m[name] = d.normalize(field["type"], v, namespace)
				}
			}
			return m
		case "array":
			if items, ok := def.([]any); ok {
				for i, v := range items {
					items[i] = d.normalize(t["items"], v, namespace)
				}
			}
		case "map":
			if m, ok := def.(map[string]any); ok {
				for k, v := range m {
					m[k] = d.normalize(t["values"], v, namespace)
				}
			}
		default:
			// A wrapped primitive, e.g. a long with a logical type.
			return d.normalize(t["type"], def, namespace)
		}
	}
	return def
}

func fullName(name any, namespace string) string {
	n, _ := name.(string)
	if namespace == "" || strings.Contains(n, ".") {
		return n
	}
	return namespace + "." + n
}

// bytesToDefaultString is the spec's encoding for a bytes or fixed default:
// each byte becomes the Unicode code point of the same value.
func bytesToDefaultString(bytes []any) string {
	runes := make([]rune, 0, len(bytes))
	for _, b := range bytes {
		n, _ := b.(float64)
		runes = append(runes, rune(int(n)&0xff))
	}
	return string(runes)
}
