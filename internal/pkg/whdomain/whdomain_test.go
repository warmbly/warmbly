package whdomain

import "testing"

func TestHostAllowed(t *testing.T) {
	domains := []string{".acme.com", "partner.io"}
	cases := map[string]bool{
		"hooks.acme.com":    true,  // subdomain of leading-dot entry
		"eu.hooks.acme.com": true,  // nested subdomain
		"acme.com":          true,  // apex matches leading-dot entry
		"partner.io":        true,  // exact bare entry
		"api.partner.io":    false, // subdomain of a bare entry does NOT match
		"evil.com":          false,
		"notacme.com":       false,
		"acme.com.evil.com": false, // suffix-confusion must not pass
		"xacme.com":         false, // must match on a label boundary
		"":                  false,
	}
	for host, want := range cases {
		if got := HostAllowed(host, domains); got != want {
			t.Errorf("HostAllowed(%q) = %v, want %v", host, got, want)
		}
	}
	if HostAllowed("anything.com", nil) {
		t.Error("an empty allowlist must deny everything")
	}
}

func TestNormalize(t *testing.T) {
	ok := map[string]string{
		"acme.com":                       "acme.com",
		" Hooks.Acme.com ":               "hooks.acme.com",
		".acme.com":                      ".acme.com",
		"https://acme.com/x":             "acme.com",
		"https://acme.com:8443/path?q=1": "acme.com",
	}
	for in, want := range ok {
		got, err := Normalize(in)
		if err != nil {
			t.Errorf("Normalize(%q) unexpected error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
	bad := []string{"", "localhost", "*.acme.com", "10.0.0.1", "1.2.3.4", "   "}
	for _, in := range bad {
		if _, err := Normalize(in); err == nil {
			t.Errorf("Normalize(%q) should have errored", in)
		}
	}
}
