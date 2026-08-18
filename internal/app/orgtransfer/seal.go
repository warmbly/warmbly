package orgtransfer

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"

	"github.com/warmbly/warmbly/internal/models"
)

// Secret values inside an archive are sealed under a key derived from the
// operator's passphrase, not under either instance's keys. That is the only
// arrangement that works: the source's keys mean nothing on the destination,
// and a key that travelled inside the archive would protect nothing.
//
// The passphrase is used once on each side and never stored.

const (
	sealAlgorithm = "argon2id-aes256gcm"

	// sealPrefix marks a sealed value. Sealed and unsealed columns are both
	// strings, so without it an importer reading an unsealed archive would
	// hand ciphertext to the mailbox layer as if it were a password.
	sealPrefix = "wbx1:"

	// Argon2id parameters. Deliberately heavier than the login hash in
	// internal/pkg/argon2: this key guards every mailbox credential in a
	// workspace, the derivation happens once per archive rather than once per
	// sign-in, and the attacker gets the file to grind offline.
	sealTime    = uint32(4)
	sealMemory  = uint32(256 * 1024) // 256 MB
	sealThreads = uint8(4)
	sealKeyLen  = uint32(32)
	sealSaltLen = 16

	// verifierPlaintext is sealed with the derived key at export so import can
	// tell a wrong passphrase from a corrupt archive.
	verifierPlaintext = "warmbly.archive.verifier.v1"
)

// ErrWrongPassphrase is returned when the supplied passphrase does not open
// the archive's secrets.
var ErrWrongPassphrase = errors.New("passphrase does not match this archive")

// ErrSecretsNotSealed is returned when a sealed value is read without a key.
var ErrSecretsNotSealed = errors.New("archive secrets are sealed and no passphrase was supplied")

// ValidatePassphrase enforces the floor for a secrets passphrase.
func ValidatePassphrase(p string) error {
	if len([]rune(p)) < models.MinOrgExportPassphraseLength {
		return fmt.Errorf("passphrase must be at least %d characters", models.MinOrgExportPassphraseLength)
	}
	if len(p) > 1024 {
		return errors.New("passphrase must be at most 1024 bytes")
	}
	return nil
}

// NewSecretsHeader derives a fresh archive key from the passphrase and returns
// the header that lets an importer derive the same key.
func NewSecretsHeader(passphrase string) (*SecretsHeader, []byte, error) {
	if err := ValidatePassphrase(passphrase); err != nil {
		return nil, nil, err
	}
	salt := make([]byte, sealSaltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, nil, fmt.Errorf("generate salt: %w", err)
	}

	hdr := &SecretsHeader{
		Algorithm: sealAlgorithm,
		Salt:      base64.StdEncoding.EncodeToString(salt),
		Time:      sealTime,
		Memory:    sealMemory,
		Threads:   sealThreads,
	}
	key := argon2.IDKey([]byte(passphrase), salt, hdr.Time, hdr.Memory, hdr.Threads, sealKeyLen)

	verifier, err := SealValue(key, verifierPlaintext)
	if err != nil {
		return nil, nil, err
	}
	hdr.Verifier = verifier
	return hdr, key, nil
}

// DeriveArchiveKey reproduces the archive key from a passphrase and header,
// and checks it against the header's verifier so a typo fails immediately.
func DeriveArchiveKey(passphrase string, hdr *SecretsHeader) ([]byte, error) {
	if hdr == nil {
		return nil, errors.New("archive carries no secrets")
	}
	if hdr.Algorithm != sealAlgorithm {
		return nil, fmt.Errorf("unsupported secret algorithm %q", hdr.Algorithm)
	}
	salt, err := base64.StdEncoding.DecodeString(hdr.Salt)
	if err != nil || len(salt) == 0 {
		return nil, errors.New("archive has a malformed secret salt")
	}
	// Clamp the work factors so a hostile archive cannot ask this process to
	// allocate an unbounded amount of memory just by claiming it needs it.
	if hdr.Memory == 0 || hdr.Memory > 1024*1024 || hdr.Time == 0 || hdr.Time > 16 || hdr.Threads == 0 {
		return nil, errors.New("archive declares unreasonable key derivation parameters")
	}

	key := argon2.IDKey([]byte(passphrase), salt, hdr.Time, hdr.Memory, hdr.Threads, sealKeyLen)

	plain, err := OpenValue(key, hdr.Verifier)
	if err != nil || plain != verifierPlaintext {
		return nil, ErrWrongPassphrase
	}
	return key, nil
}

// SealValue encrypts one plaintext value for the archive.
func SealValue(key []byte, plaintext string) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return sealPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// OpenValue decrypts a value produced by SealValue.
func OpenValue(key []byte, sealed string) (string, error) {
	if !IsSealed(sealed) {
		return "", errors.New("value is not sealed")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sealed, sealPrefix))
	if err != nil {
		return "", fmt.Errorf("sealed value is not valid base64: %w", err)
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("sealed value is truncated")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", ErrWrongPassphrase
	}
	return string(plain), nil
}

// IsSealed reports whether a value carries the archive seal marker.
func IsSealed(v string) bool { return strings.HasPrefix(v, sealPrefix) }

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != int(sealKeyLen) {
		return nil, errors.New("archive key has the wrong length")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
