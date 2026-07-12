package encrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
)

// Encrypted holds the hex-encoded ciphertext and nonce.
// It is JSON-marshalable and can be stored in PostgreSQL (JSONB) or Redis.
type Encrypted struct {
	Ciphertext string `json:"ct" db:"ciphertext"` // hex
	Nonce      string `json:"n" db:"nonce"`       // hex
}

// Encrypter performs AES-GCM encryption with a fixed 32-byte key.
type Encrypter struct {
	aead cipher.AEAD
}

// FromEnv creates an Encrypter from the CREDENTIALS_ENCRYPTION_KEY env var
// (64 hex chars = 32 bytes). Returns (nil, nil) when the var is unset so
// callers can keep booting without credential sealing configured.
func FromEnv() (*Encrypter, error) {
	key := os.Getenv("CREDENTIALS_ENCRYPTION_KEY")
	if key == "" {
		return nil, nil
	}
	return NewEncrypterFromHex(key)
}

// NewEncrypterFromHex creates an Encrypter from a 64-character hex key.
func NewEncrypterFromHex(hexKey string) (*Encrypter, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("key must be 32 bytes (64 hex chars)")
	}
	return NewEncrypter(key)
}

// NewEncrypter creates an Encrypter from a raw 32-byte key.
func NewEncrypter(key []byte) (*Encrypter, error) {
	if len(key) != 32 {
		return nil, errors.New("key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Encrypter{aead: aead}, nil
}

// Encrypt returns a hex-encoded ciphertext that contains the nonce prefix.
func (e *Encrypter) Encrypt(plaintext string) (string, error) {
	enc, err := e.EncryptBytes([]byte(plaintext))
	if err != nil {
		return "", err
	}
	return enc.Hex(), nil
}

// Decrypt accepts the hex string produced by Encrypt.
func (e *Encrypter) Decrypt(hexStr string) (string, error) {
	enc, err := ParseHex(hexStr)
	if err != nil {
		return "", err
	}
	plain, err := e.DecryptEncrypted(enc)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// ---------------------------------------------------------------------
// Low-level byte-oriented API (preferred for services/repositories)
// ---------------------------------------------------------------------

// EncryptBytes encrypts a byte slice and returns a structured Encrypted.
func (e *Encrypter) EncryptBytes(plain []byte) (*Encrypted, error) {
	nonce := make([]byte, e.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ct := e.aead.Seal(nil, nonce, plain, nil)
	return &Encrypted{
		Ciphertext: hex.EncodeToString(ct),
		Nonce:      hex.EncodeToString(nonce),
	}, nil
}

// DecryptEncrypted decrypts a structured Encrypted value.
func (e *Encrypter) DecryptEncrypted(enc *Encrypted) ([]byte, error) {
	nonce, err := hex.DecodeString(enc.Nonce)
	if err != nil {
		return nil, err
	}
	ct, err := hex.DecodeString(enc.Ciphertext)
	if err != nil {
		return nil, err
	}
	return e.aead.Open(nil, nonce, ct, nil)
}

// ---------------------------------------------------------------------
// Helper for the old "single hex string" format
// ---------------------------------------------------------------------

// Hex returns ciphertext||nonce as a single hex string (same as your original code).
func (e *Encrypted) Hex() string {
	// nonce (12 bytes) + ciphertext
	rawNonce, _ := hex.DecodeString(e.Nonce)
	rawCt, _ := hex.DecodeString(e.Ciphertext)
	return hex.EncodeToString(append(rawNonce, rawCt...))
}

// ParseHex restores an Encrypted from the single-hex format.
func ParseHex(hexStr string) (*Encrypted, error) {
	data, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, err
	}
	nonceSize := 12 // GCM standard nonce
	if len(data) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}
	return &Encrypted{
		Nonce:      hex.EncodeToString(data[:nonceSize]),
		Ciphertext: hex.EncodeToString(data[nonceSize:]),
	}, nil
}
