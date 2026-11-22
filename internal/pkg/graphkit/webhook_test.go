package graphkit

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestVerifySignature(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	body := []byte(`{"id":"evt_1","type":"mailbox.active"}`)
	current, previous := "whsec_current", "whsec_previous"
	signed := Sign(current, now, body)
	byPrevious := Sign(previous, now, body)
	// During a rotation GraphKit signs with both: t=…,v1=<current>,v1=<previous>.
	rotation := signed + ",v1=" + strings.SplitN(byPrevious, "v1=", 2)[1]

	cases := []struct {
		name    string
		header  string
		body    []byte
		secrets []string
		at      time.Time
		want    error
	}{
		{"current secret", signed, body, []string{current}, now, nil},
		{"previous secret still verifies", byPrevious, body, []string{current, previous}, now, nil},
		{"rotation header with only the new secret", rotation, body, []string{current}, now, nil},
		{"rotation header with only the old secret", rotation, body, []string{"", previous}, now, nil},
		{"4m59s old", signed, body, []string{current}, now.Add(4*time.Minute + 59*time.Second), nil},
		{"older than five minutes", signed, body, []string{current}, now.Add(5*time.Minute + time.Second), ErrSignatureExpired},
		{"from the future", signed, body, []string{current}, now.Add(-6 * time.Minute), ErrSignatureExpired},
		{"tampered body", signed, []byte(`{"id":"evt_1","type":"mailbox.cancelled"}`), []string{current}, now, ErrSignatureMismatch},
		{"unknown secret", signed, body, []string{"whsec_other"}, now, ErrSignatureMismatch},
		{"previous secret no longer configured", byPrevious, body, []string{current}, now, ErrSignatureMismatch},
		{"missing header", "", body, []string{current}, now, ErrSignatureMissing},
		{"no v1", "t=" + strings.TrimPrefix(strings.SplitN(signed, ",", 2)[0], "t="), body, []string{current}, now, ErrSignatureMissing},
		{"short v1", "t=1,v1=abcd", body, []string{current}, now, ErrSignatureMissing},
		{"no secret configured", signed, body, []string{"", " "}, now, ErrNoSecret},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifySignature(tc.header, tc.body, tc.secrets, tc.at)
			if !errors.Is(err, tc.want) {
				t.Fatalf("VerifySignature = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestSignatureTimestampIsSigned(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	body := []byte(`{}`)
	h := Sign("s", now, body)
	// Replaying the same v1 with a fresher t must not verify.
	moved := strings.Replace(h, "t=1800000000", "t=1800000100", 1)
	if err := VerifySignature(moved, body, []string{"s"}, now.Add(100*time.Second)); !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("re-timestamped signature: %v, want mismatch", err)
	}
}
