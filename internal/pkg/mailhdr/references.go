package mailhdr

import (
	"fmt"
	"strings"
)

// ReferenceIDs preserves ordered RFC message identifiers without accepting header text.
func ReferenceIDs(ids []string) ([]string, error) {
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	if len(ids) > 64 {
		return nil, fmt.Errorf("too many reference identifiers")
	}
	for _, id := range ids {
		id = strings.Trim(strings.TrimSpace(id), "<>")
		if id == "" || !strings.Contains(id, "@") || strings.ContainsAny(id, "<> \t\r\n\x00") || len(id) > 254 {
			return nil, fmt.Errorf("invalid reference identifier")
		}
		if !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	return out, nil
}

func References(ids []string) (string, error) {
	normalized, err := ReferenceIDs(ids)
	if err != nil {
		return "", err
	}
	wrapped := make([]string, len(normalized))
	for i, id := range normalized {
		wrapped[i] = "<" + id + ">"
	}
	value := strings.Join(wrapped, " ")
	if len(value) > 900 {
		return "", fmt.Errorf("reference ancestry exceeds header limit")
	}
	return value, nil
}

// maxReferencesLen is the header length References refuses to exceed.
const maxReferencesLen = 900

// ConversationReferences fits a conversation's Message-IDs, oldest first, into
// a References chain: invalid and repeated ids are dropped, and a chain too
// long keeps the first id and the newest ones (RFC 5322 3.6.4, RFC 5537 3.4.4).
func ConversationReferences(ids []string) []string {
	var valid []string
	seen := map[string]bool{}
	for _, id := range ids {
		norm, err := ReferenceIDs([]string{id})
		if err != nil || len(norm) == 0 || seen[norm[0]] {
			continue
		}
		seen[norm[0]] = true
		valid = append(valid, norm[0])
	}
	if len(valid) == 0 {
		return nil
	}
	size := func(id string) int { return len(id) + 3 }
	budget := maxReferencesLen + 1 - size(valid[0])
	tail := []string{}
	for i := len(valid) - 1; i > 0 && len(tail) < 63; i-- {
		if budget-size(valid[i]) < 0 {
			break
		}
		budget -= size(valid[i])
		tail = append(tail, valid[i])
	}
	out := []string{valid[0]}
	for i := len(tail) - 1; i >= 0; i-- {
		out = append(out, tail[i])
	}
	return out
}
