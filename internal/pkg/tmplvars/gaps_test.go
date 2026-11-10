package tmplvars

import "testing"

func TestBlankGaps(t *testing.T) {
	cases := []struct {
		in  string
		gap bool
	}{
		{"Hi {{.FirstName}},", true},
		{"Hi {{first_name}},", true},
		{"Hi {{.first_name | title}},", true},
		{`{{printf "Hi %s," .FirstName}}`, true},
		{"Hi {{$.FirstName}},", true},
		{"{{if .FirstName}}Bonjour {{.FirstName}},{{else}}Bonjour,{{end}}", false},
		{"{{if .first_name}}Hi {{first_name}}{{end}}", false},
		{"{{if .FirstName}}Hi {{.FirstName}}{{end}} and later {{.FirstName}}", true},
		{"{{if .FirstName}}Hi{{else}}Hi {{.FirstName}}{{end}}", true},
		{"{{with .FirstName}}Hi {{.}},{{else}}Hi,{{end}}", false},
		{`{{.FirstName | default "there"}}`, false},
		{`{{default "there" .FirstName}}`, false},
		{`{{or .FirstName "there"}}`, false},
		{"{{if and .FirstName .Company}}{{.FirstName}} at {{.Company}}{{end}}", false},
		{"{{if not .FirstName}}Hi,{{else}}Hi {{.FirstName}},{{end}}", false},
		{`{{if ne .FirstName ""}}Hi {{.FirstName}}{{end}}`, false},
		{`{{if eq .FirstName ""}}Hi{{else}}Hi {{.FirstName}}{{end}}`, false},
		{"{{if $n := .FirstName}}Hi {{$n}}{{end}}", false},
		{"{{$n := .FirstName}}Hi {{$n}}", true},
		{"{{if .Company}}Hi {{.FirstName}}{{end}}", true},
		{"{{if .FirstName | trim}}Hi {{.FirstName}}{{end}}", false},
		{"{{if .FirstName}}{{else if .Company}}Team {{.Company}}{{end}}", false},
		{"{{range .tags}}{{$.FirstName}}{{end}}", true},
		{"{{.Sender.FirstName}}", false},
		{"{{if .FirstName}}{{end}}", false},
		{"no template here", false},
	}
	for _, tc := range cases {
		gaps, ok := BlankGaps(tc.in)
		if !ok {
			t.Errorf("%q: did not parse", tc.in)
			continue
		}
		if gaps["FirstName"] != tc.gap {
			t.Errorf("%q: gap=%v, want %v", tc.in, gaps["FirstName"], tc.gap)
		}
	}
	if _, ok := BlankGaps("{{if .FirstName}}unclosed"); ok {
		t.Error("an unparseable template reported ok")
	}
}
