package utils

func Difference(a, b []string) []string {
	m := make(map[string]struct{}, len(b))
	for _, v := range b {
		m[v] = struct{}{}
	}

	var diff []string
	for _, v := range a {
		if _, found := m[v]; !found {
			diff = append(diff, v)
		}
	}
	return diff
}
