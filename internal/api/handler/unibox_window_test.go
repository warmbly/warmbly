package handler

import (
	"testing"
	"time"
)

func TestParseUniboxWindow(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, tc := range []struct {
		name, since, until    string
		wantSince, wantBefore string
		wantErr               bool
	}{
		{name: "empty"},
		{name: "date since is UTC midnight", since: "2026-03-04", wantSince: "2026-03-04T00:00:00Z"},
		{name: "date until keeps the whole day", until: "2026-03-04", wantBefore: "2026-03-05T00:00:00Z"},
		{name: "one day", since: "2026-03-04", until: "2026-03-04", wantSince: "2026-03-04T00:00:00Z", wantBefore: "2026-03-05T00:00:00Z"},
		{name: "month end rolls over", until: "2026-02-28", wantBefore: "2026-03-01T00:00:00Z"},
		{name: "RFC 3339 instants are exact", since: "2026-03-04T10:00:00Z", until: "2026-03-04T12:30:00.5+02:00", wantSince: "2026-03-04T10:00:00Z", wantBefore: "2026-03-04T10:30:00.5Z"},
		{name: "unescaped plus offset", since: "2026-03-04T10:00:00 02:00", wantSince: "2026-03-04T08:00:00Z"},
		{name: "garbage since", since: "yesterday", wantErr: true},
		{name: "garbage until", until: "03/04/2026", wantErr: true},
		{name: "impossible date", since: "2026-02-30", wantErr: true},
		{name: "timestamp without zone", until: "2026-03-04T10:00:00", wantErr: true},
		{name: "unix seconds", since: "1772582400", wantErr: true},
		{name: "an inverted window is empty, not refused", since: "2026-03-05", until: "2026-03-04", wantSince: "2026-03-05T00:00:00Z", wantBefore: "2026-03-05T00:00:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			since, before, xerr := parseUniboxWindow(tc.since, tc.until)
			if tc.wantErr {
				if xerr == nil {
					t.Fatalf("want a refusal, got [%v, %v)", since, before)
				}
				if xerr.ResponseCode() != "invalid_filter" {
					t.Fatalf("code = %q, want invalid_filter", xerr.ResponseCode())
				}
				return
			}
			if xerr != nil {
				t.Fatalf("unexpected refusal: %v", xerr)
			}
			check := func(label string, got *time.Time, want string) {
				t.Helper()
				if want == "" {
					if got != nil {
						t.Fatalf("%s = %v, want unset", label, got)
					}
					return
				}
				if got == nil || !got.Equal(at(want)) {
					t.Fatalf("%s = %v, want %s", label, got, want)
				}
			}
			check("since", since, tc.wantSince)
			check("before", before, tc.wantBefore)
		})
	}
}
