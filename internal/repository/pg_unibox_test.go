package repository

import "testing"

func TestNonNilStringsConvertsNilToEmpty(t *testing.T) {
	got := nonNilStrings(nil)
	if got == nil {
		t.Fatal("nonNilStrings(nil) returned nil")
	}
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}

func TestNonNilStringsKeepsValues(t *testing.T) {
	in := []string{"a"}
	got := nonNilStrings(in)
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("got %#v, want %#v", got, in)
	}
}
