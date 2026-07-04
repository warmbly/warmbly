package imap

import (
	"slices"
	"strings"
	"testing"
)

func TestParseHeaderFlags(t *testing.T) {
	raw := "X-Mailtrace-Verify: tok123\r\n" +
		"Auto-Submitted: auto-replied\r\n" +
		"Content-Type: multipart/report; report-type=delivery-status\r\n" +
		"Subject: not in the fetch list\r\n\r\n"

	got := parseHeaderFlags(strings.NewReader(raw))

	if !slices.Contains(got, "X-Mailtrace-Verify:tok123") {
		t.Errorf("warmup token flag missing: %v", got)
	}
	if !slices.Contains(got, "Auto-Submitted:auto-replied") {
		t.Errorf("classification flag missing: %v", got)
	}
	// Content-Type value contains ':' and ';' — must survive intact (split on
	// the first colon only, by the consumer's buildReplyHeaders).
	if !slices.Contains(got, "Content-Type:multipart/report; report-type=delivery-status") {
		t.Errorf("content-type flag wrong: %v", got)
	}
	// Subject is not in headerFetchFields, so it must not appear.
	for _, f := range got {
		if strings.HasPrefix(f, "Subject:") {
			t.Errorf("unexpected Subject flag: %v", got)
		}
	}
}

func TestParseHeaderFlags_NilAndEmpty(t *testing.T) {
	if got := parseHeaderFlags(nil); got != nil {
		t.Errorf("nil reader should yield nil, got %v", got)
	}
	if got := parseHeaderFlags(strings.NewReader("\r\n")); len(got) != 0 {
		t.Errorf("empty header block should yield no flags, got %v", got)
	}
}
