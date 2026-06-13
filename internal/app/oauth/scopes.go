package oauth

import (
	"sort"
	"strings"

	"github.com/warmbly/warmbly/internal/models"
)

// OAuth scope strings are the lowercased API-permission names (READ_EMAILS ->
// "read_emails"), so a token's granted scopes ARE an API-permission bitmask and
// flow through the exact same route gates as an API key. One vocabulary, no
// remapping.

var (
	scopeToBit = map[string]uint64{}
	bitToScope = map[uint64]string{}
)

func init() {
	for _, p := range models.AllAPIPermissions {
		s := strings.ToLower(p.Name)
		scopeToBit[s] = p.Value
		bitToScope[p.Value] = s
	}
}

// ScopeString turns a permission bitmask into a stable, sorted, space-separated
// scope string (the form returned in token responses and shown on consent).
func ScopeString(mask uint64) string {
	out := make([]string, 0, len(bitToScope))
	for bit, s := range bitToScope {
		if mask&bit == bit {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

// ParseScopes parses a space-separated scope string into a bitmask plus the list
// of tokens it didn't recognize (so the caller can reject an invalid_scope).
func ParseScopes(raw string) (uint64, []string) {
	var mask uint64
	var unknown []string
	for _, s := range strings.Fields(raw) {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		if bit, ok := scopeToBit[s]; ok {
			mask |= bit
		} else {
			unknown = append(unknown, s)
		}
	}
	return mask, unknown
}

// ScopeList expands a bitmask into its individual scope strings (for the consent
// screen, which lists each permission being requested).
func ScopeList(mask uint64) []string {
	if mask == 0 {
		return []string{}
	}
	return strings.Fields(ScopeString(mask))
}
