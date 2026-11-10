package displayname

import (
	"strings"
	"testing"
)

func TestCleanOr(t *testing.T) {
	long := strings.Repeat("é", 70)
	cases := []struct{ in, want string }{
		{"Acme Staging", "Acme Staging"},
		{"warmbly.acme.com", "fallback"},
		{"visit https://evil.example", "fallback"},
		{"", "fallback"},
		{long, strings.Repeat("é", WorkspaceMaxLength)},
	}
	for _, c := range cases {
		if got := CleanOr(c.in, Workspace, "fallback"); got != c.want {
			t.Errorf("CleanOr(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCheck(t *testing.T) {
	cases := []struct {
		in   string
		kind Kind
		want Rejection
	}{
		{"Ada", Person, OK},
		{"  Mary   Ann ", Person, OK},
		{"O'Brien-Smith", Person, OK},
		{"J.R.R. Tolkien", Person, OK},
		{"St. John", Person, OK},
		{"Zo\u00eb", Person, OK},
		{"\u674e\u5c0f\u9f99", Person, OK},
		{"Acme 2.0", Workspace, OK},
		{"3M", Workspace, OK},
		{"", Person, Empty},
		{"   ", Person, Empty},
		{"https://www.google.com", Person, Link},
		{"www.example", Person, Link},
		{"google.com", Person, Link},
		{"Visit evil.co now", Workspace, Link},
		{"\uff47\uff4f\uff4f\uff47\uff4c\uff45\uff0e\uff43\uff4f\uff4d", Person, Link},
		{"google\u3002com", Person, Link},
		{"a@b", Person, Link},
		{"javascript:alert(1)", Person, Link},
		{"mailto:x", Person, Link},
		{"Big Data: EU", Workspace, OK},
		{"//evil", Person, Link},
		{"10.0.0.1", Workspace, Link},
		{"xn--80ak6aa92e.xn--p1ai", Workspace, Link},
		{"Ada\u202eevil", Person, Characters},
		{"Ada\u200b", Person, Characters},
		{"Ada\nLovelace", Person, Characters},
		{"<b>Ada</b>", Person, Characters},
		{"Z\u0301\u0301\u0301\u0301\u0301", Person, Characters},
		{"1234", Person, NoLetter},
		{"!!!", Workspace, NoLetter},
		{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Person, TooLong},
		{"Welcome back", Heading, OK},
		{"Sign in to see how your campaigns are doing. Questions? Ask your account manager.", Message, OK},
		{strings.Repeat("a", HeadingMaxLength), Heading, OK},
		{strings.Repeat("a", HeadingMaxLength+1), Heading, TooLong},
		{strings.Repeat("é", MessageMaxLength), Message, OK},
		{strings.Repeat("a", MessageMaxLength+1), Message, TooLong},
		{"Sign in at portal.agency.com", Message, Link},
		{"Write to help@agency.com", Message, Link},
		{"Welcome <b>back</b>", Heading, Characters},
		{"Welcome\u202eback", Heading, Characters},
		{"Line one\nline two", Message, Characters},
		{"2026", Heading, OK},
		{"...", Message, NoLetter},
	}
	for _, c := range cases {
		if _, got := Check(c.in, c.kind); got != c.want {
			t.Errorf("Check(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestFromEmail(t *testing.T) {
	cases := map[string]string{
		"john.smith@example.com": "john smith",
		"ada+tag@example.com":    "ada",
		"google.com@example.com": "google com",
		"www.evil.com@x.io":      "www evil com",
		"12345@example.com":      "",
		"no-at-sign":             "",
	}
	for in, want := range cases {
		if got := FromEmail(in); got != want {
			t.Errorf("FromEmail(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDisplayable(t *testing.T) {
	if got := Displayable("https://evil.example"); got != "" {
		t.Errorf("Displayable kept a link: %q", got)
	}
	long := "A very long workspace name that predates the sixty four character bound"
	if got := Displayable(long); got != long {
		t.Errorf("Displayable dropped a long legacy name: %q", got)
	}
}

func TestDefaultWorkspace(t *testing.T) {
	cases := map[string]string{
		"":         "My Organization",
		"  ":       "My Organization",
		"Ada":      "Ada's Organization",
		"evil.com": "My Organization",
	}
	for in, want := range cases {
		if got := DefaultWorkspace(in); got != want {
			t.Errorf("DefaultWorkspace(%q) = %q, want %q", in, got, want)
		}
	}
}
