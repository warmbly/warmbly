package kms

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"io"
	"testing"
)

func newTestLocal(t *testing.T) *LocalProvider {
	t.Helper()
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		t.Fatal(err)
	}
	p, err := NewLocal(key)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLocalProvider_RoundTrip(t *testing.T) {
	p := newTestLocal(t)
	ctx := context.Background()

	plain, ct, err := p.GenerateDataKey(ctx)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(plain) != 32 {
		t.Fatalf("plaintext DEK should be 32 bytes, got %d", len(plain))
	}
	if ct == "" {
		t.Fatal("ciphertext should not be empty")
	}

	got, err := p.GetDecryptedKey(ctx, ct)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(plain, got) {
		t.Fatal("round-trip plaintext mismatch")
	}
}

func TestLocalProvider_FreshNonce(t *testing.T) {
	p := newTestLocal(t)
	ctx := context.Background()

	_, ct1, err := p.GenerateDataKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, ct2, err := p.GenerateDataKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ct1 == ct2 {
		t.Fatal("two DEKs should produce distinct ciphertexts (nonce reuse?)")
	}
}

func TestLocalProvider_RejectsBadKey(t *testing.T) {
	if _, err := NewLocal(make([]byte, 16)); err == nil {
		t.Fatal("expected error for 16-byte key")
	}
	if _, err := NewLocal(nil); err == nil {
		t.Fatal("expected error for nil key")
	}
}

func TestLocalProvider_RejectsTamperedCiphertext(t *testing.T) {
	p := newTestLocal(t)
	ctx := context.Background()

	_, ct, err := p.GenerateDataKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.StdEncoding.DecodeString(ct)
	raw[len(raw)-1] ^= 0xFF
	tampered := base64.StdEncoding.EncodeToString(raw)

	if _, err := p.GetDecryptedKey(ctx, tampered); err == nil {
		t.Fatal("expected decrypt to fail on tampered ciphertext")
	}
}

func TestLocalProvider_Name(t *testing.T) {
	p := newTestLocal(t)
	if p.Name() != "local" {
		t.Fatalf("expected name 'local', got %q", p.Name())
	}
}
