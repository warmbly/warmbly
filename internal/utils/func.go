package utils

import "slices"

func Contains(a []string, b string) bool {
	return slices.Contains(a, b)
}

func Filter[T any](slice []T, predicate func(T) bool) []T {
	result := make([]T, 0, len(slice))
	for _, v := range slice {
		if predicate(v) {
			result = append(result, v)
		}
	}
	return result
}

func MakeArray(s string, count int) []string {
	result := make([]string, count)
	for i := range count {
		result[i] = s
	}
	return result
}
