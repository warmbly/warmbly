package crypt

import (
	"encoding/base64"
	"testing"
)

func TestNonce(t *testing.T) {
	n1, err := Nonce()
	if err != nil {
		t.Fatalf("Nonce() returned unexpected error: %v", err)
	}
	n2, err := Nonce()
	if err != nil {
		t.Fatalf("Nonce() returned unexpected error: %v", err)
	}

	// Should not be empty
	if n1 == "" {
		t.Error("Nonce() returned empty string")
	}

	// Should differ most of the time
	if n1 == n2 {
		t.Error("Nonce() returned same value twice (unexpectedly identical)")
	}

	// Should decode as base64.RawURLEncoding
	data, err := base64.RawURLEncoding.DecodeString(n1)
	if err != nil {
		t.Fatalf("Nonce() returned invalid base64 string: %v", err)
	}

	// Should be 16 bytes decoded
	if len(data) != 16 {
		t.Errorf("Nonce() decoded to %d bytes, want 16", len(data))
	}
}
