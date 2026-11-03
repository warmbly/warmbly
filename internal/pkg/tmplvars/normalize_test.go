package tmplvars

import (
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"canonical untouched", "Hi {{.FirstName}}", "Hi {{.FirstName}}"},
		{"bare snake", "Hi {{first_name}}", "Hi {{$.first_name}}"},
		{"bare camel", "Hi {{ firstName }}", "Hi {{ $.firstName }}"},
		{"bare lowercase", "{{company}} / {{company_name}}", "{{$.company}} / {{$.company_name}}"},
		{"dotted alias is a field already", "{{.first_name}}", "{{.first_name}}"},
		{"bare in if", "{{if first_name}}x{{end}}", "{{if $.first_name}}x{{end}}"},
		{"bare in pipe", `{{first_name | default "there"}}`, `{{$.first_name | default "there"}}`},
		{"bare as argument", `{{default "there" first_name}}`, `{{default "there" $.first_name}}`},
		{"bare in eq", `{{if eq company "Acme"}}y{{end}}`, `{{if eq $.company "Acme"}}y{{end}}`},
		{"literal braces outside actions", "Use first_name or {first_name} in text", "Use first_name or {first_name} in text"},
		{"string literal untouched", `{{default "first_name" .X}}`, `{{default "first_name" .X}}`},
		{"spaced custom field", "{{.job title}}", `{{(index . "job title")}}`},
		{"sender alias", "{{.Sender.first_name}}", "{{.Sender.FirstName}}"},
		{"sender bare", "{{sender.first_name}}", "{{$.Sender.FirstName}}"},
		{"sender variable", "{{$.sender.signature_html}}", "{{$.Sender.SignatureHTML}}"},
		{"function untouched", "{{upper .FirstName}}", "{{upper .FirstName}}"},
		{"unsubscribe alias", "{{unsubscribe_link}}", "{{$.unsubscribe_link}}"},
		{"sender through a variable", "{{$s := .Sender}}{{$s.first_name}}", "{{$s := .Sender}}{{$s.FirstName}}"},
		{"sender through with", "{{with .sender}}{{.signature_html}}{{end}}", "{{with .sender}}{{.SignatureHTML}}{{end}}"},
		{"sender through root variable", "{{$r := .}}{{$r.Sender.email}}", "{{$r := .}}{{$r.Sender.Email}}"},
		{"range element untouched", "{{range .Sender.Tags}}{{.}}{{end}}", "{{range .Sender.Tags}}{{.}}{{end}}"},
		{"range variables untouched", "{{range $k, $v := .}}{{$k}}={{$v}}{{end}}", "{{range $k, $v := .}}{{$k}}={{$v}}{{end}}"},
	}
	for _, tc := range cases {
		got, err := Normalize(tc.in)
		if err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestCheckRefusesWhatCannotRender(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Hi {{frist_name}}", `unknown variable "frist_name"`},
		{"{{if .FirstName}}open", "unexpected EOF"},
		{"{{.Sender.Bogus}}", `unknown sender field "Bogus"`},
		{"{{shout .FirstName}}", `function "shout" is not defined`},
		{"{{.Sender.Email.Bogus}}", `sender field Email has no field "Bogus"`},
		{"{{$s := .Sender}}{{$s.Bogus}}", `unknown sender field "Bogus"`},
		{"{{with .Sender}}{{.Bogus}}{{end}}", `unknown sender field "Bogus"`},
		{"{{range $k, $v := .}}{{$v.x}}{{end}}", "fields of $v cannot be checked"},
	}
	for _, tc := range cases {
		err := Check(tc.in)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want an error containing %q", tc.in, err, tc.want)
		}
	}
	for _, ok := range []string{"", "plain", "{{.FirstName}}", "{{first_name}}", "{{.any_custom_key}}", `{{index . "city"}}`, "{{.job title}}", "book: {{form_link:abc123}}"} {
		if err := Check(ok); err != nil {
			t.Errorf("%q: unexpected error %v", ok, err)
		}
	}
}

func TestCheckNamesTheLine(t *testing.T) {
	err := Check("line one\n{{if .X}}")
	if err == nil || !strings.HasPrefix(err.Error(), "line 2:") {
		t.Fatalf("got %v", err)
	}
	// A form-link marker spanning lines keeps the lines after it numbered as written.
	err = Check("{{\nform_link:abc123\n}}\n{{frist_name}}")
	if err == nil || !strings.HasPrefix(err.Error(), "line 4:") {
		t.Fatalf("marker shifted the line: %v", err)
	}
}

func TestAliasesDerivedFromCanonicalFields(t *testing.T) {
	for alias, want := range map[string]string{
		"first_name": "FirstName", "firstName": "FirstName", "firstname": "FirstName",
		"last_name": "LastName", "email": "Email", "company_name": "Company",
		"companyName": "Company", "phone": "Phone", "unsubscribe_link": UnsubscribeLink,
	} {
		if got := topNames[alias]; got != want {
			t.Errorf("%s: got %q, want %q", alias, got, want)
		}
	}
	if got := senderNames["signature_html"]; got != "SignatureHTML" {
		t.Errorf("signature_html: got %q", got)
	}
}

func TestAddAliasesLetsCustomFieldWin(t *testing.T) {
	data := map[string]any{"FirstName": "Sam", "first_name": "custom"}
	AddAliases(data, map[string]string{"first_name": "custom"})
	if data["first_name"] != "custom" || data["firstName"] != "Sam" {
		t.Fatalf("got %v", data)
	}
}
