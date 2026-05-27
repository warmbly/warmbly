package kms

import "context"

// Provider is the abstraction over a KEK (key-encryption key) source.
//
// Implementations:
//   - AWS KMS         (kms.KMS — historical default)
//   - Local           (kms.LocalProvider — self-hostable, master key in env/file)
//
// The ciphertext blob format is opaque to the caller; only the provider that
// produced it can decrypt it. Switching providers requires a DEK migration:
// existing encrypted DEKs are unreadable to a new provider.
type Provider interface {
	// GenerateDataKey returns a fresh 32-byte AES-256 plaintext DEK and a
	// provider-specific ciphertext form of it (base64-encoded) suitable for
	// storage in a KeyVaultStore.
	GenerateDataKey(ctx context.Context) (plaintext []byte, ciphertextB64 string, err error)

	// GetDecryptedKey reverses GenerateDataKey.
	GetDecryptedKey(ctx context.Context, ciphertextB64 string) (plaintext []byte, err error)

	// Name returns a short identifier (e.g. "aws-kms", "local") used in admin
	// UI and audit logs.
	Name() string
}

// Compile-time interface checks.
var (
	_ Provider = (*KMS)(nil)
	_ Provider = (*LocalProvider)(nil)
)

// Name satisfies Provider for the AWS implementation.
func (k *KMS) Name() string { return "aws-kms" }
