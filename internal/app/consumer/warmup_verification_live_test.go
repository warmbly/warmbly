package jobs

import (
	"testing"
)

func TestFirstSenderAddress(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Warm Up <a@b.com>", "a@b.com"},
		{"a@b.com", "a@b.com"},
		{`"Up, Warm" <a@b.com>`, "a@b.com"},
		{"<a@b.com>", "a@b.com"},
		{"not an address", ""},
		{"", ""},
	} {
		if got := firstSenderAddress([]string{tc.in}); got != tc.want {
			t.Errorf("firstSenderAddress(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := firstSenderAddress(nil); got != "" {
		t.Errorf("firstSenderAddress(nil) = %q, want empty", got)
	}
}
