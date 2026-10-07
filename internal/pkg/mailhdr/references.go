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
