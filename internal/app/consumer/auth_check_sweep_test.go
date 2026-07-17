package jobs

import "testing"

func TestAuthDomainOf(t *testing.T) {
	tests := []struct {
		email string
		want  string
	}{
		{"sales@acme.com", "acme.com"},
		{"Sales@Acme.COM", "acme.com"},
		{"  user@Example.Org  ", "example.org"},
		{"first.last@sub.domain.co", "sub.domain.co"},
		{"plus+tag@acme.com", "acme.com"},
		{"noatsign", ""},
		{"trailingat@", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := authDomainOf(tt.email); got != tt.want {
			t.Errorf("authDomainOf(%q) = %q, want %q", tt.email, got, tt.want)
		}
	}
}
