package outreachsource

import "testing"

func TestTranslate(t *testing.T) {
	d := Dialect{Fields: CommonFields(), Spin: []string{"RANDOM"}}
	d.Fields["firstname"] = "FirstName"
	cases := []struct{ in, want string }{
		{"Hi {{firstName}},", "Hi {{.FirstName}},"},
		{"Hi {{first_name|there}},", `Hi {{.FirstName | default "there"}},`},
		{"{{RANDOM | Hi | Hey}} {{first_name}}", "{Hi|Hey} {{.FirstName}}"},
		{"At {{Company Size}}", "At {{.company_size}}"},
		{"Bye{{signature}}", "Bye"},
		{"{{unsubscribe_link}}", "{{unsubscribe_link}}"},
		{"keep {{.FirstName}}", "keep {{.FirstName}}"},
		{"{Hi|Hey} there", "{Hi|Hey} there"},
		{`{{first_name | "a" b}}`, `{{.FirstName | default "\"a\" b"}}`},
		{"{{RANDOM | Hi {{firstName}} | Hey}}!", "{Hi {{.FirstName}}|Hey}!"},
		{"{{firstName | lastName | there}}", `{{.FirstName | default (.LastName | default "there")}}`},
		{"{{2nd touch}}", `{{(index . "2nd_touch")}}`},
	}
	for _, c := range cases {
		if got := Translate(c.in, d).Text; got != c.want {
			t.Errorf("Translate(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if f := Translate("{{Company Size}} {{company_size}}", d).Fields; len(f) != 1 || f[0] != "company_size" {
		t.Errorf("fields = %v", f)
	}
}

func TestFieldKey(t *testing.T) {
	for in, want := range map[string]string{"Company Size": "company_size", "companySize": "company_size", "first_name": "first_name", "LinkedIn URL": "linked_in_url", "ICP-tier 2": "icp_tier_2"} {
		if got := FieldKey(in); got != want {
			t.Errorf("FieldKey(%q) = %q, want %q", in, got, want)
		}
	}
}
