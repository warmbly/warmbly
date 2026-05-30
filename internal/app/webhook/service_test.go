package webhook

import (
	"strings"
	"testing"
	"time"
)

func TestSign_DeterministicForSameInputs(t *testing.T) {
	secret := "whsec_test"
	ts := time.Unix(1700000000, 0)
	payload := []byte(`{"hello":"world"}`)

	a := Sign(secret, ts, payload)
	b := Sign(secret, ts, payload)
	if a != b {
		t.Fatalf("signature should be deterministic; got %s vs %s", a, b)
	}
	if len(a) != 64 { // SHA-256 hex = 64 chars
		t.Errorf("expected 64-char hex signature, got %d chars", len(a))
	}
}

func TestSign_DiffersBySecret(t *testing.T) {
	ts := time.Unix(1700000000, 0)
	payload := []byte(`{"x":1}`)
	a := Sign("whsec_a", ts, payload)
	b := Sign("whsec_b", ts, payload)
	if a == b {
		t.Error("different secrets must produce different signatures")
	}
}

func TestSign_DiffersByTimestamp(t *testing.T) {
	secret := "whsec_test"
	payload := []byte(`{}`)
	a := Sign(secret, time.Unix(100, 0), payload)
	b := Sign(secret, time.Unix(200, 0), payload)
	if a == b {
		t.Error("different timestamps must produce different signatures")
	}
}

func TestFormatSignatureHeader_Shape(t *testing.T) {
	ts := time.Unix(1700000000, 0)
	got := FormatSignatureHeader(ts, "abc123")
	if !strings.HasPrefix(got, "t=1700000000,v1=") {
		t.Errorf("unexpected header format: %q", got)
	}
}

func TestBackoffFor_GrowsAndCaps(t *testing.T) {
	got1 := backoffFor(1)
	if got1 != 30*time.Second {
		t.Errorf("attempt 1 should be 30s, got %v", got1)
	}
	got2 := backoffFor(2)
	if got2 != time.Minute {
		t.Errorf("attempt 2 should be 1m, got %v", got2)
	}
	// Attempts past cap should clamp at 1h.
	got20 := backoffFor(20)
	if got20 != time.Hour {
		t.Errorf("very large attempt count should clamp to 1h, got %v", got20)
	}
}

func TestValidateURL_RejectsBadInput(t *testing.T) {
	cases := map[string]bool{
		"":                           true,
		"not-a-url":                  true,
		"ftp://x.com":                true,
		"javascript:alert(1)":        true,
		"http://":                    true,
		"https://example.com/hook":   false,
		"http://localhost:3000/hook": true,
		"https://localhost/hook":     true,
		"https://127.0.0.1/hook":     true,
		"https://10.0.0.10/hook":     true,
	}
	for input, wantErr := range cases {
		err := validateURL(input)
		if wantErr && err == nil {
			t.Errorf("validateURL(%q) should have errored", input)
		}
		if !wantErr && err != nil {
			t.Errorf("validateURL(%q) unexpected error: %v", input, err)
		}
	}
}

func TestValidateURL_AllowsUnsafeLocalDevelopmentWhenEnabled(t *testing.T) {
	t.Setenv("WARMBLY_ALLOW_UNSAFE_WEBHOOK_URLS", "true")

	if err := validateURL("http://localhost:3000/hook"); err != nil {
		t.Fatalf("expected unsafe local URL to be accepted in development mode: %v", err)
	}
}
