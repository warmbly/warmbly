package imap

import "strings"

func ContainsAny(a, b []string) bool {
	for _, i := range a {
		for _, l := range b {
			if strings.EqualFold(i, l) {
				return true
			}
		}
	}
	return false
}
