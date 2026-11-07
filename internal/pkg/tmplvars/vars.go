// Package tmplvars is the variable vocabulary of campaign copy: the built-in
// fields a template can name, the spellings accepted for them, and the one
// normalisation every compile and validation of campaign copy goes through.
package tmplvars

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode"
)

// UnsubscribeLink is the variable a step can place the opt-out link with by hand.
const UnsubscribeLink = "UnsubscribeLink"

// SenderField is the top-level name of the typed sending-mailbox namespace.
const SenderField = "Sender"

// builtins are the canonical top-level fields, each with any extra synonym
// whose spellings are accepted too.
var builtins = []struct {
	name     string
	synonyms []string
}{
	{"FirstName", nil},
	{"LastName", nil},
	{"Email", nil},
	{"Company", []string{"CompanyName"}},
	{"Phone", nil},
	{SenderField, nil},
	{UnsubscribeLink, nil},
}

// Alias is one accepted spelling of a canonical field.
type alias struct{ name, canonical string }

var (
	// topAliases are the non-canonical spellings, in a stable order.
	topAliases []alias
	// topNames resolves a canonical name or an alias to the canonical name.
	topNames = map[string]string{}
	// senderNames resolves a Sender field or one of its aliases to the field.
	senderNames = map[string]string{}
	// canonicalList and senderList name the valid fields in error messages.
	canonicalList string
	senderList    string
)

func init() {
	names := make([]string, 0, len(builtins))
	for _, b := range builtins {
		names = append(names, b.name)
		topNames[b.name] = b.name
		for _, form := range append([]string{b.name}, b.synonyms...) {
			for _, spelling := range append(spellings(form), form) {
				if spelling == b.name {
					continue
				}
				register(topNames, spelling, b.name)
			}
		}
	}
	for spelling, canonical := range topNames {
		if spelling != canonical {
			topAliases = append(topAliases, alias{spelling, canonical})
		}
	}
	sort.Slice(topAliases, func(i, j int) bool { return topAliases[i].name < topAliases[j].name })
	canonicalList = strings.Join(names, ", ")

	typ := reflect.TypeOf(Sender{})
	fields := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		fields = append(fields, name)
		register(senderNames, name, name)
	}
	for _, name := range fields {
		for _, spelling := range spellings(name) {
			if _, taken := senderNames[spelling]; !taken {
				register(senderNames, spelling, name)
			}
		}
	}
	senderList = strings.Join(fields, ", ")
}

// register panics on a spelling claimed by two fields, so the tables can never be ambiguous.
func register(table map[string]string, spelling, canonical string) {
	if prev, ok := table[spelling]; ok && prev != canonical {
		panic(fmt.Sprintf("tmplvars: %q names both %s and %s", spelling, prev, canonical))
	}
	table[spelling] = canonical
}

// spellings are the snake_case, camelCase and lowercase forms of a Go field name.
func spellings(name string) []string {
	r := []rune(name)
	camel := string(unicode.ToLower(r[0])) + string(r[1:])
	return []string{snake(name), camel, strings.ToLower(name)}
}

// snake turns FirstName into first_name and SignatureHTML into signature_html.
func snake(name string) string {
	r := []rune(name)
	var b strings.Builder
	for i, c := range r {
		if i > 0 && unicode.IsUpper(c) {
			prevLower := unicode.IsLower(r[i-1]) || unicode.IsDigit(r[i-1])
			acronymEnd := unicode.IsUpper(r[i-1]) && i+1 < len(r) && unicode.IsLower(r[i+1])
			if prevLower || acronymEnd {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(c))
	}
	return b.String()
}

// AddAliases sets every accepted alias of a built-in field in data to that
// field's value, except where the contact has a custom field of that name,
// which keeps its own value as it always has.
func AddAliases(data map[string]any, custom map[string]string) {
	for _, a := range topAliases {
		if _, ok := custom[a.name]; ok {
			continue
		}
		data[a.name] = data[a.canonical]
	}
}

// AliasCount is how many keys AddAliases may add, for sizing the data map.
func AliasCount() int { return len(topAliases) }
