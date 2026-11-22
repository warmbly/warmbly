package repository

import (
	"testing"
)

func TestContactSearchTermsSplit(t *testing.T) {
	cases := []struct {
		query string
		want  []string
	}{
		{"", nil},
		{"   ", nil},
		{"Test", []string{"Test"}},
		{"  Test   Demo ", []string{"Test", "Demo"}},
		{"a b c d e f g h", []string{"a", "b", "c", "d", "e", "f"}},
	}
	for _, tc := range cases {
		got := contactSearchTerms(tc.query)
		if len(got) != len(tc.want) {
			t.Fatalf("contactSearchTerms(%q) = %v, want %v", tc.query, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("contactSearchTerms(%q) = %v, want %v", tc.query, got, tc.want)
			}
		}
	}
}
