package emailaddr

import "testing"

func TestExtract_liveUniboxAndRFC(t *testing.T) {
	// Live VPS Unibox from_addr samples (CONFENGE self-smoke / replies).
	cases := []struct {
		in   string
		want string
	}{
		// Hostinger self-reply form that blocked confenge handoff
		{" (tiago.sasaki@confenge.com.br)", "tiago.sasaki@confenge.com.br"},
		{"(tiago.sasaki@confenge.com.br)", "tiago.sasaki@confenge.com.br"},
		// Display + parenthetical (also seen on VPS)
		{"Tiago Sasaki (tiago.sasaki@confenge.com.br)", "tiago.sasaki@confenge.com.br"},
		{"Tiago Sasaki (tiago.sasaki@gmail.com)", "tiago.sasaki@gmail.com"},
		// Clean bare (force path / some IMAP rows)
		{"tiago.sasaki@confenge.com.br", "tiago.sasaki@confenge.com.br"},
		// RFC 5322
		{"Aiden Park <aiden.park@northwind.test>", "aiden.park@northwind.test"},
		{"\"Tiago Sasaki\" <tiago.sasaki@confenge.com.br>", "tiago.sasaki@confenge.com.br"},
		{"<tiago.sasaki@confenge.com.br>", "tiago.sasaki@confenge.com.br"},
		// Empty / garbage
		{"", ""},
		{"   ", ""},
		{"not-an-email", ""},
		{"()", ""},
	}
	for _, tc := range cases {
		got := Extract(tc.in)
		if got != tc.want {
			t.Errorf("Extract(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestExtractFirst(t *testing.T) {
	got := ExtractFirst([]string{"", " (tiago.sasaki@confenge.com.br)"})
	if got != "tiago.sasaki@confenge.com.br" {
		t.Fatalf("ExtractFirst = %q", got)
	}
	if ExtractFirst(nil) != "" {
		t.Fatal("nil should be empty")
	}
}
