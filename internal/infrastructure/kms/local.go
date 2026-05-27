package kms

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
)

// LocalProvider implements Provider using a single AES-256 master key held in
// process memory. The master key is sourced from an env var or a file at boot
// and never leaves the process.
//
// Ciphertext blob format:
//
//	nonce (12 bytes) || ciphertext+tag (AES-256-GCM)
//
// base64-encoded for storage.
//
// Suitable for self-hosted deployments that don't want a managed KMS. Key
// rotation requires re-encrypting every stored DEK; see the docs/encryption
// runbook before rotating.
type LocalProvider struct {
	gcm cipher.AEAD
}

// NewLocal builds a LocalProvider from a 32-byte master key.
func NewLocal(masterKey []byte) (*LocalProvider, error) {
	if len(masterKey) != 32 {
		return nil, fmt.Errorf("local kms: master key must be 32 bytes (AES-256), got %d", len(masterKey))
	}
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &LocalProvider{gcm: gcm}, nil
}

// NewLocalFromEnv constructs a LocalProvider from either:
//
//	KMS_LOCAL_MASTER_KEY      — base64-encoded 32-byte key
//	KMS_LOCAL_MASTER_KEY_FILE — path to a file containing the base64 key
//
// Exactly one must be set.
func NewLocalFromEnv() (*LocalProvider, error) {
	raw := os.Getenv("KMS_LOCAL_MASTER_KEY")
	path := os.Getenv("KMS_LOCAL_MASTER_KEY_FILE")
	switch {
	case raw != "" && path != "":
		return nil, errors.New("local kms: set KMS_LOCAL_MASTER_KEY or KMS_LOCAL_MASTER_KEY_FILE, not both")
	case path != "":
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("local kms: read master key file: %w", err)
		}
		raw = string(b)
	case raw == "":
		return nil, errors.New("local kms: KMS_LOCAL_MASTER_KEY or KMS_LOCAL_MASTER_KEY_FILE required")
	}
	// Trim trailing whitespace/newlines that typically end up in key files.
	for len(raw) > 0 && (raw[len(raw)-1] == '\n' || raw[len(raw)-1] == '\r' || raw[len(raw)-1] == ' ') {
		raw = raw[:len(raw)-1]
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("local kms: master key is not valid base64: %w", err)
	}
	return NewLocal(key)
}

func (p *LocalProvider) Name() string { return "local" }

func (p *LocalProvider) GenerateDataKey(_ context.Context) ([]byte, string, error) {
	dek := make([]byte, 32) // AES-256
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, "", err
	}
	nonce := make([]byte, p.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, "", err
	}
	blob := p.gcm.Seal(nonce, nonce, dek, nil)
	return dek, base64.StdEncoding.EncodeToString(blob), nil
}

func (p *LocalProvider) GetDecryptedKey(_ context.Context, ciphertextB64 string) ([]byte, error) {
	blob, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return nil, fmt.Errorf("local kms: decode ciphertext: %w", err)
	}
	ns := p.gcm.NonceSize()
	if len(blob) < ns+p.gcm.Overhead() {
		return nil, errors.New("local kms: ciphertext too short")
	}
	nonce, ct := blob[:ns], blob[ns:]
	plain, err := p.gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("local kms: decrypt: %w", err)
	}
	return plain, nil
}
