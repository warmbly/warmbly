package crypt

import "testing"

func TestSHA256(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{"hello", "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"},
	}

	for _, tt := range tests {
		got := SHA256(tt.input)
		if got != tt.want {
			t.Errorf("SHA256(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
