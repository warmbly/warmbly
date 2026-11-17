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

func TestAddressListDropsUnparseableEntries(t *testing.T) {
	got := AddressList([]string{"not an address", "a@b.com\r\nBcc: c@d.com", "a@b.com, c@d.com", "ok@b.com"})
	if got != "ok@b.com" {
		t.Fatalf("got %q", got)
	}
}

func TestAddressReadsLegacyParenthesisedForm(t *testing.T) {
	if got := Address("Ana (a@b.com)"); got != "a@b.com" {
		t.Fatalf("got %q", got)
	}
}

func TestValidAddress(t *testing.T) {
	for _, s := range []string{"a@b.com", " Ana <a@b.com> ", `"Doe, John" <j@d.com>`, "Ana Rodríguez <a@b.com>"} {
		if !ValidAddress(s) {
			t.Errorf("ValidAddress(%q) = false", s)
		}
	}
	for _, s := range []string{"", "nope", "a@b.com, c@d.com", "Ana <a@b.com>, Bob <b@c.com>", "a@b.com\r\nBcc: c@d.com", "Ana\n<a@b.com>", "a@b.com\x00"} {
		if ValidAddress(s) {
			t.Errorf("ValidAddress(%q) = true", s)
		}
	}
}

func TestValidMessageID(t *testing.T) {
	for _, s := range []string{"<abc@example.com>", "abc@example.com", "<CAF+x=y_z@mail.gmail.com>"} {
		if !ValidMessageID(s) {
			t.Errorf("ValidMessageID(%q) = false", s)
		}
	}
	for _, s := range []string{"", "<>", "<a@b>\r\nBcc: x@y.com", "<a b@c>", "<a@b><c@d>", "a\tb"} {
		if ValidMessageID(s) {
			t.Errorf("ValidMessageID(%q) = true", s)
		}
	}
}

func TestCheckHeader(t *testing.T) {
	if err := CheckHeader("In-Reply-To", "<a@b.com>"); err != nil {
		t.Fatalf("CheckHeader refused a plain header: %v", err)
	}
	for _, h := range [][2]string{{"To", "a@b.com\r\nBcc: c@d.com"}, {"To", "a@b.com\nX: y"}, {"X-A", "v\x00"}, {"Bad Name", "v"}, {"X:Y", "v"}, {"", "v"}} {
		if CheckHeader(h[0], h[1]) == nil {
			t.Errorf("CheckHeader(%q, %q) = nil", h[0], h[1])
		}
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

func TestReplySubjectPrefixesOnce(t *testing.T) {
	for in, want := range map[string]string{
		"Coffee or tea at work": "Re: Coffee or tea at work",
		"  Podcast habits ":     "Re: Podcast habits",
		"Re: Podcast habits":    "Re: Podcast habits",
		"RE: Podcast habits":    "RE: Podcast habits",
		"":                      "Re:",
	} {
		if got := ReplySubject(in); got != want {
			t.Errorf("ReplySubject(%q) = %q, want %q", in, got, want)
		}
	}
}
