package tmplvars

import (
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"canonical untouched", "Hi {{.FirstName}}", "Hi {{.FirstName}}"},
		{"bare snake", "Hi {{first_name}}", "Hi {{.FirstName}}"},
		{"bare camel", "Hi {{ firstName }}", "Hi {{ .FirstName }}"},
		{"bare lowercase", "{{company}} / {{company_name}}", "{{.Company}} / {{.Company}}"},
		{"dotted alias is a field already", "{{.first_name}}", "{{.first_name}}"},
		{"bare in if", "{{if first_name}}x{{end}}", "{{if .FirstName}}x{{end}}"},
		{"bare in pipe", `{{first_name | default "there"}}`, `{{.FirstName | default "there"}}`},
		{"bare as argument", `{{default "there" first_name}}`, `{{default "there" .FirstName}}`},
		{"bare in eq", `{{if eq company "Acme"}}y{{end}}`, `{{if eq .Company "Acme"}}y{{end}}`},
		{"literal braces outside actions", "Use first_name or {first_name} in text", "Use first_name or {first_name} in text"},
		{"string literal untouched", `{{default "first_name" .X}}`, `{{default "first_name" .X}}`},
		{"spaced custom field", "{{.job title}}", `{{(index . "job title")}}`},
		{"sender alias", "{{.Sender.first_name}}", "{{.Sender.FirstName}}"},
		{"sender bare", "{{sender.first_name}}", "{{.Sender.FirstName}}"},
		{"sender variable", "{{$.sender.signature_html}}", "{{$.Sender.SignatureHTML}}"},
		{"function untouched", "{{upper .FirstName}}", "{{upper .FirstName}}"},
		{"unsubscribe alias", "{{unsubscribe_link}}", "{{.UnsubscribeLink}}"},
		{"sender through a variable", "{{$s := .Sender}}{{$s.first_name}}", "{{$s := .Sender}}{{$s.FirstName}}"},
		{"sender through with", "{{with .sender}}{{.signature_html}}{{end}}", "{{with .sender}}{{.SignatureHTML}}{{end}}"},
		{"sender through root variable", "{{$r := .}}{{$r.Sender.email}}", "{{$r := .}}{{$r.Sender.Email}}"},
		{"range element untouched", "{{range .Sender.Tags}}{{.}}{{end}}", "{{range .Sender.Tags}}{{.}}{{end}}"},
		{"range variables untouched", "{{range $k, $v := .}}{{$k}}={{$v}}{{end}}", "{{range $k, $v := .}}{{$k}}={{$v}}{{end}}"},
		{"bare inside with uses the root", "{{with .Sender}}{{first_name}}{{end}}", "{{with .Sender}}{{$.FirstName}}{{end}}"},
		{"sender through parentheses", "{{with (.Sender)}}{{.first_name}}{{end}}", "{{with (.Sender)}}{{.FirstName}}{{end}}"},
		{"selector after parenthesised sender", "{{(.Sender).first_name}}", "{{(.Sender).FirstName}}"},
		{"spaced key stops before an alias operand", "{{if and .Company first_name}}y{{end}}", "{{if and .Company .FirstName}}y{{end}}"},
		{"spaced key as an argument", `{{eq .job role "x"}}`, `{{eq (index . "job role") "x"}}`},
		{"spaced key stops before a function word", `{{and .job title}}`, `{{and .job title}}`},
		{"spaced key at the head keeps every word", "{{.Company Email}} {{if .Work Phone}}y{{end}}", `{{(index . "Company Email")}} {{if (index . "Work Phone")}}y{{end}}`},
		{"dashed key keeps an alias word", "{{and .X .job-email}}", `{{and .X (index . "job-email")}}`},
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
		{"{{.FirstName.Bogus}}", `field FirstName has no field "Bogus"`},
		{"{{(.Sender).Bogus}}", `unknown sender field "Bogus"`},
		{"{{first_name.Bogus}}", `field first_name has no field "Bogus"`},
		{`{{with .Sender | default .Sender}}{{.first_name}}{{end}}`, "fields of this value cannot be checked"},
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

func TestCheckScopesBranchVariables(t *testing.T) {
	for _, ok := range []string{
		"{{$s := .Sender}}{{if .Company}}{{$s = .Company}}{{else}}{{$s.Email}}{{end}}",
		"{{$s := .Sender}}{{if .X}}{{$s := .Other}}{{else}}{{$s.first_name}}{{end}}{{$s.Email}}",
		"{{$s := .Sender}}{{with .Company}}{{$s := .}}{{else}}{{$s.Email}}{{end}}{{$s.Name}}",
		"{{$s := .Sender}}{{range .Sender.Tags}}{{$s := .}}{{$s}}{{end}}{{$s.Name}}",
	} {
		if err := Check(ok); err != nil {
			t.Errorf("%q: unexpected error %v", ok, err)
		}
	}
	// A reassignment in a branch leaves the variable unknown after the structure.
	if err := Check("{{$s := .Sender}}{{if .Company}}{{$s = .Company}}{{end}}{{$s.Email}}"); err == nil {
		t.Error("a field of a variable a branch reassigned should be refused")
	}
}

func TestCanonicalizeStoresBareAliasesInCanonicalForm(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Hi {{first_name}}", "Hi {{.FirstName}}"},
		{"{{ company_name }} and {{companyName}}", "{{ .Company }} and {{.Company}}"},
		{`{{if first_name}}{{first_name | default "there"}}{{end}}`, `{{if .FirstName}}{{.FirstName | default "there"}}{{end}}`},
		{"{{with .Sender}}{{first_name}} / {{.first_name}}{{end}}", "{{with .Sender}}{{$.FirstName}} / {{.FirstName}}{{end}}"},
		{"{{.first_name}} {{.firstName}}", "{{.first_name}} {{.firstName}}"},
		{"{{sender.email}}", "{{.Sender.Email}}"},
		{"{{.job title}} {{first_name}} {{.dept-name}} {{last_name}}", "{{.job title}} {{.FirstName}} {{.dept-name}} {{.LastName}}"},
		{"first_name {first_name} {{form_link:abc123}} {{first_name}}", "first_name {first_name} {{form_link:abc123}} {{.FirstName}}"},
		{"{{if .X}}broken {{first_name}}", "{{if .X}}broken {{first_name}}"},
		{"no actions", "no actions"},
	}
	for _, tc := range cases {
		if got := Canonicalize(tc.in); got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCheckHTMLRefusesEscaperBeforePipe(t *testing.T) {
	for _, bad := range []string{
		`<p>{{html .FirstName | printf "%s"}}</p>`,
		`{{if .Company}}<a href="{{else}}<p>{{end}}x</p>`,
		`<a {{if .X}}href="{{end}}x">`,
	} {
		if err := CheckHTML(bad); err == nil {
			t.Errorf("%q: the HTML renderer refuses it, so the check should too", bad)
		}
	}
	for _, ok := range []string{
		`<!--[if mso]><table width="{{.width}}"><![endif]--><p>{{if .Company}}At <b>{{.Company}}</b>{{else}}Hi{{end}}</p>`,
		`<a href="https://example.com/?n={{.FirstName}}">{{.Sender.Name}}</a><style>p{color:{{.color}}}</style>`,
		`<p>Hi {{.FirstName | default "there"}},</p>{{.Sender.SignatureHTML}}`,
	} {
		if err := CheckHTML(ok); err != nil {
			t.Errorf("%q: unexpected error %v", ok, err)
		}
	}
	for _, ok := range []string{"", "<p>Hi {{.FirstName}}</p>", `<a href="{{.website}}">x</a>`, "<!-- {{.FirstName}} --><p>{{.missing}}</p>", `<p>{{.FirstName | html}}</p>`} {
		if err := CheckHTML(ok); err != nil {
			t.Errorf("%q: unexpected error %v", ok, err)
		}
	}
	if err := CheckCopy(Part{Name: "body", Text: `{{urlquery .X | printf "%s"}}`, HTML: true}); err == nil || !strings.Contains(err.Error(), "the body") {
		t.Errorf("CheckCopy should run the HTML check on an HTML part: %v", err)
	}
}

func TestReferences(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"Hi {{.FirstName}}", true},
		{"Hi {{first_name}}", true},
		{`{{.firstName | default "there"}}`, true},
		{`{{$.first_name}}`, true},
		{`{{printf "firstname"}}`, false},
		{"{{.firstname_source}}", false},
		{"{{.Sender.FirstName}}", false},
		{"firstname in prose", false},
	}
	for _, tc := range cases {
		if got := References(tc.in)["FirstName"]; got != tc.want {
			t.Errorf("%q: got %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestContactReads(t *testing.T) {
	cases := []struct {
		in        string
		builtins  []string
		other, ok bool
	}{
		{"no fields", nil, false, true},
		{"Hi {{.FirstName}} at {{company_name}}", []string{"FirstName", "Company"}, false, true},
		{`Hi {{.first_name | default "there"}}, {{.Sender.FirstName}}`, []string{"FirstName", "Sender"}, false, true},
		{"About {{.industry}}", nil, true, true},
		{`{{index . "city"}}`, nil, true, true},
		{`{{index $ "city"}}`, nil, true, true},
		{"{{range $k, $v := .}}{{$k}}{{end}}", nil, true, true},
		{"{{with .FirstName}}{{.}}{{end}}", []string{"FirstName"}, false, true},
		{"Hi {{frist_name}}", nil, false, false},
	}
	for _, tc := range cases {
		builtins, other, ok := ContactReads(tc.in)
		if ok != tc.ok || other != tc.other {
			t.Errorf("%q: other=%v ok=%v, want other=%v ok=%v", tc.in, other, ok, tc.other, tc.ok)
			continue
		}
		for _, b := range tc.builtins {
			if !builtins[b] {
				t.Errorf("%q: %s not reported among %v", tc.in, b, builtins)
			}
		}
	}
}
