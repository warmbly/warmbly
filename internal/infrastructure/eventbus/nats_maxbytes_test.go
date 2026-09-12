package eventbus

import "testing"

// The ceiling is written by hand in a dashboard or an env file, so the shapes
// an operator actually types have to parse: a bare count, binary and decimal
// suffixes, and the fractional form a console shows (2.5 GiB).
func TestParseByteSize(t *testing.T) {
	cases := map[string]int64{
		"":         0,
		"1024":     1024,
		"1KiB":     1024,
		"1 KiB":    1024,
		"2GiB":     2 << 30,
		"2.5GiB":   int64(2.5 * float64(1<<30)),
		"512MB":    512_000_000,
		"1G":       1 << 30,
		"1B":       1,
		"  4MiB  ": 4 << 20,
		"1gib":     1 << 30,
	}
	for in, want := range cases {
		got, err := parseByteSize(in)
		if err != nil {
			t.Errorf("%q: unexpected error %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%q = %d, want %d", in, got, want)
		}
	}
}

// A typo must stop the process at boot, not silently leave the stream
// unbounded on an account that will then refuse to create it.
func TestParseByteSizeRejectsNonsense(t *testing.T) {
	for _, in := range []string{"big", "1XB", "-5", "-1GiB", "1.2.3GiB"} {
		if _, err := parseByteSize(in); err == nil {
			t.Errorf("%q was accepted", in)
		}
	}
}

// JetStream spells "no ceiling" as -1; a literal 0 in a StreamConfig means
// zero bytes, which would reject every message.
func TestMaxBytesOrUnlimited(t *testing.T) {
	if got := maxBytesOrUnlimited(0); got != -1 {
		t.Errorf("unset = %d, want -1", got)
	}
	if got := maxBytesOrUnlimited(1 << 30); got != 1<<30 {
		t.Errorf("set = %d, want %d", got, 1<<30)
	}
}
