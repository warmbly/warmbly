package partner

import "testing"

func TestValidatePersonContactEmail(t *testing.T) {
	for _, tc := range []struct {
		email, want string
		ok          bool
	}{
		{"", "", true},
		{"  ", "", true},
		{"Ada@Acme.com", "ada@acme.com", true},
		{"Ada Lovelace <ada@acme.com>", "ada@acme.com", true},
		{"not an address", "", false},
		{"p-1@graphkit.partner.invalid", "", false},
	} {
		p := PersonInput{ExternalID: "p1", Email: tc.email}
		xerr := validatePerson(&p)
		if (xerr == nil) != tc.ok {
			t.Errorf("validatePerson(%q) error = %v; want ok=%v", tc.email, xerr, tc.ok)
			continue
		}
		if tc.ok && p.Email != tc.want {
			t.Errorf("validatePerson(%q) email = %q; want %q", tc.email, p.Email, tc.want)
		}
	}
}
