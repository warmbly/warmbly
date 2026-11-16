package outreachsource

import (
	"sort"
	"strings"
	"unicode"
)

// Registry is the set of providers an instance offers.
type Registry struct {
	byID  map[ProviderID]Provider
	order []ProviderID
}

// NewRegistry indexes providers in the order given, which is the picker's order.
func NewRegistry(ps ...Provider) *Registry {
	r := &Registry{byID: map[ProviderID]Provider{}}
	for _, p := range ps {
		id := p.Info().ID
		if _, dup := r.byID[id]; dup {
			panic("toolmigrate: provider registered twice: " + string(id))
		}
		r.byID[id] = p
		r.order = append(r.order, id)
	}
	return r
}

// Get returns a provider by id.
func (r *Registry) Get(id ProviderID) (Provider, bool) {
	p, ok := r.byID[id]
	return p, ok
}

// Infos lists every provider in picker order.
func (r *Registry) Infos() []Info {
	out := make([]Info, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.byID[id].Info())
	}
	return out
}

// Supports reports whether a provider's API can read a kind.
func (i Info) Supports(k Kind) bool {
	for _, have := range i.Kinds {
		if have == k {
			return true
		}
	}
	return false
}

// FieldKey normalises a provider's custom field name to the snake_case key
// Warmbly stores and templates reference: "Company Size" and "companySize"
// both become "company_size".
func FieldKey(name string) string {
	var b strings.Builder
	prevLower := false
	pendingSep := false
	for _, r := range strings.TrimSpace(name) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if unicode.IsUpper(r) && prevLower {
				pendingSep = true
			}
			if pendingSep && b.Len() > 0 {
				b.WriteByte('_')
			}
			pendingSep = false
			b.WriteRune(unicode.ToLower(r))
			prevLower = unicode.IsLower(r) || unicode.IsDigit(r)
		default:
			pendingSep = true
			prevLower = false
		}
	}
	return b.String()
}

// SortedKeys returns a map's keys in order, for deterministic output.
func SortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
