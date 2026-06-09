// Package tmplfuncs is the single shared text/template function set used for
// user-authored customization: campaign email bodies, automation condition
// expressions, and automation action values. Keeping one map means a helper
// learned in one place works everywhere.
//
// Everything coerces loosely-typed input (event data / merge fields arrive as
// strings or numbers) so a user can "play with numbers" without worrying about
// types: arithmetic, numeric comparison, string helpers, and a default/fallback.
package tmplfuncs

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"text/template"
)

// toFloat coerces a value (number or numeric string) to float64; 0 otherwise.
func toFloat(raw any) float64 {
	switch t := raw.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(t), 64); err == nil {
			return f
		}
	}
	return 0
}

// valueString renders any value to a comparable/printable string.
func valueString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

var funcs = template.FuncMap{
	// Arithmetic (coercing; division/modulo by zero yield 0, never panic).
	"add": func(a, b any) float64 { return toFloat(a) + toFloat(b) },
	"sub": func(a, b any) float64 { return toFloat(a) - toFloat(b) },
	"mul": func(a, b any) float64 { return toFloat(a) * toFloat(b) },
	"div": func(a, b any) float64 {
		d := toFloat(b)
		if d == 0 {
			return 0
		}
		return toFloat(a) / d
	},
	"mod": func(a, b any) float64 {
		d := toFloat(b)
		if d == 0 {
			return 0
		}
		return math.Mod(toFloat(a), d)
	},
	"num": func(a any) float64 { return toFloat(a) },
	// Numeric comparisons that coerce (use these when a value may be a string).
	"gtf": func(a, b any) bool { return toFloat(a) > toFloat(b) },
	"ltf": func(a, b any) bool { return toFloat(a) < toFloat(b) },
	"gef": func(a, b any) bool { return toFloat(a) >= toFloat(b) },
	"lef": func(a, b any) bool { return toFloat(a) <= toFloat(b) },
	// String helpers.
	"lower": func(a any) string { return strings.ToLower(valueString(a)) },
	"upper": func(a any) string { return strings.ToUpper(valueString(a)) },
	"trim":  func(a any) string { return strings.TrimSpace(valueString(a)) },
	"title": func(a any) string { return strings.Title(strings.ToLower(valueString(a))) }, //nolint:staticcheck // ASCII title-case is fine for names
	"contains": func(s, sub any) bool {
		return strings.Contains(strings.ToLower(valueString(s)), strings.ToLower(valueString(sub)))
	},
	"hasPrefix": func(s, p any) bool { return strings.HasPrefix(valueString(s), valueString(p)) },
	"hasSuffix": func(s, p any) bool { return strings.HasSuffix(valueString(s), valueString(p)) },
	// Fallback: {{.FirstName | default "there"}} -> "there" when the field is blank.
	"default": func(def, v any) any {
		if valueString(v) == "" {
			return def
		}
		return v
	},
}

// FuncMap returns the shared template function set. The returned map is shared
// (the funcs are stateless), so callers must not mutate it.
func FuncMap() template.FuncMap {
	return funcs
}
