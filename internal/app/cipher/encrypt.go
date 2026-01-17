package cipher

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"io"
)

func (c *Cipher) Encrypt(ctx context.Context, s string) (string, error) {
	if s == "" {
		return "", nil // handle empty string
	}
	block, err := aes.NewCipher(c.plainDEK)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(s), nil)

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}
