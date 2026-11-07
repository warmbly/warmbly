package utils

import (
	"regexp"
	"strings"
)

// MaxJSONKeyLength caps a custom-field key so one row can't push an
// unbounded string into the JSONB column.
const MaxJSONKeyLength = 255

// jsonKeyPattern is the set of custom-field keys the whole product can
// actually address. A key is either a plain identifier ("role") or
// identifier segments joined by spaces/dashes ("Company Mobile",
// "first-name"). That is exactly what the campaign template engine can
// resolve: tmplvars.Normalize turns `{{.Company Mobile}}`
// into `(index . "Company Mobile")`, so spaced and dashed keys work
// everywhere a plain one does. Anything outside this set would produce a
// field the user can never reference in an email.
var jsonKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_]+(?:[ -]+[A-Za-z0-9_]+)*$`)

// JSONKeyRules is the human-readable version of jsonKeyPattern, for
// error messages the user has to act on.
const JSONKeyRules = "use letters, numbers, underscores, spaces or dashes"

// NormalizeJSONKey trims a custom-field key and collapses internal
// whitespace runs to a single space. CSV headers routinely arrive with a
// trailing space or a tab, and "Company Mobile" and "Company  Mobile"
// must not become two different fields.
func NormalizeJSONKey(key string) string {
	return strings.Join(strings.Fields(key), " ")
}

// IsValidJSONKey reports whether key is usable as a contact custom-field
// key. Callers should NormalizeJSONKey first.
func IsValidJSONKey(key string) bool {
	if len(key) == 0 || len(key) > MaxJSONKeyLength {
		return false
	}
	return jsonKeyPattern.MatchString(key)
}
