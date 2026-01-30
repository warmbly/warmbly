package cipher

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
)

func (c *Cipher) Decrypt(ctxc context.Context, s string) (string, error) {
	encSecret, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(c.plainDEK)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(encSecret) < nonceSize {
		return "", errors.New("invalid ciphertext")
	}
	nonce, ciphertext := encSecret[:nonceSize], encSecret[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}
