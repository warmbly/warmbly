package mailhdr

import "testing"

func TestSubjectEncodesNonASCIIOnly(t *testing.T) {
	if got := Subject("Quick question about pricing"); got != "Quick question about pricing" {
		t.Fatalf("ascii subject should pass through, got %q", got)
	}
	got := Subject("Café ☕ update")
	if got == "Café ☕ update" {
		t.Fatalf("non-ascii subject was not encoded: %q", got)
	}
	if got[:2] != "=?" {
		t.Fatalf("expected an RFC 2047 encoded-word, got %q", got)
	}
}

func TestAddressListEncodesDisplayNames(t *testing.T) {
	got := AddressList([]string{"ana@example.com", "Ana Rodríguez <ana2@example.com>", "  "})
	want := `ana@example.com, =?utf-8?q?Ana_Rodr=C3=ADguez?= <ana2@example.com>`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestAddressListKeepsUnparseableEntries(t *testing.T) {
	if got := AddressList([]string{"not an address"}); got != "not an address" {
		t.Fatalf("got %q", got)
	}
}

func TestBareStripsDisplayName(t *testing.T) {
	cases := map[string]string{
		"Ana <a@b.com>":         "a@b.com",
		"a@b.com":               "a@b.com",
		`"Doe, John" <j@d.com>`: "j@d.com",
		"Broken <x@y.com":       "Broken <x@y.com",
	}
	for in, want := range cases {
		if got := Bare(in); got != want {
			t.Fatalf("Bare(%q) = %q want %q", in, got, want)
		}
	}
}

func TestDecodeWords(t *testing.T) {
	cases := map[string]string{
		"=?utf-8?q?caf=C3=A9?= update":   "café update",
		"=?UTF-8?B?SsO2cmc=?= <j@d.com>": "Jörg <j@d.com>",
		"=?iso-8859-1?Q?caf=E9?=":        "café",
		"Plain subject":                  "Plain subject",
		"=?not-a-charset?Q?x?=":          "=?not-a-charset?Q?x?=",
	}
	for in, want := range cases {
		if got := DecodeWords(in); got != want {
			t.Fatalf("DecodeWords(%q) = %q want %q", in, got, want)
		}
	}
}

func TestSubjectRoundTrip(t *testing.T) {
	original := "Café ☕ update"
	if got := DecodeWords(Subject(original)); got != original {
		t.Fatalf("round trip lost content: %q", got)
	}
}
