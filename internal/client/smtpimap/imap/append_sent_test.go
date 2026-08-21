package imap

import "testing"

func TestLeaf(t *testing.T) {
	cases := map[string]string{
		"Sent":            "Sent",
		"INBOX.Sent":      "Sent",
		"INBOX/Sent Mail": "Sent Mail",
		"":                "",
		"Sent/":           "Sent/",
	}
	for in, want := range cases {
		if got := leaf(in); got != want {
			t.Fatalf("leaf(%q) = %q want %q", in, got, want)
		}
	}
}

func TestImapSentCoversCommonNames(t *testing.T) {
	// The name list is the fallback when a server does not advertise the
	// RFC 6154 \Sent attribute; these are what the common servers call it.
	for _, name := range []string{"Sent", "Sent Items", "Sent Mail"} {
		found := false
		for _, candidate := range ImapSent {
			if candidate == name {
				found = true
			}
		}
		if !found {
			t.Fatalf("ImapSent does not cover %q", name)
		}
	}
}
